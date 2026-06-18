// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Fixed-point inverse MDCT (the AAC-LC synthesis filterbank), a 1:1 port of the
// vendored libFDK mdct.cpp: imlt_block (FrequencyToTime) + imdct_gain +
// imdct_adapt_parameters + mdct_init + imdct_drain + imdct_copy_ov_and_nr. Every
// value is an int32 FIXP_DBL in Q-format and the overlap-add window coefficients
// are int16 FIXP_SGL (Q1.15) packed as FIXP_WTP == FIXP_SPK — mdct.cpp contains
// ZERO floats — so these kernels are bit-identical regardless of build/
// vectorization and carry no aac_strict FP gate (cf. the integer-kernel note in
// nativeaac.go).
//
// Data flow (mdct.cpp comment block, 421-464): each spectrum is DCT-IV'd (the
// no-alias core, dct.go), optionally gained/de-scaled, then folded against the
// 50%-overlap carry buffer (mdct_t.overlap.freq, the previous block's tail) via
// the window slope to produce time samples; the new block's left half is saved
// as the next overlap. The block exponent (the e in the (mantissa,exponent)
// pair) is carried exactly: imdct_gain folds the 2/N IMDCT gain and the MPEG-4
// output gain into transform_gain_e, dct_IV adds its +2 twiddling scale, and
// scaleValuesSaturate applies scalefactor[w]+specShiftScale before the fold.
//
// Scope: imlt_block is ported in full (all alias-symmetry / FAC-ZIR / asym-
// overlap branches) to stay 1:1 with the reference, though AAC-LC only ever
// drives flags==0 (currAliasSymmetry==0), gain==0, prevAliasSymmetry==0,
// pFacZir==NULL and pAsymOvlp==NULL — the USAC/LD-only state stays nil.

// MDCT_OUT_HEADROOM is the output additional headroom (mdct.h:108).
const mdctOutHeadroom = 2

// mdctOutputScale == MDCT_OUTPUT_SCALE (mdct.h:113) == -MDCT_OUT_HEADROOM +
// (DFRACT_BITS - PCM_OUT_BITS). PCM_OUT_BITS == DFRACT_BITS == 32, so the
// (DFRACT_BITS - PCM_OUT_BITS) term is 0 and this is -2.
const mdctOutputScale = -mdctOutHeadroom

// mdctOutputGain == MDCT_OUTPUT_GAIN (mdct.h:115).
const mdctOutputGain = 16

// mltFlagCurrAliasSymmetry == MLT_FLAG_CURR_ALIAS_SYMMETRY (mdct.h:122).
const mltFlagCurrAliasSymmetry = 1

// mdctT is the MDCT persistent data, a 1:1 port of mdct_t (mdct.h:136-155). The
// union overlap.freq/overlap.time is one FIXP_DBL slice here (both views alias
// the same backing store in C). prevWrs holds the previous right window slope
// (a FIXP_WTP == FIXP_SPK == fixSTP slice). pFacZir/pAsymOvlp are the USAC/LPD
// transition buffers, NULL on the AAC-LC path.
type mdctT struct {
	overlap []int32 // overlap.freq / overlap.time union

	prevWrs  []fixSTP // previous right window slope
	prevTl   int      // previous transform length
	prevNr   int      // previous right window offset
	prevFr   int      // previous right window slope length
	ovOffset int      // overlap time data fill level
	ovSize   int      // overlap buffer size in words

	prevAliasSymmetry     int
	prevPrevAliasSymmetry int

	pFacZir   []int32
	pAsymOvlp []int32
}

// imdctScaleDblLsh1 is IMDCT_SCALE_DBL_LSH1 (mdct.h:120) ==
// SATURATE_LEFT_SHIFT_ALT(x, 1, DFRACT_BITS), forwarding to the shared
// saturateLeftShiftAlt (tns_apply.go). IMDCT_SCALE_DBL(x) (mdct.h:119) is the
// identity, used inline.
func imdctScaleDblLsh1(x int32) int32 { return saturateLeftShiftAlt(x, 1) }

