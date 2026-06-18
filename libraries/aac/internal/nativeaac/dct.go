// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

import "math/bits"

// Fixed-point DCT/DST primitives for the AAC-LC synthesis filterbank, a 1:1
// port of the vendored libFDK dct.cpp (FFT-kernel-based DCT type II/III/IV and
// DST type III/IV). Every value is an int32 FIXP_DBL in Q-format and the trig
// twiddles are int16 FIXP_SGL (Q1.15) — dct.cpp contains ZERO floats — so these
// kernels are bit-identical regardless of build/vectorization and carry no
// aac_strict FP gate (cf. the integer-kernel note in nativeaac.go). The block
// exponent travels in *pDatE: each transform adds its twiddling scale (+2) on
// top of the SCALEFACTOR<n> the inner fft() accumulates.
//
// Scope: the AAC-LC inverse filterbank (imdct_block, mdct.cpp:518-538) calls
// dct_IV for the no-alias-symmetry core path with transform length tl ==
// {1024 (long), 128 (short)}; dct_III/dst_III/dst_IV serve the LD/ELD alias
// variants. dct_II is the analysis-side companion. All five are ported here 1:1
// because they share dct_getTables and the same cplxMult/cplxMultDiv2 + fMult
// primitives (fft_cplxmul.go / fixmul.go).
//
// ROM handling: the C selects the twiddle (FIXP_WTP) and sin_twiddle (FIXP_STP)
// tables plus the sin_step inside dct_getTables (dct.cpp:127-172). On the build
// platform both WINDOWTABLE_16BIT and SINETABLE_16BIT are active, so both are
// packed int16 (re,im) pairs == fixSTP. Mirroring the FFT slice (where ditFFT
// takes its trigdata ROM as a parameter), the kernels here take twiddle /
// sinTwiddle / sinStep as parameters so the production decode path and the
// parity oracle supply the genuine ROM; dctGetTablesSinStep ports the sin_step
// selection logic itself for verification. The ROM table BYTES (SineTable1024,
// SineWindow1024/128, …) are a separate ROM-porting stage and not part of this
// DCT-primitive slice.

// fNormzPos counts the leading zero bits of a positive int32, the active
// (aarch64 __ARM_ARCH_8__) fixnormz_D == hardware `clz` (clz_arm.h:117-126),
// which fNormz(FIXP_DBL) forwards to (common_fix.h:292). dct_getTables only
// calls it on a positive transform length, so the sign-bit corner of the
// generic ~a fallback is never reached. clz(0) == 32, matching bits.LeadingZeros32.
func fNormzPos(x int32) int {
	return bits.LeadingZeros32(uint32(x))
}

// dctGetTablesSinStep ports the sin_step selection of dct_getTables
// (dct.cpp:127-172) for transform length L. It returns the sin_step the C
// computes (before the dct_III/dct_II `inc >>= 1`), so the kernels can be
// driven with the same stride the genuine table selection yields, and the
// parity oracle can assert it equals the C sin_step. The matching SineTable /
// windowSlope BYTES are supplied to the kernels as the sinTwiddle / twiddle
// parameters.
//
//	ld2_length = DFRACT_BITS - 1 - fNormz((FIXP_DBL)length) - 1;
//	switch ((length) >> (ld2_length - 1)) {
//	  case 0x4: *sin_step = 1 << (10 - ld2_length); break; // radix 2  -> SineTable1024
//	  case 0x7: *sin_step = 1 << ( 8 - ld2_length); break; // 10 ms    -> SineTable480
//	  case 0x6: *sin_step = 1 << ( 8 - ld2_length); break; // 3/4      -> SineTable384
//	  case 0x5: *sin_step = 1 << ( 6 - ld2_length); break; // 5/16     -> SineTable80
//	}
func dctGetTablesSinStep(L int) int {
	ld2Length := 32 - 1 - fNormzPos(int32(L)) - 1
	switch L >> (ld2Length - 1) {
	case 0x4: // radix 2
		return 1 << (10 - ld2Length)
	case 0x7: // 10 ms
		return 1 << (8 - ld2Length)
	case 0x6: // 3/4 of radix 2
		return 1 << (8 - ld2Length)
	case 0x5: // 5/16 of radix 2
		return 1 << (6 - ld2Length)
	default:
		return 0
	}
}

// wtcPiFourth is WTC(0x5a82799a) == cos(pi/4) narrowed to FIXP_SGL, the constant
// dct_IV/dst_IV multiply the M-even tail by (dct.cpp:454-455, 555-556). Under
// the active WINDOWTABLE_16BIT config WTC(a) == FX_DBL2FXCONST_SGL(a) ==
// stcNarrow (FDK_archdef.h:267). fMult(accu, WTC(...)) == fMult(LONG, SHORT) ==
// fMultDS.
var wtcPiFourth = stcNarrow(0x5a82799a)

