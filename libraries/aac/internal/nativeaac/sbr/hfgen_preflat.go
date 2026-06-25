// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of libSBRdec/src/HFgen_preFlat.cpp: the pre-flattening that the LPP
// transposer applies when fPreWhitening is set. sbrDecoder_calculateGainVec
// (HFgen_preFlat.cpp:862) computes a per-band whitening gain vector by fitting a
// degree-3 polynomial to the low-band log-energy envelope (a least-squares
// Vandermonde / normal-equations problem solved by a pre-normalised Cholesky
// backsubstitution, polyfit -> choleskySolve -> backsubst_fw/_bw) and evaluating
// it with a Horner scheme (polyval). All fixed-point integer (FIXP_DBL Q-format,
// FIXP_CHB == FIXP_SGL ROM), so EXACT-integer parity holds in any build.
//
// Helper mapping:
//   fMultNorm(a,b,&e) -> nativeaac.FMultNorm (returns product + exponent)
//   fMult(a,b)        -> nativeaac.FMultDD   (fixmul_DD)
//   CountLeadingBits  -> nativeaac.CountLeadingBits (fixnorm_D)
//   CntLeadingZeros   -> nativeaac.CntLeadingZeros  (fixnormz_D)
//   scaleValue        -> nativeaac.ScaleValue
//   GetInvInt         -> nativeaac.GetInvInt
//   CalcLog2(m,e,&re) -> nativeaac.CalcLog2 (returns mantissa + exponent)
//   f2Pow(m,e,&re)    -> nativeaac.F2Pow    (returns mantissa + exponent)
//   FX_CHB2FX_DBL(a)  -> FX_SGL2FX_DBL(a) == int32(a) << 16

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// HFgen pre-flattening constants (HFgen_preFlat.cpp:108-143).
const (
	polyOrder   = 3  // POLY_ORDER
	maxLowBands = 32 // MAXLOWBANDS
	sumSafety   = 2  // SUM_SAFETY (HFgen_preFlat.cpp:551)
)

// log10Fac / log10FacInv are LOG10FAC / LOG10FAC_INV (HFgen_preFlat.cpp:110-111),
// narrowed to FIXP_SGL through the C `f`-suffixed FL2FXCONST_SGL. The literals
// carry the C float `f` suffix, so each is narrowed through float32 first.
var (
	log10Fac    = nativeaac.Fl2fxconstSGL(float64(float32(0.752574989159953)))
	log10FacInv = nativeaac.Fl2fxconstSGL(float64(float32(0.664385618977472)))
)

// fxChb2FxDbl is FX_CHB2FX_DBL(a) == FX_SGL2FX_DBL(a): widen a FIXP_SGL to
// FIXP_DBL by a 16-bit left shift.
func fxChb2FxDbl(a int16) int32 { return int32(a) << 16 }

// backsubstFw is the 1:1 port of backsubst_fw (HFgen_preFlat.cpp:567-614):
// forward backsubstitution L*x=b. x is both the input b[] slot and the output.
func backsubstFw(numBands int, b []int32, x []int32, xSf []int) {
	e := &bsd[numBands-bsdIdxOffset]
	pLnorm1d := e.Lnorm1d[:]
	pLnorm1dSf := e.Lnorm1dSf[:]
	pLnormii := e.Lnormii[:]
	pLnormiiSf := e.LnormiiSf[:]

	x[0] = b[0]

	m := 0
	for i := 1; i <= polyOrder; i++ {
		sum := b[i] >> sumSafety
		sumSf := xSf[i]
		for k := i - 1; k > 0; k, m = k-1, m+1 {
			mult, ee := nativeaac.FMultNorm(fxChb2FxDbl(pLnorm1d[m]), x[k])
			multSf := int(pLnorm1dSf[m]) + xSf[k] + int(ee)

			diff := multSf - sumSf
			if diff > 0 {
				sum >>= uint(diff)
				sumSf = multSf
			} else if diff < 0 {
				mult >>= uint(-diff)
			}
			sum -= mult >> sumSafety
		}

		// - x[0]
		if xSf[0] > sumSf {
			sum >>= uint(xSf[0] - sumSf)
			sumSf = xSf[0]
		}
		sum -= x[0] >> uint(sumSf-xSf[0]+sumSafety)

		// instead of /L[i][i] we multiply by the inverse
		xv, ee := nativeaac.FMultNorm(sum, fxChb2FxDbl(pLnormii[i-1]))
		x[i] = xv
		xSf[i] = sumSf + int(pLnormiiSf[i-1]) + int(ee) + sumSafety
	}
}