// fAddSaturate adds two FIXP_DBL with saturation, a 1:1 port of fAddSaturate
// (fixpoint_math.h:910-916). Each operand is >>1 then summed in 32-bit, clamped
// to [MINVAL_DBL>>1, MAXVAL_DBL>>1], then <<1.
//
//	sum = (LONG)(a >> 1) + (LONG)(b >> 1);
//	sum = fMax(fMin(sum, MAXVAL_DBL >> 1), MINVAL_DBL >> 1);
//	return (FIXP_DBL)(sum << 1);
func fAddSaturate(a, b int32) int32 {
	sum := (a >> 1) + (b >> 1)
	sum = fMax(fMin(sum, int32(0x7FFFFFFF)>>1), int32(-0x80000000)>>1)
	return sum << 1
}

// scaleValueSaturate multiplies value by 2^scalefactor with saturation, a 1:1
// port of scaleValueSaturate(FIXP_DBL, INT) (scale.h:180-205). It computes the
// signed headroom via fixnormz_D(value ^ (value>>31)) (range 1..32), then
// saturates on a left shift that would overflow, clears to 0 on a right shift
// that would underflow, and otherwise shifts; the lower clamp is MINVAL_DBL+1
// (0x80000001) via fMax. scalefactor is in range -31..+31.
//
//	int headroom = fixnormz_D((INT)value ^ (INT)(value >> 31)); // 1..32
//	if (scalefactor >= 0) {
//	  if (headroom <= scalefactor)
//	    return value > 0 ? MAXVAL_DBL : MINVAL_DBL + 1;
//	  return fMax(value << scalefactor, MINVAL_DBL + 1);
//	} else {
//	  scalefactor = -scalefactor;
//	  if ((DFRACT_BITS - headroom) <= scalefactor) return 0;
//	  return fMax(value >> scalefactor, MINVAL_DBL + 1);
//	}
func scaleValueSaturate(value, scalefactor int32) int32 {
	headroom := fixnormzD(value ^ (value >> 31)) // 1..32
	const minPlus1 = int32(-0x80000000) + 1      // 0x80000001
	if scalefactor >= 0 {
		if headroom <= scalefactor {
			if value > 0 {
				return int32(0x7FFFFFFF) // MAXVAL_DBL
			}
			return minPlus1
		}
		return fMax(value<<uint(scalefactor), minPlus1)
	}
	scalefactor = -scalefactor
	if (dfractBits - headroom) <= scalefactor {
		return 0
	}
	return fMax(value>>uint(scalefactor), minPlus1)
}

// scaleValuesSaturateInPlace multiplies vector[0:length] by 2^scalefactor with
// saturation, a 1:1 port of scaleValuesSaturate(FIXP_DBL*, INT, INT)
// (scale.cpp:222-237). A zero scalefactor is a no-op; otherwise the factor is
// clamped to [-(DFRACT_BITS-1), DFRACT_BITS-1] before the per-element
// scaleValueSaturate.
func scaleValuesSaturateInPlace(vector []int32, length int, scalefactor int32) {
	if scalefactor == 0 {
		return
	}
	scalefactor = fMax(fMin(scalefactor, dfractBits-1), -(dfractBits - 1))
	for i := 0; i < length; i++ {
		vector[i] = scaleValueSaturate(vector[i], scalefactor)
	}
}

// scaleValuesSaturateDst writes dst[0:length] = src[0:length] * 2^scalefactor
// with saturation, a 1:1 port of scaleValuesSaturate(FIXP_DBL*, const FIXP_DBL*,
// INT, INT) (scale.cpp:253-271). A zero scalefactor copies src verbatim;
// otherwise the factor is clamped before the per-element scaleValueSaturate. The
// AAC-LC FrequencyToTime tail (block.cpp:1240) drives this with
// MDCT_OUT_HEADROOM - aacOutDataHeadroom.
func scaleValuesSaturateDst(dst, src []int32, length int, scalefactor int32) {
	if scalefactor == 0 {
		copy(dst[:length], src[:length])
		return
	}
	scalefactor = fMax(fMin(scalefactor, dfractBits-1), -(dfractBits - 1))
	for i := 0; i < length; i++ {
		dst[i] = scaleValueSaturate(src[i], scalefactor)
	}
}

