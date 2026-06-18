//go:build arm64 && !opus_nosimd && !opus_strict

#include "textflag.h"

// Shared arm64 NEON kernels for:
//   celt_inner_prod(x, y, N)            — single dot product
//   dual_inner_prod(x, y01, y02, N, …)  — two dot products sharing x
//
// Both mirror libopus/celt/arm/pitch_neon_intr.c's float path:
//   - 4-lane float32 accumulator, tree reduction at the end
//   - FMLA (fused mul-add) — matches the C vfmaq_f32 when built with
//     __ARM_FEATURE_FMA (M3 Pro's baseline)
//   - 8-element main loop, optional +4 tail block, scalar 0..3 tail
//
// NOT bit-exact with the scalar Go path (horizontal reduction across
// 4 lanes differs in rounding order from left-to-right scalar
// summation), so this file is excluded under -tags=opus_strict. The
// scalar Go fallback in pitch.go handles strict/nosimd builds.
//
// WORD encodings used for SIMD ops because Go 1.26's arm64 assembler
// does not expose LDR Q / FMLA .4S / FADDP .4S / MOVI .2D / FMADD S
// via direct mnemonics. Encodings verified against ARM ARM A64.

// func celtInnerProdSIMD(x, y *float32, N int) float32
TEXT ·celtInnerProdSIMD(SB), NOSPLIT, $0-32
	MOVD x+0(FP), R0
	MOVD y+8(FP), R1
	MOVD N+16(FP), R2

	WORD $0x6F00E400                  // MOVI V0.2D, #0        ; acc = 0

	CMP  $8, R2
	BLT  ip_tail4
ip_loop8:
	WORD $0x3CC10401                  // LDR  Q1, [X0], #16    ; V1 = x[0..3]
	WORD $0x3CC10422                  // LDR  Q2, [X1], #16    ; V2 = y[0..3]
	WORD $0x4E22CC20                  // FMLA V0.4S, V1.4S, V2.4S
	WORD $0x3CC10401                  // LDR  Q1, [X0], #16    ; V1 = x[4..7]
	WORD $0x3CC10422                  // LDR  Q2, [X1], #16    ; V2 = y[4..7]
	WORD $0x4E22CC20                  // FMLA V0.4S, V1.4S, V2.4S
	SUB  $8, R2, R2
	CMP  $8, R2
	BGE  ip_loop8

ip_tail4:
	CMP  $4, R2
	BLT  ip_reduce
	WORD $0x3CC10401                  // LDR  Q1, [X0], #16
	WORD $0x3CC10422                  // LDR  Q2, [X1], #16
	WORD $0x4E22CC20                  // FMLA V0.4S, V1.4S, V2.4S
	SUB  $4, R2, R2

ip_reduce:
	WORD $0x6E20D400                  // FADDP V0.4S, V0.4S, V0.4S
	WORD $0x6E20D400                  // FADDP V0.4S, V0.4S, V0.4S
	// S0 now holds the 4-lane sum.

	CBZ  R2, ip_done
ip_scalar:
	WORD $0xBC404401                  // LDR   S1, [X0], #4
	WORD $0xBC404422                  // LDR   S2, [X1], #4
	WORD $0x1F020020                  // FMADD S0, S1, S2, S0  ; S0 += S1*S2
	SUB  $1, R2, R2
	CBNZ R2, ip_scalar

ip_done:
	FMOVS F0, ret+24(FP)
	RET

// func dualInnerProdSIMD(x, y01, y02 *float32, N int, xy1, xy2 *float32)
TEXT ·dualInnerProdSIMD(SB), NOSPLIT, $0-48
	MOVD x+0(FP),    R0
	MOVD y01+8(FP),  R1
	MOVD y02+16(FP), R2
	MOVD N+24(FP),   R3

	WORD $0x6F00E400                  // MOVI V0.2D, #0   ; xy01 acc
	WORD $0x6F00E401                  // MOVI V1.2D, #0   ; xy02 acc

	CMP  $8, R3
	BLT  dp_tail4
dp_loop8:
	WORD $0x3CC10402                  // LDR  Q2, [X0], #16  ; V2 = x[0..3]
	WORD $0x3CC10423                  // LDR  Q3, [X1], #16  ; V3 = y01[0..3]
	WORD $0x3CC10444                  // LDR  Q4, [X2], #16  ; V4 = y02[0..3]
	WORD $0x4E23CC40                  // FMLA V0.4S, V2.4S, V3.4S
	WORD $0x4E24CC41                  // FMLA V1.4S, V2.4S, V4.4S
	WORD $0x3CC10402                  // LDR  Q2, [X0], #16  ; V2 = x[4..7]
	WORD $0x3CC10423                  // LDR  Q3, [X1], #16  ; V3 = y01[4..7]
	WORD $0x3CC10444                  // LDR  Q4, [X2], #16  ; V4 = y02[4..7]
	WORD $0x4E23CC40                  // FMLA V0.4S, V2.4S, V3.4S
	WORD $0x4E24CC41                  // FMLA V1.4S, V2.4S, V4.4S
	SUB  $8, R3, R3
	CMP  $8, R3
	BGE  dp_loop8

dp_tail4:
	CMP  $4, R3
	BLT  dp_reduce
	WORD $0x3CC10402                  // LDR  Q2, [X0], #16
	WORD $0x3CC10423                  // LDR  Q3, [X1], #16
	WORD $0x3CC10444                  // LDR  Q4, [X2], #16
	WORD $0x4E23CC40                  // FMLA V0.4S, V2.4S, V3.4S
	WORD $0x4E24CC41                  // FMLA V1.4S, V2.4S, V4.4S
	SUB  $4, R3, R3

dp_reduce:
	WORD $0x6E20D400                  // FADDP V0.4S, V0.4S, V0.4S
	WORD $0x6E20D400                  // FADDP V0.4S, V0.4S, V0.4S
	WORD $0x6E21D421                  // FADDP V1.4S, V1.4S, V1.4S
	WORD $0x6E21D421                  // FADDP V1.4S, V1.4S, V1.4S
	// S0 = xy01, S1 = xy02

	CBZ  R3, dp_done
dp_scalar:
	WORD $0xBC404402                  // LDR   S2, [X0], #4
	WORD $0xBC404423                  // LDR   S3, [X1], #4
	WORD $0xBC404444                  // LDR   S4, [X2], #4
	WORD $0x1F030040                  // FMADD S0, S2, S3, S0  ; S0 += S2*S3
	WORD $0x1F040441                  // FMADD S1, S2, S4, S1  ; S1 += S2*S4
	SUB  $1, R3, R3
	CBNZ R3, dp_scalar

dp_done:
	MOVD xy1+32(FP), R4
	MOVD xy2+40(FP), R5
	FMOVS F0, (R4)
	FMOVS F1, (R5)
	RET
