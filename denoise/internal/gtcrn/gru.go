package gtcrn

import "math"

// GRU kernels in the ONNX layout (VERSION trap 1): gates ordered z,r,h
// in the 3*hidden weight axis, linear_before_reset=1, opset-11 default
// activations (sigmoid, tanh). Weights are consumed verbatim from the
// onnx GRU initializers, so no PyTorch↔ONNX gate permutation is needed.

func sigmoid32(x float32) float32 { return float32(1.0 / (1.0 + math.Exp(-float64(x)))) }
func tanh32(x float32) float32    { return float32(math.Tanh(float64(x))) }

// gruCell advances one GRU step for a single direction.
//
//	W: [3*hid, in]  R: [3*hid, hid]  B: [6*hid]
//
// gate blocks in the 3*hid axis: z=[0:hid), r=[hid:2hid), h=[2hid:3hid).
// B = [Wbz,Wbr,Wbh, Rbz,Rbr,Rbh]. x:[in], h:[hid]; hNew is written into
// out (len hid) and returned.
func gruCell(x, h, W, R, B []float32, hid, in int, out []float32) []float32 {
	for j := 0; j < hid; j++ {
		zr := W[j*in:]       // z gate row j
		rr := W[(hid+j)*in:] // r gate row j
		zrec := R[j*hid:]
		rrec := R[(hid+j)*hid:]

		var zx, rx float32
		for k := 0; k < in; k++ {
			zx += zr[k] * x[k]
			rx += rr[k] * x[k]
		}
		for k := 0; k < hid; k++ {
			zx += zrec[k] * h[k]
			rx += rrec[k] * h[k]
		}
		zt := sigmoid32(zx + B[j] + B[3*hid+j])
		rt := sigmoid32(rx + B[hid+j] + B[4*hid+j])

		// linear_before_reset=1: reset gate multiplies the recurrent
		// contribution AFTER its own linear map (+ recurrent bias).
		hw := W[(2*hid+j)*in:]
		hrec := R[(2*hid+j)*hid:]
		var hx, rh float32
		for k := 0; k < in; k++ {
			hx += hw[k] * x[k]
		}
		for k := 0; k < hid; k++ {
			rh += hrec[k] * h[k]
		}
		ht := tanh32(hx + B[2*hid+j] + rt*(rh+B[5*hid+j]))
		out[j] = (1-zt)*ht + zt*h[j]
	}
	return out
}

// gruUniStep runs one unidirectional GRU step over one input vector,
// mutating h in place. W/R/B are the [1,·]-direction slices flattened.
func gruUniStep(x []float32, h, W, R, B []float32, hid, in int) {
	tmp := make([]float32, hid)
	gruCell(x, h, W, R, B, hid, in, tmp)
	copy(h, tmp)
}

// gruUniSeq runs a unidirectional GRU over a sequence xs (L steps, each
// [in]) from a fresh zero state, returning the per-step outputs
// [L*hid]. Used where the graph zero-initialises (the encoder TRA GRUs
// are cached; this is for the bidirectional intra path's construction).
func gruUniSeq(xs []float32, W, R, B []float32, hid, in, L int) []float32 {
	h := make([]float32, hid)
	out := make([]float32, L*hid)
	tmp := make([]float32, hid)
	for t := 0; t < L; t++ {
		gruCell(xs[t*in:(t+1)*in], h, W, R, B, hid, in, tmp)
		copy(h, tmp)
		copy(out[t*hid:], tmp)
	}
	return out
}

// gruBiSeq runs a bidirectional GRU over a sequence xs (L steps, each
// [in]) from fresh zero states. W/R/B carry both directions:
//
//	W: [2, 3*hid, in]  R: [2, 3*hid, hid]  B: [2, 6*hid]
//
// Output is [L*(2*hid)]: at each step, concat(forward_hid, backward_hid).
func gruBiSeq(xs []float32, W, R, B []float32, hid, in, L int) []float32 {
	wf, rf, bf := W[:3*hid*in], R[:3*hid*hid], B[:6*hid]
	wb, rb, bb := W[3*hid*in:], R[3*hid*hid:], B[6*hid:]

	fwd := gruUniSeq(xs, wf, rf, bf, hid, in, L)

	// Backward: iterate the sequence in reverse.
	hb := make([]float32, hid)
	back := make([]float32, L*hid)
	tmp := make([]float32, hid)
	for t := L - 1; t >= 0; t-- {
		gruCell(xs[t*in:(t+1)*in], hb, wb, rb, bb, hid, in, tmp)
		copy(hb, tmp)
		copy(back[t*hid:], tmp)
	}

	out := make([]float32, L*2*hid)
	for t := 0; t < L; t++ {
		copy(out[t*2*hid:], fwd[t*hid:(t+1)*hid])
		copy(out[t*2*hid+hid:], back[t*hid:(t+1)*hid])
	}
	return out
}
