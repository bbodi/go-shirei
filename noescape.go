package shirei

import "unsafe"

// noescape hides a pointer from the compiler's escape analysis. The returned
// pointer is p unchanged, but the compiler can no longer trace it back to the
// original variable, so passing it to an opaque callback does not force the
// pointee onto the heap. Same idiom as the runtime's internal/abi.NoEscape;
// //go:noescape is not an option because that directive only applies to
// functions without a Go body.
//
// The builders (Attrs, AttrsWith, TextStyleWith) use it so the struct under
// construction stays on the caller's stack while setter callbacks mutate it.
// CONTRACT: a setter (AttrsFn / TextStyleFn) must only read/write through its
// argument during the call — storing the pointer anywhere that outlives the
// call leaves a dangling stack pointer, with no compiler or race-detector
// diagnosis.
//
// go vet's unsafeptr check flags the conversion below. That is unavoidable:
// vet accepts exactly the uintptr arithmetic forms escape analysis can trace
// (+, -, &^), and tracing is what this function exists to defeat. The xor
// keeps the pointer value unchanged while staying untraceable.
//
//go:nosplit
//go:nocheckptr
func noescape(p unsafe.Pointer) unsafe.Pointer {
	x := uintptr(p)
	return unsafe.Pointer(x ^ 0)
}
