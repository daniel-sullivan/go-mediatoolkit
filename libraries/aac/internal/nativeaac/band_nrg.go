// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Band/line energy calculations for the AAC encoder psychoacoustic model — a
// 1:1 port of the vendored FDK-AAC reference libAACenc/src/band_nrg.cpp. These
// kernels compute the per-scalefactor-band (SFB) MDCT energies the rest of the
// psy model and the quantizer target: the per-SFB max-scale (headroom) of the
// spectrum, the block-floating-point SFB energies (and their log2/ldData form),
// and the mid/side energies for M/S stereo.
//
// libfdk-aac ENCODE is FIXED-POINT: every value is an int32 FIXP_DBL in Q-format
// and every block carries its own exponent (sfbMaxScaleSpec / mdctScale). These
// kernels are entirely integer — leading-bit counts, arithmetic shifts, the
// int64-product fixmul kernels, and the table-driven fLog2 — so they are
// bit-identical regardless of vectorization, with no float and no
// transcendental. The block exponents are carried bit-for-bit.

// fPow2AddDiv2 is the 1:1 port of fPow2AddDiv2(FIXP_DBL x, FIXP_DBL a)
// (common_fix.h:327) == fixpadddiv2_D(x, a) == fixmadddiv2_DD(x, a, a)
// (fixmadd.h:309) == x + fMultDiv2(a, a): accumulate half of a^2 onto x. Used to
// build SFB energies as the running sum of squared (scaled) spectral lines.
func fPow2AddDiv2(x, a int32) int32 { return x + fMultDiv2(a, a) }

// FDKaacEnc_CalcSfbMaxScaleSpec computes, per band, the number of additional
// left shifts (headroom) the largest |line| in that band tolerates before
// overflow. C counterpart: FDKaacEnc_CalcSfbMaxScaleSpec, band_nrg.cpp:111.
//
//	for (i = 0; i < numBands; i++) {
//	  maxSpc = 0;
//	  for (j = bandOffset[i]; j < bandOffset[i+1]; j++)
//	    maxSpc = fixMax(maxSpc, fixp_abs(mdctSpectrum[j]));
//	  j = CntLeadingZeros(maxSpc) - 1;
//	  sfbMaxScaleSpec[i] = fixMin((DFRACT_BITS-2), j);
//	}
//
// fixp_abs -> fixabsD, fixMax -> fixmaxD, CntLeadingZeros -> fixnormzD,
// fixMin -> fixMin (int form). Note: for maxSpc == 0, fixnormzD(0) == 32, so
// j == 31 and the result clamps to DFRACT_BITS-2 == 30 (matches the C: the
// comment notes the >0 fast path but the generic fixnormz_D is exact here).
func fdkaacEncCalcSfbMaxScaleSpec(mdctSpectrum []int32, bandOffset, sfbMaxScaleSpec []int, numBands int) {
	for i := 0; i < numBands; i++ {
		var maxSpc int32
		for j := bandOffset[i]; j < bandOffset[i+1]; j++ {
			tmp := fixabsD(mdctSpectrum[j])
			maxSpc = fixmaxD(maxSpc, tmp)
		}
		j := int(fixnormzD(maxSpc)) - 1
		sfbMaxScaleSpec[i] = fixMin(dfractBits-2, j)
	}
}

