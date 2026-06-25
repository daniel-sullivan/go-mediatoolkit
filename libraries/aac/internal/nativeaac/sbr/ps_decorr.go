// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Decorrelator, ported 1:1 from the vendored Fraunhofer FDK-AAC
// FDK_decorrelate.cpp for the HE-AAC v2 baseline PS path: FDKdecorrelateOpen /
// FDKdecorrelateInit (DECORR_PS, !partiallyComplex, isLegacyPS, 71 hybrid bands)
// -> per-frame FDKdecorrelateApply. The apply runs DuckerCalcEnergy, then the
// reverb-band filters (INDEP_CPLX_PS allpass cascade for rb0, a pure delay for
// rb1/rb2), then DuckerApplyPS (the transient/peak-decay smoothing + ducking).
// The MPS/USAC/LD decorr paths are ported only where they share a function body;
// their dedicated branches are kept faithful but unreachable for legacy PS.
//
// FIXED-POINT / arch convention: __ARM_ARCH_8__. The packed allpass coefficients
// are FIXP_STP (Q1.15), the ducker gains FIXP_SGL. cplxMultDiv2(...,FIXP_STP)
// takes the 32x16X2 arm form (CplxMultDiv2SGL). See per-call overload notes.

// fAbs is the FIXP_DBL absolute value (common_fix.h fAbs). INT32_MIN maps to
// MAXVAL_DBL (0x7FFFFFFF), matching the C saturating abs.
func fAbs(x int32) int32 {
	if x < 0 {
		if x == -2147483648 {
			return 0x7FFFFFFF
		}
		return -x
	}
	return x
}

// fdkDecorrelateOpen ports FDKdecorrelateOpen (FDK_decorrelate.cpp:1425-1440):
// assign all of bufferCplx to stateBufferCplx (reassigned in Init).
func fdkDecorrelateOpen(self *decorrDec, bufferCplx []int32) int {
	if len(bufferCplx) < 2*(825+373) {
		return 1
	}
	self.stateBufferCplx = bufferCplx
	self.lStateBufferCplx = 0
	self.delayBufferCplx = nil
	self.lDelayBufferCplx = 0
	return 0
}

// distributeBuffer ports distributeBuffer (FDK_decorrelate.cpp:1442-1454): split
// the shared cplx buffer into the state (L_stateBuf) and delay (L_delayBuf)
// regions. delayBufferCplx is a subslice of stateBufferCplx offset by 2*L_stateBuf.
func distributeBuffer(self *decorrDec, lStateBuf, lDelayBuf int) int {
	if 2*(825+373) < 2*(lStateBuf+lDelayBuf) {
		return 1
	}
	self.lStateBufferCplx = 2 * lStateBuf
	self.delayBufferCplx = self.stateBufferCplx[2*lStateBuf:]
	self.lDelayBufferCplx = 2 * lDelayBuf
	return 0
}

// decorrFilterInitPS ports DecorrFilterInitPS (FDK_decorrelate.cpp:556-575): for
// reverb band 0 assign the packed allpass coefficients + a 2*DECORR_FILTER_ORDER_PS
// state slice; always assign a 2*noSampleDelay delay slice. The state/delay
// "pointers" are subslices into the shared buffers at the running offsets.
func decorrFilterInitPS(self *decorrFilterInstance, pStateBufferCplx, pDelayBufferCplx []int32,
	offsetStateBuffer, offsetDelayBuffer *int, hybridBand, reverbBand, noSampleDelay int) {
	if reverbBand == 0 {
		self.coeffsPacked = decorrPsCoeffsCplx[hybridBand][:]
		self.stateCplx = pStateBufferCplx[*offsetStateBuffer:]
		*offsetStateBuffer += 2 * decorrFilterOrderPs
	}
	self.delayBufferCplx = pDelayBufferCplx[*offsetDelayBuffer:]
	*offsetDelayBuffer += 2 * noSampleDelay
}

