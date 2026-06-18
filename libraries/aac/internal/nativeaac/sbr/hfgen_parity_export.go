// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Thin exported drivers + ROM views for the sbr-dec-hfgen cgo parity oracle
// (internal/parity_tests/sbr-dec-hfgen). They add no algorithmic logic: each
// mirrors exactly what the C bridge does, so the oracle can drive the Go port and
// the genuine vendored C with identical inputs and assert EXACT int equality.
// Only the HE-AAC v1 (LPP) HF-gen surface is exposed (hbe.cpp / harmonic SBR is
// out of scope; see lpp_tran.go).

// --- ROM views -------------------------------------------------------------

// WhFactorsIndex / WhFactorsTableFlat return the whitening ROM
// (sbr_rom.cpp:165/177) so the oracle can verify them against the genuine
// FDK_sbrDecoder_sbr_whFactorsIndex / _whFactorsTable.
func WhFactorsIndex() []uint16 { return whFactorsIndex[:] }

func WhFactorsTableFlat() []int32 {
	out := make([]int32, 0, numWhFactorTableEntries*5)
	for i := 0; i < numWhFactorTableEntries; i++ {
		out = append(out, whFactorsTable[i][:]...)
	}
	return out
}

// GetLog2 returns the getLog2[32] ROM (HFgen_preFlat.cpp:135).
func GetLog2() []uint8 { return getLog2[:] }

// BsdFlat returns the narrowed bsd[] backsubst_data ROM
// (HFgen_preFlat.cpp:149) flattened per entry as
// [Lnorm1d(3) Lnorm1dSf(3) Lnormii(3) LnormiiSf(3) Bmul0(4) Bmul0Sf(4)
//
//	LnormInv1d(6) LnormInv1dSf(6) Bmul1(4) Bmul1Sf(4)] so the oracle can verify
//
// the FIXP_CHB narrowing + the SCHAR exponents entry-for-entry.
func BsdFlat() (chb []int16, sf []int8) {
	for i := range bsd {
		e := &bsd[i]
		chb = append(chb, e.Lnorm1d[:]...)
		chb = append(chb, e.Lnormii[:]...)
		chb = append(chb, e.Bmul0[:]...)
		chb = append(chb, e.LnormInv1d[:]...)
		chb = append(chb, e.Bmul1[:]...)
		sf = append(sf, e.Lnorm1dSf[:]...)
		sf = append(sf, e.LnormiiSf[:]...)
		sf = append(sf, e.Bmul0Sf[:]...)
		sf = append(sf, e.LnormInv1dSf[:]...)
		sf = append(sf, e.Bmul1Sf[:]...)
	}
	return chb, sf
}

// --- autocorr2nd -----------------------------------------------------------

// RunAutoCorr2ndReal drives autoCorr2ndReal over buf (which must hold the two
// history samples at indices base-2, base-1 followed by length data samples). It
// returns the ACORR_COEFS fields the C writes (r11r, r22r, r01r, r12r, r02r, det,
// det_scale) plus the scaling return.
func RunAutoCorr2ndReal(buf []int32, base, length int) (r11r, r22r, r01r, r12r, r02r, det int32, detScale, scaling int) {
	var ac acorrCoefs
	scaling = autoCorr2ndReal(&ac, buf, base, length)
	return ac.r11r, ac.r22r, ac.r01r, ac.r12r, ac.r02r, ac.det, ac.detScale, scaling
}

// RunAutoCorr2ndCplx drives autoCorr2ndCplx, returning all nine coefficients +
// det + det_scale + scaling.
func RunAutoCorr2ndCplx(re, im []int32, base, length int) (r00r, r11r, r22r, r01r, r12r, r01i, r12i, r02r, r02i, det int32, detScale, scaling int) {
	var ac acorrCoefs
	scaling = autoCorr2ndCplx(&ac, re, im, base, length)
	return ac.r00r, ac.r11r, ac.r22r, ac.r01r, ac.r12r, ac.r01i, ac.r12i, ac.r02r, ac.r02i, ac.det, ac.detScale, scaling
}

// --- HFgen pre-flattening --------------------------------------------------

// RunCalculateGainVec drives sbrDecoderCalculateGainVec over flat slot-major QMF
// energy buffers (realFlat/imagFlat are nSlots*64), returning the per-band gain
// vector (numBands) and its exponents.
func RunCalculateGainVec(realFlat, imagFlat []int32, nSlots int,
	sourceBufEOverlap, sourceBufECurrent, overlap, numBands, startSample, stopSample int) (gain []int32, gainExp []int) {

	re := make([][]int32, nSlots)
	im := make([][]int32, nSlots)
	for i := 0; i < nSlots; i++ {
		re[i] = realFlat[i*64 : i*64+64]
		im[i] = imagFlat[i*64 : i*64+64]
	}
	gain = make([]int32, numBands)
	gainExp = make([]int, numBands)
	sbrDecoderCalculateGainVec(re, im, sourceBufEOverlap, sourceBufECurrent, overlap,
		gain, gainExp, numBands, startSample, stopSample)
	return gain, gainExp
}

