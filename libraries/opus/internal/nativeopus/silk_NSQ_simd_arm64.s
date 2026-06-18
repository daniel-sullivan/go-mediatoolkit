//go:build arm64 && !opus_nosimd && !opus_strict

#include "textflag.h"

// func shortPredictionSoASIMD(sLPCAtBase *int32, coef16 *int16, order int, out *[4]int32)
//
// Four-lane parallel implementation of silk_SMLAWB-chained LPC short
// prediction, operating on an SoA layout where 4 delay-decision lanes
// are interleaved per tap:
//
//     sLPCAtBase[lane] for lane in 0..3  // tap 0
//     (sLPCAtBase - 16)[lane]            // tap 1 (-16 bytes = -1 tap)
//     (sLPCAtBase - 32)[lane]            // tap 2
//     ...
//
// Bit-exact with silk_noise_shape_quantizer_short_prediction_soa (and
// hence four serial silk_noise_shape_quantizer_short_prediction_c
// calls) in the unsaturated int32 domain. silk_SMULWB never saturates
// for int32 × int16 inputs (|b*c16| <= 2^46, >> 16 gives 2^30 which
// fits in int32), so SQDMULH's saturation is a no-op in practice.
//
// Implementation:
//   - V0 = per-lane accumulator, initialised to [order>>1]×4
//   - For each tap i:
//       V1 = load 4 int32s at [sLPCAtBase - i*16]
//       W10 = sign-extend int16(coef16[i]), shift left 15 → the
//             "pre-scaled" form that turns SQDMULH into SMULWB:
//             SQDMULH(b, c16<<15) = (b*c16*2^16) >> 32 = (b*c16) >> 16.
//       V2 = broadcast W10 into 4 lanes
//       V3 = SQDMULH(V1, V2)
//       V0 += V3
//   - Store V0 to out[0..3].
//
// WORD encodings are used for the NEON-specific instructions because
// Go 1.26's arm64 assembler does not expose DUP .4S, SQDMULH .4S,
// LDR Q, STR Q, or ADD .4S via direct mnemonics.

TEXT ·shortPredictionSoASIMD(SB), NOSPLIT, $0-32
	MOVD sLPCAtBase+0(FP), R0
	MOVD coef16+8(FP),     R1
	MOVD order+16(FP),     R2
	MOVD out+24(FP),       R3

	// V0 = [order>>1] × 4
	LSR  $1, R2, R4
	WORD $0x4E040C80                 // DUP V0.4S, W4

	MOVD ZR, R5                      // i = 0

sp_loop:
	CMP  R2, R5
	BGE  sp_done

	// Load sLPC_Q14[base-i][0..3] = [sLPCAtBase - i*16].
	LSL  $4, R5, R6                  // R6 = i * 16
	SUB  R6, R0, R7                  // R7 = sLPCAtBase - R6
	WORD $0x3DC000E1                 // LDR Q1, [X7]

	// Load coef16[i], sign-extend to 64, then << 15.
	LSL   $1, R5, R8                 // R8 = i * 2
	ADD   R8, R1, R9                 // R9 = coef16 + i*2
	MOVH  (R9), R10                  // R10 = sign_ext(int16(coef16[i]))
	LSL   $15, R10, R10              // R10 <<= 15

	// Broadcast low-32 of R10 into V2.4S.
	WORD $0x4E040D42                 // DUP V2.4S, W10

	// V3 = SQDMULH(V1, V2); V0 += V3.
	WORD $0x4EA2B423                 // SQDMULH V3.4S, V1.4S, V2.4S
	WORD $0x4EA38400                 // ADD     V0.4S, V0.4S, V3.4S

	ADD  $1, R5, R5
	B    sp_loop

sp_done:
	WORD $0x3D800060                 // STR Q0, [X3]
	RET