// FDKaacEnc_CheckBandEnergyOptim computes the SFB energies and their ldData form
// for the first energy pass, returning the largest (rescaled) band energy used
// to choose a common block-floating-point shift. C counterpart:
// FDKaacEnc_CheckBandEnergyOptim, band_nrg.cpp:140.
//
//	for (i = 0; i < numBands; i++) {
//	  scale = fixMax(0, sfbMaxScaleSpec[i] - 4);
//	  tmp = 0;
//	  for (j = bandOffset[i]; j < bandOffset[i+1]; j++) {
//	    spec = mdctSpectrum[j] << scale;
//	    tmp = fPow2AddDiv2(tmp, spec);
//	  }
//	  bandEnergy[i] = tmp << 1;
//	  bandEnergyLdData[i] = CalcLdData(bandEnergy[i]);
//	  if (bandEnergyLdData[i] != FL2FXCONST_DBL(-1.0f))
//	    bandEnergyLdData[i] -= scale * FL2FXCONST_DBL(2.0/64);
//	  if (bandEnergyLdData[i] > maxNrgLd) { maxNrgLd = bandEnergyLdData[i]; nr = i; }
//	}
//	scale = fixMax(0, sfbMaxScaleSpec[nr] - 4);
//	scale = fixMax(2*(minSpecShift - scale), -(DFRACT_BITS-1));
//	maxNrg = scaleValue(bandEnergy[nr], scale);
//	return maxNrg;
//
// FL2FXCONST_DBL(2.0/64) is folded as fl2fxconstDBL(2.0/64); the `* scale`
// (int * FIXP_DBL) is a plain integer multiply matching the C.
func fdkaacEncCheckBandEnergyOptim(mdctSpectrum, bandEnergy, bandEnergyLdData []int32,
	sfbMaxScaleSpec, bandOffset []int, numBands, minSpecShift int) int32 {
	nr := 0
	maxNrgLd := fl2fxconstDBL(-1.0)
	for i := 0; i < numBands; i++ {
		scale := fixMax(0, sfbMaxScaleSpec[i]-4)
		var tmp int32
		for j := bandOffset[i]; j < bandOffset[i+1]; j++ {
			spec := mdctSpectrum[j] << uint(scale)
			tmp = fPow2AddDiv2(tmp, spec)
		}
		bandEnergy[i] = tmp << 1

		// calculate ld of bandNrg, subtract scaling
		bandEnergyLdData[i] = calcLdData(bandEnergy[i])
		if bandEnergyLdData[i] != fl2fxconstDBL(-1.0) {
			bandEnergyLdData[i] -= int32(scale) * fl2fxconstDBL(2.0/64)
		}
		// find index of maxNrg
		if bandEnergyLdData[i] > maxNrgLd {
			maxNrgLd = bandEnergyLdData[i]
			nr = i
		}
	}

	// return unscaled maxNrg
	scale := fixMax(0, sfbMaxScaleSpec[nr]-4)
	scale = fixMax(2*(minSpecShift-scale), -(dfractBits - 1))

	return scaleValue(bandEnergy[nr], int32(scale))
}

// FDKaacEnc_CalcBandEnergyOptimLong computes long-block SFB energies and ldData,
// rescaling them down to prevent overflow and returning the applied shift.
// C counterpart: FDKaacEnc_CalcBandEnergyOptimLong, band_nrg.cpp:192.
func fdkaacEncCalcBandEnergyOptimLong(mdctSpectrum, bandEnergy, bandEnergyLdData []int32,
	sfbMaxScaleSpec, bandOffset []int, numBands int) int {
	shiftBits := 0
	maxNrgLd := fl2fxconstDBL(0.0)

	for i := 0; i < numBands; i++ {
		leadingBits := sfbMaxScaleSpec[i] - 4 // max sfbWidth = 96 ; 2^7=128 => 7/2 = 4 (spc*spc)
		var tmp int32
		// don't use scaleValue() here, it increases workload quite sufficiently...
		if leadingBits >= 0 {
			for j := bandOffset[i]; j < bandOffset[i+1]; j++ {
				spec := mdctSpectrum[j] << uint(leadingBits)
				tmp = fPow2AddDiv2(tmp, spec)
			}
		} else {
			shift := -leadingBits
			for j := bandOffset[i]; j < bandOffset[i+1]; j++ {
				spec := mdctSpectrum[j] >> uint(shift)
				tmp = fPow2AddDiv2(tmp, spec)
			}
		}
		bandEnergy[i] = tmp << 1
	}

	// calculate ld of bandNrg, subtract scaling
	ldDataVector(bandEnergy, bandEnergyLdData, numBands)
	for i := numBands; i != 0; {
		i--
		scaleDiff := int32(sfbMaxScaleSpec[i]-4) * fl2fxconstDBL(2.0/64)
		if bandEnergyLdData[i] >= ((fl2fxconstDBL(-1.0) >> 1) + (scaleDiff >> 1)) {
			bandEnergyLdData[i] = bandEnergyLdData[i] - scaleDiff
		} else {
			bandEnergyLdData[i] = fl2fxconstDBL(-1.0)
		}
		// find maxNrgLd
		maxNrgLd = fixmaxD(maxNrgLd, bandEnergyLdData[i])
	}

	if maxNrgLd <= 0 {
		for i := numBands; i != 0; {
			i--
			scale := fixMin((sfbMaxScaleSpec[i]-4)<<1, dfractBits-1)
			bandEnergy[i] = scaleValue(bandEnergy[i], int32(-scale))
		}
		return 0
	}
	// scale down NRGs
	for maxNrgLd > fl2fxconstDBL(0.0) {
		maxNrgLd -= fl2fxconstDBL(2.0 / 64)
		shiftBits++
	}
	for i := numBands; i != 0; {
		i--
		scale := fixMin(((sfbMaxScaleSpec[i]-4)+shiftBits)<<1, dfractBits-1)
		bandEnergyLdData[i] -= int32(shiftBits) * fl2fxconstDBL(2.0/64)
		bandEnergy[i] = scaleValue(bandEnergy[i], int32(-scale))
	}
	return shiftBits
}

