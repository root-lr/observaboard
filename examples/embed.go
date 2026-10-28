// Package examples embeds the example applications used by
// `observa up --all-in-one --with-example`.
package examples

import "embed"

// FS contains the example applications.
//
//go:embed all:django-shop
var FS embed.FS
