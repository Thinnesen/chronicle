//go:build !ui

// Package ui holds the built web UI. Build the server with "-tags ui" after "pnpm build".
package ui

import "io/fs"

// Files returns the built UI, or nil when the binary has no UI.
func Files() fs.FS { return nil }