// backsubstBw is the 1:1 port of backsubst_bw (HFgen_preFlat.cpp:630-674):
// backward backsubstitution L*x=b.
func backsubstBw(numBands int, b []int32, x []int32, xSf []int) {
	e := &bsd[numBands-bsdIdxOffset]
	pLnormInv1d := e.LnormInv1d[:]
	pLnormInv1dSf := e.LnormInv1dSf[:]

	x[polyOrder] = b[polyOrder]

	m := 0
	for i := polyOrder - 1; i >= 0; i-- {
		sum := b[i] >> sumSafety
		sumSf := xSf[i]

		for k := i + 1; k <= polyOrder; k, m = k+1, m+1 {
			mult, ee := nativeaac.FMultNorm(fxChb2FxDbl(pLnormInv1d[m]), x[k])
			multSf := int(pLnormInv1dSf[m]) + xSf[k] + int(ee)

			diff := multSf - sumSf
			if diff > 0 {
				sum >>= uint(diff)
				sumSf = multSf
			} else if diff < 0 {
				mult >>= uint(-diff)
			}
			sum -= mult >> sumSafety
		}

		xSf[i] = sumSf + sumSafety
		x[i] = sum
	}
}

// choleskySolve is the 1:1 port of choleskySolve (HFgen_preFlat.cpp:685-710):
// solves L*x=b with the pre-normalised Cholesky factors, in place over b/bSf.
func choleskySolve(numBands int, b []int32, bSf []int) {
	e := &bsd[numBands-bsdIdxOffset]
	pBmul0 := e.Bmul0[:]
	pBmul0Sf := e.Bmul0Sf[:]
	pBmul1 := e.Bmul1[:]
	pBmul1Sf := e.Bmul1Sf[:]

	var bnormed [polyOrder + 1]int32

	// normalize b
	for i := 0; i <= polyOrder; i++ {
		v, ee := nativeaac.FMultNorm(b[i], fxChb2FxDbl(pBmul0[i]))
		bnormed[i] = v
		bSf[i] += int(pBmul0Sf[i]) + int(ee)
	}

	backsubstFw(numBands, bnormed[:], b, bSf)

	// normalize b again
	for i := 0; i <= polyOrder; i++ {
		v, ee := nativeaac.FMultNorm(b[i], fxChb2FxDbl(pBmul1[i]))
		bnormed[i] = v
		bSf[i] += int(pBmul1Sf[i]) + int(ee)
	}

	backsubstBw(numBands, bnormed[:], b, bSf)
}

