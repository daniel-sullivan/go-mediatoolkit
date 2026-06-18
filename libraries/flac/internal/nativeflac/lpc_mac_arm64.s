//go:build arm64

#include "textflag.h"

// func lpcResidualMACNEON(data *int32, dataLen int, qlpCoeff *int32, order int, lpQuantization int, residual *int32) int
//
// arm64 NEON port of the encoder MAC body of
// FLAC__lpc_compute_residual_from_qlp_coefficients_intrin_neon
// (lpc_intrin_neon.c). Computes the LPC residual four output samples at a
// time:
//
//     sum[k] = Σ_{j=0}^{order-1} qlpCoeff[j] * data[order+i+k - j - 1]
//     residual[i+k] = data[order+i+k] - (sum[k] >> lpQuantization)
//
// for k = 0..3, advancing i by 4. The accumulation is a chain of int32
// vector MUL/MLA followed by an arithmetic right shift (SSHL by a negative
// amount) and a vector SUB — every lane is two's-complement int32
// arithmetic, identical to the scalar Go path regardless of tap-addition
// order (int32 wrap is associative). Bit-exact, so it is enabled in BOTH
// the default and flac_strict builds; the strict parity gate verifies it.
//
// Returns the number of output samples consumed (always a multiple of 4).
// The Go caller computes the [returned, dataLen) tail with the scalar
// unrolled path. Requires order >= 1.
//
// Why raw WORD encodings: Go 1.26's arm64 assembler does not expose the
// vector mnemonics used here (LDR Q, LD1R.4S, MUL.4S, MLA.4S scalar,
// SSHL.4S, SUB.4S, STR Q). The encodings were produced by Apple clang's
// assembler from the equivalent NEON listing (see /tmp scratch in the PR
// notes); each is annotated with its disassembly.
//
// Register map (Plan9 / arm64):
//   R0 data, R1 dataLen(n), R2 qlpCoeff, R3 order(o), R4 lpQuantization,
//   R5 residual
//   R6 -lpQuantization; R7 i; R9 o*4; R10 &data[o]; R12 limit n-3;
//   R13 i*4; R14 &data[o+i]; R15 &residual[i]; R16 hist ptr; R17 coeff ptr;
//   R19 tap counter
//   V0 out/result; V1 history; V2 coeff broadcast; V3 shift; V4 accumulator
TEXT ·lpcResidualMACNEON(SB), NOSPLIT, $0-56
	MOVD data+0(FP), R0
	MOVD dataLen+8(FP), R1
	MOVD qlpCoeff+16(FP), R2
	MOVD order+24(FP), R3
	MOVD lpQuantization+32(FP), R4
	MOVD residual+40(FP), R5

	NEG  R4, R6              // R6 = -lpQuantization
	WORD $0x4e040cc3         // dup.4s v3, w6        ; shift = -lpQuantization broadcast

	LSL  $2, R3, R9         // R9 = order*4
	ADD  R0, R9, R10        // R10 = &data[order]
	SUB  $3, R1, R12        // R12 = n-3
	MOVD $0, R7             // i = 0

loop:
	CMP  R12, R7
	BGE  done              // while i < n-3

	LSL  $2, R7, R13       // R13 = i*4
	ADD  R10, R13, R14     // R14 = &data[order+i]
	ADD  R5, R13, R15      // R15 = &residual[i]

	WORD $0x3dc001c0       // ldr q0, [x14]        ; V0 = data[o+i .. o+i+3]
	SUB  $4, R14, R16      // R16 = &data[o+i-1]  (history ptr, tap 0)
	MOVD R2, R17           // R17 = &qlpCoeff[0]
	MOVD R3, R19           // R19 = tap counter = order

	// tap 0: MUL
	WORD $0x4d40ca22       // ld1r.4s {v2}, [x17]  ; V2 = qlpCoeff[j] broadcast
	WORD $0x3dc00201       // ldr q1, [x16]        ; V1 = history
	WORD $0x4ea29c24       // mul.4s v4, v1, v2    ; acc = hist*coeff
	ADD  $4, R17, R17      // coeff++
	SUB  $4, R16, R16      // hist--
	SUB  $1, R19, R19

tap:
	CBZ  R19, reduce
	WORD $0x4d40ca22       // ld1r.4s {v2}, [x17]
	WORD $0x3dc00201       // ldr q1, [x16]
	WORD $0x4ea29424       // mla.4s v4, v1, v2    ; acc += hist*coeff
	ADD  $4, R17, R17
	SUB  $4, R16, R16
	SUB  $1, R19, R19
	JMP  tap

reduce:
	WORD $0x4ea34484       // sshl.4s v4, v4, v3   ; arithmetic >> lpQuantization
	WORD $0x6ea48400       // sub.4s v0, v0, v4    ; residual = out - (sum>>q)
	WORD $0x3d8001e0       // str q0, [x15]

	ADD  $4, R7, R7        // i += 4
	JMP  loop

done:
	// return i (number of samples consumed, multiple of 4)
	MOVD R7, ret+48(FP)
	RET
