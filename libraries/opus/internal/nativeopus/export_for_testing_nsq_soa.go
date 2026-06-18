package nativeopus

import "math/rand"

// Parity test exports for silk_NSQ_soa.go. These are exposed solely so
// that roundtrip tests in internal/parity_tests/benchcmp can exercise
// the AoS <-> SoA conversion helpers without promoting the helpers
// themselves to the public API of nativeopus.
//
// NSQ_del_dec_struct has unexported fields (sLPC_Q14, sAR2_Q14), so
// callers outside this package cannot populate them directly. The
// ExportTestNSQDelDecFillRandom helper does that population with a
// deterministic math/rand source supplied by the caller; the
// roundtrip tests then compare structs with == (all fields are
// comparable value types).

// ExportTestNSQDelDecAoStoSoA heap-allocates a fresh NSQDelDecSoA and
// populates it from the leading nStates entries of aos. Lanes
// k >= nStates in the returned SoA are guaranteed to be zero.
func ExportTestNSQDelDecAoStoSoA(aos []NSQ_del_dec_struct, nStates int) *NSQDelDecSoA {
	soa := new(NSQDelDecSoA)
	nsqDelDecAoStoSoA(soa, aos, opus_int(nStates))
	return soa
}

// ExportTestNSQDelDecAoStoSoAInto populates the caller-supplied SoA in
// place. Exposed so tests can verify that the zero-out contract for
// lanes k >= nStates holds even when the destination arrived with
// nonzero junk already written into those lanes.
func ExportTestNSQDelDecAoStoSoAInto(dst *NSQDelDecSoA, aos []NSQ_del_dec_struct, nStates int) {
	nsqDelDecAoStoSoA(dst, aos, opus_int(nStates))
}

// ExportTestNSQDelDecSoAtoAoS returns a freshly allocated slice of
// length nStates with each entry filled from the corresponding lane of
// src.
func ExportTestNSQDelDecSoAtoAoS(soa *NSQDelDecSoA, nStates int) []NSQ_del_dec_struct {
	out := make([]NSQ_del_dec_struct, nStates)
	nsqDelDecSoAtoAoS(out, soa, opus_int(nStates))
	return out
}

// ExportTestNSQDelDecSoALaneZero returns true iff every field of the
// given SoA has a zero value at lane `lane`. Used by the roundtrip
// test to assert that lanes k >= nStates are zeroed after conversion.
func ExportTestNSQDelDecSoALaneZero(soa *NSQDelDecSoA, lane int) bool {
	for i := 0; i < MAX_SUB_FRAME_LENGTH+NSQ_LPC_BUF_LENGTH; i++ {
		if soa.sLPC_Q14[i][lane] != 0 {
			return false
		}
	}
	for i := 0; i < DECISION_DELAY; i++ {
		if soa.RandState[i][lane] != 0 ||
			soa.Q_Q10[i][lane] != 0 ||
			soa.Xq_Q14[i][lane] != 0 ||
			soa.Pred_Q15[i][lane] != 0 ||
			soa.Shape_Q14[i][lane] != 0 {
			return false
		}
	}
	for i := 0; i < MAX_SHAPE_LPC_ORDER; i++ {
		if soa.sAR2_Q14[i][lane] != 0 {
			return false
		}
	}
	return soa.LF_AR_Q14[lane] == 0 &&
		soa.Diff_Q14[lane] == 0 &&
		soa.Seed[lane] == 0 &&
		soa.SeedInit[lane] == 0 &&
		soa.RD_Q10[lane] == 0
}

// ExportTestNSQDelDecFillRandom returns a NSQ_del_dec_struct with
// every opus_int32 field populated from r.Int31(). This is the only
// mechanism by which tests outside this package can write the
// struct's unexported fields.
func ExportTestNSQDelDecFillRandom(r *rand.Rand) NSQ_del_dec_struct {
	var s NSQ_del_dec_struct
	for i := range s.sLPC_Q14 {
		s.sLPC_Q14[i] = r.Int31()
	}
	for i := range s.RandState {
		s.RandState[i] = r.Int31()
		s.Q_Q10[i] = r.Int31()
		s.Xq_Q14[i] = r.Int31()
		s.Pred_Q15[i] = r.Int31()
		s.Shape_Q14[i] = r.Int31()
	}
	for i := range s.sAR2_Q14 {
		s.sAR2_Q14[i] = r.Int31()
	}
	s.LF_AR_Q14 = r.Int31()
	s.Diff_Q14 = r.Int31()
	s.Seed = r.Int31()
	s.SeedInit = r.Int31()
	s.RD_Q10 = r.Int31()
	return s
}

