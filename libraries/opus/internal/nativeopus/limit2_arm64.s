//go:build arm64 && !opus_nosimd && !opus_strict

#include "textflag.h"

// arm64 NEON port of opus_limit2_checkwithin1_neon
// (libopus/celt/arm/celt_neon_intr.c:91–164).
//
// Two-pass clip of `cnt` float samples to [-2.0, +2.0]:
//   - Pass 1: 16-sample main loop tracks per-lane min/max; reduces
//     to scalar min/max via FMAXV/FMINV.
//   - Pass 2 (skipped when min >= -2 && max <= +2): re-walks the
//     same 16-sample blocks and clips in place.
//   - Scalar tail: clips 0..15 leftover, updates exceeding1.
//
// exceeding1 is set iff any sample was outside [-1, +1]. Return
// value is !exceeding1 as int32: 1 means "all samples are within
// [-1, 1]" — a fast-path hint for the soft-clipper in
// opus_pcm_soft_clip_impl.
//
// WORD encodings for NEON-specific ops; verified against ARM ARM A64.

// func opusLimit2CheckWithin1SIMD(samples *float32, cnt int) int32
TEXT ·opusLimit2CheckWithin1SIMD(SB), NOSPLIT, $0-20
	MOVD samples+0(FP), R0
	MOVD cnt+8(FP),     R1

	MOVD R0, R7                          // R7 = base ptr (preserved for replay)
	MOVD ZR, R4                          // R4 = exceeding1 (0/1)

	WORD $0x6F00E400                    // MOVI V0.2D, #0   ; max_all_0
	WORD $0x6F00E401                    // MOVI V1.2D, #0   ; max_all_1
	WORD $0x6F00E402                    // MOVI V2.2D, #0   ; min_all_0
	WORD $0x6F00E403                    // MOVI V3.2D, #0   ; min_all_1

	LSR  $4, R1, R5                      // R5 = cnt / 16
	LSL  $4, R5, R5                      // R5 = blockedSize
	SUB  R5, R1, R6                      // R6 = tail length (0..15)

	MOVD R5, R2                          // R2 = loop counter
	CBZ  R2, l2_post_reduce

l2_maxmin_loop:
	WORD $0x3CC10404                    // LDR  Q4, [X0], #16
	WORD $0x3CC10405                    // LDR  Q5, [X0], #16
	WORD $0x3CC10406                    // LDR  Q6, [X0], #16
	WORD $0x3CC10407                    // LDR  Q7, [X0], #16

	WORD $0x4E25F488                    // FMAX V8.4S, V4.4S, V5.4S
	WORD $0x4E27F4C9                    // FMAX V9.4S, V6.4S, V7.4S
	WORD $0x4E28F400                    // FMAX V0.4S, V0.4S, V8.4S
	WORD $0x4E29F421                    // FMAX V1.4S, V1.4S, V9.4S

	WORD $0x4EA5F488                    // FMIN V8.4S, V4.4S, V5.4S
	WORD $0x4EA7F4C9                    // FMIN V9.4S, V6.4S, V7.4S
	WORD $0x4EA8F442                    // FMIN V2.4S, V2.4S, V8.4S
	WORD $0x4EA9F463                    // FMIN V3.4S, V3.4S, V9.4S

	SUBS $16, R2, R2
	BNE  l2_maxmin_loop

l2_post_reduce:
	WORD $0x4E21F400                    // FMAX V0.4S, V0.4S, V1.4S   ; max_all
	WORD $0x4EA3F442                    // FMIN V2.4S, V2.4S, V3.4S   ; min_all
	WORD $0x6E30F800                    // FMAXV S0, V0.4S            ; S0 = max
	WORD $0x6EB0F842                    // FMINV S2, V2.4S            ; S2 = min

	// Materialise +2.0, -2.0, +1.0, -1.0 into S17, S16, S19, S18.
	MOVD $0x40000000, R9                 // +2.0f
	WORD $0x1E270131                    // FMOV S17, W9
	MOVD $0xC0000000, R9                 // -2.0f
	WORD $0x1E270130                    // FMOV S16, W9
	MOVD $0x3F800000, R9                 // +1.0f
	WORD $0x1E270133                    // FMOV S19, W9
	MOVD $0xBF800000, R9                 // -1.0f
	WORD $0x1E270132                    // FMOV S18, W9

	// exceeding1 |= (max > 1.0 || min < -1.0) from the reduced values.
	// NOTE: this covers all full-block samples; the scalar tail
	// appends its own contribution.
	WORD $0x1E332000                    // FCMP S0, S19
	BLE  l2_skip_maxhi
	MOVD $1, R4
