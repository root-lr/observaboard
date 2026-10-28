// Package initgen builds the files `observa init` writes into a project:
// observa.yaml, docker-compose.observa.yml (labels, network, OpenTelemetry
// variables, wrapped command) and the derived Dockerfiles that install the SDK.
package initgen

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/root-lr/observaboard/internal/compose"
	"github.com/root-lr/observaboard/internal/instrument"
	"github.com/root-lr/observaboard/internal/presets"
	"github.com/root-lr/observaboard/internal/project"
	"github.com/root-lr/observaboard/internal/thirdparty/yaml"
)

// OverrideFileName is the generated compose override.
const OverrideFileName = "docker-compose.observa.yml"

// GeneratedFile is a file to write, relative to the project directory.
type GeneratedFile struct {
	Path    string
	Content []byte
}

// Plan is everything `observa init` will write, plus notes for the user.
type Plan struct {
	Files []GeneratedFile
	Notes []string
}

// ImageUser returns the USER configured in an image ("" when unknown). It is a
// variable so tests can replace the docker call.
var ImageUser = func(image string) string {
	out, err := exec.Command("docker", "image", "inspect", "-f", "{{.Config.User}}", image).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// startCommand holds how a container starts once instrumented.
type startCommand struct {
	entrypoint []string // set only when the entrypoint is wrapped
	command    []string
	fromImage  bool // taken from the Dockerfile: `$` must be escaped for Compose
}

// FromCompose builds the override and derived Dockerfiles for a compose project.
func FromCompose(cfg *project.Config, f *compose.File) (*Plan, error) {
	plan := &Plan{}
	root := yaml.Node{Kind: yaml.MappingNode}
	services := &yaml.Node{Kind: yaml.MappingNode}
	addPair(&root, "services", services)

	dockerfiles := map[string]string{} // path relative to project dir -> content
	usesNetwork := false

	for _, name := range cfg.ServiceNames() {
		svc := cfg.Services[name]
		cs, ok := f.Services[name]
		if !ok {
			plan.Notes = append(plan.Notes, fmt.Sprintf("%s : absent du fichier compose, ignoré dans l'override (utilisez observa attach s'il est lancé autrement).", name))
			continue
		}
		node := &yaml.Node{Kind: yaml.MappingNode}
		addPair(services, name, node)
		addPair(node, "labels", mapNode(instrument.Labels(cfg.Project, cfg.Env, name, svc.Preset)))

		runtime := svc.EffectiveRuntime()
		if svc.EffectiveInstrumentation() == presets.InstrOTel {
			if err := instrumentService(plan, node, dockerfiles, f, cs, cfg, name, runtime, svc.Healthcheck); err != nil {
				plan.Notes = append(plan.Notes, fmt.Sprintf("%s : instrumentation désactivée (%v).", name, err))
			}
		}

		if cs.NetworkMode != "" {
			plan.Notes = append(plan.Notes, fmt.Sprintf("%s : network_mode %q, pas de réseau observa possible ; logs et ressources restent surveillés, mais ni traces OpenTelemetry ni exporter vers ce service.", name, cs.NetworkMode))
			continue
		}
		usesNetwork = true
		nets := &yaml.Node{Kind: yaml.MappingNode}
		if cs.Networks == nil {
			addPair(nets, "default", &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle})
		}
		obs := &yaml.Node{Kind: yaml.MappingNode}
		addPair(obs, "aliases", seqNode([]string{project.Alias(cfg.Project, cfg.Env, name)}))
		addPair(nets, instrument.NetworkName, obs)
		addPair(node, "networks", nets)
	}

	if usesNetwork {
		nets := &yaml.Node{Kind: yaml.MappingNode}
		ext := &yaml.Node{Kind: yaml.MappingNode}
		addPair(ext, "external", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		addPair(nets, instrument.NetworkName, ext)
		addPair(&root, "networks", nets)
	}

	var b bytes.Buffer
	b.WriteString("# Généré par `observa init` — ne pas modifier à la main : relancez `observa init`.\n")
	b.WriteString("# Usage : " + ComposeCommand(f) + " up -d --build\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, err
	}
	enc.Close()
	plan.Files = append(plan.Files, GeneratedFile{Path: OverrideFileName, Content: b.Bytes()})

	var dfPaths []string
	for p := range dockerfiles {
		dfPaths = append(dfPaths, p)
	}
	sort.Strings(dfPaths)
	for _, p := range dfPaths {
		plan.Files = append(plan.Files, GeneratedFile{Path: p, Content: []byte(dockerfiles[p])})
	}
	return plan, nil
}

// instrumentService adds the derived image, OpenTelemetry variables and the
// instrumented start command of one service.
func instrumentService(plan *Plan, node *yaml.Node, dockerfiles map[string]string, f *compose.File, cs *compose.Service, cfg *project.Config, name, runtime, healthcheck string) error {
	dfRel, dfContent, start, err := derivedImage(f, cs, cfg, name, runtime)
	if err != nil {
		return err
	}
	if prev, ok := dockerfiles[dfRel]; ok && prev != dfContent {
		return fmt.Errorf("%s est partagé avec un autre service mais demande une instrumentation différente", dfRel)
	}
	dockerfiles[dfRel] = dfContent

	ctx, dfInCtx := ".", dfRel
	if cs.Build != nil {
		ctx = cs.Build.Context
		if rel, err := filepath.Rel(filepath.Join(f.Dir, cs.Build.Context), filepath.Join(f.Dir, dfRel)); err == nil {
			dfInCtx = filepath.ToSlash(rel)
		}
	} else {
		ctx = filepath.ToSlash(filepath.Dir(dfRel))
		dfInCtx = filepath.Base(dfRel)
	}
	build := &yaml.Node{Kind: yaml.MappingNode}
	addPair(build, "context", scalar(ctx))
	addPair(build, "dockerfile", scalar(dfInCtx))
	if cs.Build != nil && cs.Build.Target != "" {
		addPair(build, "target", scalar(cs.Build.Target))
	}
	addPair(node, "build", build)
	addPair(node, "image", scalar(fmt.Sprintf("observa-local/%s-%s:%s", cfg.Project, project.Slug(name), cfg.Env)))
	t := instrument.Target{Project: cfg.Project, Env: cfg.Env, Service: name, Preset: cfg.Services[name].Preset, Runtime: runtime, Healthcheck: healthcheck}
	addPair(node, "environment", mapNode(instrument.Env(t, cs.Environment)))
	if start.entrypoint != nil {
		addPair(node, "entrypoint", seqNode(escapeList(start.entrypoint, start.fromImage)))
	}
	if start.command != nil {
		addPair(node, "command", seqNode(escapeList(start.command, start.fromImage)))
	}
	return nil
}

// derivedImage returns the derived Dockerfile (path relative to the project,
// content) and how the instrumented container starts.
func derivedImage(f *compose.File, cs *compose.Service, cfg *project.Config, name, runtime string) (string, string, startCommand, error) {
	var start startCommand
	if runtime != presets.RuntimePython && runtime != presets.RuntimeNode {
		return "", "", start, fmt.Errorf("runtime inconnu")
	}
	var (
		user, rel, content string
		imgEntry, imgCmd   []string
	)
	switch {
	case cs.Build != nil:
		df, i, err := f.EffectiveDockerfile(cs)
		if err != nil {
			return "", "", start, fmt.Errorf("Dockerfile : %v", err)
		}
		user = df.User(i)
		if user == "" {
			user = ImageUser(df.ExternalBase(i))
		}
		block, err := instrument.DockerfileBlock(runtime, user)
		if err != nil {
			return "", "", start, err
		}
		imgEntry, imgCmd = df.Entrypoint(i), df.Cmd(i)
		if rel, err = filepath.Rel(f.Dir, f.DockerfilePath(cs)+".observa"); err != nil {
			return "", "", start, err
		}
		content = df.InsertAfterStage(i, block)
	case cs.Image != "":
		user = ImageUser(cs.Image)
		block, err := instrument.DockerfileBlock(runtime, user)
		if err != nil {
			return "", "", start, err
		}
		rel = filepath.Join(".observa", name, "Dockerfile")
		content = "# Image dérivée générée par `observa init`.\nFROM " + cs.Image + "\n" + block
	default:
		return "", "", start, fmt.Errorf("ni image ni build")
	}

	if runtime == presets.RuntimePython {
		entry, entryFromImage := cs.Entrypoint, false
		if entry == nil {
			entry, entryFromImage = imgEntry, true
		}
		cmd, cmdFromImage := cs.Command, false
		if cmd == nil {
			cmd, cmdFromImage = imgCmd, true
		}
		switch {
		case len(entry) > 0:
			// Wrap the entrypoint; overriding it resets the image CMD, so
			// the command is restated.
			start.entrypoint, _ = instrument.WrapCommand(runtime, entry)
			start.command = cmd
			if start.command == nil {
				start.command = []string{}
			}
			start.fromImage = entryFromImage || cmdFromImage
		case len(cmd) > 0:
			start.command, _ = instrument.WrapCommand(runtime, cmd)
			start.fromImage = cmdFromImage
		case cs.Build == nil:
			return "", "", start, fmt.Errorf("commande de démarrage inconnue (image %s sans command:) ; ajoutez command: dans le compose", cs.Image)
		default:
			return "", "", start, fmt.Errorf("aucune commande trouvée (ni command:, ni CMD, ni ENTRYPOINT)")
		}
	}
	return filepath.ToSlash(rel), content, start, nil
}

// escapeList protects `$` from Compose interpolation for values that were
// not written in the compose file (Dockerfile CMD/ENTRYPOINT): the variables
// must be expanded inside the container, as Docker would do.
func escapeList(l []string, fromImage bool) []string {
	if !fromImage {
		return l
	}
	out := make([]string, len(l))
	for i, v := range l {
		out[i] = strings.ReplaceAll(v, "$", "$$")
	}
	return out
}

// ComposeCommand is the docker compose invocation including the project's
// implicit override file (ignored by Compose as soon as -f is used) and ours.
func ComposeCommand(f *compose.File) string {
	parts := []string{"docker compose", "-f " + filepath.Base(f.Path)}
	for _, n := range []string{"compose.override.yaml", "compose.override.yml", "docker-compose.override.yml", "docker-compose.override.yaml"} {
		if st, err := os.Stat(filepath.Join(f.Dir, n)); err == nil && !st.IsDir() {
			parts = append(parts, "-f "+n)
			break
		}
	}
	parts = append(parts, "-f "+OverrideFileName)
	return strings.Join(parts, " ")
}

// ForDockerRun builds per-service env files and the docker run hints used when
// a project has no compose file.
func ForDockerRun(cfg *project.Config) *Plan {
	plan := &Plan{}
	for _, name := range cfg.ServiceNames() {
		svc := cfg.Services[name]
		labels := instrument.Labels(cfg.Project, cfg.Env, name, svc.Preset)
		var flags []string
		for _, k := range instrument.EnvKeys(labels) {
			flags = append(flags, fmt.Sprintf("-l %s=%s", k, labels[k]))
		}
		flags = append(flags, "--network "+instrument.NetworkName, "--network-alias "+project.Alias(cfg.Project, cfg.Env, name))
		if svc.EffectiveInstrumentation() == presets.InstrOTel {
			t := instrument.Target{Project: cfg.Project, Env: cfg.Env, Service: name, Preset: svc.Preset, Runtime: svc.EffectiveRuntime(), Healthcheck: svc.Healthcheck}
			env := instrument.Env(t, nil)
			var b strings.Builder
			b.WriteString("# Généré par `observa init` pour " + name + " : docker run --env-file " + filepath.ToSlash(filepath.Join(".observa", name+".env")) + " ...\n")
			for _, k := range instrument.EnvKeys(env) {
				fmt.Fprintf(&b, "%s=%s\n", k, env[k])
			}
			plan.Files = append(plan.Files, GeneratedFile{Path: filepath.Join(".observa", name+".env"), Content: []byte(b.String())})
			flags = append(flags, "--env-file .observa/"+name+".env")
			if blk, err := instrument.DockerfileBlock(svc.EffectiveRuntime(), ""); err == nil {
				plan.Files = append(plan.Files, GeneratedFile{
					Path:    filepath.Join(".observa", name, "Dockerfile.snippet"),
					Content: []byte("# Ajoutez ces lignes à la fin du Dockerfile de " + name + " puis reconstruisez l'image.\n" + blk),
				})
			}
		}
		cmdHint := ""
		if svc.EffectiveInstrumentation() == presets.InstrOTel && svc.EffectiveRuntime() == presets.RuntimePython {
			cmdHint = " opentelemetry-instrument <votre commande>"
		}
		plan.Notes = append(plan.Notes, fmt.Sprintf("%s : docker run -d --name %s \\\n      %s \\\n      <image>%s",
			name, containerName(cfg, name), strings.Join(flags, " \\\n      "), cmdHint))
	}
	return plan
}

func containerName(cfg *project.Config, name string) string {
	if s := cfg.Services[name]; s != nil && s.Container != "" {
		return s.Container
	}
	return project.Alias(cfg.Project, cfg.Env, name)
}

// Write writes the plan's files under dir.
func (p *Plan) Write(dir string) ([]string, error) {
	var written []string
	for _, gf := range p.Files {
		path := filepath.Join(dir, gf.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(path, gf.Content, 0o644); err != nil {
			return written, err
		}
		written = append(written, gf.Path)
	}
	return written, nil
}

// --- yaml.Node helpers ---

func scalar(v string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	return n
}

func addPair(m *yaml.Node, key string, val *yaml.Node) {
	m.Content = append(m.Content, scalar(key), val)
}

func mapNode(kv map[string]string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		addPair(n, k, scalar(kv[k]))
	}
	return n
}

func seqNode(items []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode}
	for _, it := range items {
		n.Content = append(n.Content, scalar(it))
	}
	return n
}
