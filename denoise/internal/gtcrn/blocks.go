package gtcrn

import "math"

// convTranspose2dFreq is an F-axis ConvTranspose2d (temporal kernel 1)
// that upsamples frequency by sF: weight [inC, outC/groups, 1, kF],
// stride sF, symmetric F-crop padF. Fout = (Fin-1)*sF + kF - 2*padF.
func convTranspose2dFreq(in []float32, inC, Fin int, W, bias []float32, outC, kF, sF, padF, groups int) (out []float32, Fout int) {
	Fout = (Fin-1)*sF + kF - 2*padF
	out = make([]float32, outC*Fout)
	inPerG, outPerG := inC/groups, outC/groups
	for o := 0; o < outC; o++ {
		for f := 0; f < Fout; f++ {
			out[o*Fout+f] = bias[o]
		}
	}
	for g := 0; g < groups; g++ {
		for il := 0; il < inPerG; il++ {
			ic := g*inPerG + il
			for ol := 0; ol < outPerG; ol++ {
				oc := g*outPerG + ol
				wbase := ic*(outPerG*kF) + ol*kF
				for fin := 0; fin < Fin; fin++ {
					v := in[ic*Fin+fin]
					for kf := 0; kf < kF; kf++ {
						f := fin*sF + kf - padF
						if f >= 0 && f < Fout {
							out[oc*Fout+f] += W[wbase+kf] * v
						}
					}
				}
			}
		}
	}
	return out, Fout
}

// tra applies Temporal Recurrent Attention in place: an energy-per-frame
// GRU whose sigmoid output gates x across frequency. x is [C*F]; hcache
// is the carried GRU hidden (16), updated in place.
func tra(x []float32, C, F int, w traW, hcache []float32) {
	zt := make([]float32, C)
	for c := 0; c < C; c++ {
		var s float32
		for f := 0; f < F; f++ {
			v := x[c*F+f]
			s += v * v
		}
		zt[c] = s / float32(F)
	}
	gruUniStep(zt, hcache, w.gruW, w.gruR, w.gruB, encC, C)
	at := linear(hcache, w.fcW, w.fcB, encC, C)
	for c := 0; c < C; c++ {
		g := sigmoid32(at[c])
		for f := 0; f < F; f++ {
			x[c*F+f] *= g
		}
	}
}

// shuffle recombines the ShuffleNet halves: out[2c]=h1[c], out[2c+1]=x2[c].
// h1, x2 are [C*F]; out is [2C*F].
func shuffle(h1, x2 []float32, C, F int) []float32 {
	out := make([]float32, 2*C*F)
	for c := 0; c < C; c++ {
		copy(out[(2*c)*F:(2*c+1)*F], h1[c*F:(c+1)*F])
		copy(out[(2*c+1)*F:(2*c+2)*F], x2[c*F:(c+1)*F])
	}
	return out
}

// gtConvBlockEnc runs an encoder GTConvBlock (fused Conv+BN, streaming
// depth Conv2d). x is [16*F]; returns [16*F].
func (m *Model) gtConvBlockEnc(x []float32, w gtEncW, pos int) []float32 {
	F := dpF
	x1 := x[:8*F]
	x2 := x[8*F:]
	x1s := sfeUnfold(x1, 8, F) // (24,F)
	h := conv1x1(x1s, 24, F, w.pc1W, w.pc1B, 16)
	prelu(h, w.pc1Slope)
	cache := m.convCacheSlice(0, w.dil)
	h, nc := depthConvStream(h, cache, 16, F, w.dil, w.depthW, w.depthB)
	m.putConvCache(0, w.dil, nc)
	prelu(h, w.depthSlope)
	h = conv1x1(h, 16, F, w.pc2W, w.pc2B, 8) // (8,F), no act
	tra(h, 8, F, w.tra, m.traCacheSlice(0, pos))
	return shuffle(h, x2, 8, F)
}

// gtConvBlockDec runs a decoder GTConvBlock (ConvTranspose + explicit BN,
// streaming depth ConvTranspose). x is [16*F]; returns [16*F].
func (m *Model) gtConvBlockDec(x []float32, w gtDecW, pos int) []float32 {
	F := dpF
	x1 := x[:8*F]
	x2 := x[8*F:]
	x1s := sfeUnfold(x1, 8, F) // (24,F)
	h := convT1x1(x1s, 24, F, w.pc1W, w.pc1B, 16)
	batchNorm(h, 16, F, w.pbn1.g, w.pbn1.b, w.pbn1.mean, w.pbn1.variance)
	prelu(h, w.pc1Slope)
	cache := m.convCacheSlice(1, w.dil)
	h, nc := depthConvTransposeStream(h, cache, 16, F, w.dil, w.depthW, w.depthB)
	m.putConvCache(1, w.dil, nc)
	batchNorm(h, 16, F, w.dbn.g, w.dbn.b, w.dbn.mean, w.dbn.variance)
	prelu(h, w.depthSlope)
	h = convT1x1(h, 16, F, w.pc2W, w.pc2B, 8)
	batchNorm(h, 8, F, w.pbn2.g, w.pbn2.b, w.pbn2.mean, w.pbn2.variance)
	tra(h, 8, F, w.tra, m.traCacheSlice(1, pos))
	return shuffle(h, x2, 8, F)
}

