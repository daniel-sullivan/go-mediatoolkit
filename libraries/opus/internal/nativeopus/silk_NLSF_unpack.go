package nativeopus

// Port of libopus/silk/NLSF_unpack.c.

// silk_NLSF_unpack — Unpack predictor values and indices for entropy
// coding tables.
func silk_NLSF_unpack(ec_ix []opus_int16, pred_Q8 []opus_uint8,
	psNLSF_CB *silk_NLSF_CB_struct, CB1_index opus_int) {
	ec_sel_base := opus_int(CB1_index) * opus_int(psNLSF_CB.order) / 2
	for i := opus_int(0); i < opus_int(psNLSF_CB.order); i += 2 {
		entry := psNLSF_CB.ec_sel[ec_sel_base]
		ec_sel_base++
		ec_ix[i] = opus_int16(silk_SMULBB(
			opus_int32(silk_RSHIFT(opus_int32(entry), 1)&7),
			opus_int32(2*NLSF_QUANT_MAX_AMPLITUDE+1)))
		pred_Q8[i] = psNLSF_CB.pred_Q8[i+opus_int(entry&1)*(opus_int(psNLSF_CB.order)-1)]
		ec_ix[i+1] = opus_int16(silk_SMULBB(
			opus_int32(silk_RSHIFT(opus_int32(entry), 5)&7),
			opus_int32(2*NLSF_QUANT_MAX_AMPLITUDE+1)))
		pred_Q8[i+1] = psNLSF_CB.pred_Q8[i+opus_int(silk_RSHIFT(opus_int32(entry), 4)&1)*(opus_int(psNLSF_CB.order)-1)+1]
	}
}
