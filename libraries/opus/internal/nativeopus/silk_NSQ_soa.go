package nativeopus

// Struct-of-Arrays (SoA) layout of NSQ_del_dec_struct for vectorized
// execution across the up-to-four parallel delayed-decision states in
// silk_noise_shape_quantizer_del_dec.
//
// The existing Array-of-Structs layout NSQ_del_dec_struct has every
// field scoped to a single lane, and the hot inner loop indexes
// psDelDec[k].<field>[i]. That access pattern forces per-lane scalar
// loads and stores and prevents SIMD from treating the four state
// machines as a single 4-wide computation.
//
// Here we keep the lane index INNERMOST so that for any outer tap
// index i, the four int32s for lanes 0..3 are contiguous in memory.
// That means a single vld1q_s32(&soa.<field>[i][0]) on AArch64 loads
// all four lanes into a NEON q-register with one instruction, and a
// matching vst1q_s32 writes them back. The scalar fallback in this
// file uses ordinary Go for-loops over the lanes; the NEON path will
// replace only the innermost lane loop with intrinsics while reusing
// this struct unchanged.

// NSQDelDecSoA mirrors NSQ_del_dec_struct but with an added innermost
// lane dimension of length MAX_DEL_DEC_STATES (=4) so that each outer
// tap index fans out across the four delayed-decision lanes in a
// contiguous 16-byte span.
type NSQDelDecSoA struct {
	// Arrays with [outer-index][lane]. Lane-innermost ordering lets
	// NEON load all 4 lanes for a given tap/index with one vld1q_s32.
	sLPC_Q14  [MAX_SUB_FRAME_LENGTH + NSQ_LPC_BUF_LENGTH][MAX_DEL_DEC_STATES]opus_int32
	RandState [DECISION_DELAY][MAX_DEL_DEC_STATES]opus_int32
	Q_Q10     [DECISION_DELAY][MAX_DEL_DEC_STATES]opus_int32
	Xq_Q14    [DECISION_DELAY][MAX_DEL_DEC_STATES]opus_int32
	Pred_Q15  [DECISION_DELAY][MAX_DEL_DEC_STATES]opus_int32
	Shape_Q14 [DECISION_DELAY][MAX_DEL_DEC_STATES]opus_int32
	sAR2_Q14  [MAX_SHAPE_LPC_ORDER][MAX_DEL_DEC_STATES]opus_int32

	// Per-lane scalars.
	LF_AR_Q14 [MAX_DEL_DEC_STATES]opus_int32
	Diff_Q14  [MAX_DEL_DEC_STATES]opus_int32
	Seed      [MAX_DEL_DEC_STATES]opus_int32
	SeedInit  [MAX_DEL_DEC_STATES]opus_int32
	RD_Q10    [MAX_DEL_DEC_STATES]opus_int32
}

// nsqDelDecAoStoSoA copies the leading nStates entries of src into dst
// with lane k populated from src[k]. Lanes k >= nStates are explicitly
// zeroed so that any prior contents of dst do not leak into a SIMD
// computation that always processes MAX_DEL_DEC_STATES lanes (the
// quantizer's inner loop is unrolled to four lanes even when only
// nStates < 4 are meaningful; the extra lanes' results are simply
// discarded by the surrounding scalar reduction).
//
// Lane-innermost memory order: writing each field as
// dst.<field>[i][k] = src[k].<field>[i] builds the SoA by striping
// across lanes on the inside of the i loop, which also matches the
// eventual NEON store pattern.
func nsqDelDecAoStoSoA(dst *NSQDelDecSoA, src []NSQ_del_dec_struct, nStates opus_int) {
	// Zero out dst entirely first, then overwrite the live lanes. This
	// guarantees lanes k >= nStates are zero regardless of whatever
	// state dst was in on entry.
	*dst = NSQDelDecSoA{}

	for k := opus_int(0); k < nStates; k++ {
		s := &src[k]
		lane := int(k)

		for i := 0; i < MAX_SUB_FRAME_LENGTH+NSQ_LPC_BUF_LENGTH; i++ {
			dst.sLPC_Q14[i][lane] = s.sLPC_Q14[i]
		}
		for i := 0; i < DECISION_DELAY; i++ {
			dst.RandState[i][lane] = s.RandState[i]
			dst.Q_Q10[i][lane] = s.Q_Q10[i]
			dst.Xq_Q14[i][lane] = s.Xq_Q14[i]
			dst.Pred_Q15[i][lane] = s.Pred_Q15[i]
			dst.Shape_Q14[i][lane] = s.Shape_Q14[i]
		}
		for i := 0; i < MAX_SHAPE_LPC_ORDER; i++ {
			dst.sAR2_Q14[i][lane] = s.sAR2_Q14[i]
		}

		dst.LF_AR_Q14[lane] = s.LF_AR_Q14
		dst.Diff_Q14[lane] = s.Diff_Q14
		dst.Seed[lane] = s.Seed
		dst.SeedInit[lane] = s.SeedInit
		dst.RD_Q10[lane] = s.RD_Q10
	}
}

