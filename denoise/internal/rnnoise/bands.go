package rnnoise

import "math"

// Band analysis, ported from librnnoise/src/denoise.c
// (compute_band_energy, compute_band_corr, interp_band_gain, dct) plus
// the eband20ms band-edge table. Every multiply-accumulate is routed
// through the strict primitives to match the -ffp-contract=off oracle.

// eband20ms are the ERB-ish band edges (denoise.c const int
// eband20ms[NB_BANDS+2]).
var eband20ms = [NBBands + 2]int{
	0, 2, 4, 6, 8, 10, 12, 15, 18, 21, 24, 28, 32, 36, 41, 47, 53, 60, 68,
	77, 87, 98, 110, 124, 140, 157, 176, 198, 223, 251, 282, 317, 356, 400,
}

// dct2over22 is sqrt(2./22), computed once at init in float64 exactly as
// the C dct() does per call (math.Sqrt is correctly rounded == C sqrt).
// The 22 is a vestigial 22-band constant kept verbatim (NB_BANDS is 32).
var dct2over22 = math.Sqrt(2.0 / 22.0)

// computeBandEnergy is denoise.c compute_band_energy: triangular-band
// power of the spectrum X into bandE[NB_BANDS].
func computeBandEnergy(bandE []float32, X []fftCpx) {
	var sum [NBBands + 2]float32
	for i := 0; i < NBBands+1; i++ {
		bandSize := eband20ms[i+1] - eband20ms[i]
		for j := 0; j < bandSize; j++ {
			frac := float32(j) / float32(bandSize)
			r := X[eband20ms[i]+j].r
			im := X[eband20ms[i]+j].i
			tmp := add32(mul32(r, r), mul32(im, im))
			sum[i] = add32(sum[i], mul32(sub32(1, frac), tmp))
			sum[i+1] = add32(sum[i+1], mul32(frac, tmp))
		}
	}
	sum[1] = mul32(add32(sum[0], sum[1]), 2) / 3
	sum[NBBands] = mul32(add32(sum[NBBands], sum[NBBands+1]), 2) / 3
	for i := 0; i < NBBands; i++ {
		bandE[i] = sum[i+1]
	}
}

// computeBandCorr is denoise.c compute_band_corr: triangular-band
// cross-power of X and P into bandE[NB_BANDS].
func computeBandCorr(bandE []float32, X, P []fftCpx) {
	var sum [NBBands + 2]float32
	for i := 0; i < NBBands+1; i++ {
		bandSize := eband20ms[i+1] - eband20ms[i]
		for j := 0; j < bandSize; j++ {
			frac := float32(j) / float32(bandSize)
			idx := eband20ms[i] + j
			tmp := add32(mul32(X[idx].r, P[idx].r), mul32(X[idx].i, P[idx].i))
			sum[i] = add32(sum[i], mul32(sub32(1, frac), tmp))
			sum[i+1] = add32(sum[i+1], mul32(frac, tmp))
		}
	}
	sum[1] = mul32(add32(sum[0], sum[1]), 2) / 3
	sum[NBBands] = mul32(add32(sum[NBBands], sum[NBBands+1]), 2) / 3
	for i := 0; i < NBBands; i++ {
		bandE[i] = sum[i+1]
	}
}

// interpBandGain is denoise.c interp_band_gain: linearly interpolate the
// per-band gains onto the FreqSize bins. It writes bins [0, eband20ms
// [NB_BANDS+1]=400) only; the C memset over FREQ_SIZE *bytes* is fully
// overwritten by these loops, and bins [400, FreqSize) are left as the
// caller's (zero-initialised) input — so g must be a zeroed FreqSize
// slice on entry, matching the C callers.
func interpBandGain(g []float32, bandE []float32) {
	for i := 1; i < NBBands; i++ {
		bandSize := eband20ms[i+1] - eband20ms[i]
		for j := 0; j < bandSize; j++ {
			frac := float32(j) / float32(bandSize)
			g[eband20ms[i]+j] = add32(mul32(sub32(1, frac), bandE[i-1]), mul32(frac, bandE[i]))
		}
	}
	for j := 0; j < eband20ms[1]; j++ {
		g[j] = bandE[0]
	}
	for j := eband20ms[NBBands]; j < eband20ms[NBBands+1]; j++ {
		g[j] = bandE[NBBands-1]
	}
}

// dct is denoise.c dct: the (vestigially 22-band-scaled) DCT of in
// [NB_BANDS] into out[NB_BANDS]. out[i] = (sum_j in[j]*table[j*NB+i]) *
// sqrt(2./22), the final scale computed in float64 like the C.
func dct(out, in []float32) {
	for i := 0; i < NBBands; i++ {
		var sum float32
		for j := 0; j < NBBands; j++ {
			sum = add32(sum, mul32(in[j], rnnDctTable[j*NBBands+i]))
		}
		out[i] = float32(float64(sum) * dct2over22)
	}
}
