// Package project loads, validates and writes observa.yaml, the per-project
// description that lives in each application repository.
package project

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/root-lr/observaboard/internal/presets"
	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
)

// FileName is the conventional name of the project file.
const FileName = "observa.yaml"

// Config is the content of observa.yaml.
type Config struct {
	Version  int                 `yaml:"version"`
	Project  string              `yaml:"project"`
	Env      string              `yaml:"env"`
	Services map[string]*Service `yaml:"services"`
	// Ignored lists compose services deliberately left out (observa init keeps them out).
	Ignored       []string      `yaml:"ignored,omitempty"`
	Notifications Notifications `yaml:"notifications,omitempty"`
}

// Service describes one monitored container.
type Service struct {
	Preset string `yaml:"preset"`
	// Runtime is the language runtime used for zero-code instrumentation
	// ("python", "node" or empty).
	Runtime string `yaml:"runtime,omitempty"`
	// Instrumentation: "otel", "ebpf" or "none". Empty means the preset default.
	Instrumentation string `yaml:"instrumentation,omitempty"`
	// Port is the internal port the service listens on.
	Port int `yaml:"port,omitempty"`
	// Healthcheck is an HTTP path excluded from traces (e.g. /health/).
	Healthcheck string `yaml:"healthcheck,omitempty"`
	// Container is the container name, for containers started with `docker run`.
	Container string `yaml:"container,omitempty"`
	// SecretEnv names the variable, in the agent's .env file, that holds the
	// connection string (postgres) or the password (redis) the exporter uses.
	SecretEnv string `yaml:"secret_env,omitempty"`
	// Alerts overrides threshold defaults, keyed by preset parameter.
	Alerts map[string]float64 `yaml:"alerts,omitempty"`
}

// Notifications maps a severity to channel names defined on the central server.
type Notifications struct {
	Critical []string `yaml:"critical,omitempty"`
	Warning  []string `yaml:"warning,omitempty"`
}

