#include "textflag.h"

TEXT ·accessPointThunk(SB),NOSPLIT|NOFRAME,$0
 MOVQ X1, DX
 MOVQ X2, R8
 MOVQ ·accessPointCallback(SB), AX
 JMP AX

TEXT ·accessValueThunk(SB),NOSPLIT|NOFRAME,$0
 MOVQ X1, DX
 MOVQ ·accessValueCallback(SB), AX
 JMP AX

TEXT ·accessPointThunkAddr(SB),NOSPLIT,$0-8
 LEAQ ·accessPointThunk(SB), AX
 MOVQ AX, ret+0(FP)
 RET

TEXT ·accessValueThunkAddr(SB),NOSPLIT,$0-8
 LEAQ ·accessValueThunk(SB), AX
 MOVQ AX, ret+0(FP)
 RET
