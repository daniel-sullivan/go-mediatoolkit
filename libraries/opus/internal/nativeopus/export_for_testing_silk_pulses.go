package nativeopus

// Exports for SILK shell-coder / decode_pulses parity tests.

// ExportTestSilkShellRoundtrip encodes 16 pulses via shell encoder, then
// decodes via shell decoder; returns the encoded packet bytes and the
// decoded pulses.
func ExportTestSilkShellRoundtrip(pulses []int, bufSize int) ([]byte, []int16) {
	buf := make([]byte, bufSize)
	var enc ec_enc
	ec_enc_init(&enc, buf, opus_uint32(bufSize))
	input := make([]opus_int, len(pulses))
	for i, p := range pulses {
		input[i] = opus_int(p)
	}
	silk_shell_encoder(&enc, input)
	ec_enc_done(&enc)
	pkt := append([]byte(nil), buf...)

	var dec ec_dec
	ec_dec_init(&dec, pkt, opus_uint32(len(pkt)))
	decoded := make([]opus_int16, 16)
	sum := 0
	for _, p := range pulses {
		sum += p
	}
	silk_shell_decoder(decoded, &dec, opus_int(sum))
	out := make([]int16, 16)
	for i, v := range decoded {
		out[i] = int16(v)
	}
	return pkt, out
}

// ExportTestSilkDecodePulses decodes from a packet.
func ExportTestSilkDecodePulses(pkt []byte, signalType, quantOffsetType, frame_length int) []int16 {
	var dec ec_dec
	ec_dec_init(&dec, pkt, opus_uint32(len(pkt)))
	pulses := make([]opus_int16, frame_length)
	silk_decode_pulses(&dec, pulses, opus_int(signalType), opus_int(quantOffsetType), opus_int(frame_length))
	out := make([]int16, frame_length)
	for i, v := range pulses {
		out[i] = int16(v)
	}
	return out
}

func ExportTestSilkEncodeSigns(pulses []int8, length, signalType, quantOffsetType int, sum_pulses []int, bufSize int) []byte {
	buf := make([]byte, bufSize)
	var enc ec_enc
	ec_enc_init(&enc, buf, opus_uint32(bufSize))
	sp := make([]opus_int, len(sum_pulses))
	for i, v := range sum_pulses {
		sp[i] = opus_int(v)
	}
	p8 := make([]opus_int8, len(pulses))
	for i, v := range pulses {
		p8[i] = opus_int8(v)
	}
	silk_encode_signs(&enc, p8, opus_int(length), opus_int(signalType), opus_int(quantOffsetType), sp)
	ec_enc_done(&enc)
	return append([]byte(nil), buf...)
}

func ExportTestSilkDecodeSigns(pkt []byte, pulses []int16, length, signalType, quantOffsetType int, sum_pulses []int) []int16 {
	var dec ec_dec
	ec_dec_init(&dec, pkt, opus_uint32(len(pkt)))
	p16 := make([]opus_int16, len(pulses))
	for i, v := range pulses {
		p16[i] = opus_int16(v)
	}
	sp := make([]opus_int, len(sum_pulses))
	for i, v := range sum_pulses {
		sp[i] = opus_int(v)
	}
	silk_decode_signs(&dec, p16, opus_int(length), opus_int(signalType), opus_int(quantOffsetType), sp)
	out := make([]int16, len(pulses))
	for i, v := range p16 {
		out[i] = int16(v)
	}
	return out
}