// decorrFilterApplyPASS ports DecorrFilterApplyPASS (FDK_decorrelate.cpp:578-626)
// for the complex (dataImagIn != nil) case: a pure delay line. Real and imag are
// interleaved in the delay buffer; the per-filter stride is 2*noSampleDelay.
func decorrFilterApplyPASS(filter []decorrFilterInstance, dataRealIn, dataImagIn, dataRealOut, dataImagOut []int32,
	start, stop, reverbBandNoSampleDelay, reverbBandDelayBufferIndex int) {
	offset := 2 * reverbBandNoSampleDelay
	pDelay := filter[start].delayBufferCplx
	dp := reverbBandDelayBufferIndex

	for i := start; i < stop; i++ {
		delayRe := pDelay[dp]
		delayIm := pDelay[dp+1]
		pDelay[dp] = dataRealIn[i]
		pDelay[dp+1] = dataImagIn[i]
		dataRealOut[i] = delayRe
		dataImagOut[i] = delayIm
		dp += offset
	}
}

// decorrFilterApplyCPLXPS ports DecorrFilterApplyCPLX_PS (FDK_decorrelate.cpp:
// 811-978): the 3-stage complex allpass cascade (with the Phi(k) pre-rotation)
// used in reverb band 0 of the PS decorrelator. Each cplxMultDiv2 with a packed
// FIXP_STP coefficient takes the arm 32x16X2 form (CplxMultDiv2SGL). The three
// per-stage stateBufferOffset ring pointers advance by 2 each call, wrapping at
// 4/12/22.
func decorrFilterApplyCPLXPS(filter []decorrFilterInstance, dataRealIn, dataImagIn, dataRealOut, dataImagOut []int32,
	start, stop, reverbFilterOrder, reverbBandNoSampleDelay, reverbBandDelayBufferIndex int, stateBufferOffset *[3]uint8) {

	pDelay := filter[start].delayBufferCplx
	dp := reverbBandDelayBufferIndex
	offsetDelayBuffer := (2 * reverbBandNoSampleDelay) - 1

	pStates := filter[start].stateCplx
	pStatesIncrement := 2 * reverbFilterOrder

	off0 := int(stateBufferOffset[0])
	off1 := int(stateBufferOffset[1])
	off2 := int(stateBufferOffset[2])

	for i := start; i < stop; i++ {
		// 1. input delay (real/imag interleaved).
		rDataA := pDelay[dp]
		pDelay[dp] = dataRealIn[i]
		dp++
		jDataA := pDelay[dp]
		pDelay[dp] = dataImagIn[i]
		dp += offsetDelayBuffer

		// 2. Phi(k)-stage.
		pCoeffs := filter[i].coeffsPacked
		rjCoeff := pCoeffs[0]
		rDataB, jDataB := nativeaac.CplxMultDiv2SGL(rDataA, jDataA, rjCoeff.re, rjCoeff.im)

		// stage 0.
		rjCoeff = pCoeffs[1]
		rStageMult, jStageMult := nativeaac.CplxMultDiv2SGL(rDataB, jDataB, rjCoeff.re, rjCoeff.im)
		rStageMult <<= 1
		jStageMult <<= 1
		rDataA = rStageMult + pStates[off0+0]
		jDataA = jStageMult + pStates[off0+1]
		rStageMult, jStageMult = nativeaac.CplxMultDiv2SGL(-rDataA, jDataA, rjCoeff.re, rjCoeff.im)
		rStageMult = rDataB + (rStageMult << 1)
		jStageMult = jDataB - (jStageMult << 1)
		pStates[off0+0] = rStageMult
		pStates[off0+1] = jStageMult
		off0 += pStatesIncrement

		// stage 1.
		rjCoeff = pCoeffs[2]
		rStageMult, jStageMult = nativeaac.CplxMultDiv2SGL(rDataA, jDataA, rjCoeff.re, rjCoeff.im)
		rStageMult <<= 1
		jStageMult <<= 1
		rDataB = rStageMult + pStates[off1+0]
		jDataB = jStageMult + pStates[off1+1]
		rStageMult, jStageMult = nativeaac.CplxMultDiv2SGL(-rDataB, jDataB, rjCoeff.re, rjCoeff.im)
		rStageMult = rDataA + (rStageMult << 1)
		jStageMult = jDataA - (jStageMult << 1)
		pStates[off1+0] = rStageMult
		pStates[off1+1] = jStageMult
		off1 += pStatesIncrement

		// stage 2.
		rjCoeff = pCoeffs[3]
		rStageMult, jStageMult = nativeaac.CplxMultDiv2SGL(rDataB, jDataB, rjCoeff.re, rjCoeff.im)
		rStageMult <<= 1
		jStageMult <<= 1
		rDataA = rStageMult + pStates[off2+0]
		jDataA = jStageMult + pStates[off2+1]
		rStageMult, jStageMult = nativeaac.CplxMultDiv2SGL(-rDataA, jDataA, rjCoeff.re, rjCoeff.im)
		rStageMult = rDataB + (rStageMult << 1)
		jStageMult = jDataB - (jStageMult << 1)
		pStates[off2+0] = rStageMult
		pStates[off2+1] = jStageMult
		off2 += pStatesIncrement

		// filter output.
		dataRealOut[i] = rDataA << 1
		dataImagOut[i] = jDataA << 1
	}

	// update stateBufferOffset ring pointers.
	if stateBufferOffset[0] == 4 {
		stateBufferOffset[0] = 0
	} else {
		stateBufferOffset[0] += 2
	}
	if stateBufferOffset[1] == 12 {
		stateBufferOffset[1] = 6
	} else {
		stateBufferOffset[1] += 2
	}
	if stateBufferOffset[2] == 22 {
		stateBufferOffset[2] = 14
	} else {
		stateBufferOffset[2] += 2
	}
}