// silk_noise_shape_quantizer_short_prediction_soa computes the LPC
// prediction for all MAX_DEL_DEC_STATES (=4) lanes in parallel,
// producing a bit-exact replica of four independent scalar calls to
// silk_noise_shape_quantizer_short_prediction_c. Pure Go for now;
// the NEON asm kernel will replace this with a drop-in vectorized
// implementation while keeping the same signature.
//
// base is the psLPC_base value used by the scalar caller —
// i.e. NSQ_LPC_BUF_LENGTH - 1 + i for the per-sample iteration index.
// The implementation reads soa.sLPC_Q14[base-tap][lane] for tap in
// [0, order) and fans the work out across four lanes with the lane
// index innermost, matching the load pattern the NEON kernel will
// use (one vld1q_s32 per tap loads all four lanes).
func silk_noise_shape_quantizer_short_prediction_soa(
	soa *NSQDelDecSoA,
	base opus_int,
	coef16 []opus_int16,
	order opus_int,
) [MAX_DEL_DEC_STATES]opus_int32 {
	// Bias is (order >> 1) in every lane, matching the scalar reference
	// which seeds `out` with silk_RSHIFT(order, 1) before any SMLAWB.
	bias := silk_RSHIFT(opus_int32(order), 1)
	var out [MAX_DEL_DEC_STATES]opus_int32
	for lane := 0; lane < MAX_DEL_DEC_STATES; lane++ {
		out[lane] = bias
	}

	// Single tap loop handles both order=10 and order=16. The scalar
	// reference hard-codes the 10 SMLAWBs then branches on order==16
	// for the additional 6; iterating up to `order` is behaviourally
	// identical since each tap contributes an independent SMLAWB term.
	for i := opus_int(0); i < order; i++ {
		c := opus_int32(coef16[i])
		row := &soa.sLPC_Q14[base-i]
		for lane := 0; lane < MAX_DEL_DEC_STATES; lane++ {
			out[lane] = silk_SMLAWB(out[lane], row[lane], c)
		}
	}
	return out
}

