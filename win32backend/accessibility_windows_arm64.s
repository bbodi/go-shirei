#include "textflag.h"

TEXT ·accessPointThunk(SB),NOSPLIT|NOFRAME,$0
 MOVD R1, R3
 FMOVD F0, R1
 FMOVD F1, R2
 MOVD ·accessPointCallback(SB), R16
 JMP (R16)

TEXT ·accessValueThunk(SB),NOSPLIT|NOFRAME,$0
 FMOVD F0, R1
 MOVD ·accessValueCallback(SB), R16
 JMP (R16)

TEXT ·accessPointThunkAddr(SB),NOSPLIT,$0-8
 MOVD $·accessPointThunk(SB), R0
 MOVD R0, ret+0(FP)
 RET

TEXT ·accessValueThunkAddr(SB),NOSPLIT,$0-8
 MOVD $·accessValueThunk(SB), R0
 MOVD R0, ret+0(FP)
 RET
