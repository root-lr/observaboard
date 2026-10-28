// Package assets embeds the configuration templates and static files that the
// observa binary writes to disk on the central server and on each agent host.
package assets

import "embed"

// FS holds every asset. Paths are relative to this directory.
//
//go:embed agent central runbooks
var FS embed.FS
