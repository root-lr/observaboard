package cli

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/root-lr/observaboard/internal/agent"
	"github.com/root-lr/observaboard/internal/central"
	"github.com/root-lr/observaboard/internal/dockerx"
	"github.com/root-lr/observaboard/internal/fsutil"
	"github.com/root-lr/observaboard/internal/instrument"
	"github.com/root-lr/observaboard/internal/presets"
	"github.com/root-lr/observaboard/internal/project"
)

func agentDirFlag(fs interface {
	String(name, value, usage string) *string
}, name string) *string {
	return fs.String(name, defaultAgentDir(), "dossier de l'agent (variable OBSERVA_AGENT_DIR)")
}

// defaultAgentDir: OBSERVA_AGENT_DIR, else the server install, else the
// all-in-one install of this user, else the server path.
func defaultAgentDir() string {
	if v := os.Getenv("OBSERVA_AGENT_DIR"); v != "" {
		return v
	}
	if fileExists(filepath.Join(agent.DefaultDir, "agent.yaml")) {
		return agent.DefaultDir
	}
	if local := filepath.Join(defaultLocalDir(), "agent"); fileExists(filepath.Join(local, "agent.yaml")) {
		return local
	}
	return agent.DefaultDir
}

type agentInstallOpts struct {
	dir, centralURL, token, host, caFile string
	insecure, ebpf, noStart              bool
	ebpfSet, tlsSet                      bool   // options given explicitly (re-install keeps the others)
	pingURL                              string // where to test the token from this machine ("" = centralURL)
}

func runAgentInstall(args []string) error {
	fs := newFlags("observa agent install", "observa agent install --central <url> --token <agent>.<secret> [options]")
	var o agentInstallOpts
	dir := agentDirFlag(fs, "dir")
	fs.StringVar(&o.centralURL, "central", "", "URL du central (ex. https://observa.example.com)")
	fs.StringVar(&o.token, "token", "", "token de l'agent, créé sur le central avec observa token create (évite l'historique du shell : --token-file ou OBSERVA_AGENT_TOKEN)")
	tokenFile := fs.String("token-file", "", "fichier contenant le token (- pour l'entrée standard)")
	fs.StringVar(&o.host, "host", defaultAgentName(), "nom du serveur dans les dashboards (label host)")
	fs.StringVar(&o.caFile, "ca-file", "", "certificat de l'autorité du central (mode --ip : observa central ca)")
	fs.BoolVar(&o.insecure, "insecure-skip-verify", false, "ne pas vérifier le certificat du central (déconseillé)")
	fs.BoolVar(&o.ebpf, "ebpf", false, "activer l'instrumentation eBPF (Linux 5.8+, conteneur privilégié avec accès aux processus de l'hôte)")
	fs.BoolVar(&o.noStart, "no-start", false, "générer les fichiers sans démarrer l'agent")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	o.dir = *dir
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "ebpf":
			o.ebpfSet = true
		case "ca-file", "insecure-skip-verify":
			o.tlsSet = true
		}
	})
	if o.token == "" && *tokenFile != "" {
		var data []byte
		var err error
		if *tokenFile == "-" {
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(*tokenFile)
		}
		if err != nil {
			return err
		}
		o.token = strings.TrimSpace(string(data))
	}
	if o.token == "" {
		o.token = os.Getenv("OBSERVA_AGENT_TOKEN")
	}
	if o.centralURL == "" || o.token == "" {
		fs.Usage()
		return usagef("--central et --token sont requis")
	}
	return installAgent(o)
}