// FDKaacEnc_CalcBandEnergyOptimShort computes short-block SFB energies.
// C counterpart: FDKaacEnc_CalcBandEnergyOptimShort, band_nrg.cpp:264.
//
// scaleValue(mdctSpectrum[j], leadingBits) is the general (possibly-negative
// shift) form; scaleValueSaturate is used on the final rescale.
func fdkaacEncCalcBandEnergyOptimShort(mdctSpectrum, bandEnergy []int32,
	sfbMaxScaleSpec, bandOffset []int, numBands int) {
	for i := 0; i < numBands; i++ {
		leadingBits := sfbMaxScaleSpec[i] - 3 // max sfbWidth = 36 ; 2^6=64 => 6/2 = 3 (spc*spc)
		var tmp int32
		for j := bandOffset[i]; j < bandOffset[i+1]; j++ {
			spec := scaleValue(mdctSpectrum[j], int32(leadingBits))
			tmp = fPow2AddDiv2(tmp, spec)
		}
		bandEnergy[i] = tmp
	}

	for i := 0; i < numBands; i++ {
		scale := (2 * (sfbMaxScaleSpec[i] - 3)) - 1 // max sfbWidth = 36 ; 2^6=64 => 6/2 = 3 (spc*spc)
		scale = fixMax(fixMin(scale, dfractBits-1), -(dfractBits - 1))
		bandEnergy[i] = scaleValueSaturate(bandEnergy[i], int32(-scale))
	}
}

// FDKaacEnc_CalcBandNrgMSOpt computes mid/side SFB energies (and ldData) from a
// left/right channel pair. C counterpart: FDKaacEnc_CalcBandNrgMSOpt,
// band_nrg.cpp:296.
//
// fMin(NrgMid, MAXVAL_DBL>>1) -> fMin (int32). bandEnergy...Side already
// scaled by the minScale shift; LdDataVector then subtracts the scaling.
func fdkaacEncCalcBandNrgMSOpt(
	mdctSpectrumLeft, mdctSpectrumRight []int32,
	sfbMaxScaleSpecLeft, sfbMaxScaleSpecRight, bandOffset []int, numBands int,
	bandEnergyMid, bandEnergySide []int32,
	calcLdDataFlag int, bandEnergyMidLdData, bandEnergySideLdData []int32) {

	for i := 0; i < numBands; i++ {
		var nrgMid, nrgSide int32
		minScale := fixMin(sfbMaxScaleSpecLeft[i], sfbMaxScaleSpecRight[i]) - 4
		minScale = fixMax(0, minScale)

		if minScale > 0 {
			for j := bandOffset[i]; j < bandOffset[i+1]; j++ {
				specL := mdctSpectrumLeft[j] << uint(minScale-1)
				specR := mdctSpectrumRight[j] << uint(minScale-1)
				specm := specL + specR
				specs := specL - specR
				nrgMid = fPow2AddDiv2(nrgMid, specm)
				nrgSide = fPow2AddDiv2(nrgSide, specs)
			}
		} else {
			for j := bandOffset[i]; j < bandOffset[i+1]; j++ {
				specL := mdctSpectrumLeft[j] >> 1
				specR := mdctSpectrumRight[j] >> 1
				specm := specL + specR
				specs := specL - specR
				nrgMid = fPow2AddDiv2(nrgMid, specm)
				nrgSide = fPow2AddDiv2(nrgSide, specs)
			}
		}
		bandEnergyMid[i] = fMin(nrgMid, maxvalDBL>>1) << 1
		bandEnergySide[i] = fMin(nrgSide, maxvalDBL>>1) << 1
	}

	if calcLdDataFlag != 0 {
		ldDataVector(bandEnergyMid, bandEnergyMidLdData, numBands)
		ldDataVector(bandEnergySide, bandEnergySideLdData, numBands)
	}

	for i := 0; i < numBands; i++ {
		minScale := fixMin(sfbMaxScaleSpecLeft[i], sfbMaxScaleSpecRight[i])
		scale := fixMax(0, 2*(minScale-4))

		if calcLdDataFlag != 0 {
			// using the minimal scaling of left and right channel can cause very
			// small energies; check ldNrg before subtract scaling multiplication:
			// fract*INT we don't need fMult
			minus := int32(scale) * fl2fxconstDBL(1.0/64)

			if bandEnergyMidLdData[i] != fl2fxconstDBL(-1.0) {
				bandEnergyMidLdData[i] -= minus
			}
			if bandEnergySideLdData[i] != fl2fxconstDBL(-1.0) {
				bandEnergySideLdData[i] -= minus
			}
		}
		scale = fixMin(scale, dfractBits-1)
		bandEnergyMid[i] >>= uint(scale)
		bandEnergySide[i] >>= uint(scale)
	}
}
