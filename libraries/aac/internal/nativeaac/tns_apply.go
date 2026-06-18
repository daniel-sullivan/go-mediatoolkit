// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// TNS-filter application for the AAC decoder: the all-pole synthesis lattice
// (CLpc_SynthesisLattice, FIXP_DBL coefficient overload) and the per-window
// CTns_Apply driver, ported 1:1 from the vendored FDK-AAC reference
// (libFDK/src/FDK_lpc.cpp and libAACdec/src/aacdec_tns.cpp).
//
// Parity note: like the rest of the AAC pipeline, the TNS tool is implemented
// in libfdk-aac as a fixed-point (FIXP_DBL == int32, Q1.31) kernel. The lattice
// MAC chain uses the integer fMultDiv2 (an int64 product arithmetic-shifted
// right by 32) plus integer saturating shifts — no `float` or `double` appears
// on this path. Parity is therefore EXACT integer equality, bit-identical
// regardless of -ffp-contract / vectorization, so this file carries only the
// `aacfdk` fence with no FP split (cf. the integer-kernel note in
// nativeaac.go). The parity slice tns-decode strict-gates its assertions so a
// bare `go test` stays clean while the aac_strict run asserts exact equality.

// lpcMaxOrder is LPC_MAX_ORDER (FDK_lpc.h:108). The TNS state and coefficient
// buffers are bounded by this.
const lpcMaxOrder = 24

// fMultDiv2 multiplies two FIXP_DBL fractions, returning the product scaled
// down by 2. C counterpart: fMultDiv2(LONG, LONG) -> fixmuldiv2_DD
// (common_fix.h:248 / fixmul.h:131). It forwards to the package primitive
// fMultDiv2DD (fixmul.go); this alias mirrors the C spelling used verbatim in
// the lattice port below.
func fMultDiv2(a, b int32) int32 { return fMultDiv2DD(a, b) }

// fMultSubDiv2 computes y = x - 0.5*a*b. Ported 1:1 from fMultSubDiv2(FIXP_DBL,
// FIXP_DBL, FIXP_DBL) -> fixmsubdiv2_DD (common_fix.h:352 / fixmadd.h:156):
//
//	inline FIXP_DBL fixmsubdiv2_DD(FIXP_DBL x, const FIXP_DBL a, const FIXP_DBL b) {
//	  return (x - fMultDiv2(a, b));
//	}
func fMultSubDiv2(x, a, b int32) int32 { return x - fMultDiv2(a, b) }

// fMultAddDiv2 computes y = x + 0.5*a*b. Ported 1:1 from fMultAddDiv2(FIXP_DBL,
// FIXP_DBL, FIXP_DBL) -> fixmadddiv2_DD (common_fix.h:314 / fixmadd.h:124):
//
//	inline FIXP_DBL fixmadddiv2_DD(FIXP_DBL x, const FIXP_DBL a, const FIXP_DBL b) {
//	  return (x + fMultDiv2(a, b));
//	}
func fMultAddDiv2(x, a, b int32) int32 { return x + fMultDiv2(a, b) }

// scaleValue multiplies value by 2^scalefactor with a logical-by-sign shift.
// Ported 1:1 from scaleValue(FIXP_DBL, INT) in scale.h:153-160. (A standalone
// twin of the in-place scaleValueInPlace in invquant.go, kept separate because
// the lattice reads the result by value.)
//
//	inline FIXP_DBL scaleValue(const FIXP_DBL value, INT scalefactor) {
//	  if (scalefactor > 0) return (value << scalefactor);
//	  else                 return (value >> (-scalefactor));
//	}
func scaleValue(value, scalefactor int32) int32 {
	if scalefactor > 0 {
		return value << uint(scalefactor)
	}
	return value >> uint(-scalefactor)
}

