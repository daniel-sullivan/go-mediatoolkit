//go:build arm64

#include "textflag.h"

// func fixedAbsErrors4NEON(q0, q1, q2, q3 *int32, prevErr *[16]int32, iters int, totals *[5]uint32)
//
// arm64 NEON port of the vector loop of
// FLAC__fixed_compute_best_predictor_intrin_sse2 (fixed_intrin_sse2.c).
// Computes the five fixed-order absolute-error sums over four
// independent quarters of the signal in parallel, exactly mirroring the
// SSE2 quarter-split cascade.
//
// Each lane k (0..3) walks one quarter: q0..q3 point at the first signal
// sample of quarters 0..3 respectively, and each is advanced one int32
// per iteration (iters total). prevErr holds the four pre-seeded
// per-quarter lag vectors prev_err0..prev_err3 laid out SoA: prevErr
// [0..3]=prev_err0, [4..7]=prev_err1, [8..11]=prev_err2,
// [12..15]=prev_err3 — matching the C _mm_loadu_si128 of prev_errN_scalar[].
//
// Per iteration (identical to the SSE2 body):
//   e0 = data;  total0 += |e0|; e1 = e0 - prev0; prev0 = e0
//   e1;         total1 += |e1|; e2 = e1 - prev1; prev1 = e1
//   e2;         total2 += |e2|; e3 = e2 - prev2; prev2 = e2
//   e3;         total3 += |e3|; e4 = e3 - prev3; prev3 = e3
//   e4;         total4 += |e4|
//
// All arithmetic is int32 two's-complement (vector SUB/ABS/ADD); the
// four-lane partial sums wrap identically to the scalar uint32
// accumulators, and the final per-total horizontal sum (ADDV, also
// int32-wrapping) is associative, so the result is bit-exact vs the
// scalar Go path. Integer-exact -> built in BOTH default and flac_strict.
//
// The data_len%4 remainder and prev_err seeding are handled by the Go
// caller (fixed_encode.go), matching the C scalar tail.
//
// Why raw WORD encodings: Go 1.26's arm64 assembler does not expose the
// vector mnemonics used here (MOVI.4S, LDR Q, LD1 lane, ABS.4S, ADD.4S,
// SUB.4S, MOV.16B, ADDV.4S, FMOV w<-s). Each was produced by Apple
// clang's assembler and is annotated with its disassembly.
//
// Register map:
//   R0..R3 quarter pointers q0..q3; R4 prevErr; R5 iters; R6 totals
//   V0 gathered current samples (lane per quarter); V5..V13 scratch
//   V16..V20 total0..total4 accumulators
//   V21..V24 prev_err0..prev_err3 (running, one lane per quarter)
TEXT ·fixedAbsErrors4NEON(SB), NOSPLIT, $0-56
	MOVD q0+0(FP), R0
	MOVD q1+8(FP), R1
	MOVD q2+16(FP), R2
	MOVD q3+24(FP), R3
	MOVD prevErr+32(FP), R4
	MOVD iters+40(FP), R5
	MOVD totals+48(FP), R6

	WORD $0x4f000410         // movi.4s v16, #0      ; total0
	WORD $0x4f000411         // movi.4s v17, #0      ; total1
	WORD $0x4f000412         // movi.4s v18, #0      ; total2
	WORD $0x4f000413         // movi.4s v19, #0      ; total3
	WORD $0x4f000414         // movi.4s v20, #0      ; total4

	WORD $0x3dc00095         // ldr q21, [x4]        ; prev_err0
	WORD $0x3dc00496         // ldr q22, [x4, #16]   ; prev_err1
	WORD $0x3dc00897         // ldr q23, [x4, #32]   ; prev_err2
	WORD $0x3dc00c98         // ldr q24, [x4, #48]   ; prev_err3

	CBZ  R5, reduce

loop:
	// Gather four quarter samples into V0 lanes 0..3, advancing each
	// quarter pointer by one int32.
	WORD $0x0ddf8000         // ld1.s {v0}[0], [x0], #4
	WORD $0x0ddf9020         // ld1.s {v0}[1], [x1], #4
	WORD $0x4ddf8040         // ld1.s {v0}[2], [x2], #4
	WORD $0x4ddf9060         // ld1.s {v0}[3], [x3], #4

	// order 0
	WORD $0x4ea0b805         // abs.4s  v5, v0
	WORD $0x4ea58610         // add.4s  v16, v16, v5
	WORD $0x6eb58406         // sub.4s  v6, v0, v21    ; e1 = e0 - prev0
	WORD $0x4ea01c15         // mov.16b v21, v0        ; prev0 = e0

	// order 1
	WORD $0x4ea0b8c7         // abs.4s  v7, v6
	WORD $0x4ea78631         // add.4s  v17, v17, v7
	WORD $0x6eb684c8         // sub.4s  v8, v6, v22    ; e2 = e1 - prev1
	WORD $0x4ea61cd6         // mov.16b v22, v6        ; prev1 = e1

	// order 2
	WORD $0x4ea0b909         // abs.4s  v9, v8
	WORD $0x4ea98652         // add.4s  v18, v18, v9
	WORD $0x6eb7850a         // sub.4s  v10, v8, v23   ; e3 = e2 - prev2
	WORD $0x4ea81d17         // mov.16b v23, v8        ; prev2 = e2

	// order 3
	WORD $0x4ea0b94b         // abs.4s  v11, v10
	WORD $0x4eab8673         // add.4s  v19, v19, v11
	WORD $0x6eb8854c         // sub.4s  v12, v10, v24  ; e4 = e3 - prev3
	WORD $0x4eaa1d58         // mov.16b v24, v10       ; prev3 = e3

	// order 4
	WORD $0x4ea0b98d         // abs.4s  v13, v12
	WORD $0x4ead8694         // add.4s  v20, v20, v13

	SUB  $1, R5, R5
	CBNZ R5, loop

reduce:
	WORD $0x4eb1ba10         // addv.4s s16, v16
	WORD $0x4eb1ba31         // addv.4s s17, v17
	WORD $0x4eb1ba52         // addv.4s s18, v18
	WORD $0x4eb1ba73         // addv.4s s19, v19
	WORD $0x4eb1ba94         // addv.4s s20, v20
	WORD $0x1e260214         // fmov   w20, s16
	WORD $0x1e260235         // fmov   w21, s17
	WORD $0x1e260256         // fmov   w22, s18
	WORD $0x1e260277         // fmov   w23, s19
	WORD $0x1e260298         // fmov   w24, s20
	MOVW R20, 0(R6)          // totals[0]
	MOVW R21, 4(R6)          // totals[1]
	MOVW R22, 8(R6)          // totals[2]
	MOVW R23, 12(R6)         // totals[3]
	MOVW R24, 16(R6)         // totals[4]
	RET
