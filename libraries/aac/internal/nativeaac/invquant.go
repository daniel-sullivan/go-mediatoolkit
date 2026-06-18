// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file is the 1:1 Go port of the inverse-quantization area of the
// vendored Fraunhofer FDK-AAC decoder — the per-value and per-band kernels
// that map a quantized spectral line back to its rescaled fixed-point
// magnitude (libAACdec/src/block.h and libAACdec/src/block.cpp).
//
// The whole area is integer/fixed-point (FIXP_DBL is a Q1.31 value carried in
// an int32; INT64 intermediates are int64), so it is bit-identical regardless
// of build and carries no aac_strict FP gate. The only fenced concern is the
// FDK-AAC license, hence the aacfdk build tag. Every ported function names its
// C counterpart as file:line; the algorithm is translated faithfully and is
// not "improved".
//
// FIXP_DBL maps to int32 (DFRACT_BITS == 32). The C reference's LONG is a
// platform long, but the fixed-point contract treats every value as 32-bit
// (the masks 0x80000000 / 0x0FFF, the 32-bit shifts, and the `>> 32` in
// fMultDiv2 from an INT64 product all assume 32-bit operands), so int32 is the
// faithful Go width.

// The FIXP_DBL*FIXP_DBL divide-by-two multiply fMultDiv2(LONG, LONG)
// (common_fix.h:248) forwards to fixmuldiv2_DD (libFDK/include/fixmul.h:131),
// ported as fMultDiv2DD in fixmul.go; CntLeadingZeros(x) (common_fix.h:308) /
// fNormz(FIXP_DBL) (common_fix.h:292) resolve to fixnormz_D
// (libFDK/include/clz.h:152), ported as fixnormzD in tns_scale.go. Both are
// reused here.

// fixabsD is the 1:1 port of fixabs_D (libFDK/include/abs.h:121); fAbs(FIXP_DBL)
// (common_fix.h:275) and fixp_abs (common_fix.h:305) resolve to this.
func fixabsD(x int32) int32 {
	if x > 0 {
		return x
	}
	return -x
}

// fixmaxD is the 1:1 port of fixmax<FIXP_DBL> (libFDK/include/fixminmax.h:118);
// fMax(FIXP_DBL, FIXP_DBL) (common_fix.h:401) resolves to this.
func fixmaxD(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// scaleValueInPlace is the 1:1 port of scaleValueInPlace
// (libFDK/include/scale.h:217): multiply *value by 2^scalefactor in place,
// arithmetic-shifting left for a non-negative factor and right otherwise.
func scaleValueInPlace(value *int32, scalefactor int32) {
	if newscale := scalefactor; newscale >= 0 {
		*value <<= uint(newscale)
	} else {
		*value >>= uint(-newscale)
	}
}

// evaluatePower43 is the 1:1 port of EvaluatePower43 (libAACdec/src/block.h:247):
// compute 2^(lsb/4) * (*pValue)^(4/3) for a single quantized line, writing the
// mantissa back to *pValue and returning its exponent. It interpolates
// inverseQuantTable for the (4/3)-power and applies the lsb gain from
// mantissaTable / exponentTable.
func evaluatePower43(pValue *int32, lsb uint32) int32 {
	value := *pValue
	freeBits := uint32(fixnormzD(value))
	exponent := uint32(dfractBits) - freeBits
	// FDK_ASSERT(exponent < 14)

	x := uint32((value << freeBits) >> 19)
	tableIndex := (x & 0x0FFF) >> 4

	x = x & 0x0F

	r0 := uint32(inverseQuantTable[tableIndex+0])
	r1 := uint32(inverseQuantTable[tableIndex+1])
	nx := uint16(16 - x)
	temp := r0*uint32(nx) + r1*x
	invQVal := int32(temp)

	// FDK_ASSERT(lsb < 4)
	*pValue = fMultDiv2DD(invQVal, mantissaTable[lsb][exponent])

	// + 1 compensates fMultDiv2().
	return int32(exponentTable[lsb][exponent]) + 1
}

// getScaleFromValue is the 1:1 port of GetScaleFromValue (libAACdec/src/block.h:283):
// determine the required shift scale for the given quantized value and lsb. It
// returns 0 for a zero value (scaling a zero is pointless and avoids overshift).
func getScaleFromValue(value int32, lsb uint32) int32 {
	if value != 0 {
		scale := evaluatePower43(&value, lsb)
		return fixnormzD(value) - scale - 2
	}
	return 0
}

// inverseQuantizeBand is the 1:1 port of InverseQuantizeBand
// (libAACdec/src/block.cpp:436): inverse quantize one scalefactor band in place
// per spectrum[i] = Sign(spectrum[i]) * Mantissa(spectrum[i])^(4/3) * 2^(lsb/4),
// using the precomputed inverseQuantTabler / mantissaTabler / exponentTabler
// rows for this lsb and the band's headroom scale.
func inverseQuantizeBand(spectrum []int32, inverseQuantTabler []int32, mantissaTabler []int32, exponentTabler []int8, noLines int, scale int32) {
	scale = scale + 1 // +1 to compensate fMultDiv2 shift-right in loop

	ptr := 0
	for i := noLines; i != 0; i-- {
		signedValue := spectrum[ptr]
		ptr++
		if signedValue != 0 {
			value := fixabsD(signedValue)
			freeBits := uint32(fixnormzD(value))
			exponent := uint32(32) - freeBits

			x := uint32(value) << freeBits
			x <<= 1 // shift out sign bit to avoid masking later on
			tableIndex := x >> 24
			x = (x >> 20) & 0x0F

			r0 := uint32(inverseQuantTabler[tableIndex+0])
			r1 := uint32(inverseQuantTabler[tableIndex+1])
			temp := (r1-r0)*x + (r0 << 4)

			value = fMultDiv2DD(int32(temp), mantissaTabler[exponent])

			// + 1 compensates fMultDiv2()
			scaleValueInPlace(&value, scale+int32(exponentTabler[exponent]))

			if signedValue < 0 {
				signedValue = -value
			} else {
				signedValue = value
			}
			spectrum[ptr-1] = signedValue
		}
	}
}

// maxabsD is the 1:1 port of maxabs_D (libAACdec/src/block.cpp:471): find the
// maximum absolute spectral-line value across the first noLines lines of the
// current scalefactor band.
func maxabsD(spectralCoefficient []int32, noLines int) int32 {
	locMax := int32(0)
	for i := noLines; i > 0; {
		i--
		locMax = fixmaxD(fixabsD(spectralCoefficient[i]), locMax)
	}
	return locMax
}
