//go:build darwin || linux || android || windows || js

// Package gpurender composites a shirei surface list on the GPU.
//
// Cocoa hosts it: Metal writes the existing IOSurface pool, then the shell
// presents with CALayer.contents as before. Cocoa uses this path unless
// SHIREI_GPU=0 or Metal init fails. macOS Metal is purego (no cgo).
//
// iOS hosts it: Metal encodes into a CAMetalLayer drawable on the content
// view. iOS uses this path unless Metal init fails.
//
// Wayland hosts it: GLES writes a GBM/dmabuf, then the shell attaches that
// wl_buffer. Wayland uses this path unless SHIREI_GPU=0 or GLES/dmabuf init
// fails; wl_shm + SoftRenderer is the fallback.
//
// Android hosts it: GLES encodes into an EGL window surface on the
// ANativeWindow. Android uses this path unless SHIREI_GPU=0 or EGL init
// fails; ANativeWindow_lock + SoftRenderer is the fallback.
//
// Windows hosts it: D3D11 writes a GDI-compatible DXGI texture; the shell
// BitBlts via IDXGISurface1::GetDC (same present as the software DIB).
// Windows uses this path unless SHIREI_GPU=0 or D3D11/GDI-interop init
// fails; DIB + SoftRenderer is the fallback.
//
// Web hosts it: GLES 3.00 / WebGL2 encodes into the canvas default
// framebuffer. The shell uses this path unless SHIREI_GPU=0 or WebGL2
// init fails; 2d canvas + SoftRenderer is the fallback.
//
// SoftRenderer is the fallback and the snapshot/headless oracle.
//
// Draws fills, gradients, borders, glyphs, images, and rounded clips.
// Rounded fills/borders use CPU corner-mask stamps; clip uses an SDF.
package gpurender
