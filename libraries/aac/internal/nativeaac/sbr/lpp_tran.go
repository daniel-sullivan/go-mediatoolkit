// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of libSBRdec/src/lpp_tran.cpp: the Low Power Profile transposer, the
// default HE-AAC v1 high-frequency generator. lppTransposer() generates the high
// band by patching (copying) whitened low-band QMF samples into the high band;
// the whitening is a 2nd-order LPC inverse filter whose coefficients come from
// autoCorr2nd_real/cplx, with a per-band bandwidth-expansion factor from the
// inverse-filtering level. lppTransposerHBE() is the variant fed by the HBE
// (harmonic) transposer output. createLppTransposer/resetLppTransposer compute
// the patch layout from the SBR master frequency table.
//
// Per-frame method selection (LPP vs HBE) is the bitstream flag
// hFrameData->sbrPatchingMode (sbr_dec.cpp:355/496/528) — a static if/else, NOT a
// function pointer; the decode-integration batch dispatches to lppTransposer vs
// lppTransposerHBE / QmfTransposerApply accordingly.
//
// SCOPE (HARD RULE 3 — HE-AAC v1 only, exclusions noted): in legacy AAC-LC + SBR
// (HE-AAC v1) sbrPatchingMode is forced to 1 (the LPP path) — env_extr.cpp:697-
// 708 unconditionally sets it, and EVERY harmonic-SBR (HBE) call site in
// sbr_dec.cpp (QmfTransposerApply 478/1305/1336/1350, lppTransposerHBE 500,
// QmfTransposerCreate 983, QmfTransposerReInit 1254) is gated by
// SBRDEC_USAC_HARMONICSBR. So hbe.cpp (the ~2200-line USAC/MPEG-D harmonic QMF
// transposer with its own analysis/synthesis banks, cross-product gain machine,
// and cube/fourth/3-eighth root-norm tables) is OUT OF HE-AAC v1 SCOPE and is NOT
// ported here — it belongs to the same excluded class as PS / HE-AAC v2 / USAC.
// lppTransposerHBE below is the (small, self-contained) LPP-whitening variant fed
// by the HBE output; it is ported for completeness but is only reachable on the
// excluded USAC harmonic path, so it is not exercised by the HE-AAC v1 parity.
//
// All fixed-point integer (FIXP_DBL Q-format, FIXP_SGL Q1.15), so EXACT-integer
// parity holds in any build. Helper mapping (the libFDK macros):
//   fMultDiv2(DBL,SGL)/(SGL,DBL) -> nativeaac.FMultDiv2DS (args commute)
//   fMultDiv2(DBL,DBL)           -> nativeaac.FMultDiv2DD
//   fMult(SGL,SGL)               -> nativeaac.FMultSS
//   fMult(SGL,DBL)/(DBL,SGL)     -> nativeaac.FMultDS
//   fPow2(SGL)                   -> nativeaac.FPow2S
//   FX_DBL2FX_SGL(v)             -> nativeaac.FxDbl2FxSgl (truncating >>16)
//   fDivNorm(a,b,&e)             -> nativeaac.FDivNorm
//   scaleValueSaturate           -> nativeaac.ScaleValueSaturate
//   SATURATE_LEFT_SHIFT          -> nativeaac.SaturateLeftShift
//   getScalefactor/scaleValues   -> nativeaac.GetScalefactor / ScaleValues
//   fixp_abs/fixMin/fixMax       -> nativeaac.FixpAbs / FMinI / FMaxI

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// Q1.31 / Q1.15 constants the transposer compares against, materialised through
// the shared narrowing macros (FL2FXCONST_DBL / _SGL).
// The C literals carry the float `f` suffix (FL2FXCONST_DBL(0.5f) ...), so each is
// narrowed through float32 BEFORE the Q1.31 conversion — load-bearing for
// bit-exactness (the float forms differ from their double forms in the low bits).
var (
	fxDBL0p5        = nativeaac.Fl2fxconstDBL(float64(float32(0.5)))           // FL2FXCONST_DBL(0.5f)
	fxDBL0p015625h  = nativeaac.Fl2fxconstDBL(float64(float32(0.015625))) >> 1 // FL2FXCONST_DBL(0.015625f)>>1
	fxDBL0p75       = nativeaac.Fl2fxconstDBL(float64(float32(0.75)))          // FL2FXCONST_DBL(0.75f)
	fxDBL0p25       = nativeaac.Fl2fxconstDBL(float64(float32(0.25)))          // FL2FXCONST_DBL(0.25f)
	fxDBL0p90625    = nativeaac.Fl2fxconstDBL(float64(float32(0.90625)))       // FL2FXCONST_DBL(0.90625f)
	fxDBL0p09375    = nativeaac.Fl2fxconstDBL(float64(float32(0.09375)))       // FL2FXCONST_DBL(0.09375f)
	fxDBL0p99609375 = nativeaac.Fl2fxconstDBL(float64(float32(0.99609375)))    // FL2FXCONST_DBL(0.99609375f)
)

// maxvalDBL / minvalDBL are MAXVAL_DBL / MINVAL_DBL (the FIXP_DBL extremes).
const (
	maxvalDBL = int32(0x7FFFFFFF)
	minvalDBL = int32(-0x80000000)
)

// mapInvfMode is the 1:1 port of mapInvfMode (lpp_tran.cpp:147-168): the
// bandwidth-expansion factor for an inverse-filtering level.
func mapInvfMode(mode, prevMode invfMode, whFactors whiteningFactors) int32 {
	switch mode {
	case invfLowLevel:
		if prevMode == invfOff {
			return whFactors.transitionLevel
		}
		return whFactors.lowLevel
	case invfMidLevel:
		return whFactors.midLevel
	case invfHighLvl:
		return whFactors.highLevel
	default:
		if prevMode == invfLowLevel {
			return whFactors.transitionLevel
		}
		return whFactors.off
	}
}

