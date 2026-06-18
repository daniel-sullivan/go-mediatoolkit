package nativeopus

// Port of libopus/silk/shell_coder.c.

// silk_combine_pulses — out[k] = in[2k] + in[2k+1].
func silk_combine_pulses(out, in_ []opus_int, length opus_int) {
	for k := opus_int(0); k < length; k++ {
		out[k] = in_[2*k] + in_[2*k+1]
	}
}

func silk_encode_split(psRangeEnc *ec_enc, p_child1, p opus_int, shell_table []opus_uint8) {
	if p > 0 {
		ec_enc_icdf(psRangeEnc, int(p_child1),
			asByteSlice(shell_table[silk_shell_code_table_offsets[p]:]), 8)
	}
}

func silk_decode_split(p_child1, p_child2 *opus_int16, psRangeDec *ec_dec, p opus_int, shell_table []opus_uint8) {
	if p > 0 {
		*p_child1 = opus_int16(ec_dec_icdf(psRangeDec,
			asByteSlice(shell_table[silk_shell_code_table_offsets[p]:]), 8))
		*p_child2 = opus_int16(p) - *p_child1
	} else {
		*p_child1 = 0
		*p_child2 = 0
	}
}

// asByteSlice converts an opus_uint8 slice to the []byte alias
// ec_dec_icdf/ec_enc_icdf expect. Since opus_uint8 = uint8 = byte,
// this is a zero-cost reslice.
func asByteSlice(s []opus_uint8) []byte {
	return []byte(s)
}

// silk_shell_encoder — Shell encoder for a 16-pulse frame.
func silk_shell_encoder(psRangeEnc *ec_enc, pulses0 []opus_int) {
	var pulses1 [8]opus_int
	var pulses2 [4]opus_int
	var pulses3 [2]opus_int
	var pulses4 [1]opus_int

	silk_assert(SHELL_CODEC_FRAME_LENGTH == 16)

	silk_combine_pulses(pulses1[:], pulses0, 8)
	silk_combine_pulses(pulses2[:], pulses1[:], 4)
	silk_combine_pulses(pulses3[:], pulses2[:], 2)
	silk_combine_pulses(pulses4[:], pulses3[:], 1)

	silk_encode_split(psRangeEnc, pulses3[0], pulses4[0], silk_shell_code_table3[:])

	silk_encode_split(psRangeEnc, pulses2[0], pulses3[0], silk_shell_code_table2[:])

	silk_encode_split(psRangeEnc, pulses1[0], pulses2[0], silk_shell_code_table1[:])
	silk_encode_split(psRangeEnc, pulses0[0], pulses1[0], silk_shell_code_table0[:])
	silk_encode_split(psRangeEnc, pulses0[2], pulses1[1], silk_shell_code_table0[:])

	silk_encode_split(psRangeEnc, pulses1[2], pulses2[1], silk_shell_code_table1[:])
	silk_encode_split(psRangeEnc, pulses0[4], pulses1[2], silk_shell_code_table0[:])
	silk_encode_split(psRangeEnc, pulses0[6], pulses1[3], silk_shell_code_table0[:])

	silk_encode_split(psRangeEnc, pulses2[2], pulses3[1], silk_shell_code_table2[:])

	silk_encode_split(psRangeEnc, pulses1[4], pulses2[2], silk_shell_code_table1[:])
	silk_encode_split(psRangeEnc, pulses0[8], pulses1[4], silk_shell_code_table0[:])
	silk_encode_split(psRangeEnc, pulses0[10], pulses1[5], silk_shell_code_table0[:])

	silk_encode_split(psRangeEnc, pulses1[6], pulses2[3], silk_shell_code_table1[:])
	silk_encode_split(psRangeEnc, pulses0[12], pulses1[6], silk_shell_code_table0[:])
	silk_encode_split(psRangeEnc, pulses0[14], pulses1[7], silk_shell_code_table0[:])
}

// silk_shell_decoder — Shell decoder for a 16-pulse frame.
func silk_shell_decoder(pulses0 []opus_int16, psRangeDec *ec_dec, pulses4 opus_int) {
	var pulses3 [2]opus_int16
	var pulses2 [4]opus_int16
	var pulses1 [8]opus_int16

	silk_assert(SHELL_CODEC_FRAME_LENGTH == 16)

	silk_decode_split(&pulses3[0], &pulses3[1], psRangeDec, pulses4, silk_shell_code_table3[:])

	silk_decode_split(&pulses2[0], &pulses2[1], psRangeDec, opus_int(pulses3[0]), silk_shell_code_table2[:])

	silk_decode_split(&pulses1[0], &pulses1[1], psRangeDec, opus_int(pulses2[0]), silk_shell_code_table1[:])
	silk_decode_split(&pulses0[0], &pulses0[1], psRangeDec, opus_int(pulses1[0]), silk_shell_code_table0[:])
	silk_decode_split(&pulses0[2], &pulses0[3], psRangeDec, opus_int(pulses1[1]), silk_shell_code_table0[:])

	silk_decode_split(&pulses1[2], &pulses1[3], psRangeDec, opus_int(pulses2[1]), silk_shell_code_table1[:])
	silk_decode_split(&pulses0[4], &pulses0[5], psRangeDec, opus_int(pulses1[2]), silk_shell_code_table0[:])
	silk_decode_split(&pulses0[6], &pulses0[7], psRangeDec, opus_int(pulses1[3]), silk_shell_code_table0[:])

	silk_decode_split(&pulses2[2], &pulses2[3], psRangeDec, opus_int(pulses3[1]), silk_shell_code_table2[:])

	silk_decode_split(&pulses1[4], &pulses1[5], psRangeDec, opus_int(pulses2[2]), silk_shell_code_table1[:])
	silk_decode_split(&pulses0[8], &pulses0[9], psRangeDec, opus_int(pulses1[4]), silk_shell_code_table0[:])
	silk_decode_split(&pulses0[10], &pulses0[11], psRangeDec, opus_int(pulses1[5]), silk_shell_code_table0[:])

	silk_decode_split(&pulses1[6], &pulses1[7], psRangeDec, opus_int(pulses2[3]), silk_shell_code_table1[:])
	silk_decode_split(&pulses0[12], &pulses0[13], psRangeDec, opus_int(pulses1[6]), silk_shell_code_table0[:])
	silk_decode_split(&pulses0[14], &pulses0[15], psRangeDec, opus_int(pulses1[7]), silk_shell_code_table0[:])
}
