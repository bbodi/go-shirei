//go:build darwin && !ios

#include "textflag.h"

// func objcCall(id, sel, a0, a1, a2, a3, a4 uintptr) uintptr
TEXT ·objcCall(SB), NOSPLIT, $24-64
	MOVQ R14, 16(SP) // g
	MOVQ id+0(FP), DI
	MOVQ sel+8(FP), SI
	MOVQ a0+16(FP), DX
	MOVQ a1+24(FP), CX
	MOVQ a2+32(FP), R8
	MOVQ a3+40(FP), R9
	MOVQ a4+48(FP), AX
	MOVQ AX, 0(SP)
	MOVQ ·objcMsgSend(SB), AX
	CALL AX
	MOVQ 16(SP), R14
	MOVQ AX, ret+56(FP)
	RET