func installAgent(o agentInstallOpts) error {
	name, secret, err := central.SplitToken(o.token)
	if err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimRight(o.centralURL, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return usagef("--central doit être une URL http(s) complète (ex. https://observa.example.com)")
	}
	if u.Scheme == "http" && o.pingURL == "" {
		warn("le central est en HTTP : le token circule en clair. À réserver à un réseau privé.")
	}
	if !project.SlugRE.MatchString(o.host) {
		return usagef("--host %q invalide (minuscules, chiffres, tirets)", o.host)
	}
	if err := requireWritable(o.dir); err != nil {
		return err
	}
	if !o.noStart {
		if err := dockerx.Available(); err != nil {
			return err
		}
	}
	d := agent.Dir{Path: o.dir}
	s := &agent.Settings{Name: name, Host: o.host, CentralURL: u.String(), EBPF: o.ebpf}
	if prev, err := d.Settings(); err == nil {
		// Re-install: keep previous options unless given again; TLS trust only
		// applies to the same central URL.
		if !o.ebpfSet {
			s.EBPF = prev.EBPF
		}
		if !o.tlsSet && prev.CentralURL == s.CentralURL {
			s.TLS = prev.TLS
		}
	}
	if o.caFile != "" {
		data, err := os.ReadFile(o.caFile)
		if err != nil {
			return err
		}
		if _, err := fsutil.WriteFile(d.CAPath(), data, 0o644); err != nil {
			return err
		}
		s.TLS = agent.TLS{CAFile: true}
	} else if o.insecure {
		s.TLS = agent.TLS{InsecureSkipVerify: true}
	}
	s.Desktop = dockerx.IsDesktop()
	if s.Desktop {
		warn("Docker Desktop détecté : métriques de l'hôte limitées à la VM Docker.")
		if s.EBPF {
			warn("eBPF sous Docker Desktop : dépend du noyau de la VM, non garanti (observa doctor le vérifie).")
		}
	}

	pingURL := o.pingURL
	if pingURL == "" {
		pingURL = s.CentralURL
	}
	step("Vérification du token auprès du central")
	if dockerx.DryRun {
		info("[dry-run] vérification ignorée")
	} else if err := pingCentral(pingURL, name, secret, s.TLS, d.CAPath()); err != nil {
		warn("%v", err)
		warn("L'installation continue ; l'agent réessaiera tout seul. Vérifiez ensuite avec observa doctor.")
	} else {
		ok("Token accepté par le central")
	}

	if err := d.SaveSettings(s); err != nil {
		return err
	}
	if err := d.SetEnv(map[string]string{"OBSERVA_TOKEN": secret}); err != nil {
		return err
	}
	_, warns, err := d.Render(s)
	if err != nil {
		return err
	}
	for _, w := range warns {
		warn("%s", w)
	}
	ok("Configuration de l'agent générée dans %s", o.dir)
	if o.noStart {
		return nil
	}
	if err := dockerx.EnsureNetwork(instrument.NetworkName); err != nil {
		return err
	}
	if err := dockerx.Compose(o.dir, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	ok("Agent %s démarré (serveur %s)", name, s.Host)
	info("Les conteneurs portant le label observa.enable=true sont surveillés automatiquement.")
	info("Dans un projet : observa init, puis observa apply sur ce serveur.")
	return nil
}

// pingCentral checks that the central accepts the token.
func pingCentral(base, user, secret string, t agent.TLS, caPath string) error {
	if dockerx.DryRun {
		return nil
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if t.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true
	} else if t.CAFile {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return errors.New("ca-file : aucun certificat PEM lisible")
		}
		tlsCfg.RootCAs = pool
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg, Proxy: http.ProxyFromEnvironment}}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/ingest/ping", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, secret)
	resp, err := client.Do(req)
	if err != nil {
		var cerr *tls.CertificateVerificationError
		if errors.As(err, &cerr) {
			return fmt.Errorf("certificat du central non reconnu : utilisez --ca-file (mode --ip) : %v", err)
		}
		return fmt.Errorf("central injoignable (%s) : %v", base, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return errors.New("token refusé par le central (401) : vérifiez-le ou créez-en un : observa token create <agent>")
	default:
		return fmt.Errorf("réponse inattendue du central : %s", resp.Status)
	}
}

