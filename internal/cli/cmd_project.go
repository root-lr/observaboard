package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/root-lr/observaboard/internal/agent"
	"github.com/root-lr/observaboard/internal/compose"
	"github.com/root-lr/observaboard/internal/detect"
	"github.com/root-lr/observaboard/internal/dockerx"
	"github.com/root-lr/observaboard/internal/initgen"
	"github.com/root-lr/observaboard/internal/presets"
	"github.com/root-lr/observaboard/internal/project"
)

func runInit(args []string) error {
	fs := newFlags("observa init", "observa init [--project <nom>] [--env prod] [--yes] [--runtime docker --service api=django ...]")
	dirFlag := fs.String("dir", ".", "dossier du projet")
	projectName := fs.String("project", "", "nom du projet (défaut : nom du dossier ou valeur existante)")
	env := fs.String("env", "", "environnement (défaut : prod ou valeur existante)")
	yes := fs.Bool("yes", false, "accepter les choix détectés sans poser de question")
	runtime := fs.String("runtime", "auto", "compose (fichier compose) ou docker (conteneurs lancés avec docker run) ; auto = compose si présent")
	var services multiFlag
	fs.Var(&services, "service", "mode docker : service à déclarer, nom=preset[:python|node] (répétable)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	dir, err := filepath.Abs(*dirFlag)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(dir, project.FileName)

	// Start from the existing observa.yaml so manual edits survive a re-run.
	cfg := &project.Config{Version: 1, Services: map[string]*project.Service{}}
	existing := false
	if fileExists(cfgPath) {
		c, err := project.Load(cfgPath)
		if err != nil {
			var ve project.ValidationError
			if !errors.As(err, &ve) {
				return err
			}
			warn("observa.yaml existant invalide, les erreurs seront corrigées si possible")
		}
		if c != nil {
			cfg = c
			existing = true
			if cfg.Services == nil {
				cfg.Services = map[string]*project.Service{}
			}
		}
	}
	if *projectName != "" {
		cfg.Project = *projectName
	}
	if cfg.Project == "" {
		cfg.Project = ask("Nom du projet ?", slugify(filepath.Base(dir)), *yes)
	}
	if *env != "" {
		cfg.Env = *env
	}
	if cfg.Env == "" {
		cfg.Env = ask("Environnement ?", "prod", *yes)
	}

	composePath := compose.Find(dir)
	mode := *runtime
	if mode == "auto" {
		mode = "compose"
		if composePath == "" {
			mode = "docker"
		}
	}

	switch mode {
	case "compose":
		if composePath == "" {
			return fmt.Errorf("aucun fichier compose dans %s (compose.yaml, docker-compose.yml…) ; pour des conteneurs docker run : --runtime docker --service nom=preset", dir)
		}
		f, err := compose.Load(composePath)
		if err != nil {
			return err
		}
		step("Analyse de %s", filepath.Base(composePath))
		fmt.Fprintf(stdout, "\n%-16s %-10s %-8s %-6s %s\n", "SERVICE", "PRESET", "RUNTIME", "PORT", "DÉTECTÉ PAR")
		ignored := map[string]bool{}
		for _, n := range cfg.Ignored {
			ignored[n] = true
		}
		for _, r := range detect.All(f) {
			if ignored[r.Service] {
				fmt.Fprintf(stdout, "%-16s %-10s %-8s %-6s %s\n", r.Service, "-", "-", "-", "ignoré (observa.yaml : ignored)")
				continue
			}
			if prev, ok := cfg.Services[r.Service]; ok {
				fmt.Fprintf(stdout, "%-16s %-10s %-8s %-6d %s\n", r.Service, prev.Preset, prev.EffectiveRuntime(), prev.EffectivePort(), "observa.yaml existant (conservé)")
				continue
			}
			fmt.Fprintf(stdout, "%-16s %-10s %-8s %-6d %s\n", r.Service, r.Preset, dash(r.Runtime), r.Port, strings.Join(r.Reasons, ", "))
			for _, w := range r.Warnings {
				info("  ! %s", w)
			}
			cfg.Services[r.Service] = &project.Service{Preset: r.Preset, Runtime: r.Runtime, Port: r.Port}
		}
		fmt.Fprintln(stdout)
		if !*yes {
			for _, name := range cfg.ServiceNames() {
				if _, inCompose := f.Services[name]; !inCompose {
					continue
				}
				s := cfg.Services[name]
				answer := ask(fmt.Sprintf("Preset pour %s (%s, ou « ignorer ») ?", name, strings.Join(presets.Names(), "/")), s.Preset, false)
				if answer == "ignorer" || answer == "ignore" || answer == "-" {
					delete(cfg.Services, name)
					cfg.Ignored = append(cfg.Ignored, name)
					continue
				}
				s.Preset = answer
			}
		}
		if errs := cfg.Validate(); len(errs) > 0 {
			return project.ValidationError(errs)
		}
		plan, err := initgen.FromCompose(cfg, f)
		if err != nil {
			return err
		}
		return writeInit(dir, cfgPath, cfg, plan, existing, initgen.ComposeCommand(f))

	case "docker":
		for _, sv := range services {
			name, spec, okCut := strings.Cut(sv, "=")
			if !okCut {
				return usagef("--service attend nom=preset[:runtime] (ex. api=django)")
			}
			preset, rt, _ := strings.Cut(spec, ":")
			s := cfg.Services[name]
			if s == nil {
				s = &project.Service{}
				cfg.Services[name] = s
			}
			s.Preset, s.Runtime = preset, rt
		}
		if len(cfg.Services) == 0 {
			return usagef("mode docker : déclarez les services avec --service nom=preset (ex. --service api=django --service db=postgres)")
		}
		if errs := cfg.Validate(); len(errs) > 0 {
			return project.ValidationError(errs)
		}
		plan := initgen.ForDockerRun(cfg)
		return writeInit(dir, cfgPath, cfg, plan, existing, "")
	default:
		return usagef("--runtime : compose, docker ou auto")
	}
}

func writeInit(dir, cfgPath string, cfg *project.Config, plan *initgen.Plan, existing bool, composeName string) error { // composeName: full docker compose command, "" in docker run mode
	if err := cfg.Save(cfgPath); err != nil {
		return err
	}
	if existing {
		ok("observa.yaml mis à jour")
	} else {
		ok("observa.yaml créé")
	}
	written, err := plan.Write(dir)
	if err != nil {
		return err
	}
	for _, w := range written {
		ok("%s généré", w)
	}
	for _, n := range plan.Notes {
		warn("%s", n)
	}
	fmt.Fprintln(stdout)
	info("Étapes suivantes, sur le serveur où tourne le projet (agent installé) :")
	info("  1. observa apply                      # enregistre seuils, routage et bases auprès de l'agent")
	if composeName != "" {
		info("  2. %s up -d --build", composeName)
	} else {
		info("  2. relancez chaque conteneur avec la commande docker run indiquée ci-dessus")
	}
	info("  3. observa doctor                     # vérifie que les données arrivent")
	info("Ajustez seuils et notifications dans observa.yaml (services.<nom>.alerts, notifications).")
	return nil
}

func runValidate(args []string) error {
	fs := newFlags("observa validate", "observa validate [observa.yaml]")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	path := project.FileName
	if len(pos) > 0 {
		path = pos[0]
	}
	cfg, err := project.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return project.ErrNotFound
		}
		return err
	}
	// Cross-check with the compose file when there is one.
	if cp := compose.Find(filepath.Dir(path)); cp != "" {
		if f, err := compose.Load(cp); err == nil {
			for _, name := range cfg.ServiceNames() {
				if _, ok := f.Services[name]; !ok && cfg.Services[name].Container == "" {
					warn("services.%s absent de %s (lancé avec docker run ? précisez container:)", name, filepath.Base(cp))
				}
			}
		}
	}
	ok("%s valide : projet %s, env %s, %d service(s)", path, cfg.Project, cfg.Env, len(cfg.Services))
	return nil
}

