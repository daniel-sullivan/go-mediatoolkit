//go:build arm64 && !flac_strict

#include "textflag.h"

// func lpcAutocorrelationNEON(data *float32, dataLen int, autoc *float64)
//
// arm64 float64x2 NEON port of the autocorrelation reduction body of
// lpc_compute_autocorrelation_intrin_neon.c. Always computes the first 16
// lags (8 float64x2 accumulators sum0..sum7 → autoc[0..15]); the Go
// dispatcher buckets the requested lag to {8,12,16} and only reads back
// the meaningful prefix.
//
// For i from dataLen-1 down to 0:
//     d        = (float64)data[i] broadcast into both lanes
//     window   right-shifts: d_k = {d_{k-1}[1], d_k[0]} (ext #8) so the
//              stream slides through the d0..d7 stack
//     sum_k   += d * d_k          (fused fmla.2d — FMADDD per lane)
// then sum_k is stored to autoc[2k..2k+1].
//
// The 8 independent float64x2 accumulators break the single-accumulator
// FMADDD latency chain of the scalar path (multiple in-flight FMAs), and
// the fused multiply-add reassociates rounding relative to the scalar
// -ffp-contract=off oracle. This kernel is therefore FP-NON-bit-exact and
// is compiled into the DEFAULT build ONLY (//go:build !flac_strict); the
// flac_strict path uses the scalar f64-helper body in lpc_autoc_strict.go.
//
// Why raw WORD encodings: Go 1.26's arm64 assembler does not expose the
// vector mnemonics used here (FCVT s->d, DUP.2D lane, EXT.16b, FMLA.2D,
// MOVI.2D, LDR/STR Q). Each WORD is the instruction word emitted by Apple
// clang for the equivalent NEON listing (see /tmp scratch in PR notes) and
// is annotated with its disassembly.
//
// Register map (Plan9 / arm64):
//   R0 data; R1 dataLen; R3 autoc; R4 running &data[i]; R5 scratch
//   V8..V15  sum0..sum7 (accumulators)
//   V16..V23 d0..d7      (sliding window)
//   V24 d (data[i] bcast); V25 scratch f32->f64
TEXT ·lpcAutocorrelationNEON(SB), NOSPLIT, $0-24
	MOVD data+0(FP), R0
	MOVD dataLen+8(FP), R1
	MOVD autoc+16(FP), R3

	// Zero accumulators sum0..sum7 (V8..V15) and window d0..d7 (V16..V23).
	WORD $0x6f00e408 // movi.2d v8, #0
	WORD $0x6f00e409 // movi.2d v9, #0
	WORD $0x6f00e40a // movi.2d v10, #0
	WORD $0x6f00e40b // movi.2d v11, #0
	WORD $0x6f00e40c // movi.2d v12, #0
	WORD $0x6f00e40d // movi.2d v13, #0
	WORD $0x6f00e40e // movi.2d v14, #0
	WORD $0x6f00e40f // movi.2d v15, #0
	WORD $0x6f00e410 // movi.2d v16, #0
	WORD $0x6f00e411 // movi.2d v17, #0
	WORD $0x6f00e412 // movi.2d v18, #0
	WORD $0x6f00e413 // movi.2d v19, #0
	WORD $0x6f00e414 // movi.2d v20, #0
	WORD $0x6f00e415 // movi.2d v21, #0
	WORD $0x6f00e416 // movi.2d v22, #0
	WORD $0x6f00e417 // movi.2d v23, #0

	CBZ  R1, store          // dataLen == 0 → all sums stay zero

	// R4 = &data[dataLen-1] = data + (dataLen-1)*4
	SUB  $1, R1, R5         // R5 = dataLen-1
	LSL  $2, R5, R5         // R5 = (dataLen-1)*4
	ADD  R0, R5, R4         // R4 = &data[dataLen-1]

loop:
	WORD $0xbd400099 // ldr s25, [x4]        ; V25.s = data[i]
	WORD $0x1e22c339 // fcvt d25, s25        ; V25.d = (float64)data[i]
	WORD $0x4e080738 // dup.2d v24, v25[0]   ; V24 = {d, d}

	// Window right-shift (high→low) then push d into d0.
	WORD $0x6e1742d7 // ext.16b v23, v22, v23, #8
	WORD $0x6e1642b6 // ext.16b v22, v21, v22, #8
	WORD $0x6e154295 // ext.16b v21, v20, v21, #8
	WORD $0x6e144274 // ext.16b v20, v19, v20, #8
	WORD $0x6e134253 // ext.16b v19, v18, v19, #8
	WORD $0x6e124232 // ext.16b v18, v17, v18, #8
	WORD $0x6e114211 // ext.16b v17, v16, v17, #8
	WORD $0x6e104310 // ext.16b v16, v24, v16, #8

	// sum_k += d * d_k (fused).
	WORD $0x4e70cf08 // fmla.2d v8, v24, v16
	WORD $0x4e71cf09 // fmla.2d v9, v24, v17
	WORD $0x4e72cf0a // fmla.2d v10, v24, v18
	WORD $0x4e73cf0b // fmla.2d v11, v24, v19
	WORD $0x4e74cf0c // fmla.2d v12, v24, v20
	WORD $0x4e75cf0d // fmla.2d v13, v24, v21
	WORD $0x4e76cf0e // fmla.2d v14, v24, v22
	WORD $0x4e77cf0f // fmla.2d v15, v24, v23

	SUB  $4, R4, R4         // i-- (step back one float32)
	SUBS $1, R1, R1
	BNE  loop

store:
	WORD $0x3d800068 // str q8, [x3]
	WORD $0x3d800469 // str q9, [x3, #16]
	WORD $0x3d80086a // str q10, [x3, #32]
	WORD $0x3d800c6b // str q11, [x3, #48]
	WORD $0x3d80106c // str q12, [x3, #64]
	WORD $0x3d80146d // str q13, [x3, #80]
	WORD $0x3d80186e // str q14, [x3, #96]
	WORD $0x3d801c6f // str q15, [x3, #112]
	RET
