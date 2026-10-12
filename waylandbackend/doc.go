// Package waylandbackend is shirei's native Wayland shell: it owns the window and
// input and presents a BGRA buffer the compositor can scan out. GLES
// (shirei/gpurender) writes a dmabuf and is the default compositor unless
// SHIREI_GPU=0 or init fails; wl_shm + SoftRenderer is the fallback.
//
// The Wayland protocol is spoken in pure Go via github.com/neurlang/wayland
// (the maintained successor to rajveermalviya/go-wayland), so the backend
// cross-compiles from any host with CGO disabled — except keyboard text, which
// needs libxkbcommon (added later). The real implementation is in the
// build-tagged *_linux.go files; on other platforms this is an empty package so
// `go build ./...` stays green.
package waylandbackend
