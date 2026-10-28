// Package dockerx runs docker and docker compose commands for the CLI.
package dockerx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DryRun makes every command print instead of run (set by --dry-run).
var DryRun bool

// Out is where command output and dry-run lines go.
var Out io.Writer = os.Stdout

// Run executes docker with the given arguments, streaming its output.
func Run(args ...string) error {
	if DryRun {
		fmt.Fprintln(Out, "  [dry-run] docker "+strings.Join(args, " "))
		return nil
	}
	cmd := exec.Command("docker", args...)
	cmd.Stdout = Out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s : %w", strings.Join(args, " "), err)
	}
	return nil
}

// Output executes docker and returns its standard output.
func Output(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

// Compose runs `docker compose` in dir.
func Compose(dir string, args ...string) error {
	return Run(append([]string{"compose", "--project-directory", dir, "-f", dir + "/docker-compose.yml"}, args...)...)
}

// ComposeOutput runs `docker compose` in dir and returns its output.
func ComposeOutput(dir string, args ...string) (string, error) {
	return Output(append([]string{"compose", "--project-directory", dir, "-f", dir + "/docker-compose.yml"}, args...)...)
}

// Available reports whether the docker CLI can reach a daemon.
func Available() error {
	if DryRun {
		return nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("la commande docker est introuvable : installez Docker Engine (Linux) ou Docker Desktop (Mac, Windows)")
	}
	if _, err := Output("version", "--format", "{{.Server.Version}}"); err != nil {
		return fmt.Errorf("le démon Docker ne répond pas (%v) ; est-il démarré, et votre utilisateur a-t-il accès au socket ?", err)
	}
	if _, err := Output("compose", "version"); err != nil {
		return errors.New("le plugin docker compose v2 est requis (docker compose version)")
	}
	return nil
}

// EnsureNetwork creates a bridge network if it does not exist yet.
func EnsureNetwork(name string) error {
	if DryRun {
		fmt.Fprintln(Out, "  [dry-run] docker network create "+name+" (si absent)")
		return nil
	}
	if _, err := Output("network", "inspect", name); err == nil {
		return nil
	}
	_, err := Output("network", "create", "--driver", "bridge", name)
	if err != nil {
		return fmt.Errorf("création du réseau %s : %w", name, err)
	}
	return nil
}

// IsDesktop reports whether the daemon is Docker Desktop (Mac/Windows VM).
func IsDesktop() bool {
	if DryRun {
		return false
	}
	out, err := Output("info", "--format", "{{.OperatingSystem}}")
	if err != nil {
		return false
	}
	return strings.Contains(out, "Docker Desktop") || strings.Contains(strings.ToLower(out), "orbstack")
}