// duckerInit ports DuckerInit (FDK_decorrelate.cpp:984-1033) for the PS (20
// param band, 71 hybrid band) configuration.
func duckerInit(self *duckerInstance, hybridBands, partiallyComplex int, duckerType fdkDuckerType, nParamBands, initStatesFlag int) int {
	switch nParamBands {
	case 20:
		self.mapHybBands2ProcBands = kernels20To71Ps[:]
		self.mapProcBands2HybBands = kernels20To71OffsetPs[:]
		self.parameterBands = 20
	default:
		return 1
	}
	self.qsNext = self.mapProcBands2HybBands[1:]

	self.maxValDirectData = nativeaac.Fl2fxconstDBL(-1.0)
	self.maxValReverbData = nativeaac.Fl2fxconstDBL(-1.0)
	self.scaleDirectNrg = 2 * duckerMaxNrgScale
	self.scaleReverbNrg = 2 * duckerMaxNrgScale
	self.scaleSmoothDirRevNrg = 2 * duckerMaxNrgScale
	self.headroomSmoothDirRevNrg = 2 * duckerMaxNrgScale
	self.hybridBands = hybridBands
	self.partiallyComplex = partiallyComplex

	if initStatesFlag != 0 && duckerType == duckerPs {
		for pb := 0; pb < self.parameterBands; pb++ {
			self.smoothDirRevNrg[pb] = 0
		}
	}
	return 0
}

