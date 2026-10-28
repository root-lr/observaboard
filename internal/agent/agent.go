// Package agent manages the ObservaBoard agent installed on each Docker host:
// its settings, the projects applied to it, `observa attach` rules, the
// rendered Alloy configuration and the threshold/routing metrics.
package agent

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/root-lr/observaboard/assets"
	"github.com/root-lr/observaboard/internal/fsutil"
	"github.com/root-lr/observaboard/internal/presets"
	"github.com/root-lr/observaboard/internal/project"
	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
	"github.com/root-lr/observaboard/internal/version"
)

// DefaultDir is where the agent lives on a server.
const DefaultDir = "/opt/observa/agent"

// File names inside the agent directory.
const (
	settingsFile = "agent.yaml"
	envFile      = ".env"
	attachFile   = "attach.yaml"
	projectsDir  = "projects"
	textfileDir  = "textfile"
	configDir    = "config" // mounted as /etc/alloy (directory mount: reloads see new files)
	caFile       = "ca.crt" // inside configDir
)

// TLS describes how the agent trusts the central server.
type TLS struct {
	CAFile             bool `yaml:"ca_file,omitempty"` // ca.crt present in the agent dir
	InsecureSkipVerify bool `yaml:"insecure_skip_verify,omitempty"`
}

// Settings is agent.yaml.
type Settings struct {
	Version    int    `yaml:"version"`
	Name       string `yaml:"name"`        // agent identifier, also the token user name
	Host       string `yaml:"host"`        // value of the `host` label
	CentralURL string `yaml:"central_url"` // e.g. https://observa.example.com
	TLS        TLS    `yaml:"tls,omitempty"`
	EBPF       bool   `yaml:"ebpf,omitempty"`
	Desktop    bool   `yaml:"desktop,omitempty"` // Docker Desktop: no host mounts
}

// AttachRule labels a container that has no observa.* Docker labels.
type AttachRule struct {
	Container string `yaml:"container"`
	Project   string `yaml:"project"`
	Env       string `yaml:"env"`
	Service   string `yaml:"service"`
	Preset    string `yaml:"preset"`
}

// Dir is an agent directory on disk.
type Dir struct{ Path string }

// Settings loads agent.yaml.
func (d Dir) Settings() (*Settings, error) {
	data, err := os.ReadFile(filepath.Join(d.Path, settingsFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("aucun agent dans %s (installez-le avec `observa agent install` ou précisez --agent-dir)", d.Path)
		}
		return nil, err
	}
	var s Settings
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s : %w", settingsFile, err)
	}
	return &s, nil
}

// SaveSettings writes agent.yaml.
func (d Dir) SaveSettings(s *Settings) error {
	s.Version = 1
	data, err := marshal2(s)
	if err != nil {
		return err
	}
	header := "# Réglages de l'agent ObservaBoard. Après modification : observa agent reload\n"
	_, err = fsutil.WriteFile(filepath.Join(d.Path, settingsFile), append([]byte(header), data...), 0o644)
	return err
}

// Env reads the agent .env file (token and exporter secrets).
func (d Dir) Env() (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(filepath.Join(d.Path, envFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			v = strings.TrimSpace(v)
			if len(v) >= 2 && ((v[0] == '\'' && v[len(v)-1] == '\'') || (v[0] == '"' && v[len(v)-1] == '"')) {
				v = v[1 : len(v)-1]
			}
			out[strings.TrimSpace(k)] = v
		}
	}
	return out, sc.Err()
}

var envKeyRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// SetEnv sets variables in the .env file (mode 0600), keeping the others.
func (d Dir) SetEnv(kv map[string]string) error {
	cur, err := d.Env()
	if err != nil {
		return err
	}
	for k, v := range kv {
		if !envKeyRE.MatchString(k) {
			return fmt.Errorf("nom de variable invalide : %q", k)
		}
		if strings.ContainsAny(v, "\n\r'") {
			return fmt.Errorf("la valeur de %s ne doit contenir ni retour à la ligne ni apostrophe (')", k)
		}
		cur[k] = v
	}
	var keys []string
	for k := range cur {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteString("# Secrets de l'agent ObservaBoard (token, accès aux bases). Ne pas versionner.\n")
	for _, k := range keys {
		// Single quotes: Docker Compose reads the value literally ($, # and spaces kept).
		fmt.Fprintf(&b, "%s='%s'\n", k, cur[k])
	}
	_, err = fsutil.WriteFile(filepath.Join(d.Path, envFile), b.Bytes(), 0o600)
	return err
}

// AttachRules loads attach.yaml.
func (d Dir) AttachRules() ([]AttachRule, error) {
	data, err := os.ReadFile(filepath.Join(d.Path, attachFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rules []AttachRule
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("%s : %w", attachFile, err)
	}
	return rules, nil
}

// SaveAttachRules writes attach.yaml.
func (d Dir) SaveAttachRules(rules []AttachRule) error {
	sort.Slice(rules, func(i, j int) bool { return rules[i].Container < rules[j].Container })
	data, err := yaml.Marshal(rules)
	if err != nil {
		return err
	}
	header := "# Conteneurs rattachés avec `observa attach` (sans labels Docker).\n"
	_, err = fsutil.WriteFile(filepath.Join(d.Path, attachFile), append([]byte(header), data...), 0o644)
	return err
}

// Projects loads every project applied with `observa apply`.
func (d Dir) Projects() ([]*project.Config, error) {
	files, _ := filepath.Glob(filepath.Join(d.Path, projectsDir, "*.yaml"))
	sort.Strings(files)
	var out []*project.Config
	for _, f := range files {
		c, err := project.Load(f)
		if err != nil {
			return nil, fmt.Errorf("%s : %w", filepath.Base(f), err)
		}
		out = append(out, c)
	}
	return out, nil
}

// ProjectFile is where an applied project is stored.
func (d Dir) ProjectFile(projectName, env string) string {
	return filepath.Join(d.Path, projectsDir, projectName+"__"+env+".yaml")
}

// SaveProject stores a copy of observa.yaml and its threshold metrics.
func (d Dir) SaveProject(c *project.Config) error {
	data, err := c.Marshal()
	if err != nil {
		return err
	}
	if _, err := fsutil.WriteFile(d.ProjectFile(c.Project, c.Env), data, 0o644); err != nil {
		return err
	}
	_, err = fsutil.WriteFile(filepath.Join(d.Path, textfileDir, "observa_"+c.Project+"__"+c.Env+".prom"), ThresholdMetrics(c), 0o644)
	return err
}

// RemoveProject forgets an applied project.
func (d Dir) RemoveProject(projectName, env string) error {
	err1 := os.Remove(d.ProjectFile(projectName, env))
	err2 := os.Remove(filepath.Join(d.Path, textfileDir, "observa_"+projectName+"__"+env+".prom"))
	if errors.Is(err1, os.ErrNotExist) && errors.Is(err2, os.ErrNotExist) {
		return fmt.Errorf("projet %s/%s non appliqué sur cet agent", projectName, env)
	}
	return nil
}

// ThresholdMetrics renders the textfile-collector metrics for a project:
// threshold overrides and notification routing, read by the central rules.
func ThresholdMetrics(c *project.Config) []byte {
	var b bytes.Buffer
	b.WriteString("# Généré par `observa apply` — ne pas modifier à la main.\n")
	b.WriteString("# HELP observa_service_info Service déclaré dans observa.yaml (routage des notifications).\n")
	b.WriteString("# TYPE observa_service_info gauge\n")
	for _, name := range c.ServiceNames() {
		s := c.Services[name]
		fmt.Fprintf(&b, "observa_service_info{project=%q,env=%q,service=%q,preset=%q,notify_critical=%q,notify_warning=%q} 1\n",
			c.Project, c.Env, name, s.Preset,
			strings.Join(c.Notifications.Critical, ","), strings.Join(c.Notifications.Warning, ","))
	}
	b.WriteString("# HELP observa_service_threshold Seuil d'alerte surchargé dans observa.yaml.\n")
	b.WriteString("# TYPE observa_service_threshold gauge\n")
	for _, name := range c.ServiceNames() {
		s := c.Services[name]
		p, ok := presets.Get(s.Preset)
		if !ok {
			continue
		}
		var keys []string
		for k := range s.Alerts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			param, ok := p.Param(k)
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "observa_service_threshold{project=%q,env=%q,service=%q,threshold=%q} %g\n",
				c.Project, c.Env, name, param.Metric, param.MetricValue(s.Alerts[k]))
		}
	}
	return b.Bytes()
}

