//go:build linux || (darwin && x11darwin)

package x11backend

import "go.hasen.dev/shirei"

func perfLog(format string, args ...any) { shirei.PerfLog(format, args...) }