// duckerCalcEnergy ports DuckerCalcEnergy (FDK_decorrelate.cpp:1039-1136), PS
// branch (mode==1): per-param-band direct-signal energy with a leading-zeros
// based normalisation. maxVal is always FL2FXCONST_DBL(-1) for legacy PS (the
// maxValDirectData seed), so the getScalefactor path is taken.
func duckerCalcEnergy(self *duckerInstance, inputReal, inputImag []int32, energy []int32, inputMaxVal int32, nrgScale *int8, mode, startHybBand int) {
	maxHybridBand := self.hybridBands - 1
	maxHybBand := maxHybridBand

	for i := 0; i < 28; i++ {
		energy[i] = 0
	}

	if mode == 1 {
		var clz int
		maxVal := nativeaac.Fl2fxconstDBL(-1.0)
		if maxVal == nativeaac.Fl2fxconstDBL(-1.0) {
			clz = fMinI(nativeaac.GetScalefactor(inputReal[startHybBand:], fMaxI(0, maxHybridBand-startHybBand+1)),
				nativeaac.GetScalefactor(inputImag[startHybBand:], fMaxI(0, maxHybBand-startHybBand+1)))
		} else {
			clz = nativeaac.CntLeadingZeros(maxVal) - 1
		}
		clz = fMinI(fMaxI(0, clz-duckerHeadroomBits), duckerMaxNrgScale)
		*nrgScale = int8(clz) << 1

		pb := int(kernels20To71Ps[maxHybBand])
		qs := startHybBand
		for ; qs <= maxHybBand; qs++ {
			pb = int(kernels20To71Ps[qs])
			energy[pb] = nativeaac.SaturateLeftShift(
				(energy[pb]>>1)+(nativeaac.FPow2Div2(inputReal[qs]<<clz)>>1)+(nativeaac.FPow2Div2(inputImag[qs]<<clz)>>1), 1)
		}
		pb++

		for ; pb <= int(kernels20To71Ps[maxHybridBand]); pb++ {
			var nrg int32
			qsNext := int(self.qsNext[pb])
			for ; qs < qsNext; qs++ {
				nrg = nativeaac.FAddSaturate(nrg, nativeaac.FPow2Div2(inputReal[qs]<<clz))
			}
			energy[pb] = nrg
		}
	} else {
		var clz int
		maxVal := inputMaxVal
		if maxVal == nativeaac.Fl2fxconstDBL(-1.0) {
			clz = fMinI(nativeaac.GetScalefactor(inputReal[startHybBand:], fMaxI(0, maxHybridBand-startHybBand+1)),
				nativeaac.GetScalefactor(inputImag[startHybBand:], fMaxI(0, maxHybBand-startHybBand+1)))
		} else {
			clz = nativeaac.CntLeadingZeros(maxVal) - 1
		}
		clz = fMinI(fMaxI(0, clz-duckerHeadroomBits), duckerMaxNrgScale)
		*nrgScale = int8(clz) << 1

		qs := startHybBand
		for ; qs <= maxHybBand; qs++ {
			pb := int(kernels20To71Ps[qs])
			energy[pb] = nativeaac.SaturateLeftShift(
				(energy[pb]>>1)+(nativeaac.FPow2Div2(inputReal[qs]<<clz)>>1)+(nativeaac.FPow2Div2(inputImag[qs]<<clz)>>1), 1)
		}
		for ; qs <= maxHybridBand; qs++ {
			pb := int(kernels20To71Ps[qs])
			energy[pb] = nativeaac.FAddSaturate(energy[pb], nativeaac.FPow2Div2(inputReal[qs]<<clz))
		}
	}

	// Catch overflows (mask to MAXVAL_DBL).
	for pb := 0; pb < 28; pb++ {
		energy[pb] = energy[pb] & 0x7FFFFFFF
	}
}

