package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/root-lr/observaboard/internal/central"
	"github.com/root-lr/observaboard/internal/dockerx"
	"github.com/root-lr/observaboard/internal/instrument"
)

func runCentralInstall(args []string) error {
	fs := newFlags("observa central install", "observa central install (--domain <nom> | --ip <adresse>) [options]")
	dir := fs.String("dir", central.DefaultDir, "dossier d'installation du central")
	domain := fs.String("domain", "", "nom de domaine pointant vers ce serveur (certificat Let's Encrypt automatique)")
	ip := fs.String("ip", "", "adresse IP publique, si vous n'avez pas de domaine (certificat interne)")
	email := fs.String("email", "", "adresse de contact pour Let's Encrypt (optionnel)")
	firstAgent := fs.String("first-agent", defaultAgentName(), "nom du premier agent pour lequel créer un token")
	noStart := fs.Bool("no-start", false, "générer les fichiers sans démarrer les conteneurs")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	d := central.Dir{Path: *dir}
	var s *central.Settings
	if d.Exists() {
		var err error
		if s, err = d.Settings(); err != nil {
			return err
		}
		step("Central existant dans %s : mise à jour", *dir)
	} else {
		s = central.Defaults(central.ModeServer)
	}
	if *domain != "" {
		s.Domain, s.IP = strings.ToLower(*domain), ""
	}
	if *ip != "" {
		s.IP, s.Domain = *ip, ""
	}
	if *email != "" {
		s.Email = *email
	}
	if errs := s.Validate(); len(errs) > 0 {
		return usagef("réglages invalides :\n  - %s", strings.Join(errs, "\n  - "))
	}
	return installCentral(d, s, *firstAgent, *noStart)
}

// installCentral writes and starts a central server. It returns after the
// containers are started (or files written with noStart).
func installCentral(d central.Dir, s *central.Settings, firstAgent string, noStart bool) error {
	if err := requireWritable(d.Path); err != nil {
		return err
	}
	if !noStart {
		if err := dockerx.Available(); err != nil {
			return err
		}
	}
	if err := d.SaveSettings(s); err != nil {
		return err
	}
	password, err := d.EnsureAdminPassword()
	if err != nil {
		return err
	}
	var newToken string
	if tokens, err := d.Tokens(); err != nil {
		return err
	} else if len(tokens) == 0 && firstAgent != "" {
		if newToken, err = d.CreateToken(firstAgent); err != nil {
			return err
		}
	}
	res, err := d.Render(s)
	if err != nil {
		return err
	}
	ok("Configuration générée dans %s (%d fichiers modifiés)", d.Path, len(res.Changed))
	if !noStart {
		if err := dockerx.EnsureNetwork(instrument.NetworkName); err != nil {
			return err
		}
		wasRunning := !dockerx.DryRun && centralRunning(d)
		step("Démarrage du central (le premier lancement télécharge les images, quelques minutes)")
		if err := dockerx.Compose(d.Path, "up", "-d", "--remove-orphans"); err != nil {
			return err
		}
		if wasRunning {
			// containers kept running: apply config-only changes
			if err := reloadCentral(d, res); err != nil {
				warn("rechargement partiel : %v", err)
			}
		}
		ok("Central démarré")
	}
	fmt.Fprintln(stdout)
	info("Grafana    : %s", s.BaseURL())
	info("Runbooks   : %s/runbooks/", s.BaseURL())
	info("Utilisateur: %s", s.AdminUser)
	if password != "" {
		info("Mot de passe (affiché une seule fois, aussi dans %s/.env) : %s", d.Path, password)
	}
	if s.Mode == central.ModeServer {
		fmt.Fprintln(stdout)
		if newToken != "" {
			info("Token du premier agent (%s), affiché une seule fois :", firstAgent)
			info("  %s", newToken)
			fmt.Fprintln(stdout)
			info("Sur chaque serveur à surveiller :")
			info("  observa agent install --central %s --token %s%s", s.BaseURL(), newToken, caHint(s))
		} else {
			info("Créer un token pour un nouveau serveur : observa token create <nom-du-serveur>")
		}
		info("Canaux de notification : éditez %s/central.yaml puis observa central reload", d.Path)
	}
	return nil
}

