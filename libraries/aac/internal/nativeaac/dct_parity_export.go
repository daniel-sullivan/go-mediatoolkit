// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the unexported fixed-point DCT/DST kernels
// (dct.go) so the cgo parity oracle in internal/parity_tests/dct can drive them
// without being in-package. These add no logic — they forward 1:1, converting
// the oracle's flat int16 (re,im) ROM into the in-package fixSTP slice. The
// production decode path uses the unexported forms with the ported ROM.

// packFixSTP converts a flat [re0,im0,re1,im1,...] int16 ROM (as the parity
// oracle copies it out of the genuine C FIXP_SPK table) into a fixSTP slice.
func packFixSTP(flat []int16) []fixSTP {
	out := make([]fixSTP, len(flat)/2)
	for i := range out {
		out[i] = fixSTP{re: flat[2*i], im: flat[2*i+1]}
	}
	return out
}

// DctGetTablesSinStep exposes dctGetTablesSinStep so the oracle can assert the
// ported sin_step selection matches the C dct_getTables sin_step for length L.
func DctGetTablesSinStep(L int) int { return dctGetTablesSinStep(L) }

// DctIV runs the in-place DCT-IV of length L over pDat and returns the exponent
// delta added to the block exponent (always +2 plus the inner fft scalefactor).
// twiddle/sinTwiddle are the genuine FIXP_WTP/FIXP_STP ROM (flat int16 re/im)
// dct_getTables selected for L; sinStep the selected sin_step. Wraps dctIV.
func DctIV(pDat []int32, L, sinStep int, twiddle, sinTwiddle []int16) int {
	e := 0
	dctIV(pDat, L, sinStep, packFixSTP(twiddle), packFixSTP(sinTwiddle), &e)
	return e
}

// DstIV runs the in-place DST-IV of length L. Wraps dstIV.
func DstIV(pDat []int32, L, sinStep int, twiddle, sinTwiddle []int16) int {
	e := 0
	dstIV(pDat, L, sinStep, packFixSTP(twiddle), packFixSTP(sinTwiddle), &e)
	return e
}

// DctIII runs the in-place DCT-III of length L. tmp must be length >= L. Wraps
// dctIII. Only sinTwiddle (the SineTable ROM) is needed.
func DctIII(pDat, tmp []int32, L, sinStep int, sinTwiddle []int16) int {
	e := 0
	dctIII(pDat, tmp, L, sinStep, packFixSTP(sinTwiddle), &e)
	return e
}

// DstIII runs the in-place DST-III of length L. tmp must be length >= L. Wraps
// dstIII.
func DstIII(pDat, tmp []int32, L, sinStep int, sinTwiddle []int16) int {
	e := 0
	dstIII(pDat, tmp, L, sinStep, packFixSTP(sinTwiddle), &e)
	return e
}

// DctII runs the in-place DCT-II of length L. tmp must be length >= L. Wraps
// dctII.
func DctII(pDat, tmp []int32, L, sinStep int, sinTwiddle []int16) int {
	e := 0
	dctII(pDat, tmp, L, sinStep, packFixSTP(sinTwiddle), &e)
	return e
}