// duckerApplyPS ports DuckerApplyPS (FDK_decorrelate.cpp:1293-1423): per-param-band
// transient/peak-decay smoothing and ducking of the decorrelated (reverb) output.
func duckerApplyPS(self *duckerInstance, directNrg []int32, outputReal, outputImag []int32, startHybBand int) {
	qs := startHybBand
	startParamBand := int(kernels20To71Ps[startHybBand])

	doScaleNrg := false
	var scaleDirectNrg, scaleSmoothDirRevNrg int
	var maxDirRevNrg int32

	if self.scaleDirectNrg != self.scaleSmoothDirRevNrg || self.headroomSmoothDirRevNrg == 0 {
		// scale is computed in int: SCHAR + SCHAR - 2 promotes to int, and fixMin
		// compares the promoted ints, then narrows to SCHAR on store.
		scale := fMinI(int(self.scaleDirectNrg), int(self.scaleSmoothDirRevNrg)+int(self.headroomSmoothDirRevNrg)-2)
		scaleDirectNrg = fMaxI(fMinI(int(self.scaleDirectNrg)-scale, dfractBits-1), -(dfractBits - 1))
		scaleSmoothDirRevNrg = fMaxI(fMinI(int(self.scaleSmoothDirRevNrg)-scale, dfractBits-1), -(dfractBits - 1))
		self.scaleSmoothDirRevNrg = int8(scale)
		doScaleNrg = true
	}

	hybBands := self.hybridBands

	for pb := startParamBand; pb < self.parameterBands; pb++ {
		directNrg2 := directNrg[pb]

		if doScaleNrg {
			directNrg2 = nativeaac.ScaleValue(directNrg2, int32(-scaleDirectNrg))
			self.peakDiff[pb] = nativeaac.ScaleValue(self.peakDiff[pb], int32(-scaleSmoothDirRevNrg))
			self.peakDecay[pb] = nativeaac.ScaleValue(self.peakDecay[pb], int32(-scaleSmoothDirRevNrg))
			self.smoothDirRevNrg[pb] = nativeaac.ScaleValue(self.smoothDirRevNrg[pb], int32(-scaleSmoothDirRevNrg))
		}
		self.peakDecay[pb] = fMaxI32(directNrg2, nativeaac.FMultDS(self.peakDecay[pb], psDuckPeakDecayFactor))
		self.peakDiff[pb] = self.peakDiff[pb] +
			nativeaac.FMultDS(self.peakDecay[pb]-directNrg2-self.peakDiff[pb], psDuckFilterCoeff)
		self.smoothDirRevNrg[pb] = fMaxI32(self.smoothDirRevNrg[pb]+
			nativeaac.FMultDS(directNrg2-self.smoothDirRevNrg[pb], psDuckFilterCoeff), 0)

		maxDirRevNrg |= fAbs(self.peakDiff[pb])
		maxDirRevNrg |= fAbs(self.smoothDirRevNrg[pb])

		if self.peakDiff[pb] == 0 && self.smoothDirRevNrg[pb] == 0 {
			qs = fMaxI(qs, int(kernels20To71OffsetPs[pb]))
			qsNext := fMinI(int(self.qsNext[pb]), self.hybridBands)
			if qs < hybBands {
				for ; qs < qsNext; qs++ {
					outputReal[qs] = 0
					outputImag[qs] = 0
				}
			} else {
				for ; qs < qsNext; qs++ {
					outputReal[qs] = 0
				}
			}
		} else if self.peakDiff[pb] != 0 {
			multiplication := nativeaac.FMultDS(self.peakDiff[pb], duck0p75)
			if multiplication > (self.smoothDirRevNrg[pb] >> 1) {
				// implement x/y as (sqrt(x)*invSqrt(y))^2.
				num := nativeaac.SqrtFixp(self.smoothDirRevNrg[pb] >> 1)
				denom := self.peakDiff[pb] + absThrDenomBias
				denomInv, scale := nativeaac.InvSqrtNorm2(denom)

				qs = fMaxI(qs, int(kernels20To71OffsetPs[pb]))
				qsNext := fMinI(int(self.qsNext[pb]), self.hybridBands)

				duckGain := nativeaac.FMultDD(num, denomInv)
				duckGain = nativeaac.FPow2Div2(duckGain << scale)
				duckGain = nativeaac.FMultDiv2DS(duckGain, duck2div3) << 3

				if qs < hybBands {
					for ; qs < qsNext; qs++ {
						outputReal[qs] = nativeaac.FMultDD(outputReal[qs], duckGain)
						outputImag[qs] = nativeaac.FMultDD(outputImag[qs], duckGain)
					}
				} else {
					for ; qs < qsNext; qs++ {
						outputReal[qs] = nativeaac.FMultDD(outputReal[qs], duckGain)
					}
				}
			}
		}
	}

	self.headroomSmoothDirRevNrg = int8(fMaxI(0, nativeaac.CntLeadingZeros(maxDirRevNrg)-1))
}

