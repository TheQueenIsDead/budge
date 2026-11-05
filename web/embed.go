// Package web holds the templates and static files, compiled into the binary
// so it runs from any directory and its markup and styles always come from
// the same build.
package web

import "embed"

//go:embed templates/*.gohtml
var Templates embed.FS

//go:embed public
var Public embed.FS

// Libraries are the files the pages load from the libraries in package.json,
// embedded straight from node_modules. pnpm install must run first; see
// pnpm-workspace.yaml. A stylesheet's fonts are embedded beside it.
//
//go:embed node_modules/bootstrap/dist/css/bootstrap.min.css
//go:embed node_modules/bootstrap/dist/js/bootstrap.bundle.min.js
//go:embed node_modules/bootstrap-icons/font/bootstrap-icons.min.css
//go:embed node_modules/bootstrap-icons/font/fonts
//go:embed node_modules/@fontsource-variable/inter/wght.css
//go:embed node_modules/@fontsource-variable/inter/files/*-wght-normal.woff2
//go:embed node_modules/chart.js/dist/chart.umd.min.js
//go:embed node_modules/htmx.org/dist/htmx.min.js
//go:embed node_modules/alpinejs/dist/cdn.min.js
//go:embed node_modules/@alpinejs/morph/dist/cdn.min.js
var Libraries embed.FS