// ExportTestShortPredictionSoA exposes the 4-lane SoA short_prediction
// kernel for bit-exact parity tests against the scalar reference.
func ExportTestShortPredictionSoA(soa *NSQDelDecSoA, base int, coef16 []opus_int16, order int) [MAX_DEL_DEC_STATES]opus_int32 {
	return silk_noise_shape_quantizer_short_prediction_soa(soa, opus_int(base), coef16, opus_int(order))
}

// ExportTestShortPredictionSoASIMD exposes the arm64 NEON assembly
// kernel (falls back to a no-op on non-arm64 or with -tags=opus_strict
// / opus_nosimd) so parity tests can assert bit-exact match against the
// scalar reference on supported platforms.
func ExportTestShortPredictionSoASIMD(soa *NSQDelDecSoA, base int, coef16 []opus_int16, order int) [MAX_DEL_DEC_STATES]opus_int32 {
	var out [MAX_DEL_DEC_STATES]opus_int32
	shortPredictionSoASIMD(&soa.sLPC_Q14[base][0], &coef16[0], opus_int(order), &out)
	return out
}

// ExportTestNSQSIMDAvailable reports whether the NSQ SIMD build path is
// compiled in for the current build tags / platform.
func ExportTestNSQSIMDAvailable() bool { return nsqSIMDAvailable }

// ExportTestShortPredictionScalar exposes the unexported scalar
// reference so the parity test can invoke the exact same function the
// del-dec inner loop calls (rather than re-implementing it).
func ExportTestShortPredictionScalar(buf32 []opus_int32, base int, coef16 []opus_int16, order int) opus_int32 {
	return silk_noise_shape_quantizer_short_prediction_c(buf32, opus_int(base), coef16, opus_int(order))
}

// ExportTestNSQDelDecSAR2Q14 returns the sAR2_Q14 array of an
// NSQ_del_dec_struct as a slice view. Exposed for parity tests that
// need to read the allpass state vector without otherwise reaching
// into unexported fields.
func ExportTestNSQDelDecSAR2Q14(s *NSQ_del_dec_struct) []opus_int32 {
	return s.sAR2_Q14[:]
}

// ExportTestNSQDelDecSLPCQ14 returns the sLPC_Q14 array of an
// NSQ_del_dec_struct as a slice view. Exposed for parity tests that
// need to pass buf32 to ExportTestShortPredictionScalar without
// otherwise reaching into unexported fields.
func ExportTestNSQDelDecSLPCQ14(s *NSQ_del_dec_struct) []opus_int32 {
	return s.sLPC_Q14[:]
}

// ExportTestNSQAllpassSoA exposes the 4-lane SoA noise-shape allpass
// kernel for bit-exact parity tests against the scalar reference. The
// kernel mutates soa.sAR2_Q14 in place and returns the per-lane
// n_AR_Q14 accumulator.
func ExportTestNSQAllpassSoA(soa *NSQDelDecSoA, warping_Q16 int32, AR_shp_Q13 []opus_int16, shapingLPCOrder int) [MAX_DEL_DEC_STATES]opus_int32 {
	return silk_noise_shape_allpass_soa(soa, opus_int32(warping_Q16), AR_shp_Q13, opus_int(shapingLPCOrder))
}

