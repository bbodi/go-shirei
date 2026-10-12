//go:build js

package app

import (
	"image"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/jsbackend"
)

// SetupWindow records the document title and preferred CSS-pixel content size.
// Call it before Run. On a top-level desktop-sized page the floating shell grows
// by the titlebar so the app body keeps that size; iframe embeds stay exact-fit.
// Mobile hosts and short/narrow host slots fill #shirei-root (no CSD). Pass 0,0
// to fill the host slot instead.
func SetupWindow(title string, width, height int) {
	shirei.GetHost().WindowSize = shirei.Vec2{float32(width), float32(height)}
	jsbackend.SetupWindow(title, width, height)
}

// SetupQuiet is a no-op on the web.
func SetupQuiet() {}

// SetupIcon is a no-op on the web for the first cut (use a favicon in HTML).
func SetupIcon(imagePath string) {
	jsbackend.SetupIcon(imagePath)
}

// SetupIconImage is a no-op on the web for the first cut.
func SetupIconImage(img image.Image) {
	jsbackend.SetupIconImage(img)
}

// SetupIconBytes is a no-op on the web for the first cut.
func SetupIconBytes(data []byte) {
	jsbackend.SetupIconBytes(data)
}

// Run attaches to the page canvas and drives frames via requestAnimationFrame.
// It never returns; pagehide (not bfcache) calls generic.ExitWithCleanup.
func Run(frameFn shirei.FrameFn) {
	jsbackend.Run(frameFn)
}
