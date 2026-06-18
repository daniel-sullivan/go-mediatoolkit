// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// AAC-LC ENCODE analysis filterbank: the forward (analysis) MDCT. This is the
// encoder's first DSP stage — it turns a block of time-domain INT_PCM samples
// into the FIXP_DBL MDCT spectrum (per window: 1 long spectrum of frameLen
// lines, or 8 short spectra of frameLen/8 lines) with the overlap-aware
// fold+window and the shared inner DCT-IV.
//
// Ported 1:1 from:
//   - libFDK/src/mdct.cpp:135  mdct_block            -> mdctBlockFwd
//   - libAACenc/src/transform.cpp:117 FDKaacEnc_Transform_Real -> transformReal
//
// Integer parity: libfdk-aac's encoder is fixed-point. FIXP_PCM == FIXP_SGL (int16,
// because SAMPLE_BITS == 16, machine_type.h:226-230) and FIXP_DBL == int32
// Q-format spectral data; the window slopes FIXP_WTP == FIXP_SPK == packed int16
// (fixSTP) under the active WINDOWTABLE_16BIT config. The fold is integer
// shifts (DFRACT_BITS - SAMPLE_BITS - 1 == 15) plus the int64-product>>32
// fMultDiv2 family, and the inner dct_IV is the same integer transform shared
// with the decoder. No `float`/`double` and no transcendental appear on this
// path, so it is bit-identical regardless of -ffp-contract / vectorization and
// asserts EXACT int32 equality with no aac_strict gate (cf. the integer-kernel
// note in nativeaac.go).

// Window block types, a 1:1 port of the WINDOW_TYPE enum (psy_const.h:120-125).
const (
	longWindowEnc  = 0
	startWindowEnc = 1
	shortWindowEnc = 2
	stopWindowEnc  = 3
)

// Window shapes, a 1:1 port of the WINDOW_SHAPE enum (psy_const.h:130-133).
const (
	sineWindowShapeEnc = 0
	kbdWindowShapeEnc  = 1
	lolWindowShapeEnc  = 2
)

// mdctBlockFwd computes the forward (analysis) MDCT, a 1:1 port of mdct_block
// (libFDK/src/mdct.cpp:135-270). It writes nSpec spectra of tl lines each into
// mdctData from noInSamples time samples in timeData (INT_PCM == int16). The
// fold uses the left window slope carried in hMdct (prevWrs/prevFr from the
// previous block — the MDCT is stateful across blocks) and the right window
// slope pRightWindowPart/fr supplied for this block; the persistent fields are
// then updated. pMdctData_e receives the per-spectrum block exponent (SHORT).
//
// SAMPLE_BITS == 16 != DFRACT_BITS (== 32), so the two "A is zero" / "D is zero"
// folds take the #else branch: -(FIXP_DBL)sample << (DFRACT_BITS-SAMPLE_BITS-1)
// == -(int32(sample)) << 15. The windowed folds use fMultDiv2((FIXP_PCM)sample,
// coeff) == fMultDiv2DS(int32(sample), coeff). Returns nSpec*tl.
func mdctBlockFwd(hMdct *mdctT, timeData []int16, noInSamples int, mdctData []int32,
	nSpec, tl int, pRightWindowPart []fixSTP, fr int, pMdctData_e []int16) int {
	// const shift for SAMPLE_BITS(16) != DFRACT_BITS(32): DFRACT_BITS-SAMPLE_BITS-1
	const pcmShift = dfractBits - 16 - 1 // == 15

	wrs := pRightWindowPart

	// Detect FRprevious / FL mismatches and override parameters accordingly
	// (mdct.cpp:151-157). At start just initialize and pass parameters as-is.
	if hMdct.prevFr == 0 {
		hMdct.prevFr = fr
		hMdct.prevWrs = wrs
		hMdct.prevTl = tl
	}

	// Derive NR (mdct.cpp:160).
	nr := (tl - fr) >> 1

	// Skip input samples if tl is smaller than block size (mdct.cpp:163).
	tdOff := (noInSamples - tl) >> 1

	mdOff := 0

	for n := 0; n < nSpec; n++ {
		// MDCT scale: +1 fMultDiv2() in windowing, +1 Princen-Bradley factor
		// 1/2 (mdct.cpp:172).
		mdctData_e := 1 + 1

		// Derive left parameters (mdct.cpp:175-177).
		wls := hMdct.prevWrs
		fl := hMdct.prevFr
		nl := (tl - fl) >> 1

		// 0(A)-Br fold for the all-zero-A region (mdct.cpp:183-190, #else).
		for i := 0; i < nl; i++ {
			mdctData[mdOff+(tl/2)+i] = -int32(timeData[tdOff+tl-i-1]) << pcmShift
		}

		// A*window - Br*window (mdct.cpp:208-214). Both timeData (FIXP_PCM ==
		// FIXP_SGL) and the window coeff (FIXP_WTP == FIXP_SGL) are int16, so the
		// fMultDiv2 / fMultSubDiv2 overloads resolve to the SS form:
		// fixmuldiv2_SS(a,b) == (LONG)a*b (fixmul.h:159), a plain 32-bit product
		// (NOT the int64>>32 DD kernel); fMultSubDiv2(x,a,b) == x - that product
		// (common_fix.h:361 -> fixmsubdiv2_SS).
		for i := 0; i < fl/2; i++ {
			tmp0 := int32(timeData[tdOff+i+nl]) * int32(wls[i].im) // a*window (SS)
			mdctData[mdOff+(tl/2)+i+nl] =
				tmp0 - int32(timeData[tdOff+tl-nl-i-1])*int32(wls[i].re) // A*window-Br*window
		}

		// -C flipped, all-zero-D region (mdct.cpp:221-230, #else).
		for i := 0; i < nr; i++ {
			mdctData[mdOff+(tl/2)-1-i] = -int32(timeData[tdOff+tl+i]) << pcmShift
		}

		// -(C*window + Dr*window), flipped (mdct.cpp:246-254). Same SS resolution:
		// fMultAddDiv2(x,a,b) == x + (LONG)a*b (common_fix.h:323 -> fixmadddiv2_SS).
		for i := 0; i < fr/2; i++ {
			tmp1 := int32(timeData[tdOff+tl+nr+i]) * int32(wrs[i].re) // C*window (SS)
			mdctData[mdOff+(tl/2)-nr-i-1] =
				-(tmp1 + int32(timeData[tdOff+(tl*2)-nr-i-1])*int32(wrs[i].im)) // -(Cr+D)
		}

		// Pass the shortened folded data (-D-Cr,A-Br) to the MDCT (mdct.cpp:257).
		dctIVForward(mdctData[mdOff:], tl, &mdctData_e)

		pMdctData_e[n] = int16(mdctData_e)

		tdOff += tl
		mdOff += tl

		hMdct.prevWrs = wrs
		hMdct.prevFr = fr
		hMdct.prevTl = tl
	}

	return nSpec * tl
}

