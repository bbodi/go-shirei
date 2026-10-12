//go:build darwin && !ios

#include "textflag.h"

// func objcCall(id, sel, a0, a1, a2, a3, a4 uintptr) uintptr
TEXT ·objcCall(SB), NOSPLIT, $32-64
	STP (R27, g), 8(RSP) // REGTMP + g
	MOVD R30, 24(RSP)
	MOVD id+0(FP), R0
	MOVD sel+8(FP), R1
	MOVD a0+16(FP), R2
	MOVD a1+24(FP), R3
	MOVD a2+32(FP), R4
	MOVD a3+40(FP), R5
	MOVD a4+48(FP), R6
	MOVD ·objcMsgSend(SB), R8
	CALL R8
	LDP 8(RSP), (R27, g)
	MOVD 24(RSP), R30
	MOVD R0, ret+56(FP)
	RET
