package nativeopus

// 1:1 port of libopus/silk/float/find_LPC_FLP.c.
// LPC analysis: runs Burg over the full frame, optionally searches
// NLSF interpolation indices to minimise residual energy over the
// first-half subframes, and emits final NLSF_Q15. Also includes
// 1:1 ports of the wrappers silk_A2NLSF_FLP / silk_NLSF2A_FLP from
// libopus/silk/float/wrappers_FLP.c because find_LPC_FLP is their
// only caller in this port's current scope.

// silk_A2NLSF_FLP — Convert AR filter coefficients to NLSF parameters.
// C: float/wrappers_FLP.c:37-51.
func silk_A2NLSF_FLP(
	NLSF_Q15 []opus_int16, // [LPC_order]
	pAR []silk_float, // [LPC_order]
	LPC_order opus_int,
) {
	var i opus_int
	var a_fix_Q16 [MAX_LPC_ORDER]opus_int32

	for i = 0; i < LPC_order; i++ {
		// a_fix_Q16[i] = silk_float2int( pAR[i] * 65536.0f );
		a_fix_Q16[i] = silk_float2int(mul_f32(pAR[i], 65536.0))
	}

	silk_A2NLSF(NLSF_Q15, a_fix_Q16[:], LPC_order)
}

// silk_NLSF2A_FLP — Convert LSF parameters to AR prediction filter coefficients.
// C: float/wrappers_FLP.c:54-69.
func silk_NLSF2A_FLP(
	pAR []silk_float, // [LPC_order] output
	NLSF_Q15 []opus_int16, // [LPC_order]
	LPC_order opus_int,
	arch int,
) {
	var i opus_int
	var a_fix_Q12 [MAX_LPC_ORDER]opus_int16

	silk_NLSF2A(a_fix_Q12[:], NLSF_Q15, LPC_order, arch)

	for i = 0; i < LPC_order; i++ {
		// pAR[i] = (silk_float)a_fix_Q12[i] * (1.0f / 4096.0f);
		pAR[i] = mul_f32(silk_float(a_fix_Q12[i]), 1.0/4096.0)
	}
}

// silk_find_LPC_FLP — LPC analysis.
// C: float/find_LPC_FLP.c:37-105.
func silk_find_LPC_FLP(
	psEncC *silk_encoder_state, // I/O encoder state (common part)
	NLSF_Q15 []opus_int16, // O: NLSFs
	x []silk_float, // I: input signal
	minInvGain silk_float,
	arch int,
) {
	var k, subfr_length opus_int
	var a [MAX_LPC_ORDER]silk_float

	// Used only for NLSF interpolation.
	var res_nrg, res_nrg_2nd, res_nrg_interp silk_float
	var NLSF0_Q15 [MAX_LPC_ORDER]opus_int16
	var a_tmp [MAX_LPC_ORDER]silk_float
	var LPC_res [MAX_FRAME_LENGTH + MAX_NB_SUBFR*MAX_LPC_ORDER]silk_float

	subfr_length = psEncC.subfr_length + psEncC.predictLPCOrder

	// Default: No interpolation.
	psEncC.indices.NLSFInterpCoef_Q2 = 4

	// Burg AR analysis for the full frame.
	res_nrg = silk_burg_modified_FLP(a[:], x, minInvGain, subfr_length, psEncC.nb_subfr, psEncC.predictLPCOrder, arch)

	if psEncC.useInterpolatedNLSFs != 0 && psEncC.first_frame_after_reset == 0 && psEncC.nb_subfr == MAX_NB_SUBFR {
		// Optimal solution for last 10 ms; subtract residual energy here, as that's easier than
		// adding it to the residual energy of the first 10 ms in each iteration of the search below.
		// C: res_nrg -= silk_burg_modified_FLP(...);
		// res_nrg is float32; subtract is separately-rounded float32.
		res_nrg = sub_f32(res_nrg,
			silk_burg_modified_FLP(a_tmp[:], x[(MAX_NB_SUBFR/2)*subfr_length:], minInvGain, subfr_length, MAX_NB_SUBFR/2, psEncC.predictLPCOrder, arch))

		// Convert to NLSFs.
		silk_A2NLSF_FLP(NLSF_Q15, a_tmp[:], psEncC.predictLPCOrder)

		// Search over interpolation indices to find the one with lowest residual energy.
		res_nrg_2nd = silk_float_MAX
		for k = 3; k >= 0; k-- {
			// Interpolate NLSFs for first half.
			silk_interpolate(NLSF0_Q15[:], psEncC.prev_NLSFq_Q15[:], NLSF_Q15, k, psEncC.predictLPCOrder)

			// Convert to LPC for residual energy evaluation.
			silk_NLSF2A_FLP(a_tmp[:], NLSF0_Q15[:], psEncC.predictLPCOrder, psEncC.arch)

			// Calculate residual energy with LSF interpolation.
			silk_LPC_analysis_filter_FLP(LPC_res[:], a_tmp[:], x, 2*subfr_length, psEncC.predictLPCOrder)

			// C:
			//   res_nrg_interp = (silk_float)(
			//       silk_energy_FLP( LPC_res + predictLPCOrder, subfr_length - predictLPCOrder ) +
			//       silk_energy_FLP( LPC_res + predictLPCOrder + subfr_length, subfr_length - predictLPCOrder ) );
			// Both silk_energy_FLP calls return double. The sum is in double,
			// then narrowed to silk_float on the outer cast.
			e1 := silk_energy_FLP(LPC_res[psEncC.predictLPCOrder:], subfr_length-psEncC.predictLPCOrder)
			e2 := silk_energy_FLP(LPC_res[psEncC.predictLPCOrder+subfr_length:], subfr_length-psEncC.predictLPCOrder)
			res_nrg_interp = silk_float(add_f64(e1, e2))

			// Determine whether current interpolated NLSFs are best so far.
			if res_nrg_interp < res_nrg {
				// Interpolation has lower residual energy.
				res_nrg = res_nrg_interp
				psEncC.indices.NLSFInterpCoef_Q2 = opus_int8(k)
			} else if res_nrg_interp > res_nrg_2nd {
				// No reason to continue iterating - residual energies will continue to climb.
				break
			}
			res_nrg_2nd = res_nrg_interp
		}
	}

	if psEncC.indices.NLSFInterpCoef_Q2 == 4 {
		// NLSF interpolation is currently inactive, calculate NLSFs from full frame AR coefficients.
		silk_A2NLSF_FLP(NLSF_Q15, a[:], psEncC.predictLPCOrder)
	}

	celt_assert(psEncC.indices.NLSFInterpCoef_Q2 == 4 ||
		(psEncC.useInterpolatedNLSFs != 0 && psEncC.first_frame_after_reset == 0 && psEncC.nb_subfr == MAX_NB_SUBFR))

	// res_nrg is computed but only used by the interpolation-branch
	// comparison; the C function doesn't return it either.
	_ = res_nrg
}
