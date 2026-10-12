//go:build linux

package waylandbackend

import "go.hasen.dev/shirei"

func perfLog(format string, args ...any) { shirei.PerfLog(format, args...) }
