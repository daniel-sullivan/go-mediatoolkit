package gtcrn

import "math"

// linear applies y = x·W + bias, where W is the ONNX MatMul layout
// [inF, outF] (so y[o] = bias[o] + Σ_i x[i]·W[i*outF+o]). bias may be nil.
func linear(x, W, bias []float32, inF, outF int) []float32 {
	out := make([]float32, outF)
	for o := 0; o < outF; o++ {
		var acc float32
		if bias != nil {
			acc = bias[o]
		}
		for i := 0; i < inF; i++ {
			acc += x[i] * W[i*outF+o]
		}
		out[o] = acc
	}
	return out
}

// bnEps is the BatchNormalization epsilon (ONNX default, torch
// BatchNorm2d default 1e-5).
const bnEps = 1e-5

// batchNorm applies per-channel inference batch-norm in place over a
// [C*F] feature map: y = (x-mean)/sqrt(var+eps)·gamma + beta.
func batchNorm(x []float32, C, F int, gamma, beta, mean, variance []float32) {
	for c := 0; c < C; c++ {
		scale := gamma[c] / float32(math.Sqrt(float64(variance[c])+bnEps))
		shift := beta[c] - mean[c]*scale
		row := x[c*F:]
		for f := 0; f < F; f++ {
			row[f] = row[f]*scale + shift
		}
	}
}

// conv1x1 is a pointwise Conv2d: weight [outC, inC, 1, 1], in [inC*F],
// out[o][f] = bias[o] + Σ_i W[o*inC+i]·in[i*F+f].
func conv1x1(in []float32, inC, F int, W, bias []float32, outC int) []float32 {
	out := make([]float32, outC*F)
	for o := 0; o < outC; o++ {
		wr := W[o*inC:]
		for f := 0; f < F; f++ {
			acc := bias[o]
			for i := 0; i < inC; i++ {
				acc += wr[i] * in[i*F+f]
			}
			out[o*F+f] = acc
		}
	}
	return out
}

// convT1x1 is a pointwise ConvTranspose2d: weight [inC, outC, 1, 1]
// (transposed vs Conv), out[o][f] = bias[o] + Σ_i W[i*outC+o]·in[i*F+f].
func convT1x1(in []float32, inC, F int, W, bias []float32, outC int) []float32 {
	out := make([]float32, outC*F)
	for o := 0; o < outC; o++ {
		for f := 0; f < F; f++ {
			acc := bias[o]
			for i := 0; i < inC; i++ {
				acc += W[i*outC+o] * in[i*F+f]
			}
			out[o*F+f] = acc
		}
	}
	return out
}

// depthConvStream is the encoder GTConvBlock depth conv: a grouped
// (per-channel) Conv2d over the temporal cache. cache is [C*Tc*F] with
// Tc = 2*dilation prior frames (c-major, then time, then freq); cur is
// the current frame [C*F]. weight is [C,1,3,3] (index c*9+kt*3+kf), F is
// padded by 1. Returns the output frame [C*F] and the updated cache
// (last Tc frames of concat(cache, cur)).
func depthConvStream(cur, cache []float32, C, F, dil int, weight, bias []float32) (out, newCache []float32) {
	Tc := 2 * dil
	out = make([]float32, C*F)
	newCache = make([]float32, C*Tc*F)

	tap := func(c, t, f int) float32 { // buf value at channel c, time t (0..Tc), freq f
		if f < 0 || f >= F {
			return 0
		}
		if t < Tc {
			return cache[c*Tc*F+t*F+f]
		}
		return cur[c*F+f]
	}
	for c := 0; c < C; c++ {
		w := weight[c*9:]
		for f := 0; f < F; f++ {
			acc := bias[c]
			for kt := 0; kt < 3; kt++ {
				for kf := 0; kf < 3; kf++ {
					acc += w[kt*3+kf] * tap(c, kt*dil, f-1+kf)
				}
			}
			out[c*F+f] = acc
		}
		// New cache = concat(cache, cur)[1:], i.e. drop oldest T slice.
		for t := 0; t < Tc; t++ {
			for f := 0; f < F; f++ {
				newCache[c*Tc*F+t*F+f] = tap(c, t+1, f)
			}
		}
	}
	return out, newCache
}

// depthConvTransposeStream is the decoder GTConvBlock depth conv: a
// grouped ConvTranspose2d over the temporal cache. For stride 1 with the
// ONNX crop pads [(2*dil),1,(2*dil),1] this reduces to the encoder conv
// with the kernel accessed time- AND freq-reversed (the ConvTranspose
// transpose): out[c][f] = bias + Σ_{kt,kf} W[kt][kf]·buf[c][(2-kt)*dil][f+1-kf].
// Cache handling is identical to depthConvStream.
func depthConvTransposeStream(cur, cache []float32, C, F, dil int, weight, bias []float32) (out, newCache []float32) {
	Tc := 2 * dil
	out = make([]float32, C*F)
	newCache = make([]float32, C*Tc*F)

	tap := func(c, t, f int) float32 {
		if f < 0 || f >= F {
			return 0
		}
		if t < Tc {
			return cache[c*Tc*F+t*F+f]
		}
		return cur[c*F+f]
	}
	for c := 0; c < C; c++ {
		w := weight[c*9:]
		for f := 0; f < F; f++ {
			acc := bias[c]
			for kt := 0; kt < 3; kt++ {
				for kf := 0; kf < 3; kf++ {
					acc += w[kt*3+kf] * tap(c, (2-kt)*dil, f+1-kf)
				}
			}
			out[c*F+f] = acc
		}
		for t := 0; t < Tc; t++ {
			for f := 0; f < F; f++ {
				newCache[c*Tc*F+t*F+f] = tap(c, t+1, f)
			}
		}
	}
	return out, newCache
}

// lnEps is the DPGRNN LayerNorm epsilon (VERSION: 1e-8).
const lnEps = 1e-8

// layerNorm normalises x over the joint (F, C) axes (a single mean/var
// over all F*C elements), then scales by per-(f,c) gamma/beta. x, gamma,
// beta are [F*C] with index f*C+c. In place.
func layerNorm(x []float32, F, C int, gamma, beta []float32) {
	n := F * C
	var mean float32
	for i := 0; i < n; i++ {
		mean += x[i]
	}
	mean /= float32(n)
	var variance float32
	for i := 0; i < n; i++ {
		d := x[i] - mean
		variance += d * d
	}
	variance /= float32(n)
	inv := float32(1.0 / math.Sqrt(float64(variance)+lnEps))
	for i := 0; i < n; i++ {
		x[i] = (x[i]-mean)*inv*gamma[i] + beta[i]
	}
}
