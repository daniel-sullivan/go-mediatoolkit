//go:build arm64

#include "textflag.h"

// NEON float64/conversion vector instructions not in Go's assembler,
// hand-encoded as WORDs (same convention as inspection/butterfly_arm64.s).
// Encodings verified against the ARM ARM (C7.2) and empirically against
// scalar Go arithmetic (bit-compare including -0.0) before use.
//
// NOTE: VFADD_2D and VFMUL_2D are byte-identical to the macros of the
// same names in inspection/butterfly_arm64.s. They are duplicated
// rather than shared via a header because a cross-package #include,
// while accepted by the assembler, is not tracked by the go build
// cache (edits to the shared file would not rebuild this package).
// If you change an encoding here, update inspection/butterfly_arm64.s
// to match, and vice versa.
//
// FADD  Vd.2D, Vn.2D, Vm.2D:  0x4E60D400 | (Vm<<16) | (Vn<<5) | Vd
// FMUL  Vd.2D, Vn.2D, Vm.2D:  0x6E60DC00 | (Vm<<16) | (Vn<<5) | Vd
// FCVTL Vd.2D, Vn.2S:         0x0E617800 | (Vn<<5) | Vd   (widen 2x f32 -> 2x f64)
// FCVTN Vd.2S, Vn.2D:         0x0E616800 | (Vn<<5) | Vd   (narrow 2x f64 -> 2x f32)
//
// Deliberately NO FMLA anywhere: the multiply and the add must round
// separately (the C oracle is built with -ffp-contract=off, and the Go
// reference kernels carry an explicit float64() rounding barrier between
// them). A fused multiply-add would break bit-exactness.

#define VFADD_2D(Vm, Vn, Vd) \
	WORD $(0x4E60D400 | (Vm << 16) | (Vn << 5) | Vd)

#define VFMUL_2D(Vm, Vn, Vd) \
	WORD $(0x6E60DC00 | (Vm << 16) | (Vn << 5) | Vd)

#define VFCVTL_2D(Vn, Vd) \
	WORD $(0x0E617800 | (Vn << 5) | Vd)

#define VFCVTN_2S(Vn, Vd) \
	WORD $(0x0E616800 | (Vn << 5) | Vd)

// func oversamplePairAsm(z, in, out []float32, coeff []float64,
//         frames, factor, count, ch, c0, delay, phase0Index, zi int) int
//
// 2-lane (channel-pair) polyphase interpolation kernel. Lane 0 is
// channel c0, lane 1 is channel c0+1; the interleaved delay-line layout
// (z[slot*ch + c]) makes each pair load/store a single 64-bit FMOVD.
// Per frame:
//
//   1. load the input pair, store it to ring slot zi AND its mirror
//      slot zi+delay (see the TruePeaker doc for why the mirror makes
//      every read wrap-free);
//   2. phase 0 (single center tap, coefficient exactly 1.0): widen the
//      pair at slot zi-phase0Index and FADD it to a zero vector — the
//      +0.0 reproduces the reference loop's `prod + acc(=0.0)`, which
//      canonicalises a -0.0 sample to +0.0 (a raw copy would not);
//   3. phases 1..factor-1: descending contiguous tap walk from the
//      just-written slot; per tap, widen the pair (FCVTL), broadcast
//      the float64 coefficient (VDUP), multiply (FMUL.2D), then
//      accumulate with a SEPARATE add (FADD.2D, acc' = prod + acc) —
//      exactly the reference's float64(float64(z)*c) + acc per lane;
//   4. narrow the accumulator (FCVTN) and store the output pair.
//
// Stack layout (each slice = 24 bytes):
//   z_base+0(FP)    z_len+8(FP)    z_cap+16(FP)
//   in_base+24(FP)  in_len+32(FP)  in_cap+40(FP)
//   out_base+48(FP) out_len+56(FP) out_cap+64(FP)
//   coeff_base+72(FP) coeff_len+80(FP) coeff_cap+88(FP)
//   frames+96(FP) factor+104(FP) count+112(FP) ch+120(FP)
//   c0+128(FP) delay+136(FP) phase0Index+144(FP) zi+152(FP)
//   ret+160(FP)
TEXT ·oversamplePairAsm(SB), NOSPLIT, $0-168
	MOVD	z_base+0(FP), R0	// z base ptr
	MOVD	in_base+24(FP), R1	// in cursor
	MOVD	out_base+48(FP), R2	// out cursor
	MOVD	coeff_base+72(FP), R3	// coeff base ptr
	MOVD	frames+96(FP), R4	// frame counter
	MOVD	factor+104(FP), R5	// factor
	MOVD	count+112(FP), R6	// taps per non-zero phase
	MOVD	ch+120(FP), R7		// channels
	MOVD	c0+128(FP), R13		// first channel of this pair
	MOVD	delay+136(FP), R9	// ring length (slots)
	MOVD	phase0Index+144(FP), R22
	MOVD	zi+152(FP), R11		// ring cursor

	// Byte strides.
	LSL	$2, R7, R7		// R7  = rowbytes  = ch * 4
	LSL	$2, R13, R13		// R13 = c0 * 4 (column byte offset)
	MUL	R7, R9, R14		// R14 = delayBytes = delay * rowbytes
	MUL	R7, R22, R22		// R22 = p0bytes = phase0Index * rowbytes
	SUB	$1, R5, R25		// R25 = factor - 1 (non-passthrough phases)
	ADD	R13, R1, R1		// in  += c0*4 (point at this pair's column)
	ADD	R13, R2, R2		// out += c0*4

	CBZ	R4, done

