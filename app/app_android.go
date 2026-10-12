//go:build android

package app

import (
	"image"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/androidbackend"
	"go.hasen.dev/shirei/internal/iconimg"
)

// SetupWindow records the window's title and preferred size. On Android the
// surface is always full-screen; size is accepted for API parity with desktop.
func SetupWindow(title string, width, height int) {
	shirei.GetHost().WindowSize = shirei.Vec2{float32(width), float32(height)}
	androidbackend.SetupWindow(title, width, height)
}

// SetupQuiet is a no-op on Android.
func SetupQuiet() {}

// SetupIcon is a no-op on Android for the spike (the launcher icon comes from
// the APK's resources, assembled by shirei_mobilerun / shirei_bundle).
func SetupIcon(imagePath string) {}

// SetupIconImage is a no-op on Android for the spike.
func SetupIconImage(img image.Image) { _ = img }

// SetupIconBytes is a no-op on Android for the spike.
func SetupIconBytes(data []byte) {
	_ = iconimg.DecodeBytes(data)
}

// Run enters the NativeActivity frame loop and never returns. Activity destroy
// calls generic.ExitWithCleanup. Must be called on the glue's app thread — the
// packaging tool's export file arranges for main() to run there
// (android_main → shirei_android_main → main).
func Run(frameFn shirei.FrameFn) {
	androidbackend.Run(frameFn)
}
