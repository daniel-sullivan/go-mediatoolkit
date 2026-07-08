//go:build amd64

#include "textflag.h"

// func oversamplePairAsm(z, in, out []float32, coeff []float64,
//         frames, factor, count, ch, c0, delay, phase0Index, zi int) int
//
// SSE2 2-lane (channel-pair) polyphase interpolation kernel. Lane 0 is
// channel c0, lane 1 is channel c0+1; the interleaved delay-line layout
// (z[slot*ch + c]) makes each pair load/store a single 64-bit MOVSD.
// Baseline SSE2 throughout: CVTPS2PD/CVTPD2PS for the float32<->float64
// conversions, UNPCKLPD for the coefficient broadcast, and SEPARATE
// MULPD + ADDPD for the MAC — deliberately NO FMA (VFMADD*) anywhere:
// the multiply and the add must round separately (the C oracle is built
// with -ffp-contract=off, and the Go reference kernels carry an
// explicit float64() rounding barrier between them).
//
// Operand order is arranged so each lane performs EXACTLY the reference
// sequence float64(float64(z)*coeff) + acc: the widened sample is the
// MULPD destination (prod = v * c), the product is the ADDPD
// destination (acc' = prod + acc), then the product register becomes
// the new accumulator. Phase 0 (single center tap, coefficient exactly
// 1.0) skips the multiply — float64(v)*1.0 == float64(v) exactly — but
// keeps the add against a zeroed accumulator, because the reference's
// `prod + 0.0` canonicalises a -0.0 sample to +0.0 (a raw copy would
// not).
//
// Stack layout (each slice = 24 bytes):
//   z_base+0(FP)    z_len+8(FP)    z_cap+16(FP)
//   in_base+24(FP)  in_len+32(FP)  in_cap+40(FP)
//   out_base+48(FP) out_len+56(FP) out_cap+64(FP)
//   coeff_base+72(FP) coeff_len+80(FP) coeff_cap+88(FP)
//   frames+96(FP) factor+104(FP) count+112(FP) ch+120(FP)
//   c0+128(FP) delay+136(FP) phase0Index+144(FP) zi+152(FP)
//   ret+160(FP)
//
// Locals ($16): 0(SP) = this frame's top pointer (high-mirror slot zi),
// 8(SP) = remaining non-passthrough phase count.
TEXT ·oversamplePairAsm(SB), NOSPLIT, $16-168
	MOVQ	z_base+0(FP), SI	// z base ptr
	MOVQ	in_base+24(FP), DI	// in cursor
	MOVQ	out_base+48(FP), DX	// out cursor
	MOVQ	coeff_base+72(FP), R15	// coeff base ptr
	MOVQ	frames+96(FP), R8	// frame counter
	MOVQ	count+112(FP), R10	// taps per non-zero phase
	MOVQ	ch+120(FP), R9
	SHLQ	$2, R9			// R9  = rowbytes = ch * 4
	MOVQ	c0+128(FP), AX
	SHLQ	$2, AX			// c0 * 4 (column byte offset)
	ADDQ	AX, SI			// fold the pair's column into every base:
	ADDQ	AX, DI			// all accesses become base + slot*rowbytes
	ADDQ	AX, DX
	MOVQ	delay+136(FP), R12	// ring length (slots)
	MOVQ	R12, R13
	IMULQ	R9, R13			// R13 = delayBytes = delay * rowbytes
	MOVQ	phase0Index+144(FP), R14
	IMULQ	R9, R14			// R14 = p0bytes = phase0Index * rowbytes
	MOVQ	zi+152(FP), R11		// ring cursor

	TESTQ	R8, R8
	JZ	done

frame_loop:
	// Load input pair [in[c0], in[c0+1]] (two float32 in one 64-bit load).
	MOVSD	(DI), X0
	ADDQ	R9, DI			// in += rowbytes (next frame)

	// Write both mirrors: lo = &z[zi*ch + c0], top = lo + delayBytes.
	MOVQ	R11, AX
	IMULQ	R9, AX			// AX = zi * rowbytes
	LEAQ	(SI)(AX*1), BX		// BX = lo
	MOVSD	X0, (BX)
	ADDQ	R13, BX			// BX = top (high-mirror slot zi)
	MOVSD	X0, (BX)
	MOVQ	BX, 0(SP)		// save top for the phase loops

	// ── Phase 0: passthrough tap (coeff exactly 1.0) ─────────────
	MOVQ	BX, AX
	SUBQ	R14, AX			// &z[(zi+delay-phase0Index)*ch + c0]
	MOVSD	(AX), X1		// pair as 2 x float32
	CVTPS2PD X1, X2			// widen to 2 x float64 (prod)
	XORPD	X3, X3			// X3 = [0.0, 0.0] (the reference's acc)
	ADDPD	X3, X2			// X2 = prod + 0.0 (fixes -0.0)
	CVTPD2PS X2, X4			// narrow to 2 x float32
	MOVSD	X4, (DX)
	ADDQ	R9, DX			// out += rowbytes (next phase row)

	// ── Phases 1..factor-1 ───────────────────────────────────────
	MOVQ	R15, CX			// coeff cursor = coeff base
	MOVQ	factor+104(FP), AX
	DECQ	AX
	MOVQ	AX, 8(SP)		// phase counter (factor - 1)

phase_loop:
	XORPD	X5, X5			// acc = [0.0, 0.0]
	MOVQ	0(SP), BX		// tap ptr = top (descending walk)
	MOVQ	R10, AX			// tap counter

tap_loop:
	MOVSD	(BX), X1		// z pair (2 x float32)
	CVTPS2PD X1, X2			// X2 = widened pair (v)
	MOVSD	(CX), X3		// coeff[t] (float64), upper lane zeroed
	UNPCKLPD X3, X3			// broadcast: X3 = [c, c]
	MULPD	X3, X2			// X2 = v * c   (separate multiply)
	ADDPD	X5, X2			// X2 = prod + acc (separate add)
	MOVAPD	X2, X5			// acc = X2
	SUBQ	R9, BX			// tap ptr -= rowbytes (one slot older)
	ADDQ	$8, CX			// next coefficient
	DECQ	AX
	JNZ	tap_loop

	CVTPD2PS X5, X6			// narrow accumulator pair
	MOVSD	X6, (DX)
	ADDQ	R9, DX			// out += rowbytes
	DECQ	8(SP)
	JNZ	phase_loop

	// zi++ with wrap at delay.
	INCQ	R11
	CMPQ	R11, R12
	JNE	no_wrap
	XORQ	R11, R11

no_wrap:
	DECQ	R8
	JNZ	frame_loop

done:
	MOVQ	R11, ret+160(FP)
	RET