func caHint(s *central.Settings) string {
	if s.IP != "" {
		return " --ca-file central-ca.crt   (obtenu avec : observa central ca > central-ca.crt)"
	}
	return ""
}

func centralRunning(d central.Dir) bool {
	if dockerx.DryRun {
		return true
	}
	out, err := dockerx.ComposeOutput(d.Path, "ps", "-q", "caddy")
	return err == nil && strings.TrimSpace(out) != ""
}

// reloadCentral applies changed files to running containers without downtime
// where the component supports it.
func reloadCentral(d central.Dir, res central.Result) error {
	if len(res.Changed) == 0 {
		return nil
	}
	var errs []string
	exec := func(service string, cmd ...string) {
		if err := dockerx.Compose(d.Path, append([]string{"exec", "-T", service}, cmd...)...); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if res.Has("caddy/") {
		exec("caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile")
	}
	// Prometheus and Alertmanager reload their configuration on SIGHUP.
	var hup []string
	if res.Has("prometheus/") {
		hup = append(hup, "prometheus")
	}
	if res.Has("alertmanager/") {
		hup = append(hup, "alertmanager")
	}
	if len(hup) > 0 {
		if err := dockerx.Compose(d.Path, append([]string{"kill", "-s", "SIGHUP"}, hup...)...); err != nil {
			errs = append(errs, err.Error())
		}
	}
	var restart []string
	if res.Has("loki/") {
		restart = append(restart, "loki")
	}
	if res.Has("tempo/") {
		restart = append(restart, "tempo")
	}
	if res.Has("grafana/provisioning/") {
		restart = append(restart, "grafana")
	}
	if len(restart) > 0 {
		if err := dockerx.Compose(d.Path, append([]string{"restart"}, restart...)...); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, " ; "))
	}
	return nil
}

func centralDirFlag(fs interface {
	String(name, value, usage string) *string
}) *string {
	return fs.String("dir", defaultCentralDir(), "dossier du central (variable OBSERVA_CENTRAL_DIR)")
}

// defaultCentralDir: OBSERVA_CENTRAL_DIR, else the server install, else the
// all-in-one install of this user, else the server path.
func defaultCentralDir() string {
	if v := os.Getenv("OBSERVA_CENTRAL_DIR"); v != "" {
		return v
	}
	if fileExists(filepath.Join(central.DefaultDir, "central.yaml")) {
		return central.DefaultDir
	}
	if local := filepath.Join(defaultLocalDir(), "central"); fileExists(filepath.Join(local, "central.yaml")) {
		return local
	}
	return central.DefaultDir
}

func runCentralReload(args []string) error {
	fs := newFlags("observa central reload", "observa central reload [--dir <dossier>]")
	dir := centralDirFlag(fs)
	if _, err := parse(fs, args); err != nil {
		return err
	}
	d := central.Dir{Path: *dir}
	s, err := d.Settings()
	if err != nil {
		return err
	}
	res, err := d.Render(s)
	if err != nil {
		return err
	}
	if len(res.Changed) == 0 {
		ok("Aucun changement")
		return nil
	}
	for _, c := range res.Changed {
		info("modifié : %s", c)
	}
	if !centralRunning(d) {
		warn("Le central n'est pas démarré : docker compose --project-directory %s up -d", d.Path)
		return nil
	}
	if res.Has("docker-compose.yml") {
		if err := dockerx.Compose(d.Path, "up", "-d", "--remove-orphans"); err != nil {
			return err
		}
	}
	if err := reloadCentral(d, res); err != nil {
		return err
	}
	ok("Central rechargé")
	return nil
}