// dpgrnn runs one Grouped Dual-Path RNN: bidirectional grouped GRU over
// frequency (intra, uncached) then unidirectional grouped GRU over time
// (inter, cached), each with a residual Linear + LayerNorm. x is [16*33]
// ([c*33+f]); returns [16*33].
func (m *Model) dpgrnn(x []float32, w dpgrnnW, idx int) []float32 {
	F, C := dpF, encC
	// (C,F) -> (F,C)
	xfc := make([]float32, F*C)
	for c := 0; c < C; c++ {
		for f := 0; f < F; f++ {
			xfc[f*C+c] = x[c*F+f]
		}
	}

	// Intra: bidirectional grouped GRU over the F=33 sequence.
	x1seq := make([]float32, F*8)
	x2seq := make([]float32, F*8)
	for f := 0; f < F; f++ {
		copy(x1seq[f*8:f*8+8], xfc[f*C:f*C+8])
		copy(x2seq[f*8:f*8+8], xfc[f*C+8:f*C+16])
	}
	y1 := gruBiSeq(x1seq, w.i1W, w.i1R, w.i1B, 4, 8, F) // (F,8)
	y2 := gruBiSeq(x2seq, w.i2W, w.i2R, w.i2B, 4, 8, F)
	intra := make([]float32, F*C)
	for f := 0; f < F; f++ {
		pre := make([]float32, C)
		copy(pre[:8], y1[f*8:f*8+8])
		copy(pre[8:], y2[f*8:f*8+8])
		copy(intra[f*C:], linear(pre, w.intraFCW, w.intraFCB, C, C))
	}
	layerNorm(intra, F, C, w.intraLNg, w.intraLNb)
	for i := range intra {
		intra[i] += xfc[i] // residual (intra_out)
	}

	// Inter: unidirectional grouped GRU over time (seq=1), cached per freq.
	hcache := m.interCacheSlice(idx)
	inter := make([]float32, F*C)
	h1new := make([]float32, 8)
	h2new := make([]float32, 8)
	for f := 0; f < F; f++ {
		xf := intra[f*C : f*C+C]
		hs := hcache[f*C : f*C+C]
		gruCell(xf[:8], hs[:8], w.t1W, w.t1R, w.t1B, 8, 8, h1new)
		gruCell(xf[8:], hs[8:], w.t2W, w.t2R, w.t2B, 8, 8, h2new)
		copy(hs[:8], h1new)
		copy(hs[8:], h2new)
		pre := make([]float32, C)
		copy(pre[:8], h1new)
		copy(pre[8:], h2new)
		copy(inter[f*C:], linear(pre, w.interFCW, w.interFCB, C, C))
	}
	layerNorm(inter, F, C, w.interLNg, w.interLNb)
	for i := range inter {
		inter[i] += intra[i] // residual (inter_out)
	}

	// (F,C) -> (C,F)
	out := make([]float32, C*F)
	for c := 0; c < C; c++ {
		for f := 0; f < F; f++ {
			out[c*F+f] = inter[f*C+c]
		}
	}
	return out
}

// addMaps returns a+b elementwise.
func addMaps(a, b []float32) []float32 {
	out := make([]float32, len(a))
	for i := range a {
		out[i] = a[i] + b[i]
	}
	return out
}

// Forward enhances one STFT frame (specRe, specImag each [257]) and
// advances the three caches. It returns the enhanced frame as [514]
// interleaved re,im per bin ([re0,im0,re1,im1,…]) — the ONNX enh layout.
func (m *Model) Forward(specRe, specImag []float32) []float32 {
	return m.forward(specRe, specImag, nil)
}

