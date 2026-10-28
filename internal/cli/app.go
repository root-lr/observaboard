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
		{name: "up", summary: "Run central + agent on this machine (--all-in-one)", run: runUp},
		{name: "down", summary: "Stop the all-in-one install", run: runDown},
		{name: "central", summary: "Install and manage the central server", sub: []command{
			{name: "install", summary: "Install the central (Grafana, Prometheus, Loki, Tempo, Alertmanager, Caddy)", run: runCentralInstall},
			{name: "reload", summary: "Regenerate the config from central.yaml and reload", run: runCentralReload},
			{name: "upgrade", summary: "Update images, keeping data and config", run: runCentralUpgrade},
			{name: "status", summary: "Show the status of central components", run: runCentralStatus},
			{name: "ca", summary: "Print the internal CA certificate (--ip mode)", run: runCentralCA},
		}},
		{name: "token", summary: "Manage agent tokens (on the central)", sub: []command{
			{name: "create", summary: "Create a token for an agent: observa token create <agent>", run: runTokenCreate},
			{name: "list", summary: "List tokens", run: runTokenList},
			{name: "revoke", summary: "Revoke an agent's token: observa token revoke <agent>", run: runTokenRevoke},
		}},
		{name: "agent", summary: "Install and manage a server's agent", sub: []command{
			{name: "install", summary: "Install the agent: --central <url> --token <token>", run: runAgentInstall},
			{name: "reload", summary: "Regenerate the agent config and reload it", run: runAgentReload},
			{name: "status", summary: "Show agent status and discovered services", run: runAgentStatus},
			{name: "env", summary: "Manage agent secrets: observa agent env set NAME=value", run: runAgentEnv},
		}},
		{name: "init", summary: "Detect a project's stack and generate observa.yaml + the override", run: runInit},
		{name: "validate", summary: "Validate observa.yaml", run: runValidate},
		{name: "apply", summary: "Register the project with the local agent (thresholds, routing, exporters)", run: runApply},
		{name: "attach", summary: "Monitor an existing container without labels: observa attach <container>", run: runAttach},
		{name: "detach", summary: "Remove an observa attach rule: observa detach <container>", run: runDetach},
		{name: "preset", summary: "List or describe presets", sub: []command{
			{name: "list", summary: "List presets", run: runPresetList},
			{name: "show", summary: "Describe a preset: observa preset show <name>", run: runPresetShow},
		}},
		{name: "doctor", summary: "Diagnose the install (agent, central, project)", run: runDoctor},
		{name: "alert", summary: "Alerting tools", sub: []command{
			{name: "test", summary: "Send a test alert to check notifications", run: runAlertTest},
		}},
		{name: "version", summary: "Print the version and the images in use", run: runVersion},
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
	return usagef("unknown command: %s %s", prefix, args[0])
}

func printHelp(cmds []command, prefix string) {
	if prefix == "observa" {
		fmt.Fprintf(stdout, "ObservaBoard %s — self-hosted observability in one command.\n\n", version.Version)
	}
	fmt.Fprintf(stdout, "Usage: %s <command> [options]\n\nCommands:\n", prefix)
	for _, c := range cmds {
		fmt.Fprintf(stdout, "  %-10s %s\n", c.name, c.summary)
	}
	fmt.Fprintf(stdout, "\nCommand help: %s <command> --help · Global option: --dry-run (print docker commands without running them)\n", prefix)
}

// newFlags creates a FlagSet with a custom usage output.
func newFlags(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s\n\nOptions:\n", usage)
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
	fmt.Fprintf(stdout, "%s [y/N] ", question)
	line := strings.ToLower(readLine())
	return line == "y" || line == "yes"
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
			return fmt.Errorf("cannot write to %s: rerun with sudo, or choose another directory with --dir", dir)
		}
		return err
	}
	probe := filepath.Join(dir, ".observa-write-test")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return fmt.Errorf("cannot write to %s: rerun with sudo, or choose another directory with --dir", dir)
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
	fmt.Fprintf(stdout, "observa %s\n\nImages:\n", version.Version)
	im := version.Images
	for _, l := range [][2]string{
		{"Grafana", im.Grafana}, {"Prometheus", im.Prometheus}, {"Alertmanager", im.Alertmanager},
		{"Loki", im.Loki}, {"Tempo", im.Tempo}, {"Caddy", im.Caddy}, {"Alloy (agent)", im.Alloy},
	} {
		fmt.Fprintf(stdout, "  %-14s %s\n", l[0], l[1])
	}
	return nil
}