// fdkDecorrelateInit ports FDKdecorrelateInit (FDK_decorrelate.cpp:1455-1619) for
// the DECORR_PS / !partiallyComplex / isLegacyPS path: distribute buffers
// (360 state / 257 delay cplx pairs), set the PS reverb layout + stateBufferOffset,
// clear states, init each reverb band's filters (CPLX_PS for rb0, delay for
// rb1/rb2), and init the 20-param-band PS ducker.
func fdkDecorrelateInit(self *decorrDec, nrHybBands int, decorrType fdkDecorrType, duckerType fdkDuckerType,
	decorrConfig, seed, partiallyComplex, useFractDelay, isLegacyPS, initStatesFlag int) int {

	nParamBands := 28
	offsetStateBuffer := 0
	offsetDelayBuffer := 0

	self.partiallyComplex = partiallyComplex
	self.numbins = nrHybBands

	switch decorrType {
	case decorrPs:
		// !partiallyComplex (HQ).
		self.revBandOffset = revBandOffsetPsHQ[:]
		self.revDelay = revDelayPsHQ[:]
		if distributeBuffer(self, 360, 257) != 0 {
			return 1
		}
		self.revFilterOrder = revFilterOrderPs[:]
		self.revFiltType = revFiltTypePs[:]
		for i := 0; i < 3; i++ {
			self.stateBufferOffset[i] = stateBufferOffsetInit[i]
		}
	default:
		return 1
	}

	if initStatesFlag != 0 {
		clearInt32(self.stateBufferCplx, self.lStateBufferCplx)
		clearInt32(self.delayBufferCplx, self.lDelayBufferCplx)
		for i := range self.reverbBandDelayBufferIndex {
			self.reverbBandDelayBufferIndex[i] = 0
		}
	}

	iStart := 0
	for rb := 0; rb < 4; rb++ {
		iStop := int(self.revBandOffset[rb])
		if iStop <= iStart {
			continue
		}
		for i := iStart; i < iStop; i++ {
			decorrFilterInitPS(&self.filter[i], self.stateBufferCplx, self.delayBufferCplx,
				&offsetStateBuffer, &offsetDelayBuffer, i, rb, int(self.revDelay[rb]))
		}
		iStart = iStop
	}

	if offsetStateBuffer > self.lStateBufferCplx || offsetDelayBuffer > self.lDelayBufferCplx {
		return 1
	}

	if duckerType == duckerAutomatic {
		self.ducker.duckerType = duckerPs
		if isLegacyPS != 0 {
			nParamBands = 20
		} else {
			nParamBands = 28
		}
	}

	return duckerInit(&self.ducker, self.numbins, self.partiallyComplex, self.ducker.duckerType, nParamBands, initStatesFlag)
}

// fdkDecorrelateApply ports FDKdecorrelateApply (FDK_decorrelate.cpp:1637-1714):
// the per-frame decorrelation — ducker energy, reverb-band filtering (CPLX_PS /
// delay), delay-index advance, then the PS ducker.
func fdkDecorrelateApply(self *decorrDec, dataRealIn, dataImagIn, dataRealOut, dataImagOut []int32, startHybBand int) {
	nHybBands := self.numbins

	var directNrg [28]int32
	mode := 0
	if self.ducker.duckerType == duckerPs {
		mode = 1
	}
	duckerCalcEnergy(&self.ducker, dataRealIn, dataImagIn, directNrg[:], self.ducker.maxValDirectData,
		&self.ducker.scaleDirectNrg, mode, startHybBand)

	stop := 0
	for rb := 0; rb < 4; rb++ {
		start := fMaxI(stop, startHybBand)
		stop = fMinI(int(self.revBandOffset[rb]), nHybBands)
		if start < stop {
			switch self.revFiltType[rb] {
			case delayBand:
				decorrFilterApplyPASS(self.filter[:], dataRealIn, dataImagIn, dataRealOut, dataImagOut,
					start, stop, int(self.revDelay[rb]), self.reverbBandDelayBufferIndex[rb])
			case indepCplxPs:
				decorrFilterApplyCPLXPS(self.filter[:], dataRealIn, dataImagIn, dataRealOut, dataImagOut,
					start, stop, int(self.revFilterOrder[rb]), int(self.revDelay[rb]),
					self.reverbBandDelayBufferIndex[rb], &self.stateBufferOffset)
			}
		}
	}

	for rb := 0; rb < 4; rb++ {
		self.reverbBandDelayBufferIndex[rb] += 2
		if self.reverbBandDelayBufferIndex[rb] >= 2*int(self.revDelay[rb]) {
			self.reverbBandDelayBufferIndex[rb] = 0
		}
	}

	switch self.ducker.duckerType {
	case duckerPs:
		duckerApplyPS(&self.ducker, directNrg[:], dataRealOut, dataImagOut, startHybBand)
	}
}

// fMaxI32 is the int32 fixMax used above (fMinI/fMaxI over int are shared from
// qmf.go).
func fMaxI32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
