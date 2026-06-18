//go:build arm64

#include "textflag.h"

// NEON float64 vector instructions not in Go's assembler.
// Encoding: see ARM ARM C7.2. Verified against Go's arm64enc.s TODO comments.
//
// FADD Vd.2D, Vn.2D, Vm.2D:  0x4E60D400 | (Vm<<16) | (Vn<<5) | Vd
// FSUB Vd.2D, Vn.2D, Vm.2D:  0x4EE0D400 | (Vm<<16) | (Vn<<5) | Vd
// FMUL Vd.2D, Vn.2D, Vm.2D:  0x6E60DC00 | (Vm<<16) | (Vn<<5) | Vd

#define VFADD_2D(Vm, Vn, Vd) \
	WORD $(0x4E60D400 | (Vm << 16) | (Vn << 5) | Vd)

#define VFSUB_2D(Vm, Vn, Vd) \
	WORD $(0x4EE0D400 | (Vm << 16) | (Vn << 5) | Vd)

#define VFMUL_2D(Vm, Vn, Vd) \
	WORD $(0x6E60DC00 | (Vm << 16) | (Vn << 5) | Vd)

// func butterflyPass(x []complex128, tw []complex128, half, step int)
//
// Stack layout (each slice = 24 bytes):
//   x_base+0(FP)  x_len+8(FP)  x_cap+16(FP)
//   tw_base+24(FP) tw_len+32(FP) tw_cap+40(FP)
//   half+48(FP)    step+56(FP)
TEXT ·butterflyPass(SB), NOSPLIT, $0-64
	MOVD	x_base+0(FP), R0	// x base ptr
	MOVD	x_len+8(FP), R1	// n = len(x) (elements)
	MOVD	tw_base+24(FP), R2	// tw base ptr
	MOVD	half+48(FP), R3		// half
	MOVD	step+56(FP), R4		// step

	// Byte offsets.
	LSL	$4, R3, R6		// R6 = half * 16 bytes
	LSL	$4, R4, R7		// R7 = step * 16 bytes (twiddle stride)
	LSL	$1, R3, R5		// R5 = size = half * 2 (elements)
	LSL	$4, R5, R8		// R8 = size * 16 bytes (group stride)

	// Build sign mask V16 = [-1.0, +1.0] for complex multiply.
	FMOVD	$(-1.0), F16		// V16.D[0] = -1.0
	FMOVD	$(1.0), F17		// V17.D[0] = +1.0
	VMOV	V17.D[0], V16.D[1]	// V16 = [-1.0, +1.0]

	// Outer loop over groups.
	MOVD	R0, R10			// R10 = &x[i] (current group start)
	MOVD	ZR, R9			// i = 0 (element counter)

outer:
	CMP	R1, R9
	BGE	done

	// R11 = &x[i + half]
	ADD	R6, R10, R11
	// R12 = twiddle pointer (reset each group)
	MOVD	R2, R12
	// R13 = j counter
	MOVD	R3, R13

inner:
	CBZ	R13, next_group

	// Load twiddle w = [w_re, w_im]
	VLD1	(R12), [V0.D2]
	ADD	R7, R12, R12		// tw_ptr += step * 16

	// Load u = x[i+j] and v_raw = x[i+j+half]
	VLD1	(R10), [V1.D2]		// V1 = u = [u_re, u_im]
	VLD1	(R11), [V2.D2]		// V2 = [v_re, v_im]

	// ── Complex multiply: V3 = V2 * V0 ──────────────────────
	// Step 1: broadcast w_re, compute [v_re*w_re, v_im*w_re]
	VDUP	V0.D[0], V4.D2		// V4 = [w_re, w_re]
	VFMUL_2D(4, 2, 3)		// V3 = V2 * V4

	// Step 2: swap V2 lanes and apply sign mask → [-v_im, v_re]
	VEXT	$8, V2.B16, V2.B16, V5.B16  // V5 = [v_im, v_re]
	VFMUL_2D(16, 5, 5)		// V5 = [-v_im, v_re]

	// Step 3: V3 += V5 * w_im → [re*wre - im*wim, im*wre + re*wim]
	VDUP	V0.D[1], V4.D2		// V4 = [w_im, w_im]
	VFMLA	V4.D2, V5.D2, V3.D2	// V3 += V5 * V4

	// ── Butterfly: u+v, u-v ──────────────────────────────────
	VFADD_2D(3, 1, 4)		// V4 = u + v
	VFSUB_2D(3, 1, 5)		// V5 = u - v

	// Store results
	VST1	[V4.D2], (R10)		// x[i+j] = u + v
	VST1	[V5.D2], (R11)		// x[i+j+half] = u - v

	// Advance pointers
	ADD	$16, R10, R10
	ADD	$16, R11, R11
	SUB	$1, R13, R13
	B	inner

next_group:
	// R10 is now at x[i+half]. Skip to x[i+size] = R10 + half*16.
	ADD	R6, R10, R10
	ADD	R5, R9, R9		// i += size
	B	outer

done:
	RET
