//go:build amd64 && !opus_nosimd

#include "textflag.h"

// func xcorrKernelSIMD(x, y *float32, sum *[4]float32, ln int)
//
// amd64 SSE implementation of the xcorr_kernel_c main MAC body.
// For each i in 0..ln-1, computes:
//
//     sum[k] += x[i] * y[i+k]   for k = 0..3
//
// Uses separate-round MULPS then ADDPS (never a single fused op, and
// the x86 SSE path has no packed FMA in the SSE ISA anyway — FMA3
// would require an opt-in build and is deliberately avoided for
// parity). Per-lane rounding is identical to four scalar FMUL+FADD
// pairs under IEEE 754 round-to-nearest-even, so the output is
// bit-exact vs the scalar -ffp-contract=off Go path.
//
// Preconditions:
//   - ln >= 0 (ln == 0 is a no-op)
//   - x has at least ln float32 elements addressable at *x
//   - y has at least ln+3 float32 elements addressable at *y
//   - sum points at 4 contiguous float32 accumulators

TEXT ·xcorrKernelSIMD(SB), NOSPLIT, $0-32
	MOVQ x+0(FP), DI
	MOVQ y+8(FP), SI
	MOVQ sum+16(FP), DX
	MOVQ ln+24(FP), CX

	TESTQ CX, CX
	JLE   done

	MOVUPS (DX), X0             // X0 = sum[0..3]

loop:
	MOVSS  (DI), X1             // X1 low = x[i], upper lanes zeroed
	SHUFPS $0, X1, X1           // broadcast X1.lane0 to all 4 lanes
	MOVUPS (SI), X2             // X2 = y[i..i+3]
	MULPS  X1, X2               // X2 = X2 * X1 (lane-wise, separate round)
	ADDPS  X2, X0               // X0 = X0 + X2

	ADDQ $4, DI                 // x++
	ADDQ $4, SI                 // y++ (window slides by 1 float)
	DECQ CX
	JNE  loop

	MOVUPS X0, (DX)             // sum[0..3] = X0
done:
	RET
