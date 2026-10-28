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
	return fs.String(name, defaultAgentDir(), "agent directory (env OBSERVA_AGENT_DIR)")
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
	fs.StringVar(&o.centralURL, "central", "", "central URL (e.g. https://observa.example.com)")
	fs.StringVar(&o.token, "token", "", "agent token, created on the central with observa token create (to keep it out of shell history: --token-file or OBSERVA_AGENT_TOKEN)")
	tokenFile := fs.String("token-file", "", "file containing the token (- for stdin)")
	fs.StringVar(&o.host, "host", defaultAgentName(), "server name in dashboards (host label)")
	fs.StringVar(&o.caFile, "ca-file", "", "central CA certificate (--ip mode: observa central ca)")
	fs.BoolVar(&o.insecure, "insecure-skip-verify", false, "skip verification of the central certificate (not recommended)")
	fs.BoolVar(&o.ebpf, "ebpf", false, "enable eBPF instrumentation (Linux 5.8+, privileged container with access to host processes)")
	fs.BoolVar(&o.noStart, "no-start", false, "generate files without starting the agent")
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
		return usagef("--central and --token are required")
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
		return usagef("--central must be a full http(s) URL (e.g. https://observa.example.com)")
	}
	if u.Scheme == "http" && o.pingURL == "" {
		warn("the central uses plain HTTP: the token is sent unencrypted. Use only on a private network.")
	}
	if !project.SlugRE.MatchString(o.host) {
		return usagef("invalid --host %q (lowercase letters, digits, hyphens)", o.host)
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
		warn("Docker Desktop detected: host metrics are limited to the Docker VM.")
		if s.EBPF {
			warn("eBPF on Docker Desktop depends on the VM kernel and is not guaranteed (observa doctor checks it).")
		}
	}

	pingURL := o.pingURL
	if pingURL == "" {
		pingURL = s.CentralURL
	}
	step("Checking the token with the central")
	if dockerx.DryRun {
		info("[dry-run] check skipped")
	} else if err := pingCentral(pingURL, name, secret, s.TLS, d.CAPath()); err != nil {
		warn("%v", err)
		warn("Continuing the install; the agent will retry on its own. Check afterwards with observa doctor.")
	} else {
		ok("Token accepted by the central")
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
	ok("Agent configuration generated in %s", o.dir)
	if o.noStart {
		return nil
	}
	if err := dockerx.EnsureNetwork(instrument.NetworkName); err != nil {
		return err
	}
	if err := dockerx.Compose(o.dir, "up", "-d", "--remove-orphans"); err != nil {
		return err
	}
	ok("Agent %s started (server %s)", name, s.Host)
	info("Containers labeled observa.enable=true are monitored automatically.")
	info("In a project: observa init, then observa apply on this server.")
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
			return errors.New("ca-file: no readable PEM certificate")
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
			return fmt.Errorf("central certificate not trusted: use --ca-file (--ip mode): %v", err)
		}
		return fmt.Errorf("central unreachable (%s): %v", base, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return errors.New("token rejected by the central (401): check it or create a new one: observa token create <agent>")
	default:
		return fmt.Errorf("unexpected response from the central: %s", resp.Status)
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
		warn("The agent is not running: docker compose --project-directory %s up -d", d.Path)
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
		ok("Agent configuration reloaded")
	} else {
		ok("Agent configuration unchanged")
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
	fs := newFlags("observa agent reload", "observa agent reload [--dir <directory>]")
	dir := agentDirFlag(fs, "dir")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	return reloadAgent(agent.Dir{Path: *dir})
}

func runAgentStatus(args []string) error {
	fs := newFlags("observa agent status", "observa agent status [--dir <directory>]")
	dir := agentDirFlag(fs, "dir")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	d := agent.Dir{Path: *dir}
	s, err := d.Settings()
	if err != nil {
		return err
	}
	info("Agent %s · server %s · central %s", s.Name, s.Host, s.CentralURL)
	if agentRunning() {
		ok("Container observa-agent is running")
	} else {
		fail("Container observa-agent is stopped: docker compose --project-directory %s up -d", d.Path)
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
		info("No monitored containers: add the observa.enable=true label (observa init) or use observa attach.")
		return nil
	}
	fmt.Fprintf(stdout, "\n%-32s %-14s %-10s %-14s %-9s %s\n", "CONTAINER", "PROJECT", "ENV", "SERVICE", "PRESET", "STATUS")
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
	fs := newFlags("observa agent env", "observa agent env set NAME=value [NAME=value...] | observa agent env list")
	dir := agentDirFlag(fs, "dir")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	d := agent.Dir{Path: *dir}
	if len(pos) == 0 {
		return usagef("observa agent env set NAME=value | observa agent env list")
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
				return usagef("expected NAME=value: %q", a)
			}
			if k == "OBSERVA_TOKEN" {
				return usagef("to change the token, reinstall the agent (observa agent install --token ...)")
			}
			kv[k] = v
		}
		if len(kv) == 0 {
			return usagef("nothing to set")
		}
		if err := d.SetEnv(kv); err != nil {
			return err
		}
		ok("%d variable(s) saved to %s/.env", len(kv), d.Path)
		return renderAndRecreate(d)
	default:
		return usagef("unknown subcommand %q (set, list)", pos[0])
	}
}

// --- attach / detach ---

func runAttach(args []string) error {
	fs := newFlags("observa attach", "observa attach <container> --project <p> --env <e> --preset <preset> [--service <name>]")
	dir := agentDirFlag(fs, "agent-dir")
	projectName := fs.String("project", "", "project")
	env := fs.String("env", "prod", "environment")
	service := fs.String("service", "", "service name (default: container name)")
	preset := fs.String("preset", "generic", "preset ("+strings.Join(presets.Names(), ", ")+")")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("specify the container: observa attach <container> --project <p> --preset <preset>")
	}
	container := strings.TrimPrefix(pos[0], "/")
	if *service == "" {
		*service = nonSlug.ReplaceAllString(strings.ToLower(container), "-")
	}
	for _, v := range []string{*projectName, *env, *service} {
		if !project.SlugRE.MatchString(v) {
			return usagef("project, env and service: lowercase letters, digits and hyphens (%q)", v)
		}
	}
	if _, ok := presets.Get(*preset); !ok {
		return usagef("unknown preset %q (%s)", *preset, strings.Join(presets.Names(), ", "))
	}
	d := agent.Dir{Path: *dir}
	if _, err := d.Settings(); err != nil {
		return err
	}
	if _, err := dockerx.Output("inspect", container); err != nil && !dockerx.DryRun {
		return fmt.Errorf("container %s not found: %v", container, err)
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
		warn("network %s: %v (already connected?)", instrument.NetworkName, err)
	}
	if err := reloadAgent(d); err != nil {
		return err
	}
	ok("%s monitored as %s/%s/%s (preset %s), without restart", container, *projectName, *env, *service, *preset)
	info("Logs and resources: immediately. Detailed traces: observa init --runtime docker, then restart the container.")
	return nil
}

func runDetach(args []string) error {
	fs := newFlags("observa detach", "observa detach <container>")
	dir := agentDirFlag(fs, "agent-dir")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("specify the container: observa detach <container>")
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
		return fmt.Errorf("no attach rule for %s", pos[0])
	}
	if err := d.SaveAttachRules(kept); err != nil {
		return err
	}
	if err := reloadAgent(d); err != nil {
		return err
	}
	ok("%s is no longer monitored", pos[0])
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
	ok("Agent restarted with the new configuration")
	return nil
}