// ExportTestNSQAllpassScalar is a byte-for-byte extraction of the
// scalar allpass/noise-shape feedback loop body at
// silk_NSQ_del_dec.go:403-421. It mutates psDD.sAR2_Q14 in place and
// returns the resulting n_AR_Q14 accumulator along with a snapshot of
// the full mutated sAR2_Q14 array for convenience in parity tests.
//
// This lives in the nativeopus package (not in silk_NSQ_del_dec.go) so
// that the hot per-sample loop body in the real encoder is not
// perturbed in any way by refactoring-for-testing concerns.
func ExportTestNSQAllpassScalar(psDD *NSQ_del_dec_struct, warping_Q16 int32, AR_shp_Q13 []opus_int16, shapingLPCOrder int) (n_AR_Q14 opus_int32, mutatedSAR2 [MAX_SHAPE_LPC_ORDER]opus_int32) {
	wQ16 := opus_int32(warping_Q16)
	order := opus_int(shapingLPCOrder)

	tmp2 := silk_SMLAWB(psDD.Diff_Q14, psDD.sAR2_Q14[0], wQ16)
	tmp1 := silk_SMLAWB(psDD.sAR2_Q14[0], silk_SUB32_ovflw(psDD.sAR2_Q14[1], tmp2), wQ16)
	psDD.sAR2_Q14[0] = tmp2
	n_AR_Q14 = silk_RSHIFT(opus_int32(order), 1)
	n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp2, opus_int32(AR_shp_Q13[0]))
	for j := opus_int(2); j < order; j += 2 {
		tmp2 = silk_SMLAWB(psDD.sAR2_Q14[j-1], silk_SUB32_ovflw(psDD.sAR2_Q14[j+0], tmp1), wQ16)
		psDD.sAR2_Q14[j-1] = tmp1
		n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp1, opus_int32(AR_shp_Q13[j-1]))
		tmp1 = silk_SMLAWB(psDD.sAR2_Q14[j+0], silk_SUB32_ovflw(psDD.sAR2_Q14[j+1], tmp2), wQ16)
		psDD.sAR2_Q14[j+0] = tmp2
		n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp2, opus_int32(AR_shp_Q13[j]))
	}
	psDD.sAR2_Q14[order-1] = tmp1
	n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp1, opus_int32(AR_shp_Q13[order-1]))

	for i := 0; i < MAX_SHAPE_LPC_ORDER; i++ {
		mutatedSAR2[i] = psDD.sAR2_Q14[i]
	}
	return n_AR_Q14, mutatedSAR2
}

// ExportTestNSQAllpassSIMD exposes the arm64 NEON assembly kernel (if
// compiled in) as a 4-lane noise-shape allpass. On platforms / build
// tags where the asm kernel is not linked, callers are expected to
// gate invocations on ExportTestNSQAllpassSIMDAvailable — the noasm
// stub deliberately does nothing so there is no silent-fallback
// coverage gap.
func ExportTestNSQAllpassSIMD(soa *NSQDelDecSoA, warping_Q16 int32, AR_shp_Q13 []opus_int16, shapingLPCOrder int) [MAX_DEL_DEC_STATES]opus_int32 {
	var out [MAX_DEL_DEC_STATES]opus_int32
	shortNSQAllpassSIMD(&soa.sAR2_Q14[0][0], &soa.Diff_Q14[0], warping_Q16, &AR_shp_Q13[0], shapingLPCOrder, &out)
	return out
}

// ExportTestNSQAllpassSIMDAvailable reports whether the noise-shape
// allpass SIMD build path is compiled in for the current build tags /
// platform.
func ExportTestNSQAllpassSIMDAvailable() bool { return nsqAllpassSIMDAvailable }

// ExportTestNSQDelDecSoAFillLane writes v to every slot of the given
// SoA at lane `lane`. Used to pre-garbage a SoA before testing the
// zero-out contract of nsqDelDecAoStoSoA.
func ExportTestNSQDelDecSoAFillLane(soa *NSQDelDecSoA, lane int, v int32) {
	x := opus_int32(v)
	for i := 0; i < MAX_SUB_FRAME_LENGTH+NSQ_LPC_BUF_LENGTH; i++ {
		soa.sLPC_Q14[i][lane] = x
	}
	for i := 0; i < DECISION_DELAY; i++ {
		soa.RandState[i][lane] = x
		soa.Q_Q10[i][lane] = x
		soa.Xq_Q14[i][lane] = x
		soa.Pred_Q15[i][lane] = x
		soa.Shape_Q14[i][lane] = x
	}
	for i := 0; i < MAX_SHAPE_LPC_ORDER; i++ {
		soa.sAR2_Q14[i][lane] = x
	}
	soa.LF_AR_Q14[lane] = x
	soa.Diff_Q14[lane] = x
	soa.Seed[lane] = x
	soa.SeedInit[lane] = x
	soa.RD_Q10[lane] = x
}
