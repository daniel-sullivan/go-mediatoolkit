//go:build arm64 && !opus_nosimd && !opus_strict

#include "textflag.h"

// func shortNSQAllpassSIMD(soaSAR2 *int32, diffQ14 *int32,
//                          warping_Q16 int32, ARshp *int16, order int,
//                          out *[4]int32)
//
// Four-lane parallel noise-shape allpass / AR-feedback chain, the
// second hot inner chunk of silk_noise_shape_quantizer_del_dec. The
// SoA layout has MAX_DEL_DEC_STATES (=4) delay-decision lanes
// interleaved per tap:
//
//     soaSAR2[j*4 + lane]     for lane in 0..3, j in 0..order-1.
//     diffQ14[lane]           for lane in 0..3.
//
// Bit-exact (in the unsaturated int32 domain) with
// silk_noise_shape_allpass_soa, and hence with four serial copies of
// the scalar reference at silk_NSQ_del_dec.go:403-421.
//
// SMLAWB-as-SQDMULH trick (same as shortPredictionSoASIMD):
//   SMLAWB(a, b, c) = a + ((b * int16(c)) >> 16).
//   If we pre-shift int16(c) << 15 into W = int32, then
//     SQDMULH(b, W) = (b * W * 2) >> 32 = (b * c) >> 16
//   modulo saturation. Saturation only fires when b = INT32_MIN and
//   c = INT16_MIN simultaneously, and the parity tests bound both
//   inputs to (INT16_MIN, INT16_MAX] and rely on scalar reference
//   randomised sAR2_Q14 staying away from that corner.
//
// Register plan:
//   X0  soaSAR2 (base pointer, not mutated)
//   X1  diffQ14
//   X2  warping_Q16 (int32 in low half)
//   X3  ARshp
//   X4  order (int, loop bound for j)
//   X5  out
//   X6  scratch: (j-1)*16 byte offset
//   X7  scratch: j*16 byte offset
//   X8  scratch: (j+1)*16 byte offset
//   X9  scratch: address soaSAR2 + offset
//   X10 scratch: ARshp[i] sign-extended int16 << 15
//   X11 scratch: ARshp pointer + i*2
//   X12 scratch: j loop index
//   X13 scratch: order-1 for terminal store
//
//   V0  n_AR_Q14 accumulator (4 lanes)
//   V1  tmp1 (4 lanes)
//   V2  tmp2 (4 lanes)
//   V3  warping_Q16 broadcast, pre-shifted << 15 (4 lanes)
//   V4,V5 scratch: sAR2 loads
//   V6  scratch: SUB / SQDMULH result
//   V7  scratch: AR_shp broadcast, pre-shifted << 15
//   V8  scratch: SQDMULH into n_AR
//
// WORD encodings are used because Go 1.26's arm64 assembler does not
// expose DUP .4S, SQDMULH .4S, LDR Q, STR Q, ADD .4S, SUB .4S via
// direct mnemonics.

TEXT ·shortNSQAllpassSIMD(SB), NOSPLIT, $0-48
	MOVD soaSAR2+0(FP),    R0
	MOVD diffQ14+8(FP),    R1
	MOVW warping_Q16+16(FP), R2
	MOVD ARshp+24(FP),     R3
	MOVD order+32(FP),     R4
	MOVD out+40(FP),       R5

	// V3 = broadcast warping_Q16 (low 16 bits sign-extended) << 15.
	// Matches the SMLAWB-as-SQDMULH trick for the warping coefficient.
	// Low 16 bits of R2 are sign-extended into R6, then << 15.
	SXTH  R2, R6                      // R6 = sign_ext(int16(R2))
	LSL   $15, R6, R6                 // R6 <<= 15
	WORD $0x4E040CC3                  // DUP V3.4S, W6

	// V0 = (order >> 1) broadcast to all 4 lanes.
	LSR   $1, R4, R6
	WORD $0x4E040CC0                  // DUP V0.4S, W6

	// --- Pre-loop: tmp2 = SMLAWB(Diff_Q14, sAR2[0], warping)
	//            tmp1 = SMLAWB(sAR2[0], sAR2[1] - tmp2, warping)
	//            sAR2[0] = tmp2
	//            n_AR = SMLAWB(n_AR, tmp2, AR_shp[0]) ---

	// V4 = load 4x int32 at diffQ14 (per-lane Diff_Q14).
	WORD $0x3DC00024                  // LDR Q4, [X1]
	// V5 = load 4x int32 at soaSAR2 + 0 = sAR2[0] lanes.
	WORD $0x3DC00005                  // LDR Q5, [X0]

	// V6 = SQDMULH(V5, V3); V2 = tmp2 = V4 + V6.
	WORD $0x4EA3B4A6                  // SQDMULH V6.4S, V5.4S, V3.4S
	WORD $0x4EA68482                  // ADD     V2.4S, V4.4S, V6.4S

	// V4 = load sAR2[1] lanes (offset +16 bytes from soaSAR2).
	ADD   $16, R0, R9
	WORD $0x3DC00124                  // LDR Q4, [X9]

	// V6 = V4 - V2 (non-saturating int32 sub, matches silk_SUB32_ovflw).
	WORD $0x6EA28486                  // SUB V6.4S, V4.4S, V2.4S
	// V6 = SQDMULH(V6, V3).
	WORD $0x4EA3B4C6                  // SQDMULH V6.4S, V6.4S, V3.4S
	// V1 = tmp1 = V5 + V6 (V5 still holds sAR2[0]).
	WORD $0x4EA684A1                  // ADD V1.4S, V5.4S, V6.4S

	// Store V2 (tmp2) -> sAR2[0].
	WORD $0x3D800002                  // STR Q2, [X0]

	// n_AR = SMLAWB(n_AR, tmp2, AR_shp[0]).
	MOVH  (R3), R10                   // R10 = sign_ext(int16(AR_shp[0]))
	LSL   $15, R10, R10
	WORD $0x4E040D47                  // DUP V7.4S, W10
	WORD $0x4EA7B448                  // SQDMULH V8.4S, V2.4S, V7.4S
	WORD $0x4EA88400                  // ADD V0.4S, V0.4S, V8.4S

	// j = 2 (in R12); j*16 (in R7); (j-1)*16 in R6; (j+1)*16 in R8.
	MOVD  $2, R12