// Exporter is a data-store exporter the agent runs for an applied service.
type Exporter struct {
	ID, Kind, Project, Env, Service, Address, SecretEnv string
}

// EBPFService is a service instrumented through eBPF.
type EBPFService struct {
	Project, Env, Service string
	Port                  int
}

// RenderInput gathers everything the Alloy template needs.
type RenderInput struct {
	Settings
	AgentName    string
	Attached     []AttachRule
	Exporters    []Exporter
	EBPFServices []EBPFService
	AlloyVersion string
}

// Warnings collects non-fatal problems found while rendering.
type Warnings []string

var idRE = regexp.MustCompile(`[^a-z0-9_]`)

// BuildInput loads projects, attach rules and secrets and prepares rendering.
func (d Dir) BuildInput(s *Settings) (*RenderInput, Warnings, error) {
	var warns Warnings
	in := &RenderInput{Settings: *s, AgentName: s.Name, AlloyVersion: version.Images.Alloy}
	var err error
	if in.Attached, err = d.AttachRules(); err != nil {
		return nil, nil, err
	}
	projects, err := d.Projects()
	if err != nil {
		return nil, nil, err
	}
	env, err := d.Env()
	if err != nil {
		return nil, nil, err
	}
	ebpfPorts := map[int]string{}
	for _, c := range projects {
		for _, name := range c.ServiceNames() {
			svc := c.Services[name]
			p, _ := presets.Get(svc.Preset)
			alias := project.Alias(c.Project, c.Env, name)
			if svc.Container != "" {
				alias = svc.Container
			}
			if p.Exporter != presets.ExporterNone {
				ex := Exporter{
					ID:      idRE.ReplaceAllString(strings.ToLower(p.Exporter+"_"+c.Project+"_"+c.Env+"_"+name), "_"),
					Kind:    p.Exporter,
					Project: c.Project, Env: c.Env, Service: name,
					Address: fmt.Sprintf("%s:%d", alias, svc.EffectivePort()),
				}
				secret := c.EffectiveSecretEnv(name)
				switch p.Exporter {
				case presets.ExporterPostgres:
					if env[secret] == "" {
						warns = append(warns, fmt.Sprintf("%s/%s/%s : variable %s absente du .env de l'agent, métriques PostgreSQL désactivées.\n    Ajoutez-la : observa agent env set %s='postgresql://utilisateur:motdepasse@%s/<base>?sslmode=disable'",
							c.Project, c.Env, name, secret, secret, ex.Address))
						continue
					}
					ex.SecretEnv = secret
				case presets.ExporterRedis:
					if env[secret] != "" {
						ex.SecretEnv = secret
					}
				}
				in.Exporters = append(in.Exporters, ex)
			}
			if svc.EffectiveInstrumentation() == presets.InstrEBPF {
				if !s.EBPF {
					warns = append(warns, fmt.Sprintf("%s/%s/%s : instrumentation ebpf demandée mais eBPF désactivé sur cet agent (observa agent install --ebpf).", c.Project, c.Env, name))
					continue
				}
				if prev, dup := ebpfPorts[svc.EffectivePort()]; dup {
					warns = append(warns, fmt.Sprintf("%s/%s/%s : port %d déjà suivi en eBPF pour %s sur ce serveur ; eBPF ne distingue que les ports, service ignoré.", c.Project, c.Env, name, svc.EffectivePort(), prev))
					continue
				}
				ebpfPorts[svc.EffectivePort()] = c.Project + "/" + c.Env + "/" + name
				in.EBPFServices = append(in.EBPFServices, EBPFService{Project: c.Project, Env: c.Env, Service: name, Port: svc.EffectivePort()})
			}
		}
	}
	return in, warns, nil
}

type labelPair struct{ Container, Label, Value string }

