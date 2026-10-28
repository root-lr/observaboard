package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/root-lr/observaboard/internal/agent"
	"github.com/root-lr/observaboard/internal/central"
	"github.com/root-lr/observaboard/internal/dockerx"
	"github.com/root-lr/observaboard/internal/instrument"
	"github.com/root-lr/observaboard/internal/presets"
	"github.com/root-lr/observaboard/internal/project"
)

type doctorReport struct{ problems int }

func (r *doctorReport) bad(format string, a ...any) { r.problems++; fail(format, a...) }

func runDoctor(args []string) error {
	fs := newFlags("observa doctor", "observa doctor [--agent-dir <dossier>] [--central-dir <dossier>] [--project-dir <dossier>]")
	agentDir := agentDirFlag(fs, "agent-dir")
	centralDir := fs.String("central-dir", defaultCentralDir(), "dossier du central")
	projectDir := fs.String("project-dir", ".", "dossier du projet (observa.yaml)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	r := &doctorReport{}

	fmt.Fprintln(stdout, "Docker")
	if err := dockerx.Available(); err != nil {
		r.bad("%v", err)
		return fmt.Errorf("diagnostic interrompu")
	}
	ok("Docker et docker compose disponibles")
	if dockerx.IsDesktop() {
		warn("Docker Desktop : les métriques de l'hôte sont celles de la VM Docker ; eBPF non garanti.")
	}

	ad := agent.Dir{Path: *agentDir}
	if s, err := ad.Settings(); err == nil {
		fmt.Fprintln(stdout, "\nAgent ("+ad.Path+")")
		doctorAgent(r, ad, s)
	} else {
		info("\nPas d'agent dans %s (normal sur un serveur central seul).", ad.Path)
	}

	cd := central.Dir{Path: *centralDir}
	if cd.Exists() {
		fmt.Fprintln(stdout, "\nCentral ("+cd.Path+")")
		doctorCentral(r, cd)
	}

	pf := filepath.Join(*projectDir, project.FileName)
	if fileExists(pf) {
		fmt.Fprintln(stdout, "\nProjet ("+pf+")")
		doctorProject(r, pf, ad)
	}

	fmt.Fprintln(stdout)
	if r.problems > 0 {
		return fmt.Errorf("%d problème(s) détecté(s)", r.problems)
	}
	ok("Aucun problème détecté")
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func doctorAgent(r *doctorReport, ad agent.Dir, s *agent.Settings) {
	if agentRunning() {
		ok("observa-agent en marche (agent %s, serveur %s)", s.Name, s.Host)
	} else {
		r.bad("observa-agent arrêté : docker compose --project-directory %s up -d", ad.Path)
	}
	if _, err := dockerx.Output("network", "inspect", instrument.NetworkName); err != nil {
		r.bad("réseau Docker %s absent : docker network create %s", instrument.NetworkName, instrument.NetworkName)
	}
	env, _ := ad.Env()
	if env["OBSERVA_TOKEN"] == "" {
		r.bad("OBSERVA_TOKEN absent de %s/.env : réinstallez l'agent avec --token", ad.Path)
	} else {
		ping := s.CentralURL
		if u, err := url.Parse(s.CentralURL); err == nil && u.Hostname() == "observa-caddy" {
			// all-in-one: the central is reachable from the host through the published port
			if cs, err := (central.Dir{Path: filepath.Join(filepath.Dir(ad.Path), "central")}).Settings(); err == nil {
				ping = cs.BaseURL()
			}
		}
		if err := pingCentral(ping, s.Name, env["OBSERVA_TOKEN"], s.TLS, ad.CAPath()); err != nil {
			r.bad("%v", err)
		} else {
			ok("Central joignable et token accepté (%s)", s.CentralURL)
		}
	}
	if s.EBPF {
		if out, err := dockerx.Output("info", "--format", "{{.KernelVersion}}"); err == nil {
			info("eBPF activé, noyau %s (5.8+ requis)", strings.TrimSpace(out))
		}
	}
	// Errors reported by Alloy itself.
	if out, err := dockerx.Output("logs", "--since", "10m", "observa-agent"); err == nil {
		var errs []string
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "level=error") {
				errs = append(errs, l)
			}
		}
		if len(errs) > 0 {
			warn("%d erreur(s) dans les logs de l'agent (10 dernières minutes), dernière :", len(errs))
			info("%s", truncate(errs[len(errs)-1], 300))
		}
	}
	// Discovered containers must be on the observa network for OTLP and exporters.
	out, err := dockerx.Output("ps", "--filter", "label=observa.enable=true", "--format", "{{.Names}}")
	if err == nil {
		names := strings.Fields(out)
		if len(names) == 0 {
			warn("Aucun conteneur avec le label observa.enable=true sur ce serveur")
		} else {
			ok("%d conteneur(s) surveillé(s) : %s", len(names), strings.Join(names, ", "))
		}
		for _, n := range names {
			nets, err := dockerx.Output("inspect", "-f", "{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}", n)
			if err == nil && !strings.Contains(" "+nets+" ", " "+instrument.NetworkName+" ") {
				warn("%s n'est pas sur le réseau %s : traces et métriques OpenTelemetry n'arriveront pas (relancez avec l'override)", n, instrument.NetworkName)
			}
		}
	}
	projects, err := ad.Projects()
	if err != nil {
		r.bad("%v", err)
		return
	}
	for _, c := range projects {
		for _, name := range c.ServiceNames() {
			p, _ := presets.Get(c.Services[name].Preset)
			if p.Exporter == presets.ExporterPostgres && env[c.EffectiveSecretEnv(name)] == "" {
				r.bad("%s/%s/%s : %s absent du .env de l'agent (observa agent env set %s=...)", c.Project, c.Env, name, c.EffectiveSecretEnv(name), c.EffectiveSecretEnv(name))
			}
		}
	}
}

