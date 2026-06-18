// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Exported views of the shared fixed-point kernels and ROM the SBR QMF
// filterbank (internal/nativeaac/sbr) builds on. The QMF is a distinct subsystem
// living in its own package, but it reuses — bit-for-bit, never re-ports — the
// libFDK DCT/DST transforms (dct.go, which themselves call the fft.go /
// fft_hardcoded.go kernels) and the FIXP_DBL/FIXP_SGL multiply + scale
// primitives (fixmul.go / tns_apply.go / mdct.go) that the AAC-LC core already
// ports. These thin wrappers add no logic; they keep ONE coherent definition of
// each kernel/ROM in this package and grant the sbr package access without
// duplicating any static symbol. Only the symbols the QMF analysis/synthesis HQ
// STD path actually uses are exposed; later SBR batches extend this surface.
//
// All values are int32 FIXP_DBL (Q-format) / int16 FIXP_SGL (Q1.15); every
// kernel here is a pure integer kernel (cf. the integer-kernel note in
// nativeaac.go), so EXACT-integer parity holds in any build.

// --- Fixed-point multiply / scale primitives -------------------------------

// StcNarrow narrows a Q1.31 hex constant to FIXP_SGL (Q1.15) via the C
// FX_DBL2FXCONST_SGL macro. Exposes stcNarrow so the SBR ROM (QFC/QTC/WTCP
// narrowing) and its parity oracle materialise the same Q1.15 values.
func StcNarrow(val int32) int16 { return stcNarrow(val) }

// Fl2fxconstSGL materialises a float literal to FIXP_SGL (Q1.15) via the
// FL2FXCONST_SGL macro (common_fix.h:179), bit-for-bit as the C compiler folds
// it. The SBR decode ROM (sbr_rom.cpp's FL2FXCONST_SGL tables: limGains_m,
// smoothFilter, randomPhase, limiterBandsPerOctaveDiv4) is built through this.
func Fl2fxconstSGL(val float64) int16 { return fl2fxconstSGL(val) }

// Fl2fxconstDBL materialises a float literal to FIXP_DBL (Q1.31) via the
// FL2FXCONST_DBL macro (common_fix.h:191). The SBR whFactorsTable /
// limiterBandsPerOctaveDiv4_DBL FIXP_DBL tables are built through this.
func Fl2fxconstDBL(val float64) int32 { return fl2fxconstDBL(val) }

// Fl2fxconstSGLf / Fl2fxconstDBLf are the f-suffixed (single-precision source)
// forms of the above: the C FL2FXCONST_*(x) macro casts (double)(val), so an
// f-suffixed literal (e.g. FL2FXCONST_DBL(1.2f / 4.0f) in sbr_rom.cpp) is FIRST
// rounded to float, THEN widened — which differs in the low mantissa bits from
// the double literal. The SBR ROM materialises each table with the form matching
// its C literal's suffix, so the result is bit-identical to the C compiler.
func Fl2fxconstSGLf(val float32) int16 { return fl2fxconstSGLf(val) }
func Fl2fxconstDBLf(val float32) int32 { return fl2fxconstDBLf(val) }

// FMultDiv2DS is the FIXP_DBL*FIXP_SGL product scaled down by 2 (fixmuldiv2_DS).
// The QMF analysis prototype FIR (qmfAnaPrototypeFirSlot) folds the FIXP_QAS
// states by the FIXP_PFT prototype taps through this.
func FMultDiv2DS(a int32, b int16) int32 { return fMultDiv2DS(a, b) }

// FMultDiv2SD is the FIXP_SGL*FIXP_DBL product scaled down by 2 (fixmuldiv2_SD).
// On this platform fixmuldiv2_SD(a,b) == fixmuldiv2_DD((INT)(a<<16), b), which is
// commutative in the int64 product with fixmuldiv2_DS — so it equals
// fMultDiv2DS(b, a). The synthesis FIR's final sta[8] tap uses fMultDiv2(SGL,DBL).
func FMultDiv2SD(a int16, b int32) int32 { return fMultDiv2DS(b, a) }

// FMultDS is the full-scale FIXP_DBL*FIXP_SGL product (fixmul_DS). The synthesis
// FIR's optional output-gain multiply uses fMult(FIXP_DBL, FIXP_SGL).
func FMultDS(a int32, b int16) int32 { return fMultDS(a, b) }