// inverseFilteringLevelEmphasis is the 1:1 port of inverseFilteringLevelEmphasis
// (lpp_tran.cpp:178-204): smooth the per-band bandwidth factors over time.
func inverseFilteringLevelEmphasis(h *sbrLppTrans, nInvfBands int,
	sbrInvfMode, sbrInvfModePrev []invfMode, bwVector []int32) {
	for i := 0; i < nInvfBands; i++ {
		bwTmp := mapInvfMode(sbrInvfMode[i], sbrInvfModePrev[i], h.pSettings.whFactors)

		var accu int32
		if bwTmp < h.bwVectorOld[i] {
			accu = nativeaac.FMultDiv2DD(fxDBL0p75, bwTmp) +
				nativeaac.FMultDiv2DD(fxDBL0p25, h.bwVectorOld[i])
		} else {
			accu = nativeaac.FMultDiv2DD(fxDBL0p90625, bwTmp) +
				nativeaac.FMultDiv2DD(fxDBL0p09375, h.bwVectorOld[i])
		}

		if accu < fxDBL0p015625h {
			bwVector[i] = 0
		} else {
			bwVector[i] = nativeaac.FMinDBL(accu<<1, fxDBL0p99609375)
		}
	}
}

// calcQmfBufferReal is the 1:1 port of calc_qmfBufferReal (lpp_tran.cpp:215-235):
// the useLP middle-part filter step writing qmfBufferReal[startSample..][hiBand].
// lowBandReal is the slice positioned so lowBandReal[i+0/1/2] map to the C
// lowBandReal[i+0/1/2] (the caller passes &lowBandReal[LPC_ORDER+startSample-2]).
func calcQmfBufferReal(qmfBufferReal [][]int32, lowBandReal []int32,
	startSample, stopSample, hiBand, dynamicScale int, a0r, a1r int16) {
	dynscale := nativeaac.FMaxI(0, dynamicScale-1) + 1
	rescale := -nativeaac.FMinI(0, dynamicScale-1) + 1
	descale := nativeaac.FMinI(dfractBits-1, lpcScaleFactor+dynamicScale+rescale)

	for i := 0; i < stopSample-startSample; i++ {
		accu := nativeaac.FMultDiv2DS(lowBandReal[i], a1r) + nativeaac.FMultDiv2DS(lowBandReal[i+1], a0r)
		accu = (lowBandReal[i+2] >> uint(descale)) + (accu >> uint(dynscale))
		qmfBufferReal[i+startSample][hiBand] = nativeaac.SaturateLeftShift(accu, uint(rescale))
	}
}

// lowBandBufSize is (((1024)/(32)*(4)/2)+(3*(4)))+LPC_ORDER (lpp_tran.cpp:379) ==
// the per-band temporal low-band scratch length.
const lowBandBufSize = (((1024) / (32) * (4) / 2) + (3 * 4)) + lpcOrder

