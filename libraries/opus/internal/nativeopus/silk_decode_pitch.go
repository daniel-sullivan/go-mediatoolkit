package nativeopus

// Port of libopus/silk/decode_pitch.c.

// silk_decode_pitch — decode pitch lag + contour indices into 4 (or 2)
// per-subframe pitch values. The C version uses a flat pointer into a
// 2D row-major table; we dispatch on the originating 2D array and
// index [row][col] directly.
func silk_decode_pitch(lagIndex opus_int16, contourIndex opus_int8,
	pitch_lags []opus_int, Fs_kHz, nb_subfr opus_int) {

	min_lag := opus_int(silk_SMULBB(PE_MIN_LAG_MS, opus_int32(Fs_kHz)))
	max_lag := opus_int(silk_SMULBB(PE_MAX_LAG_MS, opus_int32(Fs_kHz)))
	lag := min_lag + opus_int(lagIndex)

	ci := opus_int(contourIndex)

	if Fs_kHz == 8 {
		if nb_subfr == PE_MAX_NB_SUBFR {
			for k := opus_int(0); k < nb_subfr; k++ {
				pitch_lags[k] = silk_LIMIT(lag+opus_int(silk_CB_lags_stage2[k][ci]), min_lag, max_lag)
			}
		} else {
			celt_assert(nb_subfr == PE_MAX_NB_SUBFR>>1)
			for k := opus_int(0); k < nb_subfr; k++ {
				pitch_lags[k] = silk_LIMIT(lag+opus_int(silk_CB_lags_stage2_10_ms[k][ci]), min_lag, max_lag)
			}
		}
	} else {
		if nb_subfr == PE_MAX_NB_SUBFR {
			for k := opus_int(0); k < nb_subfr; k++ {
				pitch_lags[k] = silk_LIMIT(lag+opus_int(silk_CB_lags_stage3[k][ci]), min_lag, max_lag)
			}
		} else {
			celt_assert(nb_subfr == PE_MAX_NB_SUBFR>>1)
			for k := opus_int(0); k < nb_subfr; k++ {
				pitch_lags[k] = silk_LIMIT(lag+opus_int(silk_CB_lags_stage3_10_ms[k][ci]), min_lag, max_lag)
			}
		}
	}
}