// reloadAgent re-renders the agent config and applies it to the running agent.
func reloadAgent(d agent.Dir) error {
	s, err := d.Settings()
	if err != nil {
		return err
	}
	changed, warns, err := d.Render(s)
	if err != nil {
		return err
	}
	for _, w := range warns {
		warn("%s", w)
	}
	if !agentRunning() {
		warn("L'agent n'est pas démarré : docker compose --project-directory %s up -d", d.Path)
		return nil
	}
	// compose changes (eBPF toggled...) need a recreate; config changes a SIGHUP.
	if err := dockerx.Compose(d.Path, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	if changed {
		if err := dockerx.Run("kill", "--signal=HUP", "observa-agent"); err != nil {
			return err
		}
		ok("Configuration de l'agent rechargée")
	} else {
		ok("Configuration de l'agent inchangée")
	}
	return nil
}

func agentRunning() bool {
	if dockerx.DryRun {
		return true
	}
	out, err := dockerx.Output("inspect", "-f", "{{.State.Running}}", "observa-agent")
	return err == nil && strings.TrimSpace(out) == "true"
}

func runAgentReload(args []string) error {
	fs := newFlags("observa agent reload", "observa agent reload [--dir <dossier>]")
	dir := agentDirFlag(fs, "dir")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	return reloadAgent(agent.Dir{Path: *dir})
}

func runAgentStatus(args []string) error {
	fs := newFlags("observa agent status", "observa agent status [--dir <dossier>]")
	dir := agentDirFlag(fs, "dir")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	d := agent.Dir{Path: *dir}
	s, err := d.Settings()
	if err != nil {
		return err
	}
	info("Agent %s · serveur %s · central %s", s.Name, s.Host, s.CentralURL)
	if agentRunning() {
		ok("Conteneur observa-agent en marche")
	} else {
		fail("Conteneur observa-agent arrêté : docker compose --project-directory %s up -d", d.Path)
	}
	return printDiscovered(d)
}

// printDiscovered lists the containers the agent monitors.
func printDiscovered(d agent.Dir) error {
	out, err := dockerx.Output("ps", "--filter", "label=observa.enable=true", "--format",
		`{{.Names}}|{{.Label "observa.project"}}|{{.Label "observa.env"}}|{{.Label "observa.service"}}|{{.Label "observa.preset"}}|{{.Status}}`)
	if err != nil {
		return err
	}
	rules, _ := d.AttachRules()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if strings.TrimSpace(out) == "" && len(rules) == 0 {
		info("Aucun conteneur surveillé : ajoutez le label observa.enable=true (observa init) ou utilisez observa attach.")
		return nil
	}
	fmt.Fprintf(stdout, "\n%-32s %-14s %-10s %-14s %-9s %s\n", "CONTENEUR", "PROJET", "ENV", "SERVICE", "PRESET", "ÉTAT")
	for _, l := range lines {
		if l == "" {
			continue
		}
		f := strings.Split(l, "|")
		if len(f) == 6 {
			fmt.Fprintf(stdout, "%-32s %-14s %-10s %-14s %-9s %s\n", f[0], f[1], f[2], f[3], f[4], f[5])
		}
	}
	for _, r := range rules {
		state := "absent"
		if st, err := dockerx.Output("inspect", "-f", "{{.State.Status}}", r.Container); err == nil {
			state = strings.TrimSpace(st)
		}
		fmt.Fprintf(stdout, "%-32s %-14s %-10s %-14s %-9s %s (attach)\n", r.Container, r.Project, r.Env, r.Service, r.Preset, state)
	}
	return nil
}

func runAgentEnv(args []string) error {
	fs := newFlags("observa agent env", "observa agent env set NOM=valeur [NOM=valeur...] | observa agent env list")
	dir := agentDirFlag(fs, "dir")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	d := agent.Dir{Path: *dir}
	if len(pos) == 0 {
		return usagef("observa agent env set NOM=valeur | observa agent env list")
	}
	switch pos[0] {
	case "list":
		env, err := d.Env()
		if err != nil {
			return err
		}
		for _, k := range sortedKeys(env) {
			fmt.Fprintf(stdout, "%s=%s\n", k, "••••••")
		}
		return nil
	case "set":
		kv := map[string]string{}
		for _, a := range pos[1:] {
			k, v, ok := strings.Cut(a, "=")
			if !ok {
				return usagef("format attendu NOM=valeur : %q", a)
			}
			if k == "OBSERVA_TOKEN" {
				return usagef("le token se change en réinstallant l'agent (observa agent install --token ...)")
			}
			kv[k] = v
		}
		if len(kv) == 0 {
			return usagef("rien à définir")
		}
		if err := d.SetEnv(kv); err != nil {
			return err
		}
		ok("%d variable(s) enregistrée(s) dans %s/.env", len(kv), d.Path)
		return renderAndRecreate(d)
	default:
		return usagef("sous-commande inconnue %q (set, list)", pos[0])
	}
}

// --- attach / detach ---

func runAttach(args []string) error {
	fs := newFlags("observa attach", "observa attach <conteneur> --project <p> --env <e> --preset <preset> [--service <nom>]")
	dir := agentDirFlag(fs, "agent-dir")
	projectName := fs.String("project", "", "projet")
	env := fs.String("env", "prod", "environnement")
	service := fs.String("service", "", "nom du service (défaut : nom du conteneur)")
	preset := fs.String("preset", "generic", "preset ("+strings.Join(presets.Names(), ", ")+")")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("précisez le conteneur : observa attach <conteneur> --project <p> --preset <preset>")
	}
	container := strings.TrimPrefix(pos[0], "/")
	if *service == "" {
		*service = nonSlug.ReplaceAllString(strings.ToLower(container), "-")
	}
	for _, v := range []string{*projectName, *env, *service} {
		if !project.SlugRE.MatchString(v) {
			return usagef("projet, env et service : minuscules, chiffres et tirets (%q)", v)
		}
	}
	if _, ok := presets.Get(*preset); !ok {
		return usagef("preset %q inconnu (%s)", *preset, strings.Join(presets.Names(), ", "))
	}
	d := agent.Dir{Path: *dir}
	if _, err := d.Settings(); err != nil {
		return err
	}
	if _, err := dockerx.Output("inspect", container); err != nil && !dockerx.DryRun {
		return fmt.Errorf("conteneur %s introuvable : %v", container, err)
	}
	rules, err := d.AttachRules()
	if err != nil {
		return err
	}
	var kept []agent.AttachRule
	for _, r := range rules {
		if r.Container != container {
			kept = append(kept, r)
		}
	}
	kept = append(kept, agent.AttachRule{Container: container, Project: *projectName, Env: *env, Service: *service, Preset: *preset})
	if err := d.SaveAttachRules(kept); err != nil {
		return err
	}
	// Join the shared network so exporters and OTLP work, without restarting.
	alias := project.Alias(*projectName, *env, *service)
	if err := dockerx.Run("network", "connect", "--alias", alias, instrument.NetworkName, container); err != nil {
		warn("réseau %s : %v (déjà connecté ?)", instrument.NetworkName, err)
	}
	if err := reloadAgent(d); err != nil {
		return err
	}
	ok("%s surveillé comme %s/%s/%s (preset %s), sans redémarrage", container, *projectName, *env, *service, *preset)
	info("Logs et ressources : immédiat. Traces détaillées : observa init --runtime docker, puis relancer le conteneur.")
	return nil
}

