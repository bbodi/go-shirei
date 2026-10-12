//go:build (!darwin && !windows && !linux && !js && !android) || (darwin && !ios && x11darwin) || (ios && !cgo) || (android && !cgo)

package darkmode

func initPlatform() {
	// Fallback stub for unsupported platforms or headless / non-cgo mobile builds
}
