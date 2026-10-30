// Package web embeds the site templates and static assets (CSS, JS).
package web

import "embed"

//go:embed templates static
var FS embed.FS
