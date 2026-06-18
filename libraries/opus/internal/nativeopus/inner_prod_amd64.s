//go:build amd64 && !opus_nosimd && !opus_strict

#include "textflag.h"

// Shared amd64 SSE kernels for:
//   celt_inner_prod(x, y, N)            — single dot product
//   dual_inner_prod(x, y01, y02, N, …)  — two dot products sharing x
//
// Mirrors libopus/celt/x86/pitch_sse.c:
//   - 4-lane float32 accumulator, 4-element stride
//   - Separate-round MULPS then ADDPS (SSE baseline has no packed FMA;
//     FMA3 is explicitly avoided to keep this a baseline SSE2 path).
//   - Horizontal reduce: ADDPS with MOVHLPS, then scalar add of lanes
//     low..hi (matches pitch_sse.c's movehl+shuffle pattern).
//   - Scalar tail loop for N%4 remainder.
//
// NOT bit-exact with the scalar Go path (4-lane tree reduction differs
// in rounding order from left-to-right scalar summation). Excluded
// under -tags=opus_strict; scalar Go fallback in pitch.go handles
// strict/nosimd builds.

// func celtInnerProdSIMD(x, y *float32, N int) float32
TEXT ·celtInnerProdSIMD(SB), NOSPLIT, $0-32
	MOVQ x+0(FP), DI
	MOVQ y+8(FP), SI
	MOVQ N+16(FP), CX

	XORPS X0, X0                // acc = 0.0 x 4

	MOVQ CX, AX
	SHRQ $2, AX                 // AX = N / 4 (full 4-blocks)
	TESTQ AX, AX
	JE   ip_scalar

ip_loop4:
	MOVUPS (DI), X1             // X1 = x[i..i+3]
	MOVUPS (SI), X2             // X2 = y[i..i+3]
	MULPS  X2, X1               // X1 = X1 * X2 (lane-wise)
	ADDPS  X1, X0               // acc += X1
	ADDQ   $16, DI
	ADDQ   $16, SI
	DECQ   AX
	JNE    ip_loop4

ip_scalar:
	// Horizontal reduce X0.{0,1,2,3} into X0.low.
	MOVHLPS X0, X1              // X1.low = X0.high (lanes 2,3)
	ADDPS   X1, X0              // X0.{0,1} = {lane0+lane2, lane1+lane3}
	MOVAPS  X0, X1
	SHUFPS  $0x55, X0, X1       // X1.low = X0.lane1
	ADDSS   X1, X0              // X0.low += X1.low

	// Scalar tail for remaining N%4 elements.
	ANDQ $3, CX
	JE   ip_done
ip_tail:
	MOVSS (DI), X1
	MULSS (SI), X1
	ADDSS X1, X0
	ADDQ  $4, DI
	ADDQ  $4, SI
	DECQ  CX
	JNE   ip_tail

ip_done:
	MOVSS X0, ret+24(FP)
	RET

// func dualInnerProdSIMD(x, y01, y02 *float32, N int, xy1, xy2 *float32)
TEXT ·dualInnerProdSIMD(SB), NOSPLIT, $0-48
	MOVQ x+0(FP), DI
	MOVQ y01+8(FP), SI
	MOVQ y02+16(FP), R8
	MOVQ N+24(FP), CX

	XORPS X0, X0                // acc1 = 0
	XORPS X1, X1                // acc2 = 0

	MOVQ CX, AX
	SHRQ $2, AX
	TESTQ AX, AX
	JE   dp_scalar

dp_loop4:
	MOVUPS (DI), X2             // X2 = x[i..i+3]
	MOVUPS (SI), X3             // X3 = y01[i..i+3]
	MOVUPS (R8), X4             // X4 = y02[i..i+3]
	MULPS  X2, X3               // X3 = x * y01
	MULPS  X2, X4               // X4 = x * y02
	ADDPS  X3, X0               // acc1 += X3
	ADDPS  X4, X1               // acc2 += X4
	ADDQ   $16, DI
	ADDQ   $16, SI
	ADDQ   $16, R8
	DECQ   AX
	JNE    dp_loop4

dp_scalar:
	// Horizontal reduce acc1 → X0.low.
	MOVHLPS X0, X2
	ADDPS   X2, X0
	MOVAPS  X0, X2
	SHUFPS  $0x55, X0, X2
	ADDSS   X2, X0
	// Horizontal reduce acc2 → X1.low.
	MOVHLPS X1, X3
	ADDPS   X3, X1
	MOVAPS  X1, X3
	SHUFPS  $0x55, X1, X3
	ADDSS   X3, X1

	ANDQ $3, CX
	JE   dp_done
dp_tail:
	MOVSS (DI), X2
	MOVSS (SI), X3
	MOVSS (R8), X4
	MULSS X2, X3
	MULSS X2, X4
	ADDSS X3, X0
	ADDSS X4, X1
	ADDQ  $4, DI
	ADDQ  $4, SI
	ADDQ  $4, R8
	DECQ  CX
	JNE   dp_tail

dp_done:
	MOVQ xy1+32(FP), AX
	MOVQ xy2+40(FP), DX
	MOVSS X0, (AX)
	MOVSS X1, (DX)
	RET