// mdctInit initialises an MDCT handle over the given overlap buffer, a 1:1 port
// of mdct_init (mdct.cpp:109-120). The overlap memory is NOT cleared here (the C
// leaves the FDKmemclear commented out; the caller zeroes it).
func mdctInit(hMdct *mdctT, overlap []int32, overlapBufferSize int) {
	hMdct.overlap = overlap
	hMdct.prevFr = 0
	hMdct.prevNr = 0
	hMdct.prevTl = 0
	hMdct.ovSize = overlapBufferSize
	hMdct.prevAliasSymmetry = 0
	hMdct.prevPrevAliasSymmetry = 0
	hMdct.pFacZir = nil
	hMdct.pAsymOvlp = nil
}

// imdctGain folds the 2/N IMDCT transform gain and the MPEG-4 part-3 output gain
// into the (mantissa, exponent) gain pair, a 1:1 port of imdct_gain
// (mdct.cpp:272-325). pGainM/pGainE are updated in place. For the radix-2 AAC-LC
// lengths (tl == 1024 / 128) the switch lands on case 0x4 (nothing to do), so
// gainM is untouched; the non-radix-2 amplitude-compensation cases (10ms / 3/4 /
// 5/16) are ported for fidelity but unused on this path.
//
//	gain_e += -MDCT_OUTPUT_GAIN - MDCT_OUT_HEADROOM + 1;
//	if (tl == 0) { *pGain_e = gain_e; return; }
//	log2_tl = DFRACT_BITS - 1 - fNormz((FIXP_DBL)tl);
//	gain_e += -log2_tl;
//	switch ((tl) >> (log2_tl - 2)) { ... }
func imdctGain(pGainM *int32, pGainE *int, tl int) {
	gainM := *pGainM
	gainE := *pGainE

	gainE += -mdctOutputGain - mdctOutHeadroom + 1
	if tl == 0 {
		// Don't regard the 2/N factor from the IDCT (compensated elsewhere).
		*pGainE = gainE
		return
	}

	log2tl := dfractBits - 1 - int(fixnormzD(int32(tl)))
	gainE += -log2tl

	// Detect non-radix-2 transform length and add the amplitude compensation
	// factor that cannot be folded into the exponent.
	switch tl >> uint(log2tl-2) {
	case 0x7: // 10 ms
		if gainM == 0 {
			gainM = fl2fxConstDBL053333
		} else {
			gainM = fMultDD(gainM, fl2fxConstDBL053333)
		}
	case 0x6: // 3/4 of radix 2
		if gainM == 0 {
			gainM = fl2fxConstDBL2over3
		} else {
			gainM = fMultDD(gainM, fl2fxConstDBL2over3)
		}
	case 0x5: // 5/16 of radix 2 (e.g. tl 160)
		if gainM == 0 {
			gainM = fl2fxConstDBL053333
		} else {
			gainM = fMultDD(gainM, fl2fxConstDBL053333)
		}
	case 0x4:
		// radix 2, nothing to do.
	default:
		// unsupported (FDK_ASSERT(0)) — unreachable on the AAC-LC path.
	}

	*pGainM = gainM
	*pGainE = gainE
}

// fl2fxConstDBL053333 == FL2FXCONST_DBL(0.53333333333333333333f), the 10ms /
// 5/16 amplitude-compensation mantissa (mdct.cpp:294,309). FL2FXCONST_DBL rounds
// val*2^31 to nearest (round half up), so 0.53333.. * 2^31 == 1145324612.27 ->
// 0x44444444.
const fl2fxConstDBL053333 = int32(0x44444444)

// fl2fxConstDBL2over3 == FL2FXCONST_DBL(2.0/3.0f), the 3/4-radix amplitude-
// compensation mantissa (mdct.cpp:302). 0.6666.. * 2^31 == 1431655765.33 ->
// 0x55555555.
const fl2fxConstDBL2over3 = int32(0x55555555)

