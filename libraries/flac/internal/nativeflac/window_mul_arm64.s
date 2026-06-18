//go:build arm64 && !flac_strict

#include "textflag.h"

// func windowDataMulNEON(in *int32, window *float32, out *float32, n int) int
//
// arm64 NEON kernel for the LPCWindowData multiply (lpc.c:68): for each i,
//
//     out[i] = (float32)in[i] * window[i]
//
// Each group of four lanes converts int32->float32 (SCVTF.4S) and applies a
// single per-lane FMUL.4S — a plain multiply, never a fused multiply-add, so
// every lane is one IEEE-754 round-to-nearest-even float32 product. This is
// the DEFAULT build only (//go:build arm64 && !flac_strict); the strict build
// keeps the //go:noinline f32mul scalar path so the -ffp-contract=off oracle
// parity stays bit-exact. The window multiply has no a*b+c to fuse, so the
// default vector path is within the same PSNR-noise band the default scalar
// path already accepts.
//
// Returns the number of samples consumed (a multiple of 4). The Go caller
// computes the [returned, n) tail with the scalar f32mul path.
//
// Why raw WORD encodings: Go 1.26's arm64 assembler does not expose LDR Q,
// SCVTF.4S, FMUL.4S, or STR Q vector mnemonics. The encodings below were
// produced by Apple clang -O2 -ffp-contract=off -fno-vectorize
// -fno-slp-vectorize from the equivalent vld1q_s32/vcvtq_f32_s32/vld1q_f32/
// vmulq_f32/vst1q_f32 listing; each is annotated with its disassembly.
//
//   R0 in, R1 window, R2 out, R3 n; R4 i; R5 n-3 limit
//   V0 in lanes / result; V1 window lanes
TEXT ·windowDataMulNEON(SB), NOSPLIT, $0-40
	MOVD in+0(FP), R0
	MOVD window+8(FP), R1
	MOVD out+16(FP), R2
	MOVD n+24(FP), R3

	MOVD $0, R4            // i = 0
	SUB  $3, R3, R5        // R5 = n-3

loop:
	CMP  R5, R4
	BGE  done             // while i < n-3 (i.e. i+4 <= n)

	WORD $0x3cc10400      // ldr   q0, [x0], #16   ; V0 = in[i..i+3];     x0+=16
	WORD $0x3cc10421      // ldr   q1, [x1], #16   ; V1 = window[i..i+3]; x1+=16
	WORD $0x4e21d800      // scvtf.4s v0, v0       ; V0 = (float32)V0
	WORD $0x6e20dc20      // fmul.4s  v0, v1, v0    ; V0 = V1 * V0 (per-lane round)
	WORD $0x3c810440      // str   q0, [x2], #16   ; out[i..i+3] = V0;    x2+=16

	ADD  $4, R4, R4
	B    loop

done:
	MOVD R4, ret+32(FP)
	RET