// saturateLeftShiftAlt is the SATURATE_LEFT_SHIFT_ALT macro (scale.h:269-275):
// a left shift saturating to +MAXVAL_DBL / -(MAXVAL_DBL-1) (i.e. 0x7FFFFFFF /
// 0x80000001+1 == ~(MAXVAL-2)), the "alt" variant that saturates to -0.99999
// instead of -1.0 to avoid problems when inverting the sign of the result.
// dBits is always DFRACT_BITS (32) on this path.
//
//	#define SATURATE_LEFT_SHIFT_ALT(src, scale, dBits)                        \
//	  (((LONG)(src) > ((LONG)(((1U) << ((dBits)-1)) - 1) >> (scale)))         \
//	       ? (LONG)(((1U) << ((dBits)-1)) - 1)                                \
//	       : ((LONG)(src) <= ~((LONG)(((1U) << ((dBits)-1)) - 1) >> (scale))) \
//	             ? ~((LONG)(((1U) << ((dBits)-1)) - 2))                       \
//	             : ((LONG)(src) << (scale)))
//
// With dBits == 32, (1U<<31)-1 == 0x7FFFFFFF == MAXVAL_DBL; the >> on that
// positive constant is a logical shift in C (unsigned-derived value promoted to
// LONG stays non-negative), matched here by shifting the int32 positive
// constant arithmetically (identical for a non-negative value).
func saturateLeftShiftAlt(src, scale int32) int32 {
	const maxvalDBL = int32(0x7FFFFFFF) // (1U<<31)-1
	thresh := maxvalDBL >> uint(scale)
	if src > thresh {
		return maxvalDBL
	}
	if src <= ^thresh {
		// ~((1U<<31)-2) == ~(0x7FFFFFFE) == 0x80000001
		return ^(maxvalDBL - 1)
	}
	return src << uint(scale)
}

// clpcSynthesisLatticeDBL applies the all-pole TNS synthesis lattice in place
// over signal[0:signalSize], the FIXP_DBL coefficient overload. Ported 1:1 from
// CLpc_SynthesisLattice(FIXP_DBL *signal, ..., const FIXP_DBL *coeff, ...) in
// FDK_lpc.cpp:168-209. Decode-side TNS coefficients are FIXP_TCC == FIXP_DBL
// (aac_rom.h:195), so CTns_Apply dispatches this overload.
//
// signalE / signalEOut are the input/output exponents (both 0 for CTns_Apply,
// matching aacdec_tns.cpp:350). inc is +1 (forward) or -1 (backward) filtering
// direction. state must hold `order` FIXP_DBL accumulators, zeroed by the
// caller (CTns_Apply clears it before each filter, aacdec_tns.cpp:349).
//
//	for (i = signal_size; i != 0; i--) {
//	  FIXP_DBL *pState = state + order - 1;
//	  const FIXP_DBL *pCoeff = coeff + order - 1;
//	  FIXP_DBL tmp, accu;
//	  accu = fMultSubDiv2(scaleValue(*pSignal, signal_e - 1), *pCoeff--, *pState--);
//	  tmp = SATURATE_LEFT_SHIFT_ALT(accu, 1, DFRACT_BITS);
//	  for (j = order - 1; j != 0; j--) {
//	    accu = fMultSubDiv2(tmp >> 1, pCoeff[0], pState[0]);
//	    tmp = SATURATE_LEFT_SHIFT_ALT(accu, 1, DFRACT_BITS);
//	    accu = fMultAddDiv2(pState[0] >> 1, *pCoeff--, tmp);
//	    pState[1] = SATURATE_LEFT_SHIFT_ALT(accu, 1, DFRACT_BITS);
//	    pState--;
//	  }
//	  *pSignal = scaleValue(tmp, -signal_e_out);
//	  pState[1] = tmp;          // exponent of state[] is 0
//	  pSignal += inc;
//	}
func clpcSynthesisLatticeDBL(signal []int32, signalSize, signalE, signalEOut, inc int, coeff []int32, order int, state []int32) {
	// FDK_ASSERT(order <= LPC_MAX_ORDER); FDK_ASSERT(order > 0);
	// FDK_ASSERT(signal_size > 0); — preserved as caller preconditions.
	var sig int // index of pSignal within signal
	if inc == -1 {
		sig = signalSize - 1
	} else {
		sig = 0
	}

	for i := signalSize; i != 0; i-- {
		pState := order - 1 // index into state for pState
		pCoeff := order - 1 // index into coeff for pCoeff

		accu := fMultSubDiv2(scaleValue(signal[sig], int32(signalE-1)), coeff[pCoeff], state[pState])
		pCoeff--
		pState--
		tmp := saturateLeftShiftAlt(accu, 1)

		for j := order - 1; j != 0; j-- {
			// In C, pCoeff/pState point one past the element used here; pCoeff[0]
			// and pState[0] are the next-lower index (the post-decrement above /
			// below moves the pointer down). After the first iteration pCoeff and
			// pState index the element about to be combined; pState[1] is the slot
			// above it.
			accu = fMultSubDiv2(tmp>>1, coeff[pCoeff], state[pState])
			tmp = saturateLeftShiftAlt(accu, 1)

			accu = fMultAddDiv2(state[pState]>>1, coeff[pCoeff], tmp)
			pCoeff--
			state[pState+1] = saturateLeftShiftAlt(accu, 1)

			pState--
		}

		signal[sig] = scaleValue(tmp, int32(-signalEOut))

		// exponent of state[] is 0
		state[pState+1] = tmp
		sig += inc
	}
}
