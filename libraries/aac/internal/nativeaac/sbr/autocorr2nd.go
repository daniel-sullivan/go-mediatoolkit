// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of libFDK/src/autocorr2nd.cpp: the 2nd-order autocorrelation used by
// the LPP transposer to derive the LPC whitening coefficients. autoCorr2nd_real
// works on the real low-band copy (useLP path); autoCorr2nd_cplx on the complex
// low-band copy (high-quality path). Both are pure fixed-point integer kernels
// (FIXP_DBL Q-format), so EXACT-integer parity holds in any build.
//
// Helper mapping (the libFDK macros, common_fix.h / clz.h):
//   fMultDiv2(a,b)     -> nativeaac.FMultDiv2DD  (fixmuldiv2_DD)
//   fPow2Div2(a)       -> nativeaac.FPow2Div2    (fixpow2div2_D)
//   fMax(a,b)          -> max over int           (fixmax for INT)
//   fAbs(x)            -> nativeaac.FixpAbs       (fixp_abs / fAbs over FIXP_DBL)
//   fNormz(x)          -> nativeaac.CntLeadingZeros (fixnormz_D)
//   CntLeadingZeros(x) -> nativeaac.CntLeadingZeros (fixnormz_D)
//   CountLeadingBits(x)-> nativeaac.CountLeadingBits (fixnorm_D)

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// dfractBits (DFRACT_BITS, the FIXP_DBL width) is defined in qmf_synthesis.go.

// autoCorr2ndReal is the 1:1 port of autoCorr2nd_real (autocorr2nd.cpp:111-182).
// reBuffer indexes from -2 .. len-1 (the caller passes lowBandReal+LPC_ORDER, so
// reBuffer[-2], reBuffer[-1] are the two history samples). base is the index in
// reBuffer of element 0.
func autoCorr2ndReal(ac *acorrCoefs, reBuffer []int32, base, length int) int {
	var accu1, accu2, accu3, accu4, accu5 int32

	// realBuf == reBuffer (offset by base). pReBuf is a running index.
	// len_scale = fMax(DFRACT_BITS - fNormz((FIXP_DBL)(len/2)), 1).
	lenScale := nativeaac.FMaxI(dfractBits-nativeaac.CntLeadingZeros(int32(length/2)), 1)

	/* r11r,r22r ; r01r,r12r ; r02r */
	// pReBuf = realBuf - 2;
	p := base - 2
	accu5 = (nativeaac.FMultDiv2DD(reBuffer[p+0], reBuffer[p+2]) +
		nativeaac.FMultDiv2DD(reBuffer[p+1], reBuffer[p+3])) >> uint(lenScale)
	p++ // pReBuf++

	// len must be even
	accu1 = nativeaac.FPow2Div2(reBuffer[p+0]) >> uint(lenScale)
	accu3 = nativeaac.FMultDiv2DD(reBuffer[p+0], reBuffer[p+1]) >> uint(lenScale)
	p++ // pReBuf++

	for j := (length - 2) >> 1; j != 0; j, p = j-1, p+2 {
		accu1 += (nativeaac.FPow2Div2(reBuffer[p+0]) + nativeaac.FPow2Div2(reBuffer[p+1])) >> uint(lenScale)
		accu3 += (nativeaac.FMultDiv2DD(reBuffer[p+0], reBuffer[p+1]) +
			nativeaac.FMultDiv2DD(reBuffer[p+1], reBuffer[p+2])) >> uint(lenScale)
		accu5 += (nativeaac.FMultDiv2DD(reBuffer[p+0], reBuffer[p+2]) +
			nativeaac.FMultDiv2DD(reBuffer[p+1], reBuffer[p+3])) >> uint(lenScale)
	}

	// realBuf[k] == reBuffer[base+k]
	accu2 = nativeaac.FPow2Div2(reBuffer[base-2]) >> uint(lenScale)
	accu2 += accu1

	accu1 += nativeaac.FPow2Div2(reBuffer[base+length-2]) >> uint(lenScale)

	accu4 = nativeaac.FMultDiv2DD(reBuffer[base-1], reBuffer[base-2]) >> uint(lenScale)
	accu4 += accu3

	accu3 += nativeaac.FMultDiv2DD(reBuffer[base+length-1], reBuffer[base+length-2]) >> uint(lenScale)

	mScale := nativeaac.CntLeadingZeros(
		accu1|accu2|nativeaac.FixpAbs(accu3)|nativeaac.FixpAbs(accu4)|nativeaac.FixpAbs(accu5)) - 1
	autoCorrScaling := mScale - 1 - lenScale // -1 because of fMultDiv2

	// Scale to common scale factor
	ac.r11r = accu1 << uint(mScale)
	ac.r22r = accu2 << uint(mScale)
	ac.r01r = accu3 << uint(mScale)
	ac.r12r = accu4 << uint(mScale)
	ac.r02r = accu5 << uint(mScale)

	ac.det = nativeaac.FMultDiv2DD(ac.r11r, ac.r22r) - nativeaac.FMultDiv2DD(ac.r12r, ac.r12r)
	mScale = nativeaac.CountLeadingBits(nativeaac.FixpAbs(ac.det))

	ac.det <<= uint(mScale)
	ac.detScale = mScale - 1

	return autoCorrScaling
}