// polyfit is the 1:1 port of polyfit (HFgen_preFlat.cpp:727-780): degree-3
// least-squares fit of y[] over abscissas 0..numBands-1, solved via Cholesky.
// p / pSf are the output coefficients.
func polyfit(numBands int, y []int32, ySf int, p []int32, pSf []int) {
	var v [polyOrder + 1]int32 // LONG v[]
	sumSaftey := int(getLog2[numBands-1])

	// construct vector b[] temporarily stored in array p[]
	for i := 0; i <= polyOrder; i++ {
		p[i] = 0
	}
	for i := 0; i <= polyOrder; i++ {
		pSf[i] = 1 - dfractBits
	}

	for k := 0; k < numBands; k++ {
		v[0] = 1
		for i := 1; i <= polyOrder; i++ {
			v[i] = int32(k) * v[i-1]
		}

		for i := 0; i <= polyOrder; i++ {
			if v[polyOrder-i] != 0 && y[k] != 0 {
				mult, ee := nativeaac.FMultNorm(v[polyOrder-i], y[k])
				sf := dfractBits - 1 + ySf + int(ee)

				diff := sf - pSf[i]
				if diff > 0 {
					p[i] >>= uint(nativeaac.FMinI(dfractBits-1, diff))
					pSf[i] = sf
				} else if diff < 0 {
					mult >>= uint(-diff)
				}

				p[i] += mult >> uint(sumSaftey)
			}
		}
	}

	pSf[0] += sumSaftey
	pSf[1] += sumSaftey
	pSf[2] += sumSaftey
	pSf[3] += sumSaftey

	choleskySolve(numBands, p, pSf)
}

// polyval is the 1:1 port of polyval (HFgen_preFlat.cpp:800-860): evaluate the
// degree-3 polynomial p at integer x via Horner, returning the result and its
// exponent through outSf.
func polyval(p []int32, pSf []int, xInt int) (result int32, outSf int) {
	var x int32 // fractional value of x_int
	var xSf int

	if xInt != 0 {
		xSf = int(getLog2[xInt])
		x = int32(xInt) << uint(dfractBits-1-xSf)
	} else {
		return p[3], pSf[3]
	}

	result = p[0]
	resultSf := pSf[0]

	for k := 1; k <= polyOrder; k++ {
		mult := nativeaac.FMultDD(x, result)
		multSf := xSf + resultSf

		room := nativeaac.CountLeadingBits(mult)
		mult <<= uint(room)
		multSf -= room

		pp := p[k]
		ppSf := pSf[k]

		diff := ppSf - multSf
		if diff > 0 {
			diff = nativeaac.FMinI(diff, dfractBits-1)
			mult >>= uint(diff)
		} else if diff < 0 {
			diff = nativeaac.FMaxI(diff, 1-dfractBits)
			pp >>= uint(-diff)
		}

		// downshift by 1 to ensure safe summation
		mult >>= 1
		multSf++
		pp >>= 1
		ppSf++

		resultSf = nativeaac.FMaxI(ppSf, multSf)
		result = mult + pp
	}

	return result, resultSf
}