var funcs = template.FuncMap{
	"q": func(s string) string { return alloyString(s) },
	// ottl renders an OTTL statement as an Alloy raw string literal.
	"ottl": func(s string) string { return "`" + strings.ReplaceAll(s, "`", "") + "`" },
	"re":   regexp.QuoteMeta,
	"attachLabels": func(r AttachRule) []labelPair {
		p := "__meta_docker_container_label_observa_"
		return []labelPair{
			{r.Container, p + "enable", "true"},
			{r.Container, p + "project", r.Project},
			{r.Container, p + "env", r.Env},
			{r.Container, p + "service", r.Service},
			{r.Container, p + "preset", r.Preset},
		}
	},
	"attachMetricLabels": func(r AttachRule) []labelPair {
		p := "container_label_observa_"
		return []labelPair{
			{r.Container, p + "project", r.Project},
			{r.Container, p + "env", r.Env},
			{r.Container, p + "service", r.Service},
			{r.Container, p + "preset", r.Preset},
		}
	},
	"tlsConfig": func(t TLS, indent string) string {
		switch {
		case t.CAFile:
			return "\n" + indent + "tls_config {\n" + indent + "  ca_file = \"/etc/alloy/ca.crt\"\n" + indent + "}"
		case t.InsecureSkipVerify:
			return "\n" + indent + "tls_config {\n" + indent + "  insecure_skip_verify = true\n" + indent + "}"
		}
		return ""
	},
	"otlpTLS": func(t TLS, indent string) string {
		switch {
		case t.CAFile:
			return "\n" + indent + "tls {\n" + indent + "  ca_file = \"/etc/alloy/ca.crt\"\n" + indent + "}"
		case t.InsecureSkipVerify:
			return "\n" + indent + "tls {\n" + indent + "  insecure_skip_verify = true\n" + indent + "}"
		}
		return ""
	},
}

// alloyString quotes a value as an Alloy string literal.
func alloyString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// RenderConfig renders config.alloy.
func RenderConfig(in *RenderInput) ([]byte, error) {
	return renderAsset("agent/config.alloy.tmpl", in)
}

// RenderCompose renders the agent docker-compose.yml.
func RenderCompose(s *Settings) ([]byte, error) {
	return renderAsset("agent/docker-compose.yml.tmpl", map[string]any{
		"Image":   version.Images.Alloy,
		"EBPF":    s.EBPF,
		"Desktop": s.Desktop,
	})
}

func renderAsset(name string, data any) ([]byte, error) {
	src, err := assets.FS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	t, err := template.New(filepath.Base(name)).Funcs(funcs).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("template %s : %w", name, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, fmt.Errorf("template %s : %w", name, err)
	}
	return collapseBlankLines(b.Bytes()), nil
}

var blankRE = regexp.MustCompile(`\n{3,}`)

func collapseBlankLines(b []byte) []byte { return blankRE.ReplaceAll(b, []byte("\n\n")) }

// Render writes config.alloy, docker-compose.yml and the textfile directory.
// It returns whether config.alloy changed (a reload is then needed).
func (d Dir) Render(s *Settings) (changed bool, warns Warnings, err error) {
	in, warns, err := d.BuildInput(s)
	if err != nil {
		return false, nil, err
	}
	cfg, err := RenderConfig(in)
	if err != nil {
		return false, warns, err
	}
	cmp, err := RenderCompose(s)
	if err != nil {
		return false, warns, err
	}
	if err := os.MkdirAll(filepath.Join(d.Path, textfileDir), 0o755); err != nil {
		return false, warns, err
	}
	if err := os.MkdirAll(filepath.Join(d.Path, projectsDir), 0o755); err != nil {
		return false, warns, err
	}
	changed, err = fsutil.WriteFile(d.ConfigPath(), cfg, 0o644)
	if err != nil {
		return false, warns, err
	}
	if _, err := fsutil.WriteFile(filepath.Join(d.Path, "docker-compose.yml"), cmp, 0o644); err != nil {
		return false, warns, err
	}
	return changed, warns, nil
}

// CAPath is the location of the central CA certificate in the agent dir.
func (d Dir) CAPath() string { return filepath.Join(d.Path, configDir, caFile) }

// ConfigPath is the rendered Alloy configuration.
func (d Dir) ConfigPath() string { return filepath.Join(d.Path, configDir, "config.alloy") }

// marshal2 encodes YAML with a 2-space indent.
func marshal2(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