// SlugRE is the format accepted for project, env and channel names.
var SlugRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$`)

// ServiceRE accepts Docker Compose service names (letters, digits, _ . -).
var ServiceRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// Load reads and validates an observa.yaml file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes observa.yaml content, rejecting unknown keys, and validates it.
func Parse(data []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("observa.yaml illisible : %w", err)
	}
	if errs := c.Validate(); len(errs) > 0 {
		return &c, ValidationError(errs)
	}
	return &c, nil
}

// ValidationError lists every problem found in a file.
type ValidationError []string

func (v ValidationError) Error() string {
	return "observa.yaml invalide :\n  - " + strings.Join(v, "\n  - ")
}

// Validate checks the configuration and returns every problem found.
func (c *Config) Validate() []string {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	if c.Version != 1 {
		add("version doit valoir 1 (trouvé : %d)", c.Version)
	}
	if !SlugRE.MatchString(c.Project) {
		add("project %q : minuscules, chiffres et tirets uniquement (1 à 40 caractères)", c.Project)
	}
	if !SlugRE.MatchString(c.Env) {
		add("env %q : minuscules, chiffres et tirets uniquement (ex. prod, staging)", c.Env)
	}
	if len(c.Services) == 0 {
		add("services : au moins un service est requis")
	}
	for _, name := range c.ServiceNames() {
		s := c.Services[name]
		where := "services." + name
		if s == nil {
			add("%s : définition vide", where)
			continue
		}
		if !ServiceRE.MatchString(name) {
			add("%s : nom invalide (lettres, chiffres, _ . -)", where)
		}
		p, ok := presets.Get(s.Preset)
		if !ok {
			add("%s.preset %q inconnu (disponibles : %s)", where, s.Preset, strings.Join(presets.Names(), ", "))
			continue
		}
		switch s.Runtime {
		case "", presets.RuntimePython, presets.RuntimeNode:
		default:
			add("%s.runtime %q : valeurs possibles python, node", where, s.Runtime)
		}
		switch s.Instrumentation {
		case "", presets.InstrNone, presets.InstrEBPF:
		case presets.InstrOTel:
			rt := s.EffectiveRuntime()
			if rt == "" {
				add("%s.instrumentation otel : précisez runtime (python ou node)", where)
			} else if p.Runtime != "" && p.Runtime != rt {
				add("%s.instrumentation otel : le preset %s attend le runtime %s", where, p.Name, p.Runtime)
			}
			if p.Exporter != presets.ExporterNone {
				add("%s.instrumentation otel : sans objet pour le preset %s", where, p.Name)
			}
		default:
			add("%s.instrumentation %q : valeurs possibles otel, ebpf, none", where, s.Instrumentation)
		}
		if s.Port < 0 || s.Port > 65535 {
			add("%s.port %d hors limites", where, s.Port)
		}
		if s.Instrumentation == presets.InstrEBPF && s.EffectivePort() == 0 {
			add("%s.instrumentation ebpf : port requis", where)
		}
		if s.Healthcheck != "" && !strings.HasPrefix(s.Healthcheck, "/") {
			add("%s.healthcheck doit commencer par / (ex. /health/)", where)
		}
		if s.SecretEnv != "" && !envNameRE.MatchString(s.SecretEnv) {
			add("%s.secret_env %q : nom de variable invalide", where, s.SecretEnv)
		}
		for _, k := range sortedKeys(s.Alerts) {
			param, ok := p.Param(k)
			if !ok {
				if len(p.Params) == 0 {
					add("%s.alerts.%s : le preset %s n'a pas de seuil surchargeable", where, k, p.Name)
				} else {
					add("%s.alerts.%s inconnu pour le preset %s (possibles : %s)", where, k, p.Name, strings.Join(p.ParamKeys(), ", "))
				}
				continue
			}
			v := s.Alerts[k]
			if v < param.Min || v > param.Max {
				add("%s.alerts.%s = %g hors limites [%g, %g]", where, k, v, param.Min, param.Max)
			}
		}
	}
	for sev, chans := range map[string][]string{"critical": c.Notifications.Critical, "warning": c.Notifications.Warning} {
		for _, ch := range chans {
			if !SlugRE.MatchString(ch) {
				add("notifications.%s : canal %q invalide (minuscules, chiffres et tirets)", sev, ch)
			}
		}
	}
	sort.Strings(errs)
	return errs
}

var envNameRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ServiceNames returns the service names, sorted.
func (c *Config) ServiceNames() []string {
	return sortedKeys(c.Services)
}

// EffectiveRuntime returns the runtime, inferred from the preset when unset.
func (s *Service) EffectiveRuntime() string {
	if s.Runtime != "" {
		return s.Runtime
	}
	if p, ok := presets.Get(s.Preset); ok {
		return p.Runtime
	}
	return ""
}

// EffectiveInstrumentation returns the instrumentation mode, resolving defaults.
func (s *Service) EffectiveInstrumentation() string {
	if s.Instrumentation != "" {
		return s.Instrumentation
	}
	p, ok := presets.Get(s.Preset)
	if !ok {
		return presets.InstrNone
	}
	return p.DefaultInstrumentation(s.EffectiveRuntime())
}

// EffectivePort returns the port, falling back to the preset default.
func (s *Service) EffectivePort() int {
	if s.Port != 0 {
		return s.Port
	}
	if p, ok := presets.Get(s.Preset); ok {
		return p.DefaultPort
	}
	return 0
}

var nonDNS = regexp.MustCompile(`[^a-z0-9-]+`)

// Slug turns a compose service name into a DNS-safe name (celery_worker -> celery-worker).
func Slug(service string) string {
	return strings.Trim(nonDNS.ReplaceAllString(strings.ToLower(service), "-"), "-")
}

// Alias is the unique DNS name of a service on the shared `observa` network.
func Alias(project, env, service string) string {
	return project + "-" + env + "-" + Slug(service)
}

// DefaultSecretEnv is the agent .env variable holding a service secret.
func DefaultSecretEnv(project, env, service string) string {
	r := strings.NewReplacer("-", "_", ".", "_")
	return strings.ToUpper("OBSERVA_SECRET_" + r.Replace(project) + "_" + r.Replace(env) + "_" + r.Replace(service))
}

// EffectiveSecretEnv returns the secret variable name for a service.
func (c *Config) EffectiveSecretEnv(name string) string {
	if s := c.Services[name]; s != nil && s.SecretEnv != "" {
		return s.SecretEnv
	}
	return DefaultSecretEnv(c.Project, c.Env, name)
}

// Marshal renders the config as commented YAML.
func (c *Config) Marshal() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("# Description du projet pour ObservaBoard.\n")
	b.WriteString("# Référence : https://github.com/root-lr/observaboard/blob/main/docs/observa-yaml.md\n")
	b.WriteString("# Vérifier : observa validate — Appliquer sur le serveur : observa apply\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Save writes the config to path, refusing to write an invalid file.
func (c *Config) Save(path string) error {
	if errs := c.Validate(); len(errs) > 0 {
		return ValidationError(errs)
	}
	data, err := c.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}

// ErrNotFound is returned when no observa.yaml exists.
var ErrNotFound = errors.New("aucun observa.yaml dans ce dossier (lancez observa init)")
