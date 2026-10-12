package main

import (
	_ "embed"
)

// Dock icon (disk-usage platter). Embedded so `go run` works from any directory.
//
//go:embed icon.png
var iconPNG []byte
