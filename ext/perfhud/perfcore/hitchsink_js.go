//go:build js

package perfcore

// The browser build has no filesystem to write hitch logs and traces to. The
// overlay and the hitch detector still run; only the on-disk artefacts are
// skipped.

func startRecorder()                         {}
func dumpTrace(logPath string) string        { return "" }
func writeRecord(logPath string, rec Record) {}