// lppTransposer is the 1:1 port of lppTransposer (lpp_tran.cpp:255-863): the
// useLP / high-quality (!useLP) LPP high-frequency generator. qmfBufferReal /
// qmfBufferImag are [slot][band]; degreeAlias is [band]. sbrScaleFactor is the
// QMF_SCALE_FACTOR. sbrInvfMode / sbrInvfModePrev are the per-band inverse
// filtering modes. When useLP, qmfBufferImag may be nil (not read).
func lppTransposer(h *sbrLppTrans, sbrScaleFactor *ScaleFactor,
	qmfBufferReal [][]int32, degreeAlias []int32, qmfBufferImag [][]int32,
	useLP bool, fPreWhitening bool, vKMaster0 int, timeStep, firstSlotOffs, lastSlotOffs,
	nInvfBands int, sbrInvfMode, sbrInvfModePrev []invfMode) {

	var bwIndex [maxNumPatches]int
	var bwVector [maxNumPatches]int32 // pole moving factors
	var preWhiteningGains [64 / 2]int32
	var preWhiteningGainsExp [64 / 2]int

	pSettings := h.pSettings
	patchParam := &pSettings.patchParam

	var alphar [lpcOrder]int16
	var a0r, a1r int16
	var alphai [lpcOrder]int16
	var a0i, a1i int16
	var bw int16

	var k1, k1Below, k1Below2 int32

	var ac acorrCoefs

	alphai[0] = 0
	alphai[1] = 0

	startSample := firstSlotOffs * timeStep
	stopSample := int(pSettings.nCols) + lastSlotOffs*timeStep

	inverseFilteringLevelEmphasis(h, nInvfBands, sbrInvfMode, sbrInvfModePrev, bwVector[:])

	stopSampleClear := stopSample
	autoCorrLength := int(pSettings.nCols) + int(pSettings.overlap)

	if pSettings.noOfPatches > 0 {
		// Set upper subbands to zero (patches may not cover the complete highband).
		targetStopBand := int(patchParam[pSettings.noOfPatches-1].targetStartBand) +
			int(patchParam[pSettings.noOfPatches-1].numBandsInPatch)
		if !useLP {
			for i := startSample; i < stopSampleClear; i++ {
				for b := targetStopBand; b < 64; b++ {
					qmfBufferReal[i][b] = 0
					qmfBufferImag[i][b] = 0
				}
			}
		} else {
			for i := startSample; i < stopSampleClear; i++ {
				for b := targetStopBand; b < 64; b++ {
					qmfBufferReal[i][b] = 0
				}
			}
		}
	}

	// init bwIndex for each patch
	for i := range bwIndex {
		bwIndex[i] = 0
	}

	// Calc common low band scale factor
	comLowBandScale := nativeaac.FMinI(sbrScaleFactor.OvLbScale, sbrScaleFactor.LbScale)

	ovLowBandShift := sbrScaleFactor.OvLbScale - comLowBandScale
	lowBandShift := sbrScaleFactor.LbScale - comLowBandScale

	if fPreWhitening {
		sbrDecoderCalculateGainVec(qmfBufferReal, qmfBufferImag,
			dfractBits-1-16-sbrScaleFactor.OvLbScale,
			dfractBits-1-16-sbrScaleFactor.LbScale,
			int(pSettings.overlap), preWhiteningGains[:], preWhiteningGainsExp[:],
			vKMaster0, startSample, stopSample)
	}

	// outer loop over bands to do analysis only once for each band
	var start, stop int
	if !useLP {
		start = int(pSettings.lbStartPatching)
		stop = int(pSettings.lbStopPatching)
	} else {
		start = nativeaac.FMaxI(1, int(pSettings.lbStartPatching)-2)
		stop = int(patchParam[0].targetStartBand)
	}

	for loBand := start; loBand < stop; loBand++ {
		var lowBandReal [lowBandBufSize]int32
		var lowBandImag [lowBandBufSize]int32
		// pqmfBufferReal/Imag base offset (qmfBufferReal + firstSlotOffs*timeStep).
		pqBase := firstSlotOffs * timeStep
		pr := 0 // running write index into lowBandReal
		pi := 0 // running write index into lowBandImag
		resetLPCCoeffs := false
		dynamicScale := dfractBits - 1 - lpcScaleFactor
		acDetScale := 0

		for i := 0; i < lpcOrder+firstSlotOffs*timeStep; i++ {
			lowBandReal[pr] = h.lpcFilterStatesRealLegSBR[i][loBand]
			pr++
			if !useLP {
				lowBandImag[pi] = h.lpcFilterStatesImagLegSBR[i][loBand]
				pi++
			}
		}

		// Take old slope length qmf slot source values out of (overlap)qmf buffer.
		if !useLP {
			for i := 0; i < int(pSettings.nCols)+int(pSettings.overlap)-firstSlotOffs*timeStep; i++ {
				lowBandReal[pr] = qmfBufferReal[pqBase+i][loBand]
				pr++
				lowBandImag[pi] = qmfBufferImag[pqBase+i][loBand]
				pi++
			}
		} else {
			// pSettings->overlap is always even
			half := (int(pSettings.nCols) + int(pSettings.overlap) - firstSlotOffs*timeStep) >> 1
			q := pqBase
			for i := 0; i < half; i++ {
				lowBandReal[pr] = qmfBufferReal[q][loBand]
				pr++
				q++
				lowBandReal[pr] = qmfBufferReal[q][loBand]
				pr++
				q++
			}
			if int(pSettings.nCols)&1 != 0 {
				lowBandReal[pr] = qmfBufferReal[q][loBand]
				pr++
				q++
			}
		}

		// Determine dynamic scaling value.
		dynamicScale = nativeaac.FMinI(dynamicScale,
			nativeaac.GetScalefactor(lowBandReal[:], lpcOrder+int(pSettings.overlap))+ovLowBandShift)
		dynamicScale = nativeaac.FMinI(dynamicScale,
			nativeaac.GetScalefactor(lowBandReal[lpcOrder+int(pSettings.overlap):], int(pSettings.nCols))+lowBandShift)
		if !useLP {
			dynamicScale = nativeaac.FMinI(dynamicScale,
				nativeaac.GetScalefactor(lowBandImag[:], lpcOrder+int(pSettings.overlap))+ovLowBandShift)
			dynamicScale = nativeaac.FMinI(dynamicScale,
				nativeaac.GetScalefactor(lowBandImag[lpcOrder+int(pSettings.overlap):], int(pSettings.nCols))+lowBandShift)
		}

		if dynamicScale == 0 {
			// Special case: limit the spectrum to prevent -1.0.
			for i := 0; i < lpcOrder+int(pSettings.overlap)+int(pSettings.nCols); i++ {
				lowBandReal[i] = nativeaac.FMaxDBL(lowBandReal[i], int32(-0x7FFFFFFF))
			}
			if !useLP {
				for i := 0; i < lpcOrder+int(pSettings.overlap)+int(pSettings.nCols); i++ {
					lowBandImag[i] = nativeaac.FMaxDBL(lowBandImag[i], int32(-0x7FFFFFFF))
				}
			}
		} else {
			dynamicScale = nativeaac.FMaxI(0, dynamicScale-1) // one extra bit headroom
		}

		// Scale temporal QMF buffer.
		nativeaac.ScaleValues(lowBandReal[:], lpcOrder+int(pSettings.overlap), int32(dynamicScale-ovLowBandShift))
		nativeaac.ScaleValues(lowBandReal[lpcOrder+int(pSettings.overlap):], int(pSettings.nCols), int32(dynamicScale-lowBandShift))
		if !useLP {
			nativeaac.ScaleValues(lowBandImag[:], lpcOrder+int(pSettings.overlap), int32(dynamicScale-ovLowBandShift))
			nativeaac.ScaleValues(lowBandImag[lpcOrder+int(pSettings.overlap):], int(pSettings.nCols), int32(dynamicScale-lowBandShift))
		}

		if !useLP {
			acDetScale += autoCorr2ndCplx(&ac, lowBandReal[:], lowBandImag[:], lpcOrder, autoCorrLength)
		} else {
			acDetScale += autoCorr2ndReal(&ac, lowBandReal[:], lpcOrder, autoCorrLength)
		}

		// Examine dynamic of determinant in autocorrelation.
		acDetScale += 2 * (comLowBandScale + dynamicScale)
		acDetScale *= 2 // two times reflection coefficient scaling
		acDetScale += ac.detScale

		if acDetScale > 126 {
			resetLPCCoeffs = true
		}

		alphar[1] = 0
		if !useLP {
			alphai[1] = 0
		}

		if ac.det != 0 {
			absDet := nativeaac.FixpAbs(ac.det)
			var tmp, absTmp int32

			if !useLP {
				tmp = (nativeaac.FMultDiv2DD(ac.r01r, ac.r12r) >> (lpcScaleFactor - 1)) -
					((nativeaac.FMultDiv2DD(ac.r01i, ac.r12i) + nativeaac.FMultDiv2DD(ac.r02r, ac.r11r)) >> (lpcScaleFactor - 1))
			} else {
				tmp = (nativeaac.FMultDiv2DD(ac.r01r, ac.r12r) >> (lpcScaleFactor - 1)) -
					(nativeaac.FMultDiv2DD(ac.r02r, ac.r11r) >> (lpcScaleFactor - 1))
			}
			absTmp = nativeaac.FixpAbs(tmp)

			// Quick check: is first filter coeff >= 1(4)
			result, scale := nativeaac.FDivNorm(absTmp, absDet)
			scaleI := int(scale) + ac.detScale
			if scaleI > 0 && result >= (maxvalDBL>>uint(scaleI)) {
				resetLPCCoeffs = true
			} else {
				alphar[1] = nativeaac.FxDbl2FxSgl(nativeaac.ScaleValueSaturate(result, int32(scaleI)))
				if (tmp < 0) != (ac.det < 0) {
					alphar[1] = -alphar[1]
				}
			}

			if !useLP {
				tmp = (nativeaac.FMultDiv2DD(ac.r01i, ac.r12r) >> (lpcScaleFactor - 1)) +
					((nativeaac.FMultDiv2DD(ac.r01r, ac.r12i) - nativeaac.FMultDiv2DD(ac.r02i, ac.r11r)) >> (lpcScaleFactor - 1))
				absTmp = nativeaac.FixpAbs(tmp)

				result2, scale2 := nativeaac.FDivNorm(absTmp, absDet)
				scale2I := int(scale2) + ac.detScale
				if scale2I > 0 && result2 >= (maxvalDBL>>uint(scale2I)) {
					resetLPCCoeffs = true
				} else {
					alphai[1] = nativeaac.FxDbl2FxSgl(nativeaac.ScaleValueSaturate(result2, int32(scale2I)))
					if (tmp < 0) != (ac.det < 0) {
						alphai[1] = -alphai[1]
					}
				}
			}
		}

		alphar[0] = 0
		if !useLP {
			alphai[0] = 0
		}

		if ac.r11r != 0 {
			// ac.r11r is always >=0
			var tmp, absTmp int32

			if !useLP {
				tmp = (ac.r01r >> (lpcScaleFactor + 1)) +
					(nativeaac.FMultDiv2DS(ac.r12r, alphar[1]) + nativeaac.FMultDiv2DS(ac.r12i, alphai[1]))
			} else {
				if ac.r01r >= 0 {
					tmp = (ac.r01r >> (lpcScaleFactor + 1)) + nativeaac.FMultDiv2DS(ac.r12r, alphar[1])
				} else {
					tmp = -((-ac.r01r) >> (lpcScaleFactor + 1)) + nativeaac.FMultDiv2DS(ac.r12r, alphar[1])
				}
			}
			absTmp = nativeaac.FixpAbs(tmp)

			// Quick check: is first filter coeff >= 1(4)
			if absTmp >= (ac.r11r >> 1) {
				resetLPCCoeffs = true
			} else {
				result, scale := nativeaac.FDivNorm(absTmp, nativeaac.FixpAbs(ac.r11r))
				alphar[0] = nativeaac.FxDbl2FxSgl(nativeaac.ScaleValueSaturate(result, scale+1))
				if (tmp > 0) != (ac.r11r < 0) {
					alphar[0] = -alphar[0]
				}
			}

			if !useLP {
				tmp = (ac.r01i >> (lpcScaleFactor + 1)) +
					(nativeaac.FMultDiv2DS(ac.r12r, alphai[1]) - nativeaac.FMultDiv2DS(ac.r12i, alphar[1]))
				absTmp = nativeaac.FixpAbs(tmp)

				if absTmp >= (ac.r11r >> 1) {
					resetLPCCoeffs = true
				} else {
					result, scale := nativeaac.FDivNorm(absTmp, nativeaac.FixpAbs(ac.r11r))
					alphai[0] = nativeaac.FxDbl2FxSgl(nativeaac.ScaleValueSaturate(result, scale+1))
					if (tmp > 0) != (ac.r11r < 0) {
						alphai[0] = -alphai[0]
					}
				}
			}
		}

		if !useLP {
			// Now check the quadratic criteria
			if (nativeaac.FMultDiv2SS(alphar[0], alphar[0]) + nativeaac.FMultDiv2SS(alphai[0], alphai[0])) >= fxDBL0p5 {
				resetLPCCoeffs = true
			}
			if (nativeaac.FMultDiv2SS(alphar[1], alphar[1]) + nativeaac.FMultDiv2SS(alphai[1], alphai[1])) >= fxDBL0p5 {
				resetLPCCoeffs = true
			}
		}

		if resetLPCCoeffs {
			alphar[0] = 0
			alphar[1] = 0
			if !useLP {
				alphai[0] = 0
				alphai[1] = 0
			}
		}

		if useLP {
			// Aliasing detection
			if ac.r11r == 0 {
				k1 = 0
			} else {
				if nativeaac.FixpAbs(ac.r01r) >= nativeaac.FixpAbs(ac.r11r) {
					if nativeaac.FMultDiv2DD(ac.r01r, ac.r11r) < 0 {
						k1 = maxvalDBL
					} else {
						k1 = minvalDBL + 1
					}
				} else {
					result, scale := nativeaac.FDivNorm(nativeaac.FixpAbs(ac.r01r), nativeaac.FixpAbs(ac.r11r))
					k1 = nativeaac.ScaleValueSaturate(result, scale)
					if !((ac.r01r < 0) != (ac.r11r < 0)) {
						k1 = -k1
					}
				}
			}
			if loBand > 1 && loBand < vKMaster0 {
				// Check if the gain should be locked
				deg := maxvalDBL - nativeaac.FPow2(k1Below)
				degreeAlias[loBand] = 0
				if (loBand&1) == 0 && k1 < 0 {
					if k1Below < 0 { // 2-Ch Aliasing Detection
						degreeAlias[loBand] = maxvalDBL
						if k1Below2 > 0 { // 3-Ch Aliasing Detection
							degreeAlias[loBand-1] = deg
						}
					} else if k1Below2 > 0 { // 3-Ch Aliasing Detection
						degreeAlias[loBand] = deg
					}
				}
				if (loBand&1) == 1 && k1 > 0 {
					if k1Below > 0 { // 2-CH Aliasing Detection
						degreeAlias[loBand] = maxvalDBL
						if k1Below2 < 0 { // 3-CH Aliasing Detection
							degreeAlias[loBand-1] = deg
						}
					} else if k1Below2 < 0 { // 3-CH Aliasing Detection
						degreeAlias[loBand] = deg
					}
				}
			}
			// remember k1 values of the 2 QMF channels below the current channel
			k1Below2 = k1Below
			k1Below = k1
		}

		patch := 0
		for patch < int(pSettings.noOfPatches) { // inner loop over every patch
			hiBand := loBand + int(patchParam[patch].targetBandOffs)

			if loBand < int(patchParam[patch].sourceStartBand) ||
				loBand >= int(patchParam[patch].sourceStopBand) {
				patch++
				continue
			}

			// bwIndex[patch] already initialized with value from previous band.
			for hiBand >= int(pSettings.bwBorders[bwIndex[patch]]) && bwIndex[patch] < maxNumPatches-1 {
				bwIndex[patch]++
			}

			// Filter Step 2: add the left slope with the current filter to the buffer.
			bw = nativeaac.FxDbl2FxSgl(bwVector[bwIndex[patch]])

			a0r = nativeaac.FxDbl2FxSgl(nativeaac.FMultSS(bw, alphar[0])) // apply bandwidth expansion
			if !useLP {
				a0i = nativeaac.FxDbl2FxSgl(nativeaac.FMultSS(bw, alphai[0]))
			}
			bw = nativeaac.FxDbl2FxSgl(nativeaac.FPow2S(bw))
			a1r = nativeaac.FxDbl2FxSgl(nativeaac.FMultSS(bw, alphar[1]))
			if !useLP {
				a1i = nativeaac.FxDbl2FxSgl(nativeaac.FMultSS(bw, alphai[1]))
			}

			// Filter Step 3: insert the middle part which won't be windowed.
			if bw <= 0 {
				if !useLP {
					descale := nativeaac.FMinI(dfractBits-1, lpcScaleFactor+dynamicScale)
					for i := startSample; i < stopSample; i++ {
						accu1 := lowBandReal[lpcOrder+i] >> uint(descale)
						accu2 := lowBandImag[lpcOrder+i] >> uint(descale)
						if fPreWhitening {
							accu1 = nativeaac.ScaleValueSaturate(
								nativeaac.FMultDiv2DD(accu1, preWhiteningGains[loBand]),
								int32(preWhiteningGainsExp[loBand]+1))
							accu2 = nativeaac.ScaleValueSaturate(
								nativeaac.FMultDiv2DD(accu2, preWhiteningGains[loBand]),
								int32(preWhiteningGainsExp[loBand]+1))
						}
						qmfBufferReal[i][hiBand] = accu1
						qmfBufferImag[i][hiBand] = accu2
					}
				} else {
					descale := nativeaac.FMinI(dfractBits-1, lpcScaleFactor+dynamicScale)
					for i := startSample; i < stopSample; i++ {
						qmfBufferReal[i][hiBand] = lowBandReal[lpcOrder+i] >> uint(descale)
					}
				}
			} else { // bw <= 0 (else: bw > 0)
				if !useLP {
					dynscale := nativeaac.FMaxI(0, dynamicScale-2) + 1
					rescale := -nativeaac.FMinI(0, dynamicScale-2) + 1
					descale := nativeaac.FMinI(dfractBits-1, lpcScaleFactor+dynamicScale+rescale)

					for i := startSample; i < stopSample; i++ {
						accu1 := ((nativeaac.FMultDiv2DS(lowBandReal[lpcOrder+i-1], a0r) -
							nativeaac.FMultDiv2DS(lowBandImag[lpcOrder+i-1], a0i)) >> 1) +
							((nativeaac.FMultDiv2DS(lowBandReal[lpcOrder+i-2], a1r) -
								nativeaac.FMultDiv2DS(lowBandImag[lpcOrder+i-2], a1i)) >> 1)
						accu2 := ((nativeaac.FMultDiv2DS(lowBandReal[lpcOrder+i-1], a0i) +
							nativeaac.FMultDiv2DS(lowBandImag[lpcOrder+i-1], a0r)) >> 1) +
							((nativeaac.FMultDiv2DS(lowBandReal[lpcOrder+i-2], a1i) +
								nativeaac.FMultDiv2DS(lowBandImag[lpcOrder+i-2], a1r)) >> 1)

						accu1 = (lowBandReal[lpcOrder+i] >> uint(descale)) + (accu1 >> uint(dynscale))
						accu2 = (lowBandImag[lpcOrder+i] >> uint(descale)) + (accu2 >> uint(dynscale))
						if fPreWhitening {
							qmfBufferReal[i][hiBand] = nativeaac.ScaleValueSaturate(
								nativeaac.FMultDiv2DD(accu1, preWhiteningGains[loBand]),
								int32(preWhiteningGainsExp[loBand]+1+rescale))
							qmfBufferImag[i][hiBand] = nativeaac.ScaleValueSaturate(
								nativeaac.FMultDiv2DD(accu2, preWhiteningGains[loBand]),
								int32(preWhiteningGainsExp[loBand]+1+rescale))
						} else {
							qmfBufferReal[i][hiBand] = nativeaac.SaturateLeftShift(accu1, uint(rescale))
							qmfBufferImag[i][hiBand] = nativeaac.SaturateLeftShift(accu2, uint(rescale))
						}
					}
				} else {
					calcQmfBufferReal(qmfBufferReal, lowBandReal[lpcOrder+startSample-2:],
						startSample, stopSample, hiBand, dynamicScale, a0r, a1r)
				}
			} // bw <= 0

			patch++
		} // inner loop over patches
	} // outer loop over bands (loBand)

	if useLP {
		for loBand := int(pSettings.lbStartPatching); loBand < int(pSettings.lbStopPatching); loBand++ {
			patch := 0
			for patch < int(pSettings.noOfPatches) {
				hiBand := loBand + int(patchParam[patch].targetBandOffs)

				if loBand < int(patchParam[patch].sourceStartBand) ||
					loBand >= int(patchParam[patch].sourceStopBand) ||
					hiBand >= 64 {
					patch++
					continue
				}

				if hiBand != int(patchParam[patch].targetStartBand) {
					degreeAlias[hiBand] = degreeAlias[loBand]
				}
				patch++
			}
		}
	}

	for i := 0; i < nInvfBands; i++ {
		h.bwVectorOld[i] = bwVector[i]
	}

	// set high band scale factor
	sbrScaleFactor.HbScale = comLowBandScale - lpcScaleFactor
}

