//go:build amd64

#include "textflag.h"

// func butterflyPass(x []complex128, tw []complex128, half, step int)
//
// SSE2 complex butterfly. For each group of `size=half*2` elements:
//   for j := 0; j < half; j++:
//     w := tw[j*step]
//     u := x[i+j]
//     v := x[i+j+half] * w  (complex multiply)
//     x[i+j]      = u + v
//     x[i+j+half] = u - v
//
// Complex multiply uses the SSE2 technique:
//   result = (v_re*w_re - v_im*w_im, v_re*w_im + v_im*w_re)
//   1. Broadcast w_re, multiply: [v_re*w_re, v_im*w_re]
//   2. Swap v lanes, negate first, multiply by w_im: [-v_im*w_im, v_re*w_im]
//   3. Add results.
//
// Stack layout:
//   x_base+0(FP) x_len+8(FP) x_cap+16(FP)
//   tw_base+24(FP) tw_len+32(FP) tw_cap+40(FP)
//   half+48(FP) step+56(FP)
TEXT ·butterflyPass(SB), NOSPLIT, $0-64
	MOVQ	x_base+0(FP), SI	// x base
	MOVQ	x_len+8(FP), R8	// n = len(x)
	MOVQ	tw_base+24(FP), DX	// tw base
	MOVQ	half+48(FP), R9		// half
	MOVQ	step+56(FP), R10	// step

	// Byte offsets.
	SHLQ	$4, R9			// R9 = half * 16 (half_bytes)
	MOVQ	R9, R14			// save half_bytes for later
	SHLQ	$4, R10			// R10 = step * 16 (tw_stride_bytes)
	MOVQ	R9, R11
	SHLQ	$1, R11			// R11 = size * 16 (group_stride_bytes)

	// Build sign mask for complex multiply: [-0.0, +0.0] in X15.
	// Flipping sign of lane 0 via XOR with 0x8000...0000.
	XORPD	X15, X15
	MOVQ	$0x8000000000000000, AX
	MOVQ	AX, X15			// X15 = [-0.0, 0.0]

	// Outer loop: groups.
	MOVQ	SI, R12			// R12 = current group base (&x[i])
	XORQ	R13, R13		// R13 = element counter (i)
	SHLQ	$4, R8			// R8 = n * 16 (total bytes)

outer:
	CMPQ	R13, R8
	JGE	done

	// R12 = &x[i], R14 = half_bytes
	LEAQ	(R12)(R14*1), BX	// BX = &x[i+half]
	MOVQ	DX, CX			// CX = tw pointer (reset per group)
	MOVQ	R14, AX
	SHRQ	$4, AX			// AX = half (element count for inner loop)

inner:
	TESTQ	AX, AX
	JZ	next_group

	// Load twiddle w = [w_re, w_im]
	MOVUPD	(CX), X0
	ADDQ	R10, CX		// tw_ptr += step_bytes

	// Load u = x[i+j]
	MOVUPD	(R12), X1		// X1 = [u_re, u_im]
	// Load v_raw = x[i+j+half]
	MOVUPD	(BX), X2		// X2 = [v_re, v_im]

	// ── Complex multiply: X5 = X2 * X0 ──────────────────
	// Step 1: broadcast w_re, multiply
	MOVAPD	X0, X3
	SHUFPD	$0, X3, X3		// X3 = [w_re, w_re]
	MOVAPD	X2, X5
	MULPD	X3, X5			// X5 = [v_re*w_re, v_im*w_re]

	// Step 2: swap v, negate first lane, multiply by w_im
	SHUFPD	$1, X2, X2		// X2 = [v_im, v_re] (swap)
	XORPD	X15, X2			// X2 = [-v_im, v_re] (negate lane 0)
	MOVAPD	X0, X4
	SHUFPD	$3, X4, X4		// X4 = [w_im, w_im]
	MULPD	X4, X2			// X2 = [-v_im*w_im, v_re*w_im]

	// Step 3: add
	ADDPD	X2, X5			// X5 = complex product

	// ── Butterfly ────────────────────────────────────────
	MOVAPD	X1, X6
	ADDPD	X5, X6			// X6 = u + v
	SUBPD	X5, X1			// X1 = u - v

	MOVUPD	X6, (R12)		// x[i+j] = u + v
	MOVUPD	X1, (BX)		// x[i+j+half] = u - v

	ADDQ	$16, R12
	ADDQ	$16, BX
	DECQ	AX
	JMP	inner

next_group:
	// R12 is now at x[i+half]. Skip to x[i+size].
	ADDQ	R14, R12
	ADDQ	R11, R13		// i += size_bytes
	JMP	outer

done:
	RET
