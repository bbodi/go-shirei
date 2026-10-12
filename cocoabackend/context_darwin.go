//go:build darwin && !ios

package cocoabackend

import (
	"unsafe"

	"go.hasen.dev/shirei"
)

// Context is the BackendContext for the macOS (AppKit) backend.
// The backend sets Host.EscapeHatchBackendContext to this value at Run.
type Context struct{}

// Platform implements shirei.BackendContext.
func (Context) Platform() string { return "darwin" }

// NSWindow returns the live NSWindow as an opaque pointer, or nil if the host
// has not created a window yet. Cast with objc / AppKit (NSWindow *).
func (Context) NSWindow() unsafe.Pointer {
	return nsWindowPtr()
}

var _ shirei.BackendContext = Context{}