// silk_noise_shape_allpass_soa — computes the noise-shape feedback
// allpass chain for all MAX_DEL_DEC_STATES (=4) lanes in parallel.
// Bit-exact replica of 4 serial scalar copies of the loop body at
// silk_NSQ_del_dec.go:403-425. Mutates soa.sAR2_Q14 in place (the
// scalar path mutates psDD.sAR2_Q14; the SoA function mutates the
// corresponding lane).
//
// Returns the per-lane n_AR_Q14 accumulator.
//
// Implementation notes:
//   - Uses [MAX_DEL_DEC_STATES]opus_int32 arrays as 4-lane vectors.
//     Each scalar silk_SMLAWB / silk_SUB32_ovflw in the reference maps
//     to 4 parallel invocations over lanes.
//   - The lane-innermost SoA layout means soa.sAR2_Q14[j][0..3] is a
//     contiguous 16-byte span and is the exact load shape a NEON
//     vld1q_s32 consumes.
//   - warping_Q16 and AR_shp_Q13 are broadcast scalars (shared across
//     lanes), matching the C scalar path where these are loop-invariant
//     across the inner k loop.
func silk_noise_shape_allpass_soa(
	soa *NSQDelDecSoA,
	warping_Q16 opus_int32,
	AR_shp_Q13 []opus_int16,
	shapingLPCOrder opus_int,
) [MAX_DEL_DEC_STATES]opus_int32 {
	var tmp1, tmp2 [MAX_DEL_DEC_STATES]opus_int32
	var n_AR_Q14 [MAX_DEL_DEC_STATES]opus_int32

	// tmp2 = silk_SMLAWB(psDD.Diff_Q14, psDD.sAR2_Q14[0], warping_Q16)
	// tmp1 = silk_SMLAWB(psDD.sAR2_Q14[0],
	//                    silk_SUB32_ovflw(psDD.sAR2_Q14[1], tmp2),
	//                    warping_Q16)
	// psDD.sAR2_Q14[0] = tmp2
	// n_AR_Q14 = shapingLPCOrder >> 1
	// n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp2, AR_shp_Q13[0])
	bias := silk_RSHIFT(opus_int32(shapingLPCOrder), 1)
	arShp0 := opus_int32(AR_shp_Q13[0])
	sAR2_0 := &soa.sAR2_Q14[0]
	sAR2_1 := &soa.sAR2_Q14[1]
	for lane := 0; lane < MAX_DEL_DEC_STATES; lane++ {
		t2 := silk_SMLAWB(soa.Diff_Q14[lane], sAR2_0[lane], warping_Q16)
		t1 := silk_SMLAWB(sAR2_0[lane], silk_SUB32_ovflw(sAR2_1[lane], t2), warping_Q16)
		sAR2_0[lane] = t2
		tmp2[lane] = t2
		tmp1[lane] = t1
		n_AR_Q14[lane] = silk_SMLAWB(bias, t2, arShp0)
	}

	// Loop over allpass sections (j = 2, 4, ..., shapingLPCOrder-2).
	for j := opus_int(2); j < shapingLPCOrder; j += 2 {
		arShpJm1 := opus_int32(AR_shp_Q13[j-1])
		arShpJ := opus_int32(AR_shp_Q13[j])
		sAR2_jm1 := &soa.sAR2_Q14[j-1]
		sAR2_j0 := &soa.sAR2_Q14[j+0]
		sAR2_j1 := &soa.sAR2_Q14[j+1]
		for lane := 0; lane < MAX_DEL_DEC_STATES; lane++ {
			// tmp2 = silk_SMLAWB(psDD.sAR2_Q14[j-1],
			//                    silk_SUB32_ovflw(psDD.sAR2_Q14[j+0], tmp1),
			//                    warping_Q16)
			// psDD.sAR2_Q14[j-1] = tmp1
			// n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp1, AR_shp_Q13[j-1])
			t2 := silk_SMLAWB(sAR2_jm1[lane], silk_SUB32_ovflw(sAR2_j0[lane], tmp1[lane]), warping_Q16)
			sAR2_jm1[lane] = tmp1[lane]
			n_AR_Q14[lane] = silk_SMLAWB(n_AR_Q14[lane], tmp1[lane], arShpJm1)
			// tmp1 = silk_SMLAWB(psDD.sAR2_Q14[j+0],
			//                    silk_SUB32_ovflw(psDD.sAR2_Q14[j+1], tmp2),
			//                    warping_Q16)
			// psDD.sAR2_Q14[j+0] = tmp2
			// n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp2, AR_shp_Q13[j])
			t1 := silk_SMLAWB(sAR2_j0[lane], silk_SUB32_ovflw(sAR2_j1[lane], t2), warping_Q16)
			sAR2_j0[lane] = t2
			n_AR_Q14[lane] = silk_SMLAWB(n_AR_Q14[lane], t2, arShpJ)
			tmp1[lane] = t1
			tmp2[lane] = t2
		}
	}

	// psDD.sAR2_Q14[shapingLPCOrder-1] = tmp1
	// n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp1, AR_shp_Q13[shapingLPCOrder-1])
	arShpLast := opus_int32(AR_shp_Q13[shapingLPCOrder-1])
	sAR2_last := &soa.sAR2_Q14[shapingLPCOrder-1]
	for lane := 0; lane < MAX_DEL_DEC_STATES; lane++ {
		sAR2_last[lane] = tmp1[lane]
		n_AR_Q14[lane] = silk_SMLAWB(n_AR_Q14[lane], tmp1[lane], arShpLast)
	}

	return n_AR_Q14
}

// nsqDelDecSoAtoAoS is the inverse projection: it reads lane k of the
// SoA into dst[k] for k in [0, nStates). Lanes k >= nStates are not
// touched — the caller is responsible for any bookkeeping on unused
// slots. All fields of NSQ_del_dec_struct have a counterpart in the
// SoA, so every destination byte is written.
//
// Lane-innermost order again: the inner i loop reads
// src.<field>[i][k] which, after a future NEON lane-extract, will
// correspond to vgetq_lane_s32(vld1q_s32(&src.<field>[i][0]), k).
func nsqDelDecSoAtoAoS(dst []NSQ_del_dec_struct, src *NSQDelDecSoA, nStates opus_int) {
	for k := opus_int(0); k < nStates; k++ {
		d := &dst[k]
		lane := int(k)

		for i := 0; i < MAX_SUB_FRAME_LENGTH+NSQ_LPC_BUF_LENGTH; i++ {
			d.sLPC_Q14[i] = src.sLPC_Q14[i][lane]
		}
		for i := 0; i < DECISION_DELAY; i++ {
			d.RandState[i] = src.RandState[i][lane]
			d.Q_Q10[i] = src.Q_Q10[i][lane]
			d.Xq_Q14[i] = src.Xq_Q14[i][lane]
			d.Pred_Q15[i] = src.Pred_Q15[i][lane]
			d.Shape_Q14[i] = src.Shape_Q14[i][lane]
		}
		for i := 0; i < MAX_SHAPE_LPC_ORDER; i++ {
			d.sAR2_Q14[i] = src.sAR2_Q14[i][lane]
		}

		d.LF_AR_Q14 = src.LF_AR_Q14[lane]
		d.Diff_Q14 = src.Diff_Q14[lane]
		d.Seed = src.Seed[lane]
		d.SeedInit = src.SeedInit[lane]
		d.RD_Q10 = src.RD_Q10[lane]
	}
}