l2_skip_maxhi:
	WORD $0x1E322040                    // FCMP S2, S18
	BGE  l2_check_range
	MOVD $1, R4

l2_check_range:
	// Need clip pass if max > 2.0 OR min < -2.0.
	WORD $0x1E312000                    // FCMP S0, S17
	BGT  l2_clip_pass
	WORD $0x1E302040                    // FCMP S2, S16
	BGE  l2_scalar_tail

l2_clip_pass:
	// Broadcast +2.0 / -2.0 across 4-lane vectors.
	WORD $0x4E040620                    // DUP V0.4S, V17.S[0]   ; {+2.0}×4
	WORD $0x4E040601                    // DUP V1.4S, V16.S[0]   ; {-2.0}×4

	MOVD R7, R0                          // replay from base ptr
	MOVD R5, R2
	CBZ  R2, l2_scalar_tail

l2_clip_loop:
	WORD $0x3DC00004                    // LDR  Q4, [X0, #0]
	WORD $0x3DC00405                    // LDR  Q5, [X0, #16]
	WORD $0x3DC00806                    // LDR  Q6, [X0, #32]
	WORD $0x3DC00C07                    // LDR  Q7, [X0, #48]

	// clipped = FMIN(FMAX(orig, -2.0), +2.0)
	WORD $0x4E21F484                    // FMAX V4.4S, V4.4S, V1.4S
	WORD $0x4E21F4A5                    // FMAX V5.4S, V5.4S, V1.4S
	WORD $0x4E21F4C6                    // FMAX V6.4S, V6.4S, V1.4S
	WORD $0x4E21F4E7                    // FMAX V7.4S, V7.4S, V1.4S
	WORD $0x4EA0F484                    // FMIN V4.4S, V4.4S, V0.4S
	WORD $0x4EA0F4A5                    // FMIN V5.4S, V5.4S, V0.4S
	WORD $0x4EA0F4C6                    // FMIN V6.4S, V6.4S, V0.4S
	WORD $0x4EA0F4E7                    // FMIN V7.4S, V7.4S, V0.4S

	WORD $0x3D800004                    // STR  Q4, [X0, #0]
	WORD $0x3D800405                    // STR  Q5, [X0, #16]
	WORD $0x3D800806                    // STR  Q6, [X0, #32]
	WORD $0x3D800C07                    // STR  Q7, [X0, #48]

	ADD  $64, R0, R0
	SUBS $16, R2, R2
	BNE  l2_clip_loop

l2_scalar_tail:
	CBZ  R6, l2_done

	// Compute tail start pointer: base + blockedSize*4 bytes.
	LSL  $2, R5, R9
	ADD  R9, R7, R10

l2_tail_loop:
	WORD $0xBD40014D                    // LDR  S13, [X10]        ; imm12=0

	WORD $0x1E3321A0                    // FCMP S13, S19
	BLE  l2_tail_nothi
	MOVD $1, R4
l2_tail_nothi:
	WORD $0x1E3221A0                    // FCMP S13, S18
	BGE  l2_tail_clip
	MOVD $1, R4

l2_tail_clip:
	WORD $0x1E3049AD                    // FMAX S13, S13, S16
	WORD $0x1E3159AD                    // FMIN S13, S13, S17
	WORD $0xBD00014D                    // STR  S13, [X10]        ; imm12=0

	ADD  $4, R10, R10
	SUB  $1, R6, R6
	CBNZ R6, l2_tail_loop

l2_done:
	// Return !exceeding1.
	EOR  $1, R4, R4
	MOVW R4, ret+16(FP)
	RET