// dctIV computes the DCT type IV of length L in place over pDat, the AAC-LC
// inverse-filterbank core (dct_IV, dct.cpp:371-464). twiddle holds the genuine
// windowSlopes[...] FIXP_WTP ROM and sinTwiddle the SineTableXXX FIXP_STP ROM
// dct_getTables selected for L; sinStep is the selected sin_step. The block
// exponent in *pDatE is increased by 2 (the twiddling scale) on top of the
// fft() SCALEFACTOR. A factor sqrt(2/N) is NOT applied (dct.h:150-158).
func dctIV(pDat []int32, L int, sinStep int, twiddle, sinTwiddle []fixSTP, pDatE *int) {
	M := L >> 1

	// Pre-twiddle (dct.cpp:384-417).
	{
		p0 := 0     // pDat_0 -> &pDat[0]
		p1 := L - 2 // pDat_1 -> &pDat[L-2]
		i := 0
		for ; i < M-1; i += 2 {
			accu1 := pDat[p1+1]
			accu2 := pDat[p0+0]
			accu3 := pDat[p0+1]
			accu4 := pDat[p1+0]

			accu1, accu2 = cplxMultDiv2SGL(accu1, accu2, twiddle[i].re, twiddle[i].im)
			accu3, accu4 = cplxMultDiv2SGL(accu4, accu3, twiddle[i+1].re, twiddle[i+1].im)

			pDat[p0+0] = accu2 >> 1
			pDat[p0+1] = accu1 >> 1
			pDat[p1+0] = accu4 >> 1
			pDat[p1+1] = -(accu3 >> 1)

			p0 += 2
			p1 -= 2
		}
		if M&1 != 0 {
			accu1 := pDat[p1+1]
			accu2 := pDat[p0+0]

			accu1, accu2 = cplxMultDiv2SGL(accu1, accu2, twiddle[i].re, twiddle[i].im)

			pDat[p0+0] = accu2 >> 1
			pDat[p0+1] = accu1 >> 1
		}
	}

	fft(M, pDat, pDatE)

	// Post-twiddle (dct.cpp:421-460).
	{
		p0 := 0
		p1 := L - 2

		// Sin and Cos values are 0.0 and 1.0.
		accu1 := pDat[p1+0]
		accu2 := pDat[p1+1]

		pDat[p1+1] = -pDat[p0+1]

		for idx, i := sinStep, 1; i < (M+1)>>1; i, idx = i+1, idx+sinStep {
			twd := sinTwiddle[idx]
			accu3, accu4 := cplxMultSGL(accu1, accu2, twd.re, twd.im)
			pDat[p0+1] = accu3
			pDat[p1+0] = accu4

			p0 += 2
			p1 -= 2

			accu3, accu4 = cplxMultSGL(pDat[p0+1], pDat[p0+0], twd.re, twd.im)

			accu1 = pDat[p1+0]
			accu2 = pDat[p1+1]

			pDat[p1+1] = -accu3
			pDat[p0+0] = accu4
		}

		if M&1 == 0 {
			// Last Sin and Cos value pair are the same.
			accu1 = fMultDS(accu1, wtcPiFourth)
			accu2 = fMultDS(accu2, wtcPiFourth)

			pDat[p1+0] = accu1 + accu2
			pDat[p0+1] = accu1 - accu2
		}
	}

	*pDatE += 2
}