func runApply(args []string) error {
	fs := newFlags("observa apply", "observa apply [observa.yaml] [--agent-dir <dossier>] [--remove]")
	dir := agentDirFlag(fs, "agent-dir")
	remove := fs.Bool("remove", false, "retirer ce projet de l'agent")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	path := project.FileName
	if len(pos) > 0 {
		path = pos[0]
	}
	cfg, err := project.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return project.ErrNotFound
		}
		return err
	}
	d := agent.Dir{Path: *dir}
	if _, err := d.Settings(); err != nil {
		return err
	}
	if *remove {
		if err := d.RemoveProject(cfg.Project, cfg.Env); err != nil {
			return err
		}
		if err := reloadAgent(d); err != nil {
			return err
		}
		ok("Projet %s/%s retiré de l'agent", cfg.Project, cfg.Env)
		return nil
	}
	if err := d.SaveProject(cfg); err != nil {
		return err
	}
	ok("Projet %s/%s enregistré auprès de l'agent (%s)", cfg.Project, cfg.Env, d.Path)
	for _, name := range cfg.ServiceNames() {
		s := cfg.Services[name]
		p, _ := presets.Get(s.Preset)
		var th []string
		for _, param := range p.Params {
			v, custom := s.Alerts[param.Key]
			if !custom {
				v = param.Default
			}
			mark := ""
			if custom {
				mark = "*"
			}
			th = append(th, fmt.Sprintf("%s=%g%s", param.Key, v, mark))
		}
		info("%-14s %-9s %-5s %s", name, s.Preset, s.EffectiveInstrumentation(), strings.Join(th, " "))
	}
	if len(cfg.Notifications.Critical)+len(cfg.Notifications.Warning) > 0 {
		info("Notifications : critique → %s · avertissement → %s (canaux à définir dans central.yaml)",
			dash(strings.Join(cfg.Notifications.Critical, ",")), dash(strings.Join(cfg.Notifications.Warning, ",")))
	}
	info("(* = seuil personnalisé ; les autres valeurs sont les défauts des presets)")
	if err := reloadAgent(d); err != nil {
		return err
	}
	if out, err := dockerx.Output("ps", "--filter", "label=observa.project="+cfg.Project, "--filter", "label=observa.env="+cfg.Env, "--format", "{{.Names}}"); err == nil && strings.TrimSpace(out) == "" {
		warn("Aucun conteneur de %s/%s ne tourne encore avec les labels observa : démarrez-le avec l'override généré par observa init.", cfg.Project, cfg.Env)
	}
	return nil
}

func runPresetList(args []string) error {
	fmt.Fprintf(stdout, "%-10s %-12s %s\n", "PRESET", "DASHBOARD", "DESCRIPTION")
	for _, p := range presets.All() {
		fmt.Fprintf(stdout, "%-10s %-12s %s\n", p.Name, strings.TrimPrefix(p.Dashboard, "observa-"), p.Description)
	}
	return nil
}

func runPresetShow(args []string) error {
	if len(args) != 1 {
		return usagef("observa preset show <nom> (%s)", strings.Join(presets.Names(), ", "))
	}
	p, ok := presets.Get(args[0])
	if !ok {
		return usagef("preset %q inconnu (%s)", args[0], strings.Join(presets.Names(), ", "))
	}
	fmt.Fprint(stdout, p.Describe())
	return nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func slugify(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		return "mon-projet"
	}
	return s
}