// lppTransposerHBE is the 1:1 port of lppTransposerHBE (lpp_tran.cpp:865-1233):
// the high-quality LPP whitening filter applied to the HBE (harmonic) transposer
// output. Always complex (no useLP branch). hQmfStartBand / hQmfStopBand are the
// HBE transposer's startBand / stopBand. qmfBuffer{Real,Imag} are [slot][band].
func lppTransposerHBE(h *sbrLppTrans, hQmfStartBand, hQmfStopBand int,
	sbrScaleFactor *ScaleFactor, qmfBufferReal, qmfBufferImag [][]int32,
	timeStep, firstSlotOffs, lastSlotOffs, nInvfBands int,
	sbrInvfMode, sbrInvfModePrev []invfMode) {

	var bwIndex int
	var bwVector [maxNumPatchesHBE]int32

	pSettings := h.pSettings
	patchParam := &pSettings.patchParam

	var alphar [lpcOrder]int16
	var a0r, a1r int16
	var alphai [lpcOrder]int16
	var a0i, a1i int16
	var bw int16

	var ac acorrCoefs

	alphai[0] = 0
	alphai[1] = 0

	startSample := firstSlotOffs * timeStep
	stopSample := int(pSettings.nCols) + lastSlotOffs*timeStep

	inverseFilteringLevelEmphasis(h, nInvfBands, sbrInvfMode, sbrInvfModePrev, bwVector[:])

	stopSampleClear := stopSample
	autoCorrLength := int(pSettings.nCols) + int(pSettings.overlap)

	if pSettings.noOfPatches > 0 {
		targetStopBand := int(patchParam[pSettings.noOfPatches-1].targetStartBand) +
			int(patchParam[pSettings.noOfPatches-1].numBandsInPatch)
		for i := startSample; i < stopSampleClear; i++ {
			for b := targetStopBand; b < 64; b++ {
				qmfBufferReal[i][b] = 0
				qmfBufferImag[i][b] = 0
			}
		}
	}

	// Calc common low band scale factor
	comBandScale := sbrScaleFactor.HbScale
	ovLowBandShift := sbrScaleFactor.HbScale - comBandScale
	lowBandShift := sbrScaleFactor.HbScale - comBandScale

	start := hQmfStartBand
	stop := hQmfStopBand

	for loBand := start; loBand < stop; loBand++ {
		bwIndex = 0

		var lowBandReal [lowBandBufSize]int32
		var lowBandImag [lowBandBufSize]int32

		resetLPCCoeffs := false
		dynamicScale := dfractBits - 1 - lpcScaleFactor
		acDetScale := 0

		var i int
		for i = 0; i < lpcOrder; i++ {
			lowBandReal[i] = h.lpcFilterStatesRealHBE[i][loBand]
			lowBandImag[i] = h.lpcFilterStatesImagHBE[i][loBand]
		}
		for ; i < lpcOrder+firstSlotOffs*timeStep; i++ {
			lowBandReal[i] = h.lpcFilterStatesRealHBE[i][loBand]
			lowBandImag[i] = h.lpcFilterStatesImagHBE[i][loBand]
		}

		// Take old slope length qmf slot source values out of (overlap)qmf buffer.
		for i = firstSlotOffs * timeStep; i < int(pSettings.nCols)+int(pSettings.overlap); i++ {
			lowBandReal[i+lpcOrder] = qmfBufferReal[i][loBand]
			lowBandImag[i+lpcOrder] = qmfBufferImag[i][loBand]
		}

		// store unmodified values to buffer
		for i = 0; i < lpcOrder+int(pSettings.overlap); i++ {
			h.lpcFilterStatesRealHBE[i][loBand] = qmfBufferReal[int(pSettings.nCols)-lpcOrder+i][loBand]
			h.lpcFilterStatesImagHBE[i][loBand] = qmfBufferImag[int(pSettings.nCols)-lpcOrder+i][loBand]
		}

		// Determine dynamic scaling value.
		dynamicScale = nativeaac.FMinI(dynamicScale,
			nativeaac.GetScalefactor(lowBandReal[:], lpcOrder+int(pSettings.overlap))+ovLowBandShift)
		dynamicScale = nativeaac.FMinI(dynamicScale,
			nativeaac.GetScalefactor(lowBandReal[lpcOrder+int(pSettings.overlap):], int(pSettings.nCols))+lowBandShift)
		dynamicScale = nativeaac.FMinI(dynamicScale,
			nativeaac.GetScalefactor(lowBandImag[:], lpcOrder+int(pSettings.overlap))+ovLowBandShift)
		dynamicScale = nativeaac.FMinI(dynamicScale,
			nativeaac.GetScalefactor(lowBandImag[lpcOrder+int(pSettings.overlap):], int(pSettings.nCols))+lowBandShift)

		dynamicScale = dynamicScale - 1 // one additional bit headroom to prevent -1.0

		// Scale temporal QMF buffer.
		nativeaac.ScaleValues(lowBandReal[:], lpcOrder+int(pSettings.overlap), int32(dynamicScale-ovLowBandShift))
		nativeaac.ScaleValues(lowBandReal[lpcOrder+int(pSettings.overlap):], int(pSettings.nCols), int32(dynamicScale-lowBandShift))
		nativeaac.ScaleValues(lowBandImag[:], lpcOrder+int(pSettings.overlap), int32(dynamicScale-ovLowBandShift))
		nativeaac.ScaleValues(lowBandImag[lpcOrder+int(pSettings.overlap):], int(pSettings.nCols), int32(dynamicScale-lowBandShift))

		acDetScale += autoCorr2ndCplx(&ac, lowBandReal[:], lowBandImag[:], lpcOrder, autoCorrLength)

		// Examine dynamic of determinant in autocorrelation.
		acDetScale += 2 * (comBandScale + dynamicScale)
		acDetScale *= 2
		acDetScale += ac.detScale

		if acDetScale > 126 {
			resetLPCCoeffs = true
		}

		alphar[1] = 0
		alphai[1] = 0

		if ac.det != 0 {
			absDet := nativeaac.FixpAbs(ac.det)
			var tmp, absTmp int32

			tmp = (nativeaac.FMultDiv2DD(ac.r01r, ac.r12r) >> (lpcScaleFactor - 1)) -
				((nativeaac.FMultDiv2DD(ac.r01i, ac.r12i) + nativeaac.FMultDiv2DD(ac.r02r, ac.r11r)) >> (lpcScaleFactor - 1))
			absTmp = nativeaac.FixpAbs(tmp)

			result, scale := nativeaac.FDivNorm(absTmp, absDet)
			scaleI := int(scale) + ac.detScale
			if scaleI > 0 && result >= (maxvalDBL>>uint(scaleI)) {
				resetLPCCoeffs = true
			} else {
				alphar[1] = nativeaac.FxDbl2FxSgl(nativeaac.ScaleValueSaturate(result, int32(scaleI)))
				if (tmp < 0) != (ac.det < 0) {
					alphar[1] = -alphar[1]
				}
			}

			tmp = (nativeaac.FMultDiv2DD(ac.r01i, ac.r12r) >> (lpcScaleFactor - 1)) +
				((nativeaac.FMultDiv2DD(ac.r01r, ac.r12i) - nativeaac.FMultDiv2DD(ac.r02i, ac.r11r)) >> (lpcScaleFactor - 1))
			absTmp = nativeaac.FixpAbs(tmp)

			result2, scale2 := nativeaac.FDivNorm(absTmp, absDet)
			scale2I := int(scale2) + ac.detScale
			if scale2I > 0 && result2 >= (maxvalDBL>>uint(scale2I)) {
				resetLPCCoeffs = true
			} else {
				alphai[1] = nativeaac.FxDbl2FxSgl(nativeaac.ScaleValueSaturate(result2, int32(scale2I)))
				if (tmp < 0) != (ac.det < 0) {
					alphai[1] = -alphai[1]
				}
			}
		}

		alphar[0] = 0
		alphai[0] = 0

		if ac.r11r != 0 {
			var tmp, absTmp int32

			tmp = (ac.r01r >> (lpcScaleFactor + 1)) +
				(nativeaac.FMultDiv2DS(ac.r12r, alphar[1]) + nativeaac.FMultDiv2DS(ac.r12i, alphai[1]))
			absTmp = nativeaac.FixpAbs(tmp)

			if absTmp >= (ac.r11r >> 1) {
				resetLPCCoeffs = true
			} else {
				result, scale := nativeaac.FDivNorm(absTmp, nativeaac.FixpAbs(ac.r11r))
				alphar[0] = nativeaac.FxDbl2FxSgl(nativeaac.ScaleValueSaturate(result, scale+1))
				if (tmp > 0) != (ac.r11r < 0) {
					alphar[0] = -alphar[0]
				}
			}

			tmp = (ac.r01i >> (lpcScaleFactor + 1)) +
				(nativeaac.FMultDiv2DS(ac.r12r, alphai[1]) - nativeaac.FMultDiv2DS(ac.r12i, alphar[1]))
			absTmp = nativeaac.FixpAbs(tmp)

			if absTmp >= (ac.r11r >> 1) {
				resetLPCCoeffs = true
			} else {
				result, scale := nativeaac.FDivNorm(absTmp, nativeaac.FixpAbs(ac.r11r))
				alphai[0] = nativeaac.FxDbl2FxSgl(nativeaac.ScaleValueSaturate(result, scale+1))
				if (tmp > 0) != (ac.r11r < 0) {
					alphai[0] = -alphai[0]
				}
			}
		}

		// Now check the quadratic criteria
		if (nativeaac.FMultDiv2SS(alphar[0], alphar[0]) + nativeaac.FMultDiv2SS(alphai[0], alphai[0])) >= fxDBL0p5 {
			resetLPCCoeffs = true
		}
		if (nativeaac.FMultDiv2SS(alphar[1], alphar[1]) + nativeaac.FMultDiv2SS(alphai[1], alphai[1])) >= fxDBL0p5 {
			resetLPCCoeffs = true
		}

		if resetLPCCoeffs {
			alphar[0] = 0
			alphar[1] = 0
			alphai[0] = 0
			alphai[1] = 0
		}

		for bwIndex < maxNumPatches-1 && loBand >= int(pSettings.bwBorders[bwIndex]) {
			bwIndex++
		}

		// Filter Step 2: add the left slope with the current filter to the buffer.
		bw = nativeaac.FxDbl2FxSgl(bwVector[bwIndex])

		a0r = nativeaac.FxDbl2FxSgl(nativeaac.FMultSS(bw, alphar[0]))
		a0i = nativeaac.FxDbl2FxSgl(nativeaac.FMultSS(bw, alphai[0]))
		bw = nativeaac.FxDbl2FxSgl(nativeaac.FPow2S(bw))
		a1r = nativeaac.FxDbl2FxSgl(nativeaac.FMultSS(bw, alphar[1]))
		a1i = nativeaac.FxDbl2FxSgl(nativeaac.FMultSS(bw, alphai[1]))

		// Filter Step 3: insert the middle part which won't be windowed.
		if bw <= 0 {
			descale := nativeaac.FMinI(dfractBits-1, lpcScaleFactor+dynamicScale)
			for i = startSample; i < stopSample; i++ {
				qmfBufferReal[i][loBand] = lowBandReal[lpcOrder+i] >> uint(descale)
				qmfBufferImag[i][loBand] = lowBandImag[lpcOrder+i] >> uint(descale)
			}
		} else { // bw <= 0
			descale := nativeaac.FMinI(dfractBits-1, lpcScaleFactor+dynamicScale)
			dynamicScale += 1 // prevent negative scale factor due to 'one additional bit headroom'

			for i = startSample; i < stopSample; i++ {
				accu1 := (nativeaac.FMultDiv2DS(lowBandReal[lpcOrder+i-1], a0r) -
					nativeaac.FMultDiv2DS(lowBandImag[lpcOrder+i-1], a0i) +
					nativeaac.FMultDiv2DS(lowBandReal[lpcOrder+i-2], a1r) -
					nativeaac.FMultDiv2DS(lowBandImag[lpcOrder+i-2], a1i)) >> uint(dynamicScale)
				accu2 := (nativeaac.FMultDiv2DS(lowBandReal[lpcOrder+i-1], a0i) +
					nativeaac.FMultDiv2DS(lowBandImag[lpcOrder+i-1], a0r) +
					nativeaac.FMultDiv2DS(lowBandReal[lpcOrder+i-2], a1i) +
					nativeaac.FMultDiv2DS(lowBandImag[lpcOrder+i-2], a1r)) >> uint(dynamicScale)

				qmfBufferReal[i][loBand] = (lowBandReal[lpcOrder+i] >> uint(descale)) + (accu1 << (1 + 1))
				qmfBufferImag[i][loBand] = (lowBandImag[lpcOrder+i] >> uint(descale)) + (accu2 << (1 + 1))
			}
		} // bw <= 0
	} // outer loop over bands (loBand)

	for i := 0; i < nInvfBands; i++ {
		h.bwVectorOld[i] = bwVector[i]
	}

	// set high band scale factor
	sbrScaleFactor.HbScale = comBandScale - lpcScaleFactor
}

