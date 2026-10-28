package central

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/root-lr/observaboard/assets"
	"github.com/root-lr/observaboard/internal/dashboards"
	"github.com/root-lr/observaboard/internal/fsutil"
	"github.com/root-lr/observaboard/internal/presets"
	"github.com/root-lr/observaboard/internal/runbook"
	"github.com/root-lr/observaboard/internal/version"
)

// Result tells which parts of the central changed, so the CLI can reload
// only what is needed.
type Result struct {
	Changed []string // relative paths of changed files
}

// Has reports whether a changed file starts with prefix.
func (r Result) Has(prefix string) bool {
	for _, c := range r.Changed {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

type renderData struct {
	*Settings
	Images      any
	BaseURL     string
	SiteAddress string
	Ports       []string
	Tokens      []Token
	Params      []presets.Param
}

// RenderRules renders the Prometheus rule file for a base URL.
func RenderRules(baseURL string) ([]byte, error) {
	src, err := assets.FS.ReadFile("central/prometheus/rules.yml.tmpl")
	if err != nil {
		return nil, err
	}
	t, err := template.New("rules").Delims("[[", "]]").Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	err = t.Execute(&b, map[string]any{"BaseURL": baseURL, "Params": presets.AllThresholdParams()})
	return b.Bytes(), err
}

func renderTemplate(name string, data any) ([]byte, error) {
	src, err := assets.FS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	t, err := template.New(filepath.Base(name)).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return b.Bytes(), nil
}

// Files renders every central file in memory: relative path -> content, mode.
func Files(s *Settings, tokens []Token) (map[string][]byte, map[string]os.FileMode, error) {
	data := renderData{
		Settings:    s,
		Images:      version.Images,
		BaseURL:     s.BaseURL(),
		SiteAddress: s.SiteAddress(),
		Ports:       s.Ports(),
		Tokens:      tokens,
		Params:      presets.AllThresholdParams(),
	}
	files := map[string][]byte{}
	modes := map[string]os.FileMode{}
	templated := map[string]string{
		"docker-compose.yml":                            "central/docker-compose.yml.tmpl",
		"caddy/Caddyfile":                               "central/caddy/Caddyfile.tmpl",
		"prometheus/prometheus.yml":                     "central/prometheus/prometheus.yml.tmpl",
		"loki/loki.yaml":                                "central/loki/loki.yaml.tmpl",
		"tempo/tempo.yaml":                              "central/tempo/tempo.yaml.tmpl",
		"grafana/provisioning/datasources/observa.yaml": "central/grafana/datasources.yaml.tmpl",
		"grafana/provisioning/dashboards/observa.yaml":  "central/grafana/dashboards.yaml.tmpl",
	}
	for out, src := range templated {
		b, err := renderTemplate(src, data)
		if err != nil {
			return nil, nil, err
		}
		files[out] = b
		modes[out] = 0o644
	}
	rules, err := RenderRules(s.BaseURL())
	if err != nil {
		return nil, nil, err
	}
	files["prometheus/rules/observa.yml"] = rules
	modes["prometheus/rules/observa.yml"] = 0o644

	am, err := RenderAlertmanager(s)
	if err != nil {
		return nil, nil, err
	}
	files["alertmanager/alertmanager.yml"] = am
	// Readable by the container user (nobody); the secrets are protected by the
	// central directory itself, which is 0700 (see Render).
	modes["alertmanager/alertmanager.yml"] = 0o644

	for _, d := range dashboards.All() {
		j, err := d.JSON()
		if err != nil {
			return nil, nil, fmt.Errorf("dashboard %s: %w", d.UID, err)
		}
		p := "grafana/dashboards/" + d.Path()
		files[p] = j
		modes[p] = 0o644
	}

	pages, err := runbook.RenderAll(assets.FS, "runbooks")
	if err != nil {
		return nil, nil, err
	}
	for _, pg := range pages {
		p := "runbooks/" + pg.Name + ".html"
		files[p] = pg.HTML
		modes[p] = 0o644
	}
	return files, modes, nil
}

// Render writes all central files and returns what changed. Files that are
// no longer generated (e.g. a removed dashboard) are left in place.
func (d Dir) Render(s *Settings) (Result, error) {
	tokens, err := d.Tokens()
	if err != nil {
		return Result{}, err
	}
	files, modes, err := Files(s, tokens)
	if err != nil {
		return Result{}, err
	}
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	// The central directory holds secrets (central.yaml, tokens, notification
	// settings): only root may enter it. Containers see the mounted
	// subdirectories directly, so they are unaffected.
	if err := os.Chmod(d.Path, 0o700); err != nil {
		return Result{}, err
	}
	var res Result
	for _, p := range paths {
		changed, err := fsutil.WriteFile(filepath.Join(d.Path, filepath.FromSlash(p)), files[p], modes[p])
		if err != nil {
			return res, err
		}
		if changed {
			res.Changed = append(res.Changed, p)
		}
	}
	return res, nil
}

// EnsureAdminPassword creates the .env file with a random Grafana admin
// password if it does not exist, and returns the password when it was created.
func (d Dir) EnsureAdminPassword() (created string, err error) {
	p := filepath.Join(d.Path, ".env")
	if fsutil.Exists(p) {
		return "", nil
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	pw := base64.RawURLEncoding.EncodeToString(raw)
	content := "# ObservaBoard central secrets. Do not commit.\n" +
		"# The admin password is only read on Grafana's first start; after that, change it in Grafana.\n" +
		"GF_SECURITY_ADMIN_PASSWORD=" + pw + "\n"
	if _, err := fsutil.WriteFile(p, []byte(content), 0o600); err != nil {
		return "", err
	}
	return pw, nil
}
