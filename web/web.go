// Package web holds the templates and static files, embedded in the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates
var Templates embed.FS

//go:embed static
var static embed.FS

// Static is served under /static/.
var Static fs.FS = static