func runCentralUpgrade(args []string) error {
	fs := newFlags("observa central upgrade", "observa central upgrade [--dir <dossier>]")
	dir := centralDirFlag(fs)
	if _, err := parse(fs, args); err != nil {
		return err
	}
	d := central.Dir{Path: *dir}
	s, err := d.Settings()
	if err != nil {
		return err
	}
	if _, err := d.Render(s); err != nil {
		return err
	}
	step("Téléchargement des nouvelles images")
	if err := dockerx.Compose(d.Path, "pull"); err != nil {
		return err
	}
	if err := dockerx.Compose(d.Path, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	ok("Central à jour (données et configuration conservées)")
	return nil
}

func runCentralStatus(args []string) error {
	fs := newFlags("observa central status", "observa central status [--dir <dossier>]")
	dir := centralDirFlag(fs)
	if _, err := parse(fs, args); err != nil {
		return err
	}
	d := central.Dir{Path: *dir}
	s, err := d.Settings()
	if err != nil {
		return err
	}
	info("URL : %s (mode %s)", s.BaseURL(), s.Mode)
	return dockerx.Compose(d.Path, "ps")
}

func runCentralCA(args []string) error {
	fs := newFlags("observa central ca", "observa central ca [--dir <dossier>] > central-ca.crt")
	dir := centralDirFlag(fs)
	if _, err := parse(fs, args); err != nil {
		return err
	}
	d := central.Dir{Path: *dir}
	if _, err := d.Settings(); err != nil {
		return err
	}
	out, err := dockerx.ComposeOutput(d.Path, "exec", "-T", "caddy", "cat", "/data/caddy/pki/authorities/local/root.crt")
	if err != nil {
		return fmt.Errorf("certificat introuvable (central démarré en mode --ip ?) : %v", err)
	}
	fmt.Fprint(stdout, out)
	return nil
}

// --- tokens ---

func runTokenCreate(args []string) error {
	fs := newFlags("observa token create", "observa token create <agent> [--dir <dossier-central>]")
	dir := centralDirFlag(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("précisez le nom de l'agent : observa token create <agent> (ex. srv-a)")
	}
	d := central.Dir{Path: *dir}
	s, err := d.Settings()
	if err != nil {
		return err
	}
	tok, err := d.CreateToken(pos[0])
	if err != nil {
		return err
	}
	res, err := d.Render(s)
	if err != nil {
		return err
	}
	if centralRunning(d) {
		if err := reloadCentral(d, res); err != nil {
			return err
		}
	}
	ok("Token créé pour %s (affiché une seule fois) :", pos[0])
	fmt.Fprintf(stdout, "\n  %s\n\n", tok)
	info("Sur le serveur %s :", pos[0])
	info("  observa agent install --central %s --token %s%s", s.BaseURL(), tok, caHint(s))
	return nil
}

func runTokenList(args []string) error {
	fs := newFlags("observa token list", "observa token list [--dir <dossier-central>]")
	dir := centralDirFlag(fs)
	if _, err := parse(fs, args); err != nil {
		return err
	}
	d := central.Dir{Path: *dir}
	tokens, err := d.Tokens()
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		info("Aucun token. Créez-en un : observa token create <agent>")
		return nil
	}
	fmt.Fprintf(stdout, "%-24s %s\n", "AGENT", "CRÉÉ LE")
	for _, t := range tokens {
		fmt.Fprintf(stdout, "%-24s %s\n", t.Name, t.Created)
	}
	return nil
}

func runTokenRevoke(args []string) error {
	fs := newFlags("observa token revoke", "observa token revoke <agent> [--dir <dossier-central>]")
	dir := centralDirFlag(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("précisez l'agent : observa token revoke <agent>")
	}
	d := central.Dir{Path: *dir}
	s, err := d.Settings()
	if err != nil {
		return err
	}
	if err := d.RevokeToken(pos[0]); err != nil {
		return err
	}
	res, err := d.Render(s)
	if err != nil {
		return err
	}
	if centralRunning(d) {
		if err := reloadCentral(d, res); err != nil {
			return err
		}
	}
	ok("Token de %s révoqué : ses envois sont désormais refusés (401)", pos[0])
	return nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9-]+`)

func defaultAgentName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "srv-1"
	}
	h = strings.ToLower(strings.SplitN(h, ".", 2)[0])
	h = strings.Trim(nonSlug.ReplaceAllString(h, "-"), "-")
	if len(h) > 40 {
		h = strings.Trim(h[:40], "-")
	}
	if h == "" {
		return "srv-1"
	}
	return h
}
