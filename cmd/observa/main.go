// Command observa installs and operates an ObservaBoard observability stack.
package main

import (
	"os"

	"github.com/root-lr/observaboard/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
