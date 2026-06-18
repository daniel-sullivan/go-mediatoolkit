//go:build arm64 && !opus_nosimd

#include "textflag.h"

// func xcorrKernelSIMD(x, y *float32, sum *[4]float32, ln int)
//
// arm64 NEON implementation of the xcorr_kernel_c main MAC body.
// For each i in 0..ln-1, computes:
//
//     sum[k] += x[i] * y[i+k]   for k = 0..3
//
// Each 4-way MAC is a separate-round FMUL.4S then FADD.4S, never a
// fused FMLA. Per-lane rounding is identical to four scalar FMUL+FADD
// pairs under IEEE 754 round-to-nearest-even, so the output is
// bit-exact vs the scalar -ffp-contract=off Go path (and vs the C
// oracle compiled with -ffp-contract=off).
//
// Why raw WORD encodings: Go 1.26's arm64 assembler does not expose
// vector FMUL.4S / FADD.4S / LDR Q / LD1R.4S mnemonics. The encodings
// below were extracted from clang's output of a reference intrinsic
// (vmulq_f32 + vaddq_f32 version) compiled with -O2 -ffp-contract=off
// -fno-vectorize — see commit message / PR description for the
// reference .c file. FMLA (supported by Go asm) is deliberately NOT
// used because it changes the rounding contract.
//
// Preconditions:
//   - ln >= 0 (ln == 0 is a no-op)
//   - x has at least ln float32 elements addressable at *x
//   - y has at least ln+3 float32 elements addressable at *y
//   - sum points at 4 contiguous float32 accumulators

TEXT ·xcorrKernelSIMD(SB), NOSPLIT, $0-32
	MOVD x+0(FP), R0
	MOVD y+8(FP), R1
	MOVD sum+16(FP), R2
	MOVD ln+24(FP), R3

	CBZ  R3, done

	WORD $0x3DC00040          // LDR Q0, [X2]              ; V0 = sum[0..3]

loop:
	WORD $0x4DDFC801          // LD1R.4S {V1}, [X0], #4    ; V1 = x[i] broadcast; X0+=4
	WORD $0x3CC04422          // LDR Q2, [X1], #4          ; V2 = y[i..i+3];       X1+=4
	WORD $0x6E21DC41          // FMUL.4S V1, V2, V1        ; V1 = V2 * V1 (round-per-lane)
	WORD $0x4E21D400          // FADD.4S V0, V0, V1        ; V0 = V0 + V1

	SUB  $1, R3, R3
	CBNZ R3, loop

	WORD $0x3D800040          // STR Q0, [X2]              ; sum[0..3] = V0
done:
	RET
