package gtcrn

// Pure-Go forward kernels for the GTCRN port. All accumulation is fixed
// sequential float32 (the parity discipline in VERSION); any raw product
// later consumed by a +/- must itself round through float32, which the
// explicit float32 intermediates below enforce. A single streaming call
// carries T=1, so a feature map is stored as [C*F]float32 with element
// (c, f) at index c*F + f.

// linearHigh applies a bias-free Linear (matmul) to the high-frequency
// tail of each channel: out[f_out] = sum_k in[k] * w[k*outF + f_out],
// where w is the ONNX MatMul initializer of shape [inF, outF] (row-major).
// This is the ERB forward/inverse band map (erb_fc / ierb_fc).
func linearHigh(in []float32, w []float32, inF, outF int) []float32 {
	out := make([]float32, outF)
	for o := 0; o < outF; o++ {
		var acc float32
		for k := 0; k < inF; k++ {
			acc += in[k] * w[k*outF+o]
		}
		out[o] = acc
	}
	return out
}

// erbBM performs the ERB band merge (erb.bm): for each of the C channels,
// keep the low erbLow bins and map the high (nfreqs-erbLow) bins through
// erbFC [inHigh, erbBands] to erbBands, concatenated. in is [C*nfreqs],
// out is [C*(erbLow+erbBands)].
func erbBM(in []float32, C, nfreqs, erbLow, erbBands int, erbFC []float32) []float32 {
	inHigh := nfreqs - erbLow
	outF := erbLow + erbBands
	out := make([]float32, C*outF)
	for c := 0; c < C; c++ {
		src := in[c*nfreqs:]
		dst := out[c*outF:]
		copy(dst[:erbLow], src[:erbLow])
		high := linearHigh(src[erbLow:nfreqs], erbFC, inHigh, erbBands)
		copy(dst[erbLow:outF], high)
	}
	return out
}

// erbBS performs the ERB band split (erb.bs, the inverse): keep low
// erbLow bins, map the erbBands high bands through ierbFC [erbBands,
// outHigh] back to outHigh bins. in is [C*(erbLow+erbBands)], out is
// [C*nfreqs].
func erbBS(in []float32, C, nfreqs, erbLow, erbBands int, ierbFC []float32) []float32 {
	inF := erbLow + erbBands
	outHigh := nfreqs - erbLow
	out := make([]float32, C*nfreqs)
	for c := 0; c < C; c++ {
		src := in[c*inF:]
		dst := out[c*nfreqs:]
		copy(dst[:erbLow], src[:erbLow])
		high := linearHigh(src[erbLow:inF], ierbFC, erbBands, outHigh)
		copy(dst[erbLow:nfreqs], high)
	}
	return out
}

// sfeUnfold is Sub-band Feature Extraction: an Unfold with kernel (1,3),
// stride 1, F-padding 1, then reshape to (C*3, F). Output channel c*3+k
// at frequency f gathers input channel c at f-1+k (zero outside [0,F)).
// in is [C*F], out is [(C*3)*F].
func sfeUnfold(in []float32, C, F int) []float32 {
	out := make([]float32, C*3*F)
	for c := 0; c < C; c++ {
		for k := 0; k < 3; k++ {
			oc := c*3 + k
			for f := 0; f < F; f++ {
				src := f - 1 + k
				if src >= 0 && src < F {
					out[oc*F+f] = in[c*F+src]
				}
			}
		}
	}
	return out
}

// prelu applies x -> x>=0 ? x : slope*x in place (per-tensor scalar slope,
// the [1,1,1] PReLU parameter).
func prelu(x []float32, slope float32) {
	for i, v := range x {
		if v < 0 {
			x[i] = slope * v
		}
	}
}

// conv2dFreq is a Conv2d with temporal kernel 1 over a single time frame:
// weight [outC, inC/groups, 1, kF], stride sF over frequency, symmetric
// F-padding padF, grouped by groups. in is [inC*F], out is [outC*Fout]
// with Fout = (F + 2*padF - kF)/sF + 1.
func conv2dFreq(in []float32, inC, F int, weight, bias []float32, outC, kF, sF, padF, groups int) (out []float32, Fout int) {
	Fout = (F+2*padF-kF)/sF + 1
	out = make([]float32, outC*Fout)
	inPerG := inC / groups
	outPerG := outC / groups
	for o := 0; o < outC; o++ {
		g := o / outPerG
		wbase := o * inPerG * kF
		for fo := 0; fo < Fout; fo++ {
			acc := bias[o]
			base := fo*sF - padF
			for ig := 0; ig < inPerG; ig++ {
				ic := g*inPerG + ig
				wrow := weight[wbase+ig*kF:]
				irow := in[ic*F:]
				for k := 0; k < kF; k++ {
					fi := base + k
					if fi >= 0 && fi < F {
						acc += wrow[k] * irow[fi]
					}
				}
			}
			out[o*Fout+fo] = acc
		}
	}
	return out, Fout
}
