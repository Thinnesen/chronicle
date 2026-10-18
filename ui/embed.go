//go:build ui

// Package ui holds the built web UI. Build the server with "-tags ui" after "pnpm build".
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Files returns the built UI, or nil when the binary has no UI.
func Files() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