// FMultAddDiv2SD computes x + 0.5*a*b for FIXP_SGL a, FIXP_DBL b
// (fMultAddDiv2(FIXP_DBL, FIXP_SGL, FIXP_DBL), common_fix.h:317). The synthesis
// FIR MAC chain uses this (p_filter taps are FIXP_SGL, the slot data FIXP_DBL).
func FMultAddDiv2SD(x int32, a int16, b int32) int32 { return x + fMultDiv2DS(b, a) }

// ScaleValue multiplies value by 2^scalefactor with a logical-by-sign shift
// (scaleValue, scale.h:153). qmfAdaptFilterStates uses it for a negative shift.
func ScaleValue(value, scalefactor int32) int32 { return scaleValue(value, scalefactor) }

// ScaleValuesSaturateDst is already exported by mdct_parity_export.go (the
// AAC-LC FrequencyToTime tail uses it); the QMF inverse-modulation band scaling
// reuses that one — no second definition here.

// ScaleValuesSaturateInPlace multiplies vector by 2^scalefactor with saturation
// (scaleValuesSaturate(FIXP_DBL*, INT, INT), scale.cpp:222). qmfAdaptFilterStates
// uses it for a positive shift.
func ScaleValuesSaturateInPlace(vector []int32, length int, scalefactor int32) {
	scaleValuesSaturateInPlace(vector, length, scalefactor)
}

// SaturateRightShift is the SATURATE_RIGHT_SHIFT(src, scale, 32) macro
// (scale.h:241): an arithmetic right shift saturating to ±MAXVAL_DBL. The QMF
// synthesis FIR slot (qmfSynPrototypeFirSlot) uses it for PCM formatting.
func SaturateRightShift(src int32, scale uint) int32 {
	const maxvalDBL = int32(0x7FFFFFFF)
	v := src >> scale
	if v > maxvalDBL {
		return maxvalDBL
	}
	if v < ^maxvalDBL {
		return ^maxvalDBL
	}
	return v
}

// SaturateLeftShift is the SATURATE_LEFT_SHIFT(src, scale, 32) macro
// (scale.h:250): a left shift saturating to ±MAXVAL_DBL.
func SaturateLeftShift(src int32, scale uint) int32 {
	const maxvalDBL = int32(0x7FFFFFFF)
	thresh := maxvalDBL >> scale
	if src > thresh {
		return maxvalDBL
	}
	if src < ^thresh {
		return ^maxvalDBL
	}
	return src << scale
}

// --- DCT / DST transforms (with QMF ROM selection) -------------------------

// QmfDctIV runs the in-place DCT-IV of length L over pDat for the QMF forward
// modulation real part / inverse modulation real part, returning the exponent
// delta. twiddleFlat is the SineWindowL FIXP_WTP ROM and sinTwiddleFlat the
// SineTable1024 FIXP_STP ROM that dct_getTables selects for L (sinStep its
// sin_step). Wraps dctIV — the same kernel the AAC-LC filterbank uses, here
// driven with the QMF's L==64 ROM.
func QmfDctIV(pDat []int32, L, sinStep int, twiddleFlat, sinTwiddleFlat []int16) int {
	e := 0
	dctIV(pDat, L, sinStep, packFixSTP(twiddleFlat), packFixSTP(sinTwiddleFlat), &e)
	return e
}

// QmfDstIV runs the in-place DST-IV of length L over pDat for the QMF forward
// modulation imaginary part / inverse modulation imaginary part. Wraps dstIV.
func QmfDstIV(pDat []int32, L, sinStep int, twiddleFlat, sinTwiddleFlat []int16) int {
	e := 0
	dstIV(pDat, L, sinStep, packFixSTP(twiddleFlat), packFixSTP(sinTwiddleFlat), &e)
	return e
}

// SineTable1024RawFlat returns the genuine narrowed SineTable1024 as flat int16
// [re,im,...] (513 entries) — the FIXP_STP sin_twiddle the QMF L==64 DCT uses.
// Reuses the ported AAC-LC ROM (aac_rom_filterbank.go).
func SineTable1024RawFlat() []int16 { return flatFixSTP(sineTable1024[:]) }

// Fft runs the in-place fft() dispatcher over pInput (interleaved complex,
// length 2*length) and returns the scalefactor accumulated. Exposes fft so the
// sbr-qmf parity oracle can pin the hard-coded fft_16/fft_32 kernels — the leaf
// the QMF L==64 DCT routes through at M==32 — directly against the genuine
// fft(length, ...).
func Fft(length int, pInput []int32) int {
	sc := 0
	fft(length, pInput, &sc)
	return sc
}
