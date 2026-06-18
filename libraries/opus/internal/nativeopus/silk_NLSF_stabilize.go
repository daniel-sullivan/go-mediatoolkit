package nativeopus

// Port of libopus/silk/NLSF_stabilize.c.
//
// NLSF stabilizer for a single input data vector.
// - Moves NLSFs further apart if they are too close.
// - Moves NLSFs away from borders if they are too close.
// - High effort to achieve a modification with minimum Euclidean
//   distance to the input vector.
// - Output are sorted NLSF coefficients.

const silk_NLSF_stabilize_MAX_LOOPS = 20

// silk_NLSF_stabilize — stabilize a single NLSF vector in place.
// NDeltaMin_Q15 has length L+1 and each entry must be >= 1.
func silk_NLSF_stabilize(NLSF_Q15 []opus_int16, NDeltaMin_Q15 []opus_int16, L opus_int) {
	var I opus_int
	var loops opus_int
	var min_diff_Q15, diff_Q15, min_center_Q15, max_center_Q15 opus_int32
	var center_freq_Q15 opus_int16

	// This is necessary to ensure an output within range of an int16.
	silk_assert(NDeltaMin_Q15[L] >= 1)

	for loops = 0; loops < silk_NLSF_stabilize_MAX_LOOPS; loops++ {
		// Find smallest distance.
		// First element.
		min_diff_Q15 = opus_int32(NLSF_Q15[0]) - opus_int32(NDeltaMin_Q15[0])
		I = 0
		// Middle elements.
		for i := opus_int(1); i <= L-1; i++ {
			diff_Q15 = opus_int32(NLSF_Q15[i]) - (opus_int32(NLSF_Q15[i-1]) + opus_int32(NDeltaMin_Q15[i]))
			if diff_Q15 < min_diff_Q15 {
				min_diff_Q15 = diff_Q15
				I = i
			}
		}
		// Last element.
		diff_Q15 = (1 << 15) - (opus_int32(NLSF_Q15[L-1]) + opus_int32(NDeltaMin_Q15[L]))
		if diff_Q15 < min_diff_Q15 {
			min_diff_Q15 = diff_Q15
			I = L
		}

		// Now check if the smallest distance is non-negative.
		if min_diff_Q15 >= 0 {
			return
		}

		if I == 0 {
			// Move away from lower limit.
			NLSF_Q15[0] = NDeltaMin_Q15[0]
		} else if I == L {
			// Move away from higher limit.
			NLSF_Q15[L-1] = opus_int16(int32(1<<15) - int32(NDeltaMin_Q15[L]))
		} else {
			// Find the lower extreme for the location of the current center frequency.
			min_center_Q15 = 0
			for k := opus_int(0); k < I; k++ {
				min_center_Q15 += opus_int32(NDeltaMin_Q15[k])
			}
			min_center_Q15 += silk_RSHIFT(opus_int32(NDeltaMin_Q15[I]), 1)

			// Find the upper extreme for the location of the current center frequency.
			max_center_Q15 = 1 << 15
			for k := L; k > I; k-- {
				max_center_Q15 -= opus_int32(NDeltaMin_Q15[k])
			}
			max_center_Q15 -= silk_RSHIFT(opus_int32(NDeltaMin_Q15[I]), 1)

			// Move apart, sorted by value, keeping the same center frequency.
			center_freq_Q15 = opus_int16(silk_LIMIT_32(
				silk_RSHIFT_ROUND(opus_int32(NLSF_Q15[I-1])+opus_int32(NLSF_Q15[I]), 1),
				min_center_Q15, max_center_Q15))
			NLSF_Q15[I-1] = center_freq_Q15 - opus_int16(silk_RSHIFT(opus_int32(NDeltaMin_Q15[I]), 1))
			NLSF_Q15[I] = NLSF_Q15[I-1] + NDeltaMin_Q15[I]
		}
	}

	// Safe and simple fall-back method.
	if loops == silk_NLSF_stabilize_MAX_LOOPS {
		silk_insertion_sort_increasing_all_values_int16(NLSF_Q15, L)

		// First NLSF should be no less than NDeltaMin[0].
		if NLSF_Q15[0] < NDeltaMin_Q15[0] {
			NLSF_Q15[0] = NDeltaMin_Q15[0]
		}

		// Keep delta_min distance between the NLSFs.
		for i := opus_int(1); i < L; i++ {
			v := silk_ADD_SAT16(NLSF_Q15[i-1], NDeltaMin_Q15[i])
			if NLSF_Q15[i] < v {
				NLSF_Q15[i] = v
			}
		}

		// Last NLSF should be no higher than 1 - NDeltaMin[L].
		hi := opus_int16(int32(1<<15) - int32(NDeltaMin_Q15[L]))
		if NLSF_Q15[L-1] > hi {
			NLSF_Q15[L-1] = hi
		}

		// Keep NDeltaMin distance between the NLSFs.
		for i := L - 2; i >= 0; i-- {
			v := NLSF_Q15[i+1] - NDeltaMin_Q15[i+1]
			if NLSF_Q15[i] > v {
				NLSF_Q15[i] = v
			}
		}
	}
}