// dstIV computes the DST type IV of length L in place over pDat (dst_IV,
// dct.cpp:468-565). Parameters mirror dctIV.
func dstIV(pDat []int32, L int, sinStep int, twiddle, sinTwiddle []fixSTP, pDatE *int) {
	M := L >> 1

	// Pre-twiddle (dct.cpp:481-514).
	{
		p0 := 0
		p1 := L - 2
		i := 0
		for ; i < M-1; i += 2 {
			accu1 := pDat[p1+1] >> 1
			accu2 := -(pDat[p0+0] >> 1)
			accu3 := pDat[p0+1] >> 1
			accu4 := -(pDat[p1+0] >> 1)

			accu1, accu2 = cplxMultDiv2SGL(accu1, accu2, twiddle[i].re, twiddle[i].im)
			accu3, accu4 = cplxMultDiv2SGL(accu4, accu3, twiddle[i+1].re, twiddle[i+1].im)

			pDat[p0+0] = accu2
			pDat[p0+1] = accu1
			pDat[p1+0] = accu4
			pDat[p1+1] = -accu3

			p0 += 2
			p1 -= 2
		}
		if M&1 != 0 {
			accu1 := pDat[p1+1]
			accu2 := -pDat[p0+0]

			accu1, accu2 = cplxMultDiv2SGL(accu1, accu2, twiddle[i].re, twiddle[i].im)

			pDat[p0+0] = accu2 >> 1
			pDat[p0+1] = accu1 >> 1
		}
	}

	fft(M, pDat, pDatE)

	// Post-twiddle (dct.cpp:518-561).
	{
		p0 := 0
		p1 := L - 2

		// Sin and Cos values are 0.0 and 1.0.
		accu1 := pDat[p1+0]
		accu2 := pDat[p1+1]

		pDat[p1+1] = -pDat[p0+0]
		pDat[p0+0] = pDat[p0+1]

		for idx, i := sinStep, 1; i < (M+1)>>1; i, idx = i+1, idx+sinStep {
			twd := sinTwiddle[idx]

			accu3, accu4 := cplxMultSGL(accu1, accu2, twd.re, twd.im)
			pDat[p1+0] = -accu3
			pDat[p0+1] = -accu4

			p0 += 2
			p1 -= 2

			accu3, accu4 = cplxMultSGL(pDat[p0+1], pDat[p0+0], twd.re, twd.im)

			accu1 = pDat[p1+0]
			accu2 = pDat[p1+1]

			pDat[p0+0] = accu3
			pDat[p1+1] = -accu4
		}

		if M&1 == 0 {
			// Last Sin and Cos value pair are the same.
			accu1 = fMultDS(accu1, wtcPiFourth)
			accu2 = fMultDS(accu2, wtcPiFourth)

			pDat[p0+1] = -accu1 - accu2
			pDat[p1+0] = accu2 - accu1
		}
	}

	*pDatE += 2
}

// dctIII computes the DCT type III of length L in place over pDat, using the
// scratch buffer tmp (length >= L) (dct_III, dct.cpp:175-258). sinTwiddle is the
// SineTableXXX FIXP_STP ROM dct_getTables selected for L; sinStep is the
// selected sin_step (the C applies `inc >>= 1` internally). The block exponent
// in *pDatE is increased by 2 on top of the fft() SCALEFACTOR. Note the factor
// 0.5 for the x[0] sum term is 1.0 instead of 0.5; sqrt(2/N) is NOT applied
// (dct.h:124-134).
func dctIII(pDat, tmp []int32, L int, sinStep int, sinTwiddle []fixSTP, pDatE *int) {
	M := L >> 1
	inc := sinStep >> 1

	pTmp0 := 2           // &tmp[2]
	pTmp1 := (M - 1) * 2 // &tmp[(M-1)*2]

	index := 4 * inc

	for i := 1; i < M>>1; i++ {
		accu2, accu1 := cplxMultDiv2SGL(pDat[L-i], pDat[i], sinTwiddle[i*inc].re, sinTwiddle[i*inc].im)
		accu4, accu3 := cplxMultDiv2SGL(pDat[M+i], pDat[M-i], sinTwiddle[(M-i)*inc].re, sinTwiddle[(M-i)*inc].im)
		accu3 >>= 1
		accu4 >>= 1

		var accu5, accu6 int32
		if 2*i < M/2 {
			accu6, accu5 = cplxMultDiv2SGL(accu3-(accu1>>1), (accu2>>1)+accu4, sinTwiddle[index].re, sinTwiddle[index].im)
		} else {
			accu6, accu5 = cplxMultDiv2SGL((accu2>>1)+accu4, accu3-(accu1>>1), sinTwiddle[index].re, sinTwiddle[index].im)
			accu6 = -accu6
		}

		xr := (accu1 >> 1) + accu3
		tmp[pTmp0+0] = (xr >> 1) - accu5
		tmp[pTmp1+0] = (xr >> 1) + accu5

		xr = (accu2 >> 1) - accu4
		tmp[pTmp0+1] = (xr >> 1) - accu6
		tmp[pTmp1+1] = -((xr >> 1) + accu6)

		if 2*i < (M/2)-1 {
			index += 4 * inc
		} else if 2*i >= M/2 {
			index -= 4 * inc
		}

		pTmp0 += 2
		pTmp1 -= 2
	}

	xr := fMultDiv2DS(pDat[M], sinTwiddle[M*inc].re) // cos((PI/(2*L))*M)
	tmp[0] = ((pDat[0] >> 1) + xr) >> 1
	tmp[1] = ((pDat[0] >> 1) - xr) >> 1

	accu2, accu1 := cplxMultDiv2SGL(pDat[L-(M/2)], pDat[M/2], sinTwiddle[M*inc/2].re, sinTwiddle[M*inc/2].im)
	tmp[M] = accu1 >> 1
	tmp[M+1] = accu2 >> 1

	// dit_fft expects 1 bit scaled input values.
	fft(M, tmp, pDatE)

	pT := 0   // tmp++
	pTmp1 = L // &tmp[L]
	for i := M >> 1; i > 0; i-- {
		tmp1 := tmp[pT]
		pT++
		tmp2 := tmp[pT]
		pT++
		pTmp1--
		tmp3 := tmp[pTmp1]
		pTmp1--
		tmp4 := tmp[pTmp1]
		pDat[(M>>1-i)*4+0] = tmp1
		pDat[(M>>1-i)*4+1] = tmp3
		pDat[(M>>1-i)*4+2] = tmp2
		pDat[(M>>1-i)*4+3] = tmp4
	}

	*pDatE += 2
}