func doctorCentral(r *doctorReport, cd central.Dir) {
	s, err := cd.Settings()
	if err != nil {
		r.bad("%v", err)
		return
	}
	out, err := dockerx.ComposeOutput(cd.Path, "ps", "--format", "{{.Service}}|{{.State}}|{{.Health}}")
	if err != nil {
		r.bad("docker compose ps : %v", err)
		return
	}
	before := r.problems
	seen := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(l, "|")
		if len(f) < 2 {
			continue
		}
		seen[f[0]] = true
		switch {
		case f[1] != "running":
			r.bad("%s : %s", f[0], f[1])
		case len(f) > 2 && f[2] == "unhealthy":
			r.bad("%s : en marche mais unhealthy (docker compose logs %s)", f[0], f[0])
		}
	}
	for _, c := range []string{"caddy", "grafana", "prometheus", "alertmanager", "loki", "tempo"} {
		if !seen[c] {
			r.bad("%s absent : docker compose --project-directory %s up -d", c, cd.Path)
		}
	}
	if r.problems == before {
		ok("Tous les composants tournent (%s)", s.BaseURL())
	}
	if _, err := dockerx.ComposeOutput(cd.Path, "exec", "-T", "prometheus", "promtool", "check", "rules", "/etc/prometheus/rules/observa.yml"); err != nil {
		r.bad("règles Prometheus invalides : %v", err)
	} else {
		ok("Règles d'alerte valides (promtool)")
	}
	if _, err := dockerx.ComposeOutput(cd.Path, "exec", "-T", "alertmanager", "amtool", "check-config", "/etc/alertmanager/alertmanager.yml"); err != nil {
		r.bad("configuration Alertmanager invalide : %v", err)
	} else {
		ok("Configuration Alertmanager valide (amtool)")
	}
	if len(s.Notifications.Default) == 0 {
		warn("Aucun canal de notification par défaut : éditez %s/central.yaml (notifications) puis observa central reload", cd.Path)
	}
	// What the central receives.
	hosts, err := promQuery(cd, `count by (host) (node_boot_time_seconds{job="observa/host"})`)
	if err == nil {
		if len(hosts) == 0 {
			warn("Aucun agent ne remonte de données")
		} else {
			ok("Agents qui envoient des données : %s", strings.Join(sortedLabels(hosts, "host"), ", "))
		}
	}
	svcs, err := promQuery(cd, `count by (project, env) (observa:service:present)`)
	if err == nil && len(svcs) > 0 {
		var l []string
		for _, m := range svcs {
			l = append(l, m["project"]+"/"+m["env"])
		}
		sort.Strings(l)
		ok("Projets visibles : %s", strings.Join(l, ", "))
	}
	if traces, err := promQuery(cd, `count by (service) (traces_spanmetrics_calls_total)`); err == nil && len(traces) > 0 {
		ok("Services avec des traces : %s", strings.Join(sortedLabels(traces, "service"), ", "))
	}
}