// autoCorr2ndCplx is the 1:1 port of autoCorr2nd_cplx (autocorr2nd.cpp:186-290).
// reBuffer / imBuffer index from -2 .. len-1; base is the index of element 0.
func autoCorr2ndCplx(ac *acorrCoefs, reBuffer, imBuffer []int32, base, length int) int {
	var accu0, accu1, accu2, accu3, accu4, accu5, accu6, accu7, accu8 int32

	// len_scale = fMax(DFRACT_BITS - fNormz((FIXP_DBL)len), 1).
	lenScale := nativeaac.FMaxI(dfractBits-nativeaac.CntLeadingZeros(int32(length)), 1)

	/* r00r ; r11r,r22r ; r01r,r12r ; r01i,r12i ; r02r,r02i */
	accu1, accu3, accu5, accu7, accu8 = 0, 0, 0, 0, 0

	// pReBuf = realBuf - 2, pImBuf = imagBuf - 2;
	pr := base - 2
	pi := base - 2
	accu7 += (nativeaac.FMultDiv2DD(reBuffer[pr+2], reBuffer[pr+0]) +
		nativeaac.FMultDiv2DD(imBuffer[pi+2], imBuffer[pi+0])) >> uint(lenScale)
	accu8 += (nativeaac.FMultDiv2DD(imBuffer[pi+2], reBuffer[pr+0]) -
		nativeaac.FMultDiv2DD(reBuffer[pr+2], imBuffer[pi+0])) >> uint(lenScale)

	// pReBuf = realBuf - 1, pImBuf = imagBuf - 1;
	pr = base - 1
	pi = base - 1
	for j := length - 1; j != 0; j, pr, pi = j-1, pr+1, pi+1 {
		accu1 += (nativeaac.FPow2Div2(reBuffer[pr+0]) + nativeaac.FPow2Div2(imBuffer[pi+0])) >> uint(lenScale)
		accu3 += (nativeaac.FMultDiv2DD(reBuffer[pr+0], reBuffer[pr+1]) +
			nativeaac.FMultDiv2DD(imBuffer[pi+0], imBuffer[pi+1])) >> uint(lenScale)
		accu5 += (nativeaac.FMultDiv2DD(imBuffer[pi+1], reBuffer[pr+0]) -
			nativeaac.FMultDiv2DD(reBuffer[pr+1], imBuffer[pi+0])) >> uint(lenScale)
		accu7 += (nativeaac.FMultDiv2DD(reBuffer[pr+2], reBuffer[pr+0]) +
			nativeaac.FMultDiv2DD(imBuffer[pi+2], imBuffer[pi+0])) >> uint(lenScale)
		accu8 += (nativeaac.FMultDiv2DD(imBuffer[pi+2], reBuffer[pr+0]) -
			nativeaac.FMultDiv2DD(reBuffer[pr+2], imBuffer[pi+0])) >> uint(lenScale)
	}

	accu2 = (nativeaac.FPow2Div2(reBuffer[base-2]) + nativeaac.FPow2Div2(imBuffer[base-2])) >> uint(lenScale)
	accu2 += accu1

	accu1 += (nativeaac.FPow2Div2(reBuffer[base+length-2]) + nativeaac.FPow2Div2(imBuffer[base+length-2])) >> uint(lenScale)
	accu0 = ((nativeaac.FPow2Div2(reBuffer[base+length-1]) + nativeaac.FPow2Div2(imBuffer[base+length-1])) >> uint(lenScale)) -
		((nativeaac.FPow2Div2(reBuffer[base-1]) + nativeaac.FPow2Div2(imBuffer[base-1])) >> uint(lenScale))
	accu0 += accu1

	accu4 = (nativeaac.FMultDiv2DD(reBuffer[base-1], reBuffer[base-2]) +
		nativeaac.FMultDiv2DD(imBuffer[base-1], imBuffer[base-2])) >> uint(lenScale)
	accu4 += accu3

	accu3 += (nativeaac.FMultDiv2DD(reBuffer[base+length-1], reBuffer[base+length-2]) +
		nativeaac.FMultDiv2DD(imBuffer[base+length-1], imBuffer[base+length-2])) >> uint(lenScale)

	accu6 = (nativeaac.FMultDiv2DD(imBuffer[base-1], reBuffer[base-2]) -
		nativeaac.FMultDiv2DD(reBuffer[base-1], imBuffer[base-2])) >> uint(lenScale)
	accu6 += accu5

	accu5 += (nativeaac.FMultDiv2DD(imBuffer[base+length-1], reBuffer[base+length-2]) -
		nativeaac.FMultDiv2DD(reBuffer[base+length-1], imBuffer[base+length-2])) >> uint(lenScale)

	mScale := nativeaac.CntLeadingZeros(
		accu0|accu1|accu2|nativeaac.FixpAbs(accu3)|nativeaac.FixpAbs(accu4)|
			nativeaac.FixpAbs(accu5)|nativeaac.FixpAbs(accu6)|nativeaac.FixpAbs(accu7)|nativeaac.FixpAbs(accu8)) - 1
	autoCorrScaling := mScale - 1 - lenScale // -1 because of fMultDiv2

	// Scale to common scale factor
	ac.r00r = accu0 << uint(mScale)
	ac.r11r = accu1 << uint(mScale)
	ac.r22r = accu2 << uint(mScale)
	ac.r01r = accu3 << uint(mScale)
	ac.r12r = accu4 << uint(mScale)
	ac.r01i = accu5 << uint(mScale)
	ac.r12i = accu6 << uint(mScale)
	ac.r02r = accu7 << uint(mScale)
	ac.r02i = accu8 << uint(mScale)

	ac.det = (nativeaac.FMultDiv2DD(ac.r11r, ac.r22r) >> 1) -
		((nativeaac.FMultDiv2DD(ac.r12r, ac.r12r) + nativeaac.FMultDiv2DD(ac.r12i, ac.r12i)) >> 1)
	mScale = nativeaac.CntLeadingZeros(nativeaac.FixpAbs(ac.det)) - 1

	ac.det <<= uint(mScale)
	ac.detScale = mScale - 2

	return autoCorrScaling
}