// dstIII computes the DST type III of length L in place over pDat, by reusing
// dctIII on mirrored input and sign-flipping odd outputs (dst_III,
// dct.cpp:260-283). Parameters mirror dctIII.
func dstIII(pDat, tmp []int32, L int, sinStep int, sinTwiddle []fixSTP, pDatE *int) {
	L2 := L >> 1

	// Mirror input.
	for i := 0; i < L2; i++ {
		pDat[i], pDat[L-1-i] = pDat[L-1-i], pDat[i]
	}

	dctIII(pDat, tmp, L, sinStep, sinTwiddle, pDatE)

	// Flip signs at odd indices.
	for i := 1; i < L; i += 2 {
		pDat[i] = -pDat[i]
	}
}

// dctII computes the DCT type II of length L in place over pDat, using the
// scratch buffer tmp (length >= L) (dct_II, dct.cpp:288-366). sinTwiddle is the
// SineTableXXX FIXP_STP ROM dct_getTables selected for L; sinStep is the
// selected sin_step (the C applies `inc >>= 1` internally). The block exponent
// in *pDatE is increased by 2 on top of the fft() SCALEFACTOR. sqrt(2/(N-1)) is
// NOT applied (dct.h:113-122).
func dctII(pDat, tmp []int32, L int, sinStep int, sinTwiddle []fixSTP, pDatE *int) {
	M := L >> 1
	inc := sinStep >> 1

	for i := 0; i < M; i++ {
		tmp[i] = pDat[2*i] >> 2
		tmp[L-1-i] = pDat[2*i+1] >> 2
	}

	fft(M, tmp, pDatE)

	pTmp0 := 2           // &tmp[2]
	pTmp1 := (M - 1) * 2 // &tmp[(M-1)*2]

	index := inc * 4

	for i := 1; i < M>>1; i++ {
		a1 := (tmp[pTmp0+1] >> 1) + (tmp[pTmp1+1] >> 1)
		a2 := (tmp[pTmp1+0] >> 1) - (tmp[pTmp0+0] >> 1)

		var accu1, accu2 int32
		if 2*i < M/2 {
			accu1, accu2 = cplxMultDiv2SGL(a2, a1, sinTwiddle[index].re, sinTwiddle[index].im)
		} else {
			accu1, accu2 = cplxMultDiv2SGL(a1, a2, sinTwiddle[index].re, sinTwiddle[index].im)
			accu1 = -accu1
		}
		accu1 <<= 1
		accu2 <<= 1

		a1 = (tmp[pTmp0+0] >> 1) + (tmp[pTmp1+0] >> 1)
		a2 = (tmp[pTmp0+1] >> 1) - (tmp[pTmp1+1] >> 1)

		accu3, accu4 := cplxMultSGL(accu1+a2, a1+accu2, sinTwiddle[i*inc].re, sinTwiddle[i*inc].im)
		pDat[L-i] = -accu3
		pDat[i] = accu4

		accu3, accu4 = cplxMultSGL(accu1-a2, a1-accu2, sinTwiddle[(M-i)*inc].re, sinTwiddle[(M-i)*inc].im)
		pDat[M+i] = -accu3
		pDat[M-i] = accu4

		if 2*i < (M/2)-1 {
			index += 4 * inc
		} else if 2*i >= M/2 {
			index -= 4 * inc
		}

		pTmp0 += 2
		pTmp1 -= 2
	}

	accu1, accu2 := cplxMultSGL(tmp[M], tmp[M+1], sinTwiddle[(M/2)*inc].re, sinTwiddle[(M/2)*inc].im)
	pDat[L-(M/2)] = accu2
	pDat[M/2] = accu1

	pDat[0] = tmp[0] + tmp[1]
	pDat[M] = fMultDS(tmp[0]-tmp[1], sinTwiddle[M*inc].re) // cos((PI/(2*L))*M)

	*pDatE += 2
}
