// Package cli implements the `observa` command line.
package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/root-lr/observaboard/internal/dockerx"
	"github.com/root-lr/observaboard/internal/version"
)

// Streams used by the CLI (replaced in tests).
var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
	stdin  io.Reader = os.Stdin
)

type command struct {
	name    string
	summary string
	run     func(args []string) error
	sub     []command
}

func commands() []command {
	return []command{
		{name: "up", summary: "Lancer central + agent sur cette machine (--all-in-one)", run: runUp},
		{name: "down", summary: "Arrêter l'installation all-in-one", run: runDown},
		{name: "central", summary: "Installer et gérer le serveur central", sub: []command{
			{name: "install", summary: "Installer le central (Grafana, Prometheus, Loki, Tempo, Alertmanager, Caddy)", run: runCentralInstall},
			{name: "reload", summary: "Régénérer la config depuis central.yaml et recharger", run: runCentralReload},
			{name: "upgrade", summary: "Mettre à jour les images en conservant données et config", run: runCentralUpgrade},
			{name: "status", summary: "État des composants du central", run: runCentralStatus},
			{name: "ca", summary: "Afficher le certificat de l'autorité interne (mode --ip)", run: runCentralCA},
		}},
		{name: "token", summary: "Gérer les tokens des agents (sur le central)", sub: []command{
			{name: "create", summary: "Créer un token pour un agent : observa token create <agent>", run: runTokenCreate},
			{name: "list", summary: "Lister les tokens", run: runTokenList},
			{name: "revoke", summary: "Révoquer le token d'un agent : observa token revoke <agent>", run: runTokenRevoke},
		}},
		{name: "agent", summary: "Installer et gérer l'agent d'un serveur", sub: []command{
			{name: "install", summary: "Installer l'agent : --central <url> --token <token>", run: runAgentInstall},
			{name: "reload", summary: "Régénérer la config de l'agent et la recharger", run: runAgentReload},
			{name: "status", summary: "État de l'agent et services découverts", run: runAgentStatus},
			{name: "env", summary: "Gérer les secrets de l'agent : observa agent env set NOM=valeur", run: runAgentEnv},
		}},
		{name: "init", summary: "Détecter la stack d'un projet et générer observa.yaml + l'override", run: runInit},
		{name: "validate", summary: "Valider observa.yaml", run: runValidate},
		{name: "apply", summary: "Enregistrer le projet auprès de l'agent local (seuils, routage, exporters)", run: runApply},
		{name: "attach", summary: "Surveiller un conteneur existant sans labels : observa attach <conteneur>", run: runAttach},
		{name: "detach", summary: "Retirer une règle observa attach : observa detach <conteneur>", run: runDetach},
		{name: "preset", summary: "Lister ou décrire les presets", sub: []command{
			{name: "list", summary: "Lister les presets", run: runPresetList},
			{name: "show", summary: "Décrire un preset : observa preset show <nom>", run: runPresetShow},
		}},
		{name: "doctor", summary: "Diagnostiquer l'installation (agent, central, projet)", run: runDoctor},
		{name: "alert", summary: "Outils d'alerte", sub: []command{
			{name: "test", summary: "Envoyer une alerte de test pour vérifier les notifications", run: runAlertTest},
		}},
		{name: "version", summary: "Afficher la version et les images utilisées", run: runVersion},
	}
}

// Run executes the CLI and returns the process exit code.
func Run(args []string) int {
	var rest []string
	for _, a := range args {
		switch a {
		case "--dry-run":
			dockerx.DryRun = true
		default:
			rest = append(rest, a)
		}
	}
	if err := dispatch(commands(), rest, "observa"); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintln(stderr, "✗ "+ue.msg)
			return 2
		}
		fmt.Fprintln(stderr, "✗ "+err.Error())
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

func dispatch(cmds []command, args []string, prefix string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printHelp(cmds, prefix)
		return nil
	}
	for _, c := range cmds {
		if c.name != args[0] {
			continue
		}
		if c.sub != nil {
			return dispatch(c.sub, args[1:], prefix+" "+c.name)
		}
		return c.run(args[1:])
	}
	printHelp(cmds, prefix)
	return usagef("commande inconnue : %s %s", prefix, args[0])
}

func printHelp(cmds []command, prefix string) {
	if prefix == "observa" {
		fmt.Fprintf(stdout, "ObservaBoard %s — observabilité self-hosted en une commande.\n\n", version.Version)
	}
	fmt.Fprintf(stdout, "Usage : %s <commande> [options]\n\nCommandes :\n", prefix)
	for _, c := range cmds {
		fmt.Fprintf(stdout, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(stdout, "\nAide d'une commande : %s <commande> --help · Option globale : --dry-run (affiche les commandes docker sans les exécuter)\n", prefix)
}

// newFlags creates a FlagSet with French usage output.
func newFlags(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage : %s\n\nOptions :\n", usage)
		fs.PrintDefaults()
	}
	return fs
}

// parse parses flags placed anywhere among positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// multiFlag collects repeated string flags.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// --- output helpers ---

func step(format string, a ...any) { fmt.Fprintf(stdout, "→ "+format+"\n", a...) }
func ok(format string, a ...any)   { fmt.Fprintf(stdout, "✓ "+format+"\n", a...) }
func warn(format string, a ...any) { fmt.Fprintf(stdout, "! "+format+"\n", a...) }
func fail(format string, a ...any) { fmt.Fprintf(stdout, "✗ "+format+"\n", a...) }
func info(format string, a ...any) { fmt.Fprintf(stdout, "  "+format+"\n", a...) }

// in is the single buffered reader over stdin: one reader for the whole run,
// otherwise the first prompt would swallow the answers of the next ones.
var in *bufio.Reader

func readLine() string {
	if in == nil {
		in = bufio.NewReader(stdin)
	}
	line, _ := in.ReadString('\n')
	return strings.TrimSpace(line)
}

// confirm asks a yes/no question; assumeYes skips the prompt.
func confirm(question string, assumeYes bool) bool {
	if assumeYes {
		return true
	}
	fmt.Fprintf(stdout, "%s [o/N] ", question)
	line := strings.ToLower(readLine())
	return line == "o" || line == "oui" || line == "y" || line == "yes"
}

// ask reads a line with a default value.
func ask(question, def string, assumeYes bool) string {
	if assumeYes {
		return def
	}
	fmt.Fprintf(stdout, "%s [%s] ", question, def)
	line := readLine()
	if line == "" {
		return def
	}
	return line
}

// requireWritable explains how to fix permission errors on system paths.
func requireWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("impossible d'écrire dans %s : relancez avec sudo, ou choisissez un autre dossier avec --dir", dir)
		}
		return err
	}
	probe := filepath.Join(dir, ".observa-write-test")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return fmt.Errorf("impossible d'écrire dans %s : relancez avec sudo, ou choisissez un autre dossier avec --dir", dir)
	}
	return os.Remove(probe)
}

func sortedKeys[V any](m map[string]V) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	sort.Strings(k)
	return k
}

func runVersion(args []string) error {
	fmt.Fprintf(stdout, "observa %s\n\nImages :\n", version.Version)
	im := version.Images
	for _, l := range [][2]string{
		{"Grafana", im.Grafana}, {"Prometheus", im.Prometheus}, {"Alertmanager", im.Alertmanager},
		{"Loki", im.Loki}, {"Tempo", im.Tempo}, {"Caddy", im.Caddy}, {"Alloy (agent)", im.Alloy},
	} {
		fmt.Fprintf(stdout, "  %-14s %s\n", l[0], l[1])
	}
	return nil
}