// dctIVForward wraps the shared dctIV the way the encoder's dct_IV(pDat, L,
// pDat_e) entry point (dct.cpp:371-383) does: it selects the radix-2 twiddle /
// sin_twiddle ROM via dct_getTables, then runs the in-place DCT-IV. The decode
// path pre-fetches the tables in dctTablesRadix2; the forward path is identical,
// so this reuses it. *pDatE carries the block exponent (dct_IV adds +2).
func dctIVForward(pDat []int32, L int, pDatE *int) {
	twiddle, sinTwiddle, sinStep := dctTablesRadix2(L)
	dctIV(pDat, L, sinStep, twiddle, sinTwiddle, pDatE)
}

// transformReal computes the encoder analysis filterbank for one channel's
// block, a 1:1 port of FDKaacEnc_Transform_Real (libAACenc/src/transform.cpp:
// 117-170). It selects numSpec/numMdctLines and the right window slope length fr
// from blockType, runs the forward MDCT, validates that the 8 short-block
// exponents agree, and publishes the block exponent in *pMdctDataE and the new
// prevWindowShape. filterType is ignored on the AAC-LC (non-ELD) path. Returns 0
// on success, -1 on a short-block exponent mismatch or an invalid blockType.
//
// pTimeData length is frameLength; mdctData receives frameLength FIXP_DBL lines.
// prevWindowShape is read by the next call (carried by the caller). mdctPers is
// the channel's persistent MDCT state.
func transformReal(pTimeData []int16, mdctData []int32, blockType, windowShape int,
	prevWindowShape *int, mdctPers *mdctT, frameLength int, pMdctDataE *int) int {
	var numSpec, numMdctLines, offset, fr int

	if blockType == shortWindowEnc {
		numSpec = 8
		numMdctLines = frameLength >> 3
	} else {
		numSpec = 1
		numMdctLines = frameLength
	}

	if windowShape == lolWindowShapeEnc {
		offset = (frameLength * 3) >> 2
	} else {
		offset = 0
	}

	switch blockType {
	case longWindowEnc, stopWindowEnc:
		fr = frameLength - offset
	case startWindowEnc, shortWindowEnc:
		fr = frameLength >> 3
	default:
		return -1
	}

	var mdctData_e [8]int16

	mdctBlockFwd(mdctPers, pTimeData, frameLength, mdctData, numSpec, numMdctLines,
		fdkGetWindowSlopeEnc(fr, windowShape), fr, mdctData_e[:])

	if blockType == shortWindowEnc {
		if !(mdctData_e[0] == mdctData_e[1] && mdctData_e[1] == mdctData_e[2] &&
			mdctData_e[2] == mdctData_e[3] && mdctData_e[3] == mdctData_e[4] &&
			mdctData_e[4] == mdctData_e[5] && mdctData_e[5] == mdctData_e[6] &&
			mdctData_e[6] == mdctData_e[7]) {
			return -1
		}
	}
	*prevWindowShape = windowShape
	*pMdctDataE = int(mdctData_e[0])

	return 0
}

// fdkGetWindowSlopeEnc selects the right window slope for the encoder analysis
// MDCT, the FDKgetWindowSlope(length, shape) call in FDKaacEnc_Transform_Real
// (transform.cpp:155). On the AAC-LC path length is the radix-2 fr (frameLength
// 1024 or frameLength>>3 == 128) and shape is SINE(0) or KBD(1), so this reuses
// the shared radix-2 selector.
func fdkGetWindowSlopeEnc(length, shape int) []fixSTP {
	return getWindowSlopeRadix2(length, uint8(shape))
}
