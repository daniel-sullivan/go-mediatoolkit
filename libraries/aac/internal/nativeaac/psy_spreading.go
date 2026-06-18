// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Energy spreading across scalefactor bands for the psychoacoustic model — a
// 1:1 port of FDKaacEnc_SpreadingMax (libAACenc/src/spreading.cpp:105). The
// driver FDKaacEnc_psyMain applies it twice per window: once to the masking
// thresholds (sfbMaskLow/HighFactor) and once to the spread-energy estimate
// (sfbMaskLow/HighFactorSprEn). Pure fixed-point: FIXP_DBL fractions, fMult,
// fixMax — bit-identical regardless of build tag.

// fMultDDspread is the full-scale FIXP_DBL*FIXP_DBL product fMult resolves to
// on the build target (Apple arm64, where Go's cgo defines __arm__ and the FDK
// selects fixmul_arm.h). C counterpart: the arm64 fixmul_DD (fixmul_arm.h:
// 177-185):
//
//	inline INT fixmul_DD(const INT a, const INT b) {  // __ARM_ARCH_8__
//	  INT64 result64;
//	  __asm__("smull %x0, %w1, %w2; asr %x0, %x0, #31;" ...);
//	  return (INT)result64;
//	}
//
// i.e. (INT)((INT64)a * b >> 31) — which preserves bit 31 of the 64-bit
// product. This is NOT identical to the package's generic fMultDD
// (fixmuldiv2_DD(a,b) << 1 == ((a*b)>>32)<<1): the two differ by exactly the
// product's bit 31, so they diverge by one LSB whenever that bit is set. The
// genuine FDKaacEnc_SpreadingMax oracle (verified) uses the >>31 form, so the
// 1:1 port must too; using the generic fMultDD here loses spreading parity.
func fMultDDspread(a, b int32) int32 {
	return int32((int64(a) * int64(b)) >> 31)
}

// fixMaxDBL returns the larger of two FIXP_DBL (int32) fractions. C
// counterpart: the fixMax(LONG, LONG) overload (common_fix.h). The package's
// fixMax operates on int, so this provides the int32 form spreading needs.
func fixMaxDBL(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// fixMinDBL returns the smaller of two FIXP_DBL (int32) fractions. C
// counterpart: the fixMin(LONG, LONG) overload (common_fix.h).
func fixMinDBL(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// spreadingMax spreads pbSpreadEnergy across pbCnt bands: a slope to higher
// frequencies followed by a slope to lower frequencies, each band raised to
// fixMax(self, maskFactor * neighbour). C counterpart: FDKaacEnc_SpreadingMax,
// spreading.cpp:105.
//
//	void FDKaacEnc_SpreadingMax(const INT pbCnt,
//	                            const FIXP_DBL *maskLowFactor,
//	                            const FIXP_DBL *maskHighFactor,
//	                            FIXP_DBL *pbSpreadEnergy) {
//	  FIXP_DBL delay;
//	  delay = pbSpreadEnergy[0];
//	  for (i = 1; i < pbCnt; i++) {
//	    delay = fixMax(pbSpreadEnergy[i], fMult(maskHighFactor[i], delay));
//	    pbSpreadEnergy[i] = delay;
//	  }
//	  delay = pbSpreadEnergy[pbCnt - 1];
//	  for (i = pbCnt - 2; i >= 0; i--) {
//	    delay = fixMax(pbSpreadEnergy[i], fMult(maskLowFactor[i], delay));
//	    pbSpreadEnergy[i] = delay;
//	  }
//	}
func spreadingMax(pbCnt int, maskLowFactor, maskHighFactor, pbSpreadEnergy []int32) {
	// slope to higher frequencies
	delay := pbSpreadEnergy[0]
	for i := 1; i < pbCnt; i++ {
		delay = fixMaxDBL(pbSpreadEnergy[i], fMultDDspread(maskHighFactor[i], delay))
		pbSpreadEnergy[i] = delay
	}

	// slope to lower frequencies
	delay = pbSpreadEnergy[pbCnt-1]
	for i := pbCnt - 2; i >= 0; i-- {
		delay = fixMaxDBL(pbSpreadEnergy[i], fMultDDspread(maskLowFactor[i], delay))
		pbSpreadEnergy[i] = delay
	}
}