frame_loop:
	// Load input pair [in[c0], in[c0+1]] (two float32 in one 64-bit load).
	FMOVD	(R1), F0
	ADD	R7, R1, R1		// in += rowbytes (next frame)

	// Write both mirrors: lo = &z[zi*ch + c0], top = lo + delayBytes.
	MUL	R7, R11, R12		// R12 = zi * rowbytes
	ADD	R0, R12, R12
	ADD	R13, R12, R12		// R12 = lo
	FMOVD	F0, (R12)
	ADD	R14, R12, R15		// R15 = top (high-mirror slot zi)
	FMOVD	F0, (R15)

	// ── Phase 0: passthrough tap (coeff exactly 1.0) ─────────────
	SUB	R22, R15, R21		// &z[(zi+delay-phase0Index)*ch + c0]
	FMOVD	(R21), F1		// pair as 2 x float32
	VFCVTL_2D(1, 2)			// V2.2D = widen(V1.2S)
	VEOR	V3.B16, V3.B16, V3.B16	// V3 = [0.0, 0.0] (the reference's acc)
	VFADD_2D(3, 2, 4)		// V4 = V2 + V3  (prod + 0.0; fixes -0.0)
	VFCVTN_2S(4, 5)			// narrow to 2 x float32
	FMOVD	F5, (R2)
	ADD	R7, R2, R2		// out += rowbytes (next phase row)

	// ── Phases 1..factor-1 ───────────────────────────────────────
	MOVD	R3, R16			// coeff cursor = coeff base
	MOVD	R25, R19		// phase counter

phase_loop:
	VEOR	V6.B16, V6.B16, V6.B16	// acc = [0.0, 0.0]
	MOVD	R15, R21		// tap ptr = top (descending walk)
	MOVD	R6, R17			// tap counter

tap_loop:
	FMOVD	(R21), F0		// z pair (2 x float32)
	VFCVTL_2D(0, 1)			// V1.2D = widen(V0.2S)
	FMOVD	(R16), F2		// coeff[t] (float64) into V2.D[0]
	VDUP	V2.D[0], V2.D2		// broadcast to both lanes
	VFMUL_2D(2, 1, 3)		// V3 = V1 * V2 (separate multiply)
	VFADD_2D(6, 3, 6)		// V6 = V3 + V6 (prod + acc, separate add)
	SUB	R7, R21, R21		// tap ptr -= rowbytes (one slot older)
	ADD	$8, R16, R16		// next coefficient
	SUB	$1, R17, R17
	CBNZ	R17, tap_loop

	VFCVTN_2S(6, 7)			// narrow accumulator pair
	FMOVD	F7, (R2)
	ADD	R7, R2, R2		// out += rowbytes
	SUB	$1, R19, R19
	CBNZ	R19, phase_loop

	// zi++ with wrap at delay.
	ADD	$1, R11, R11
	CMP	R9, R11
	BNE	no_wrap
	MOVD	ZR, R11

no_wrap:
	SUB	$1, R4, R4
	CBNZ	R4, frame_loop

done:
	MOVD	R11, ret+160(FP)
	RET
