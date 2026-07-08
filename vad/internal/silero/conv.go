package silero

// conv1dK3 is the encoder convolution (VERSION step 2): 1-D
// convolution with kernel 3, zero padding 1, and the given stride,
// over row-major [inC × inT] input (time minor), producing
// [outC × outT] with outT = (inT−1)/stride + 1. w is [outC × inC × 3]
// row-major, b is [outC]. relu applies the encoder's ReLU in place.
// Accumulation is fixed sequential float32: bias first, then input
// channels in order, kernel taps in order — one deterministic order
// everywhere, so the port never depends on a summation-order
// coincidence.
func conv1dK3(x []float32, inC, inT int, w, b []float32, outC, stride int, out []float32) (outT int) {
	outT = (inT-1)/stride + 1
	for oc := 0; oc < outC; oc++ {
		wBase := oc * inC * 3
		for ot := 0; ot < outT; ot++ {
			t0 := ot*stride - 1
			acc := b[oc]
			for ic := 0; ic < inC; ic++ {
				xRow := x[ic*inT : (ic+1)*inT]
				wRow := w[wBase+ic*3 : wBase+ic*3+3]
				for k := 0; k < 3; k++ {
					if t := t0 + k; t >= 0 && t < inT {
						acc += wRow[k] * xRow[t]
					}
				}
			}
			out[oc*outT+ot] = acc
		}
	}
	return outT
}

// relu clamps negatives to zero in place.
func relu(v []float32) {
	for i, x := range v {
		if x < 0 {
			v[i] = 0
		}
	}
}