// findClosestEntry is the 1:1 port of findClosestEntry (lpp_tran.cpp:1281-1302).
func findClosestEntry(goalSb uint8, vKMaster []uint8, numMaster uint8, direction uint8) int {
	if goalSb <= vKMaster[0] {
		return int(vKMaster[0])
	}
	if goalSb >= vKMaster[numMaster] {
		return int(vKMaster[numMaster])
	}

	var index int
	if direction != 0 {
		index = 0
		for vKMaster[index] < goalSb {
			index++
		}
	} else {
		index = int(numMaster)
		for vKMaster[index] > goalSb {
			index--
		}
	}
	return int(vKMaster[index])
}

// createLppTransposer is the 1:1 port of createLppTransposer (lpp_tran.cpp:1242-
// 1279). chan 0 initialises the common data once via resetLppTransposer.
func createLppTransposer(hs *sbrLppTrans, pSettings *transposerSettings, highBandStartSb int,
	vKMaster []uint8, numMaster, usb, timeSlots, nCols int, noiseBandTable []uint8,
	noNoiseBands int, fs uint, ch, overlap int) sbrError {

	hs.pSettings = pSettings
	pSettings.nCols = uint8(nCols)
	pSettings.overlap = uint8(overlap)

	switch timeSlots {
	case 15, 16:
	default:
		return sbrdecUnsupportedConfig
	}

	if ch == 0 {
		hs.pSettings.nCols = uint8(nCols)
		return resetLppTransposer(hs, uint8(highBandStartSb), vKMaster, uint8(numMaster),
			noiseBandTable, uint8(noNoiseBands), uint8(usb), fs)
	}
	return sbrdecOK
}