// RunPolyval drives polyval directly (p/pSf length 4) at integer x.
func RunPolyval(p []int32, pSf []int, x int) (result int32, outSf int) {
	return polyval(p, pSf, x)
}

// --- LPP transposer --------------------------------------------------------

// RunResetLppTransposer drives createLppTransposer (chan 0 -> resetLppTransposer)
// over a fresh TRANSPOSER_SETTINGS and returns the computed patch layout fields so
// the oracle can verify the patch generation byte-for-byte.
func RunResetLppTransposer(highBandStartSb int, vKMaster []uint8, numMaster, usb, timeSlots, nCols int,
	noiseBandTable []uint8, noNoiseBands int, fs uint, overlap int) (
	rc int, noOfPatches int, lbStartPatching, lbStopPatching int,
	patchSourceStart, patchSourceStop, patchTargetStart, patchTargetOffs, patchGuardStart, patchNumBands []uint8,
	bwBorders []uint8, whFactors []int32) {

	var hs sbrLppTrans
	var st transposerSettings
	err := createLppTransposer(&hs, &st, highBandStartSb, vKMaster, numMaster, usb, timeSlots, nCols,
		noiseBandTable, noNoiseBands, fs, 0, overlap)

	np := int(st.noOfPatches)
	for p := 0; p <= maxNumPatches; p++ {
		patchSourceStart = append(patchSourceStart, st.patchParam[p].sourceStartBand)
		patchSourceStop = append(patchSourceStop, st.patchParam[p].sourceStopBand)
		patchTargetStart = append(patchTargetStart, st.patchParam[p].targetStartBand)
		patchTargetOffs = append(patchTargetOffs, st.patchParam[p].targetBandOffs)
		patchGuardStart = append(patchGuardStart, st.patchParam[p].guardStartBand)
		patchNumBands = append(patchNumBands, st.patchParam[p].numBandsInPatch)
	}
	bwBorders = append(bwBorders, st.bwBorders[:]...)
	whFactors = []int32{st.whFactors.off, st.whFactors.transitionLevel, st.whFactors.lowLevel,
		st.whFactors.midLevel, st.whFactors.highLevel}

	return int(err), np, int(st.lbStartPatching), int(st.lbStopPatching),
		patchSourceStart, patchSourceStop, patchTargetStart, patchTargetOffs, patchGuardStart, patchNumBands,
		bwBorders, whFactors
}

// RunLppTransposer drives the full lppTransposer over slot-major QMF buffers
// (realFlat/imagFlat each nSlots*64). It first builds the patch layout via
// createLppTransposer, then runs lppTransposer, returning the mutated QMF
// buffers, degreeAlias, and the resulting hb_scale. invfMode/invfModePrev are
// per-band (length nInvfBands). It mirrors exactly the C oracle setup.
func RunLppTransposer(realFlat, imagFlat []int32, nSlots int,
	highBandStartSb int, vKMaster []uint8, numMaster, usb, timeSlots, nCols int,
	noiseBandTable []uint8, noNoiseBands int, fs uint, overlap int,
	lbScale, ovLbScale int,
	useLP, fPreWhitening bool, vKMaster0, timeStep, firstSlotOffs, lastSlotOffs, nInvfBands int,
	invfMod, invfModPrev []int) (outReal, outImag, degreeAlias []int32, hbScale int) {

	var hs sbrLppTrans
	var st transposerSettings
	createLppTransposer(&hs, &st, highBandStartSb, vKMaster, numMaster, usb, timeSlots, nCols,
		noiseBandTable, noNoiseBands, fs, 0, overlap)

	re := make([][]int32, nSlots)
	im := make([][]int32, nSlots)
	for i := 0; i < nSlots; i++ {
		re[i] = realFlat[i*64 : i*64+64]
		im[i] = imagFlat[i*64 : i*64+64]
	}

	sf := ScaleFactor{LbScale: lbScale, OvLbScale: ovLbScale}
	degreeAlias = make([]int32, 64)

	im2 := im
	if useLP {
		im2 = nil
	}

	modes := make([]invfMode, nInvfBands)
	modesPrev := make([]invfMode, nInvfBands)
	for i := 0; i < nInvfBands; i++ {
		modes[i] = invfMode(invfMod[i])
		modesPrev[i] = invfMode(invfModPrev[i])
	}

	lppTransposer(&hs, &sf, re, degreeAlias, im2, useLP, fPreWhitening, vKMaster0,
		timeStep, firstSlotOffs, lastSlotOffs, nInvfBands, modes, modesPrev)

	return realFlat, imagFlat, degreeAlias, sf.HbScale
}