// imdctDrain drains buffered output samples into output, a 1:1 port of
// imdct_drain (mdct.cpp:327-342). Returns the number of samples drained.
func imdctDrain(hMdct *mdctT, output []int32, nrSamplesRoom int) int {
	bufferedSamples := 0
	if nrSamplesRoom > 0 {
		bufferedSamples = hMdct.ovOffset
		if bufferedSamples > 0 {
			copy(output[:bufferedSamples], hMdct.overlap[:bufferedSamples])
			hMdct.ovOffset = 0
		}
	}
	return bufferedSamples
}

// imdctCopyOvAndNr copies overlap time-domain data into pTimeData without
// changing MDCT state, a 1:1 port of imdct_copy_ov_and_nr (mdct.cpp:344-370).
// Returns nt+nf, the count of copied samples (overlap time prefix + prev_nr tail
// of the freq overlap).
func imdctCopyOvAndNr(hMdct *mdctT, pTimeData []int32, nrSamples int) int {
	nt := fMinI(hMdct.ovOffset, nrSamples)
	nrSamples -= nt
	nf := fMinI(hMdct.prevNr, nrSamples)
	copy(pTimeData[:nt], hMdct.overlap[:nt])
	pt := nt

	pOvl := hMdct.ovSize - 1 // overlap.freq + ov_size - 1
	if hMdct.prevPrevAliasSymmetry == 0 {
		for i := 0; i < nf; i++ {
			x := -hMdct.overlap[pOvl]
			pOvl--
			pTimeData[pt] = x // IMDCT_SCALE_DBL(x)
			pt++
		}
	} else {
		for i := 0; i < nf; i++ {
			x := hMdct.overlap[pOvl]
			pOvl--
			pTimeData[pt] = x // IMDCT_SCALE_DBL(x)
			pt++
		}
	}

	return nt + nf
}

// imdctAdaptParameters adapts the MDCT window parameters for a mismatch between
// the previous right window slope and the current left slope, a 1:1 port of
// imdct_adapt_parameters (mdct.cpp:372-419). pfl/pnl are updated in place. On
// AAC-LC this only runs at startup (prev_tl == 0) and where slopes equal it is
// a no-op.
func imdctAdaptParameters(hMdct *mdctT, pfl, pnl *int, tl int, wls []fixSTP, noOutSamples int) {
	fl := *pfl
	nl := *pnl
	windowDiff := 0
	useCurrent := 0
	usePrevious := 0
	if hMdct.prevTl == 0 {
		hMdct.prevWrs = wls
		hMdct.prevFr = fl
		hMdct.prevNr = (noOutSamples - fl) >> 1
		hMdct.prevTl = noOutSamples
		hMdct.ovOffset = 0
		useCurrent = 1
	}

	windowDiff = (hMdct.prevFr - fl) >> 1

	// Can the previous window slope be adjusted to match the current slope?
	if hMdct.prevNr+windowDiff > 0 {
		useCurrent = 1
	}
	// Can the current window slope be adjusted to match the previous slope?
	if nl-windowDiff > 0 {
		usePrevious = 1
	}

	// If both are possible choose the larger of both window slope lengths.
	if useCurrent != 0 && usePrevious != 0 {
		if fl < hMdct.prevFr {
			useCurrent = 0
		}
	}
	// If the previous transform block is big enough enlarge the previous
	// window overlap, else shrink the current window overlap.
	if useCurrent != 0 {
		hMdct.prevNr += windowDiff
		hMdct.prevFr = fl
		hMdct.prevWrs = wls
	} else {
		nl -= windowDiff
		fl = hMdct.prevFr
	}

	*pfl = fl
	*pnl = nl
}

// dblPtr is a faithful model of a FIXP_DBL* roaming pointer: a backing slice
// plus an index. The C imlt_block writes through pOut0/pOut1 that point into
// EITHER the output buffer OR hMdct->overlap.time (the same FIXP_DBL[] under the
// union), advancing/retreating by element. Modelling them as (buf, idx) pairs
// keeps the port 1:1 with the pointer arithmetic.
type dblPtr struct {
	buf []int32
	idx int
}

