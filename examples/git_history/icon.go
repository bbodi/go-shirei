package main

import (
	_ "embed"
)

// Dock icon (commit graph + diff). Embedded so `go run` works from any directory.
//
//go:embed icon.png
var iconPNG []byte
