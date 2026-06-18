package nativeopus

// Port of libopus/silk/gain_quant.c.

const (
	silk_gain_OFFSET        = ((MIN_QGAIN_DB * 128) / 6) + 16*128
	silk_gain_SCALE_Q16     = (65536 * (N_LEVELS_QGAIN - 1)) / (((MAX_QGAIN_DB - MIN_QGAIN_DB) * 128) / 6)
	silk_gain_INV_SCALE_Q16 = (65536 * (((MAX_QGAIN_DB - MIN_QGAIN_DB) * 128) / 6)) / (N_LEVELS_QGAIN - 1)
)

// silk_gains_quant — gain scalar quantization with hysteresis, uniform on log scale.
func silk_gains_quant(ind []opus_int8, gain_Q16 []opus_int32,
	prev_ind *opus_int8, conditional, nb_subfr opus_int) {
	for k := opus_int(0); k < nb_subfr; k++ {
		ind[k] = opus_int8(silk_SMULWB(silk_gain_SCALE_Q16, silk_lin2log(gain_Q16[k])-silk_gain_OFFSET))

		if ind[k] < *prev_ind {
			ind[k]++
		}
		ind[k] = opus_int8(silk_LIMIT_int(opus_int(ind[k]), 0, N_LEVELS_QGAIN-1))

		if k == 0 && conditional == 0 {
			ind[k] = opus_int8(silk_LIMIT_int(opus_int(ind[k]), opus_int(*prev_ind)+MIN_DELTA_GAIN_QUANT, N_LEVELS_QGAIN-1))
			*prev_ind = ind[k]
		} else {
			ind[k] = ind[k] - *prev_ind

			double_step_size_threshold := opus_int(2*MAX_DELTA_GAIN_QUANT - N_LEVELS_QGAIN + opus_int(*prev_ind))
			if opus_int(ind[k]) > double_step_size_threshold {
				ind[k] = opus_int8(double_step_size_threshold + opus_int(silk_RSHIFT(opus_int32(ind[k])-opus_int32(double_step_size_threshold)+1, 1)))
			}

			ind[k] = opus_int8(silk_LIMIT_int(opus_int(ind[k]), MIN_DELTA_GAIN_QUANT, MAX_DELTA_GAIN_QUANT))

			if opus_int(ind[k]) > double_step_size_threshold {
				*prev_ind += opus_int8(silk_LSHIFT(opus_int32(ind[k]), 1)) - opus_int8(double_step_size_threshold)
				if *prev_ind > N_LEVELS_QGAIN-1 {
					*prev_ind = N_LEVELS_QGAIN - 1
				}
			} else {
				*prev_ind += ind[k]
			}

			ind[k] -= MIN_DELTA_GAIN_QUANT
		}

		gain_Q16[k] = silk_log2lin(silk_min_32(silk_SMULWB(silk_gain_INV_SCALE_Q16, opus_int32(*prev_ind))+silk_gain_OFFSET, 3967))
	}
}

// silk_gains_dequant — gain scalar dequantization.
func silk_gains_dequant(gain_Q16 []opus_int32, ind []opus_int8,
	prev_ind *opus_int8, conditional, nb_subfr opus_int) {
	for k := opus_int(0); k < nb_subfr; k++ {
		if k == 0 && conditional == 0 {
			v := silk_max_int(opus_int(ind[k]), opus_int(*prev_ind)-16)
			*prev_ind = opus_int8(v)
		} else {
			ind_tmp := opus_int(ind[k]) + MIN_DELTA_GAIN_QUANT

			double_step_size_threshold := opus_int(2*MAX_DELTA_GAIN_QUANT - N_LEVELS_QGAIN + opus_int(*prev_ind))
			if ind_tmp > double_step_size_threshold {
				*prev_ind += opus_int8(silk_LSHIFT(opus_int32(ind_tmp), 1)) - opus_int8(double_step_size_threshold)
			} else {
				*prev_ind += opus_int8(ind_tmp)
			}
		}
		*prev_ind = opus_int8(silk_LIMIT_int(opus_int(*prev_ind), 0, N_LEVELS_QGAIN-1))

		gain_Q16[k] = silk_log2lin(silk_min_32(silk_SMULWB(silk_gain_INV_SCALE_Q16, opus_int32(*prev_ind))+silk_gain_OFFSET, 3967))
	}
}

// silk_gains_ID — compute a unique identifier of the gain indices vector.
func silk_gains_ID(ind []opus_int8, nb_subfr opus_int) opus_int32 {
	var gainsID opus_int32
	for k := opus_int(0); k < nb_subfr; k++ {
		gainsID = silk_ADD_LSHIFT32(opus_int32(ind[k]), gainsID, 8)
	}
	return gainsID
}