// EnhanceOffline runs the full pipeline on a 16 kHz mono signal: the
// center STFT, a per-frame Forward from fresh caches, and the ISTFT
// overlap-add. It resets the caches first. Returns the enhanced signal
// (center-trimmed, length (frames-1)*Hop). Used for E2E PCM parity; the
// public streaming engine drives Forward frame by frame instead.
func (m *Model) EnhanceOffline(x []float32) []float32 {
	m.Reset()
	re, im, frames := STFT(x)
	outRe := make([]float32, Bins*frames)
	outIm := make([]float32, Bins*frames)
	specRe := make([]float32, Bins)
	specIm := make([]float32, Bins)
	for f := 0; f < frames; f++ {
		for k := 0; k < Bins; k++ {
			specRe[k] = re[k*frames+f]
			specIm[k] = im[k*frames+f]
		}
		enh := m.Forward(specRe, specIm)
		for k := 0; k < Bins; k++ {
			outRe[k*frames+f] = enh[k*2]
			outIm[k*frames+f] = enh[k*2+1]
		}
	}
	return ISTFT(outRe, outIm, frames)
}

// forward is Forward with an optional per-block capture hook (keyed by
// the blocks_golden.json names) for parity gating.
func (m *Model) forward(specRe, specImag []float32, capture func(name string, v []float32)) []float32 {
	cap := func(name string, v []float32) {
		if capture != nil {
			capture(name, v)
		}
	}
	w := m.w

	// Feature: [mag, real, imag] each 257.
	feat := make([]float32, 3*nfreqs)
	for k := 0; k < nfreqs; k++ {
		r, i := specRe[k], specImag[k]
		feat[k] = float32(math.Sqrt(float64(r*r+i*i) + 1e-12))
		feat[nfreqs+k] = r
		feat[2*nfreqs+k] = i
	}

	e := erbBM(feat, 3, nfreqs, erbLow, erbBand, w.erbFC) // (3,129)
	cap("erb_bm", e)
	x := sfeUnfold(e, 3, erbF) // (9,129)
	cap("sfe", x)

	// Encoder.
	x0, _ := conv2dFreq(x, 9, erbF, w.enc[0].w, w.enc[0].b, encC, 5, 2, 2, 1) // (16,65)
	prelu(x0, w.enc[0].slope)
	cap("en_out0", x0)
	x1, _ := conv2dFreq(x0, encC, 65, w.enc[1].w, w.enc[1].b, encC, 5, 2, 2, 2) // (16,33)
	prelu(x1, w.enc[1].slope)
	cap("en_out1", x1)
	x2 := m.gtConvBlockEnc(x1, w.gtEnc[0], 0)
	cap("en_out2", x2)
	x3 := m.gtConvBlockEnc(x2, w.gtEnc[1], 1)
	cap("en_out3", x3)
	x4 := m.gtConvBlockEnc(x3, w.gtEnc[2], 2)
	cap("en_out4", x4)

	// DPGRNN stack.
	y := m.dpgrnn(x4, w.dp[0], 0)
	cap("dpgrnn1", y)
	y = m.dpgrnn(y, w.dp[1], 1)
	cap("dpgrnn2", y)

	// Decoder (skip-added to encoder outputs in reverse).
	d0 := m.gtConvBlockDec(addMaps(y, x4), w.gtDec[0], 0)
	d1 := m.gtConvBlockDec(addMaps(d0, x3), w.gtDec[1], 1)
	d2 := m.gtConvBlockDec(addMaps(d1, x2), w.gtDec[2], 2)

	d3in := addMaps(d2, x1)
	d3, _ := convTranspose2dFreq(d3in, encC, dpF, w.dec[0].w, w.dec[0].b, encC, 5, 2, 2, 2) // (16,65)
	batchNorm(d3, encC, 65, w.dec[0].bn.g, w.dec[0].bn.b, w.dec[0].bn.mean, w.dec[0].bn.variance)
	prelu(d3, w.dec[0].slope)

	d4in := addMaps(d3, x0)
	mfeat, _ := convTranspose2dFreq(d4in, encC, 65, w.dec[1].w, w.dec[1].b, 2, 5, 2, 2, 1) // (2,129)
	batchNorm(mfeat, 2, erbF, w.dec[1].bn.g, w.dec[1].bn.b, w.dec[1].bn.mean, w.dec[1].bn.variance)
	for i := range mfeat {
		mfeat[i] = tanh32(mfeat[i]) // is_last activation
	}
	cap("m_feat", mfeat)

	// ERB split back to the complex ratio mask (2,257).
	mask := erbBS(mfeat, 2, nfreqs, erbLow, erbBand, w.ierbFC)
	cap("mask", mask)

	// Apply the complex ratio mask to the reference spectrum.
	enh := make([]float32, nfreqs*2)
	for k := 0; k < nfreqs; k++ {
		mr, mi := mask[k], mask[nfreqs+k]
		r, i := specRe[k], specImag[k]
		enh[k*2] = r*mr - i*mi
		enh[k*2+1] = i*mr + r*mi
	}
	cap("enh", enh)
	return enh
}