loop_j:
	CMP   R4, R12
	BGE   loop_j_done

	// R7 = j*16, R6 = (j-1)*16, R8 = (j+1)*16.
	LSL   $4, R12, R7
	SUB   $16, R7, R6
	ADD   $16, R7, R8

	// ---- Iteration-even block ----
	// tmp2 = SMLAWB(sAR2[j-1], sAR2[j+0] - tmp1, warping)
	// sAR2[j-1] = tmp1
	// n_AR += SMLAWB(0, tmp1, AR_shp[j-1])
	//
	// Load V4 = sAR2[j+0], V5 = sAR2[j-1].
	ADD   R7, R0, R9
	WORD $0x3DC00124                  // LDR Q4, [X9]
	ADD   R6, R0, R9
	WORD $0x3DC00125                  // LDR Q5, [X9]

	// V6 = V4 - V1 (sAR2[j+0] - tmp1).
	WORD $0x6EA18486                  // SUB V6.4S, V4.4S, V1.4S
	// V6 = SQDMULH(V6, V3).
	WORD $0x4EA3B4C6                  // SQDMULH V6.4S, V6.4S, V3.4S
	// V2 = tmp2 = V5 + V6.
	WORD $0x4EA684A2                  // ADD V2.4S, V5.4S, V6.4S

	// Store V1 (tmp1) -> sAR2[j-1].
	ADD   R6, R0, R9
	WORD $0x3D800121                  // STR Q1, [X9]

	// Broadcast AR_shp[j-1] << 15 into V7, SQDMULH(V1, V7), accumulate.
	SUB   $1, R12, R13                // R13 = j - 1
	ADD   R13<<1, R3, R11             // R11 = ARshp + (j-1)*2
	MOVH  (R11), R10
	LSL   $15, R10, R10
	WORD $0x4E040D47                  // DUP V7.4S, W10
	WORD $0x4EA7B428                  // SQDMULH V8.4S, V1.4S, V7.4S
	WORD $0x4EA88400                  // ADD V0.4S, V0.4S, V8.4S

	// ---- Iteration-odd block ----
	// tmp1 = SMLAWB(sAR2[j+0], sAR2[j+1] - tmp2, warping)
	// sAR2[j+0] = tmp2
	// n_AR += SMLAWB(0, tmp2, AR_shp[j])
	//
	// Load V5 = sAR2[j+1]; V4 still holds sAR2[j+0].
	ADD   R8, R0, R9
	WORD $0x3DC00125                  // LDR Q5, [X9]

	// V6 = V5 - V2 (sAR2[j+1] - tmp2).
	WORD $0x6EA284A6                  // SUB V6.4S, V5.4S, V2.4S
	// V6 = SQDMULH(V6, V3).
	WORD $0x4EA3B4C6                  // SQDMULH V6.4S, V6.4S, V3.4S
	// V1 = tmp1 = V4 + V6.
	WORD $0x4EA68481                  // ADD V1.4S, V4.4S, V6.4S

	// Store V2 (tmp2) -> sAR2[j+0].
	ADD   R7, R0, R9
	WORD $0x3D800122                  // STR Q2, [X9]

	// Broadcast AR_shp[j] << 15 into V7, SQDMULH(V2, V7), accumulate.
	ADD   R12<<1, R3, R11             // R11 = ARshp + j*2
	MOVH  (R11), R10
	LSL   $15, R10, R10
	WORD $0x4E040D47                  // DUP V7.4S, W10
	WORD $0x4EA7B448                  // SQDMULH V8.4S, V2.4S, V7.4S
	WORD $0x4EA88400                  // ADD V0.4S, V0.4S, V8.4S

	ADD   $2, R12, R12
	B     loop_j

loop_j_done:
	// Post-loop: sAR2[order-1] = tmp1; n_AR += SMLAWB(0, tmp1, AR_shp[order-1]).
	SUB   $1, R4, R13                 // R13 = order - 1
	LSL   $4, R13, R6                 // R6 = (order-1) * 16
	ADD   R6, R0, R9
	WORD $0x3D800121                  // STR Q1, [X9]

	ADD   R13<<1, R3, R11             // R11 = ARshp + (order-1)*2
	MOVH  (R11), R10
	LSL   $15, R10, R10
	WORD $0x4E040D47                  // DUP V7.4S, W10
	WORD $0x4EA7B428                  // SQDMULH V8.4S, V1.4S, V7.4S
	WORD $0x4EA88400                  // ADD V0.4S, V0.4S, V8.4S

	// Store V0 -> out[0..3].
	WORD $0x3D8000A0                  // STR Q0, [X5]
	RET
