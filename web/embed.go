// Package web holds the templates and static files, compiled into the binary
// so it runs from any directory and its markup and styles always come from
// the same build.
package web

import "embed"

//go:embed templates/*.gohtml
var Templates embed.FS

//go:embed public
var Public embed.FS