func runDetach(args []string) error {
	fs := newFlags("observa detach", "observa detach <conteneur>")
	dir := agentDirFlag(fs, "agent-dir")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("précisez le conteneur : observa detach <conteneur>")
	}
	d := agent.Dir{Path: *dir}
	rules, err := d.AttachRules()
	if err != nil {
		return err
	}
	container := strings.TrimPrefix(pos[0], "/")
	var kept []agent.AttachRule
	found := false
	for _, r := range rules {
		if r.Container == container {
			found = true
			continue
		}
		kept = append(kept, r)
	}
	if !found {
		return fmt.Errorf("aucune règle attach pour %s", pos[0])
	}
	if err := d.SaveAttachRules(kept); err != nil {
		return err
	}
	if err := reloadAgent(d); err != nil {
		return err
	}
	ok("%s n'est plus surveillé", pos[0])
	return nil
}

// renderAndRecreate renders the agent and recreates its container: needed
// when .env changes, as env_file is only read at container creation.
func renderAndRecreate(d agent.Dir) error {
	s, err := d.Settings()
	if err != nil {
		return err
	}
	_, warns, err := d.Render(s)
	if err != nil {
		return err
	}
	for _, w := range warns {
		warn("%s", w)
	}
	if !agentRunning() {
		return nil
	}
	if err := dockerx.Compose(d.Path, "up", "-d", "--force-recreate"); err != nil {
		return err
	}
	ok("Agent redémarré avec la nouvelle configuration")
	return nil
}
