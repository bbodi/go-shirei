//go:build darwin && !ios

package gpurender

// objcMsgSend is the C function pointer for objc_msgSend, filled in bindMetal.
var objcMsgSend uintptr

// objcCall is a direct BLR/CALL to objc_msgSend with no runtime.cgocall.
// Implemented in metal_call_*.s. Extra args unused by the IMP are ignored.
func objcCall(id, sel, a0, a1, a2, a3, a4 uintptr) uintptr