// imltBlock performs nSpec inverse MLT transforms (frequency to time) with 50%
// overlap-add, a 1:1 port of imlt_block (mdct.cpp:465-727). It is the AAC-LC
// synthesis filterbank: each spectrum is DCT-IV'd (the no-alias core), de-scaled
// by scalefactor[w]+transform_gain_e, then folded against the overlap carry to
// emit time samples in output, and its left half saved as the next overlap.
// Returns the number of output samples written.
//
// twiddle/sinTwiddle are the genuine FIXP_WTP/FIXP_STP ROM dct_getTables selects
// for tl (supplied alongside wls/wrs so the production path and the parity
// oracle drive the identical ROM); sinStep is the selected sin_step.
//
// On AAC-LC: flags==0 (currAliasSymmetry==0), gain==0, all aliasSymmetry==0,
// pFacZir/pAsymOvlp==nil — so only the prevPrevAliasSymmetry==0 &&
// prevAliasSymmetry==0 && !pAsymOvlp folding branch runs. All other branches are
// ported for fidelity but unexercised here.
func imltBlock(hMdct *mdctT, output, spectrum []int32, scalefactor []int16,
	nSpec, noOutSamples, tl int, wls []fixSTP, fl int, wrs []fixSTP, fr int,
	gain int32, flags int, sinStep int, twiddle, sinTwiddle []fixSTP) int {

	pOut0 := dblPtr{buf: output, idx: 0}
	var pOut1 dblPtr
	nrSamples := 0
	specShiftScale := 0
	transformGainE := 0
	currAliasSymmetry := flags & mltFlagCurrAliasSymmetry

	// Derive NR and NL.
	nr := (tl - fr) >> 1
	nl := (tl - fl) >> 1

	// Include 2/N IMDCT gain into gain factor and exponent.
	imdctGain(&gain, &transformGainE, tl)

	// Detect FRprevious / FL mismatches and override parameters accordingly.
	if hMdct.prevFr != fl {
		imdctAdaptParameters(hMdct, &fl, &nl, tl, wls, noOutSamples)
	}

	// pOvl walks the freq overlap from the top down: overlap.freq + ov_size - 1.
	pOvl := dblPtr{buf: hMdct.overlap, idx: hMdct.ovSize - 1}

	if noOutSamples > nrSamples {
		// Purge buffered output.
		for i := 0; i < hMdct.ovOffset; i++ {
			pOut0.buf[pOut0.idx] = hMdct.overlap[i]
			pOut0.idx++
		}
		nrSamples = hMdct.ovOffset
		hMdct.ovOffset = 0
	}

	for w := 0; w < nSpec; w++ {
		// Detect FRprevious / FL mismatches and override parameters.
		if hMdct.prevFr != fl {
			imdctAdaptParameters(hMdct, &fl, &nl, tl, wls, noOutSamples)
		}

		specShiftScale = transformGainE

		// Setup window pointer (the previous right window slope).
		pWindow := hMdct.prevWrs

		// Current spectrum.
		pSpec := w * tl // index into spectrum

		// DCT IV of current spectrum.
		if currAliasSymmetry == 0 {
			if hMdct.prevAliasSymmetry == 0 {
				dctIV(spectrum[pSpec:], tl, sinStep, twiddle, sinTwiddle, &specShiftScale)
			} else {
				tmp := make([]int32, tl)
				dctIII(spectrum[pSpec:], tmp, tl, sinStep, sinTwiddle, &specShiftScale)
			}
		} else {
			if hMdct.prevAliasSymmetry == 0 {
				tmp := make([]int32, tl)
				dstIII(spectrum[pSpec:], tmp, tl, sinStep, sinTwiddle, &specShiftScale)
			} else {
				dstIV(spectrum[pSpec:], tl, sinStep, twiddle, sinTwiddle, &specShiftScale)
			}
		}

		// Optional scaling of (not yet windowed) time domain of current
		// spectrum, and de-scale the current spectrum signal.
		if gain != 0 {
			for i := 0; i < tl; i++ {
				spectrum[pSpec+i] = fMultDD(spectrum[pSpec+i], gain)
			}
		}

		{
			locScale := fMinI(int(scalefactor[w])+specShiftScale, dfractBits-1)
			scaleValuesSaturateInPlace(spectrum[pSpec:], tl, int32(locScale))
		}

		if noOutSamples <= nrSamples {
			// Divert output first half to overlap buffer if we already got
			// enough output samples.
			pOut0 = dblPtr{buf: hMdct.overlap, idx: hMdct.ovOffset}
			hMdct.ovOffset += hMdct.prevNr + fl/2
		} else {
			// Account output samples.
			nrSamples += hMdct.prevNr + fl/2
		}

		// NR output samples 0 .. NR. -overlap[TL/2..TL/2-NR].
		if hMdct.pFacZir != nil && hMdct.prevNr == fl/2 {
			// ACELP -> TCX20 -> FD short: add FAC ZIR on the nr signal part.
			for i := 0; i < hMdct.prevNr; i++ {
				x := -pOvl.buf[pOvl.idx]
				pOvl.idx--
				pOut0.buf[pOut0.idx] = fAddSaturate(x, hMdct.pFacZir[i]) // IMDCT_SCALE_DBL(pFacZir[i])
				pOut0.idx++
			}
			hMdct.pFacZir = nil
		} else {
			// Fold C/D from (-D-Cr) with D==0 here; pOut0 writes the C block.
			if hMdct.prevPrevAliasSymmetry == 0 {
				for i := 0; i < hMdct.prevNr; i++ {
					x := -pOvl.buf[pOvl.idx]
					pOvl.idx--
					pOut0.buf[pOut0.idx] = x // IMDCT_SCALE_DBL(x)
					pOut0.idx++
				}
			} else {
				for i := 0; i < hMdct.prevNr; i++ {
					x := pOvl.buf[pOvl.idx]
					pOvl.idx--
					pOut0.buf[pOut0.idx] = x // IMDCT_SCALE_DBL(x)
					pOut0.idx++
				}
			}
		}

		if noOutSamples <= nrSamples {
			// Divert output second half to overlap buffer.
			pOut1 = dblPtr{buf: hMdct.overlap, idx: hMdct.ovOffset + fl/2 - 1}
			hMdct.ovOffset += fl/2 + nl
		} else {
			pOut1 = dblPtr{buf: pOut0.buf, idx: pOut0.idx + (fl - 1)}
			nrSamples += fl/2 + nl
		}

		// output samples before/after window crossing point.
		// pCurr = pSpec + tl - fl/2.
		pCurr := pSpec + tl - fl/2

		if hMdct.prevPrevAliasSymmetry == 0 {
			if hMdct.prevAliasSymmetry == 0 {
				if hMdct.pAsymOvlp == nil {
					for i := 0; i < fl/2; i++ {
						// cplxMultDiv2(&x1, &x0, *pCurr, -*pOvl, pWindow[i])
						x1, x0 := cplxMultDiv2SGL(spectrum[pCurr], -pOvl.buf[pOvl.idx], pWindow[i].re, pWindow[i].im)
						pCurr++
						pOvl.idx--
						pOut0.buf[pOut0.idx] = imdctScaleDblLsh1(x0)
						pOut1.buf[pOut1.idx] = imdctScaleDblLsh1(-x1)
						pOut0.idx++
						pOut1.idx--
					}
				} else {
					pAsymOvl := fl/2 - 1 // index into pAsymOvlp
					for i := 0; i < fl/2; i++ {
						// x1 = -fMultDiv2(*pCurr, pWindow[i].v.re) + fMultDiv2(*pAsymOvl, pWindow[i].v.im)
						// x0 =  fMultDiv2(*pCurr, pWindow[i].v.im) - fMultDiv2(*pOvl,   pWindow[i].v.re)
						x1 := -fMultDiv2DS(spectrum[pCurr], pWindow[i].re) + fMultDiv2DS(hMdct.pAsymOvlp[pAsymOvl], pWindow[i].im)
						x0 := fMultDiv2DS(spectrum[pCurr], pWindow[i].im) - fMultDiv2DS(pOvl.buf[pOvl.idx], pWindow[i].re)
						pCurr++
						pOvl.idx--
						pAsymOvl--
						pOut0.buf[pOut0.idx] = imdctScaleDblLsh1(x0)
						pOut0.idx++
						pOut1.buf[pOut1.idx] = imdctScaleDblLsh1(x1)
						pOut1.idx--
					}
					hMdct.pAsymOvlp = nil
				}
			} else { // prevAliasSymmetry == 1
				for i := 0; i < fl/2; i++ {
					x1, x0 := cplxMultDiv2SGL(spectrum[pCurr], -pOvl.buf[pOvl.idx], pWindow[i].re, pWindow[i].im)
					pCurr++
					pOvl.idx--
					pOut0.buf[pOut0.idx] = imdctScaleDblLsh1(x0)
					pOut1.buf[pOut1.idx] = imdctScaleDblLsh1(x1)
					pOut0.idx++
					pOut1.idx--
				}
			}
		} else { // prevPrevAliasSymmetry == 1
			if hMdct.prevAliasSymmetry == 0 {
				for i := 0; i < fl/2; i++ {
					x1, x0 := cplxMultDiv2SGL(spectrum[pCurr], pOvl.buf[pOvl.idx], pWindow[i].re, pWindow[i].im)
					pCurr++
					pOvl.idx--
					pOut0.buf[pOut0.idx] = imdctScaleDblLsh1(x0)
					pOut1.buf[pOut1.idx] = imdctScaleDblLsh1(-x1)
					pOut0.idx++
					pOut1.idx--
				}
			} else { // prevAliasSymmetry == 1
				for i := 0; i < fl/2; i++ {
					x1, x0 := cplxMultDiv2SGL(spectrum[pCurr], pOvl.buf[pOvl.idx], pWindow[i].re, pWindow[i].im)
					pCurr++
					pOvl.idx--
					pOut0.buf[pOut0.idx] = imdctScaleDblLsh1(x0)
					pOut1.buf[pOut1.idx] = imdctScaleDblLsh1(x1)
					pOut0.idx++
					pOut1.idx--
				}
			}
		}

		if hMdct.pFacZir != nil {
			// Add FAC ZIR of a previous ACELP -> mdct transition.
			pOut := pOut0.idx - fl/2
			for i := 0; i < fl/2; i++ {
				pOut0.buf[pOut+i] = fAddSaturate(pOut0.buf[pOut+i], hMdct.pFacZir[i])
			}
			hMdct.pFacZir = nil
		}
		pOut0.idx += fl/2 + nl

		// NL output samples TL/2+FL/2..TL. -current[FL/2..0].
		pOut1.idx += fl/2 + 1
		pCurr = pSpec + tl - fl/2 - 1
		if hMdct.prevAliasSymmetry == 0 {
			for i := 0; i < nl; i++ {
				x := -spectrum[pCurr]
				pCurr--
				pOut1.buf[pOut1.idx] = x // IMDCT_SCALE_DBL(x)
				pOut1.idx++
			}
		} else {
			for i := 0; i < nl; i++ {
				x := spectrum[pCurr]
				pCurr--
				pOut1.buf[pOut1.idx] = x // IMDCT_SCALE_DBL(x)
				pOut1.idx++
			}
		}

		// Set overlap source pointer for next window: pOvl = pSpec + tl/2 - 1.
		pOvl = dblPtr{buf: spectrum, idx: pSpec + tl/2 - 1}

		// Previous window values.
		hMdct.prevNr = nr
		hMdct.prevFr = fr
		hMdct.prevTl = tl
		hMdct.prevWrs = wrs

		// Previous aliasing symmetry.
		hMdct.prevPrevAliasSymmetry = hMdct.prevAliasSymmetry
		hMdct.prevAliasSymmetry = currAliasSymmetry
	}

	// Save overlap: copy the last spectrum's left half into the top of the
	// freq overlap. pOvl = overlap.freq + ov_size - tl/2.
	copy(hMdct.overlap[hMdct.ovSize-tl/2:hMdct.ovSize], spectrum[(nSpec-1)*tl:(nSpec-1)*tl+tl/2])

	return nrSamples
}