func doctorProject(r *doctorReport, path string, ad agent.Dir) {
	cfg, err := project.Load(path)
	if err != nil {
		r.bad("%v", err)
		return
	}
	ok("observa.yaml valide (%s/%s, %d services)", cfg.Project, cfg.Env, len(cfg.Services))
	if _, err := ad.Settings(); err == nil && !fileExists(ad.ProjectFile(cfg.Project, cfg.Env)) {
		warn("projet non appliqué sur l'agent de ce serveur : observa apply")
	}
	out, err := dockerx.Output("ps", "--filter", "label=observa.project="+cfg.Project, "--filter", "label=observa.env="+cfg.Env, "--format", `{{.Label "observa.service"}}`)
	if err != nil {
		return
	}
	running := map[string]bool{}
	for _, n := range strings.Fields(out) {
		running[n] = true
	}
	for _, name := range cfg.ServiceNames() {
		if cfg.Services[name].Container != "" {
			continue
		}
		if !running[name] {
			warn("%s : aucun conteneur en marche avec les labels observa (lancé avec l'override ?)", name)
		}
	}
}

// promQuery runs an instant query inside the Prometheus container.
func promQuery(cd central.Dir, q string) ([]map[string]string, error) {
	out, err := dockerx.ComposeOutput(cd.Path, "exec", "-T", "prometheus", "wget", "-q", "-O-",
		"http://localhost:9090/api/v1/query?query="+url.QueryEscape(q))
	if err != nil {
		return nil, err
	}
	var resp struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, err
	}
	if resp.Status != "success" {
		return nil, errors.New("requête Prometheus en échec")
	}
	var res []map[string]string
	for _, r := range resp.Data.Result {
		res = append(res, r.Metric)
	}
	return res, nil
}

func sortedLabels(rows []map[string]string, label string) []string {
	var l []string
	for _, m := range rows {
		l = append(l, m[label])
	}
	sort.Strings(l)
	return l
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- alert test ---

func runAlertTest(args []string) error {
	fs := newFlags("observa alert test", "observa alert test [--severity critical|warning] [--project p --env e --service s] [--dir <central>]")
	dir := centralDirFlag(fs)
	sev := fs.String("severity", "critical", "sévérité de l'alerte de test")
	projectName := fs.String("project", "test", "projet (pour tester le routage d'un projet)")
	env := fs.String("env", "test", "environnement")
	service := fs.String("service", "test", "service")
	notifyCritical := fs.String("notify-critical", "", "canaux critiques à simuler (sinon : canaux par défaut)")
	notifyWarning := fs.String("notify-warning", "", "canaux avertissement à simuler")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	cd := central.Dir{Path: *dir}
	s, err := cd.Settings()
	if err != nil {
		return err
	}
	labels := []string{
		"alertname=ObservaTest", "severity=" + *sev,
		"project=" + *projectName, "env=" + *env, "service=" + *service,
	}
	if *notifyCritical != "" {
		labels = append(labels, "notify_critical="+*notifyCritical)
	}
	if *notifyWarning != "" {
		labels = append(labels, "notify_warning="+*notifyWarning)
	}
	cmd := append([]string{"exec", "-T", "alertmanager", "amtool", "--alertmanager.url=http://localhost:9093", "alert", "add"}, labels...)
	cmd = append(cmd,
		"--annotation=summary=Alerte de test ObservaBoard ("+*sev+")",
		"--annotation=description=Envoyée par observa alert test : si vous la recevez, les notifications fonctionnent.",
		"--annotation=runbook_url="+s.BaseURL()+"/runbooks/",
		"--annotation=dashboard_url="+s.BaseURL()+"/d/observa-overview",
	)
	if err := dockerx.Compose(cd.Path, cmd...); err != nil {
		return err
	}
	ok("Alerte de test envoyée à Alertmanager (elle se résout seule après 5 minutes)")
	info("Routage : %s", map[bool]string{true: "canaux simulés", false: "canaux par défaut de central.yaml"}[*notifyCritical != "" || *notifyWarning != ""])
	info("Délai d'envoi : environ 30 secondes (group_wait).")
	return nil
}