// resetLppTransposer is the 1:1 port of resetLppTransposer (lpp_tran.cpp:1310-
// 1488): compute the patch layout and whitening factors from the master table.
func resetLppTransposer(hLppTrans *sbrLppTrans, highBandStartSb uint8, vKMaster []uint8,
	numMaster uint8, noiseBandTable []uint8, noNoiseBands uint8, usb uint8, fs uint) sbrError {

	pSettings := hLppTrans.pSettings
	patchParam := &pSettings.patchParam

	lsb := int(vKMaster[0])
	xoverOffset := int(highBandStartSb) - lsb

	usbI := nativeaac.FMinI(int(usb), int(vKMaster[numMaster]))

	// Plausibility check
	if pSettings.nCols == 64 {
		if lsb < 4 {
			return sbrdecUnsupportedConfig
		}
	} else if lsb-shiftStartSb < 4 {
		return sbrdecUnsupportedConfig
	}

	// Initialize the patching parameter.
	// ISO/IEC 14496-3 (Figure 4.48): goalSb = round( 2.048e6 / fs )
	desiredBorder := (((2048000 * 2) / int(fs)) + 1) >> 1
	desiredBorder = findClosestEntry(uint8(desiredBorder), vKMaster, numMaster, 1)

	// First patch
	sourceStartBand := shiftStartSb + xoverOffset
	targetStopBand := lsb + xoverOffset

	patch := 0
	for targetStopBand < usbI {
		if patch > maxNumPatches {
			return sbrdecUnsupportedConfig
		}

		patchParam[patch].guardStartBand = uint8(targetStopBand)
		patchParam[patch].targetStartBand = uint8(targetStopBand)

		numBandsInPatch := desiredBorder - targetStopBand

		if numBandsInPatch >= lsb-sourceStartBand {
			patchDistance := targetStopBand - sourceStartBand
			patchDistance = patchDistance & ^1
			numBandsInPatch = lsb - (targetStopBand - patchDistance)
			numBandsInPatch = findClosestEntry(uint8(targetStopBand+numBandsInPatch), vKMaster, numMaster, 0) - targetStopBand
		}

		if pSettings.nCols == 64 {
			if numBandsInPatch == 0 && sourceStartBand == shiftStartSb {
				return sbrdecUnsupportedConfig
			}
		}

		// minimal even patching distance
		patchDistance := numBandsInPatch + targetStopBand - lsb
		patchDistance = (patchDistance + 1) & ^1

		if numBandsInPatch > 0 {
			patchParam[patch].sourceStartBand = uint8(targetStopBand - patchDistance)
			patchParam[patch].targetBandOffs = uint8(patchDistance)
			patchParam[patch].numBandsInPatch = uint8(numBandsInPatch)
			patchParam[patch].sourceStopBand = uint8(int(patchParam[patch].sourceStartBand) + numBandsInPatch)

			targetStopBand += int(patchParam[patch].numBandsInPatch)
			patch++
		}

		// All patches but first
		sourceStartBand = shiftStartSb

		// Check if we are close to desiredBorder
		if desiredBorder-targetStopBand < 3 {
			desiredBorder = usbI
		}
	}

	patch--

	// If highest patch contains less than three subbands: skip it
	if patch > 0 && int(patchParam[patch].numBandsInPatch) < 3 {
		patch--
		targetStopBand = int(patchParam[patch].targetStartBand) + int(patchParam[patch].numBandsInPatch)
	}

	// now check if we don't have one too many
	if patch >= maxNumPatches {
		return sbrdecUnsupportedConfig
	}

	pSettings.noOfPatches = uint8(patch + 1)

	// Check lowest and highest source subband
	pSettings.lbStartPatching = uint8(targetStopBand)
	pSettings.lbStopPatching = 0
	for p := 0; p < int(pSettings.noOfPatches); p++ {
		pSettings.lbStartPatching = uint8(nativeaac.FMinI(int(pSettings.lbStartPatching), int(patchParam[p].sourceStartBand)))
		pSettings.lbStopPatching = uint8(nativeaac.FMaxI(int(pSettings.lbStopPatching), int(patchParam[p].sourceStopBand)))
	}

	var i int
	for i = 0; i < int(noNoiseBands); i++ {
		pSettings.bwBorders[i] = noiseBandTable[i+1]
	}
	for ; i < maxNumNoiseValues; i++ {
		pSettings.bwBorders[i] = 255
	}

	// Choose whitening factors
	startFreqHz := ((lsb + xoverOffset) * int(fs)) >> 7 // Shift does a division by 2*(64)

	var wi int
	for wi = 1; wi < numWhFactorTableEntries; wi++ {
		if startFreqHz < int(whFactorsIndex[wi]) {
			break
		}
	}
	wi--

	pSettings.whFactors.off = whFactorsTable[wi][0]
	pSettings.whFactors.transitionLevel = whFactorsTable[wi][1]
	pSettings.whFactors.lowLevel = whFactorsTable[wi][2]
	pSettings.whFactors.midLevel = whFactorsTable[wi][3]
	pSettings.whFactors.highLevel = whFactorsTable[wi][4]

	return sbrdecOK
}