// sbrDecoderCalculateGainVec is the 1:1 port of sbrDecoder_calculateGainVec
// (HFgen_preFlat.cpp:862-994): compute the per-band pre-whitening gain vector
// (gainVec / gainVecExp) from the low-band complex QMF energy over the current
// copy-up frame. sourceBufferReal/Imag are indexed [slot][band]; the exponents
// sourceBufEOverlap / sourceBufECurrent are the overlap / current-region
// exponents; overlap is the overlap slot count.
func sbrDecoderCalculateGainVec(sourceBufferReal, sourceBufferImag [][]int32,
	sourceBufEOverlap, sourceBufECurrent, overlap int,
	gainVec []int32, gainVecExp []int, numBands, startSample, stopSample int) {

	var p [polyOrder + 1]int32
	var lowEnv [maxLowBands]int32
	invNumBands := nativeaac.GetInvInt(numBands)
	invNumSlots := nativeaac.GetInvInt(stopSample - startSample)
	sumScale := 5
	sumScaleOv := 3

	if overlap > 8 {
		sumScaleOv += 1
		sumScale += 1
	}

	// exponents of energy values
	sourceBufEOverlap = sourceBufEOverlap*2 + sumScaleOv
	sourceBufECurrent = sourceBufECurrent*2 + sumScale
	exp := nativeaac.FMaxI(sourceBufEOverlap, sourceBufECurrent)
	scaleNrg := sourceBufECurrent - exp
	scaleNrgOv := sourceBufEOverlap - exp

	var meanNrg int32 = 0
	// Calculate the spectral envelope in dB over the current copy-up frame.
	for loBand := 0; loBand < numBands; loBand++ {
		var nrgOv, nrg int32
		reserve := 0
		var maxVal int32 = 0

		for i := startSample; i < stopSample; i++ {
			r := sourceBufferReal[i][loBand]
			maxVal |= r ^ (r >> (dfractBits - 1))
			im := sourceBufferImag[i][loBand]
			maxVal |= im ^ (im >> (dfractBits - 1))
		}

		if maxVal != 0 {
			reserve = nativeaac.CntLeadingZeros(maxVal) - 2
		}

		nrgOv, nrg = 0, 0
		if scaleNrgOv > -31 {
			for i := startSample; i < overlap; i++ {
				nrgOv += (nativeaac.FPow2Div2(nativeaac.ScaleValue(sourceBufferReal[i][loBand], int32(reserve))) +
					nativeaac.FPow2Div2(nativeaac.ScaleValue(sourceBufferImag[i][loBand], int32(reserve)))) >> uint(sumScaleOv)
			}
		} else {
			scaleNrgOv = 0
		}
		if scaleNrg > -31 {
			for i := overlap; i < stopSample; i++ {
				nrg += (nativeaac.FPow2Div2(nativeaac.ScaleValue(sourceBufferReal[i][loBand], int32(reserve))) +
					nativeaac.FPow2Div2(nativeaac.ScaleValue(sourceBufferImag[i][loBand], int32(reserve)))) >> uint(sumScale)
			}
		} else {
			scaleNrg = 0
		}

		nrg = (nativeaac.ScaleValue(nrgOv, int32(scaleNrgOv)) >> 1) +
			(nativeaac.ScaleValue(nrg, int32(scaleNrg)) >> 1)
		nrg = nativeaac.FMultDD(nrg, invNumSlots) // fMult(nrg, invNumSlots): (DBL,DBL)

		expNew := exp - (2 * reserve) + 2

		// LowEnv = 10*log10(nrg) = log2(nrg) * 10/log2(10)
		if nrg > 0 {
			m, expLog2 := nativeaac.CalcLog2(nrg, int32(expNew))
			nrg = nativeaac.ScaleValue(m, expLog2-6)
			// fMult(FL2FXCONST_SGL(LOG10FAC), nrg): (SGL,DBL) == fixmul_DS(nrg, log10Fac)
			nrg = nativeaac.FMultDS(nrg, log10Fac)
		} else {
			nrg = 0
		}
		lowEnv[loBand] = nrg
		meanNrg += nativeaac.FMultDD(nrg, invNumBands) // fMult(nrg, invNumBands): (DBL,DBL)
	}
	exp = 6 + 2 // exponent of LowEnv

	// subtract mean before polynomial approximation
	for loBand := 0; loBand < numBands; loBand++ {
		lowEnv[loBand] = meanNrg - lowEnv[loBand]
	}

	if numBands > polyOrder+1 {
		var pSf [polyOrder + 1]int
		polyfit(numBands, lowEnv[:], exp, p[:], pSf[:])

		for i := 0; i < numBands; i++ {
			tmp, sf := polyval(p[:], pSf[:], i)
			tmp = nativeaac.FMultDS(tmp, log10FacInv)
			m, ge := nativeaac.F2Pow(tmp, int32(sf-2))
			gainVec[i] = m
			gainVecExp[i] = int(ge)
		}
	} else {
		for i := 0; i < numBands; i++ {
			sf := exp
			tmp := lowEnv[i]
			tmp = nativeaac.FMultDS(tmp, log10FacInv)
			m, ge := nativeaac.F2Pow(tmp, int32(sf-2))
			gainVec[i] = m
			gainVecExp[i] = int(ge)
		}
	}
}
