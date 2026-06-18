//go:build arm64 && !opus_nosimd && !opus_strict

#include "textflag.h"

// arm64 NEON port of celt_float2int16_neon
// (libopus/celt/arm/celt_neon_intr.c:49–89).
//
// Converts `cnt` float32 samples to int16, scaling by CELT_SIG_SCALE
// (32768.0) and saturating to the int16 range. The main loop processes
// 16 samples per iteration via four 4-lane blocks, mirroring the C
// intrinsic version. The scalar tail handles the 0..15 leftover.
//
// Rounding: FCVTAS (round-to-nearest, ties-AWAY) in both the vector
// and scalar tail. This matches the upstream NEON build and
// introduces a documented 1-ULP drift vs the pure-Go
// math.RoundToEven (ties-to-even) path on exact half-integer inputs.
// Excluded from -tags=opus_strict via build tag; the scalar Go
// fallback handles strict / nosimd builds.
//
// WORD encodings are used for the NEON-specific instructions because
// Go 1.26's arm64 assembler does not expose LDR Q / STR D / FMUL .4S
// / FCVTAS .4S / SQXTN .4H / DUP .4S via direct mnemonics. Encodings
// verified against ARM ARM A64 and cross-checked with clang output.

// func celtFloat2Int16SIMD(in *float32, out *int16, cnt int)
TEXT ·celtFloat2Int16SIMD(SB), NOSPLIT, $0-24
	MOVD in+0(FP),  R0
	MOVD out+8(FP), R1
	MOVD cnt+16(FP), R2

	// Load CELT_SIG_SCALE = 32768.0f = 0x47000000 into a 4-lane vec.
	MOVD   $0x47000000, R3             // 32768.0f bit pattern
	WORD   $0x4E040C60                 // DUP V0.4S, W3      ; V0 = {32768}×4

	CMP    $16, R2
	BLT    f2i_tail

f2i_loop16:
	// Block 0 (samples 0..3)
	WORD $0x3CC10401                   // LDR  Q1, [X0], #16
	WORD $0x6E20DC21                   // FMUL V1.4S, V1.4S, V0.4S
	WORD $0x4E21C821                   // FCVTAS V1.4S, V1.4S
	WORD $0x0E614821                   // SQXTN  V1.4H, V1.4S
	WORD $0xFC008421                   // STR   D1, [X1], #8

	// Block 1 (samples 4..7)
	WORD $0x3CC10401                   // LDR  Q1, [X0], #16
	WORD $0x6E20DC21                   // FMUL V1.4S, V1.4S, V0.4S
	WORD $0x4E21C821                   // FCVTAS V1.4S, V1.4S
	WORD $0x0E614821                   // SQXTN  V1.4H, V1.4S
	WORD $0xFC008421                   // STR   D1, [X1], #8

	// Block 2 (samples 8..11)
	WORD $0x3CC10401                   // LDR  Q1, [X0], #16
	WORD $0x6E20DC21                   // FMUL V1.4S, V1.4S, V0.4S
	WORD $0x4E21C821                   // FCVTAS V1.4S, V1.4S
	WORD $0x0E614821                   // SQXTN  V1.4H, V1.4S
	WORD $0xFC008421                   // STR   D1, [X1], #8

	// Block 3 (samples 12..15)
	WORD $0x3CC10401                   // LDR  Q1, [X0], #16
	WORD $0x6E20DC21                   // FMUL V1.4S, V1.4S, V0.4S
	WORD $0x4E21C821                   // FCVTAS V1.4S, V1.4S
	WORD $0x0E614821                   // SQXTN  V1.4H, V1.4S
	WORD $0xFC008421                   // STR   D1, [X1], #8

	SUB  $16, R2, R2
	CMP  $16, R2
	BGE  f2i_loop16

f2i_tail:
	CBZ  R2, f2i_done

	// Preload the scalar CELT_SIG_SCALE into S0 (S-reg view of V0).
	// V0.S[0] is already 32768.0 from the vector DUP; no extra move
	// needed. Also preload the int16 saturation bounds.
	MOVD $32767, R4                    // upper clamp
	MOVD $-32768, R5                   // lower clamp

f2i_tail_loop:
	WORD $0xBC404401                   // LDR  S1, [X0], #4
	WORD $0x1E200821                   // FMUL S1, S1, S0           ; S1 = x * 32768.0
	WORD $0x1E240026                   // FCVTAS W6, S1             ; W6 = round(S1) (ties-away)
	CMPW  R4, R6
	CSELW GT, R4, R6, R6               // if W6 > 32767 then W6 = 32767
	CMPW  R5, R6
	CSELW LT, R5, R6, R6               // if W6 < -32768 then W6 = -32768
	MOVH  R6, (R1)                     // *out = (int16)W6
	ADD  $2, R1, R1
	SUB  $1, R2, R2
	CBNZ R2, f2i_tail_loop

f2i_done:
	RET
