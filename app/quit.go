package app

import g "go.hasen.dev/generic"

// Quit ends the process. Safe to call from any goroutine. Run does not return.
// AddExitCleanup handlers run first.
func Quit() {
	g.ExitWithCleanup(0)
}
