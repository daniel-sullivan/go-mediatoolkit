package nativeopus

// Exports for SILK Phase 7 decoder parity tests. The parity tests
// drive silk_Decode (the top-level SILK decoder entry) through the
// same bitstream that the C oracle consumes, comparing the recovered
// PCM and the final `rng` state for bit-exact parity.

// ExportTestSilkPLCReset invokes silk_PLC_Reset on a fresh state and
// returns the resulting PLC fields. Used by parity tests to verify
// the Reset initialisation matches the C oracle.
func ExportTestSilkPLCReset(frameLength int, fsKhz int) (pitchL_Q8 int32, prevGain0, prevGain1 int32, subfrLen, nbSubfr int) {
	var st silk_decoder_state
	st.frame_length = opus_int(frameLength)
	st.fs_kHz = opus_int(fsKhz)
	silk_PLC_Reset(&st)
	return int32(st.sPLC.pitchL_Q8),
		int32(st.sPLC.prevGain_Q16[0]),
		int32(st.sPLC.prevGain_Q16[1]),
		int(st.sPLC.subfr_length),
		int(st.sPLC.nb_subfr)
}

// ExportTestSilkCNGReset invokes silk_CNG_Reset on a state with the
// given LPC_order and returns the CNG fields.
func ExportTestSilkCNGReset(LPC_order int) (smthNLSF []int16, smthGain, randSeed int32) {
	var st silk_decoder_state
	st.LPC_order = opus_int(LPC_order)
	silk_CNG_Reset(&st)
	smth := make([]int16, LPC_order)
	for i := 0; i < LPC_order; i++ {
		smth[i] = int16(st.sCNG.CNG_smth_NLSF_Q15[i])
	}
	return smth, int32(st.sCNG.CNG_smth_Gain_Q16), int32(st.sCNG.rand_seed)
}

// ExportTestSilkInitDecoder initialises a decoder state and returns
// the first_frame_after_reset / prev_gain_Q16 / arch fields. This is a
// lightweight sanity check — we don't compare entire 600+-byte structs
// because field layout differs between C and Go, but the values should
// match.
func ExportTestSilkInitDecoder() (prevGainQ16 int32, firstFrame int, arch int) {
	var st silk_decoder_state
	silk_init_decoder(&st)
	return int32(st.prev_gain_Q16), int(st.first_frame_after_reset), st.arch
}

// ExportTestSilkDecoderSetFs invokes silk_decoder_set_fs and returns a
// summary of the updated state. The fields selected are the ones set
// by the C function, and suffice to verify its behaviour.
func ExportTestSilkDecoderSetFs(nbSubfr int, fsKHz int, fsAPIHz int32) (
	ret int, subfrLen, frameLen, ltpMemLen, LPCorder, lagPrev, lastGainIndex int,
	prevSignalType int, resamplerFn int) {

	var st silk_decoder_state
	silk_init_decoder(&st)
	st.nb_subfr = opus_int(nbSubfr)
	r := silk_decoder_set_fs(&st, opus_int(fsKHz), opus_int32(fsAPIHz))
	return int(r), int(st.subfr_length), int(st.frame_length), int(st.ltp_mem_length),
		int(st.LPC_order), int(st.lagPrev), int(st.LastGainIndex), int(st.prevSignalType),
		int(st.resampler_state.resampler_function)
}

// ExportTestSilkDecodeFull drives silk_Decode with the given encoded
// opus packet payload (post-TOC SILK bytes). Returns the PCM output,
// the range-coder tell() at exit, and the silk_Decode return code.
//
// inputs mirror the C silk_Decode signature:
//   - nChannelsAPI, nChannelsInternal: 1 or 2.
//   - apiFsHz: output sample rate.
//   - internalFsHz: internal SILK fs (8000/12000/16000).
//   - payloadSizeMs: 10/20/40/60.
//   - lostFlag: 0 (normal) / 1 (packet lost) / 2 (FEC).
//   - firstFrame: 1 on the first call for this packet.
//
// nSamplesOut is selected as apiFsHz * payloadSizeMs/1000 * nChannelsAPI.
func ExportTestSilkDecodeFull(packet []byte,
	nChannelsAPI, nChannelsInternal int,
	apiFsHz, internalFsHz int32,
	payloadSizeMs int, lostFlag int, firstFrame int) (pcm []float32, rng uint32, ret int) {

	var sd silk_decoder
	silk_InitDecoder(&sd)

	var dc silk_DecControlStruct
	dc.nChannelsAPI = opus_int32(nChannelsAPI)
	dc.nChannelsInternal = opus_int32(nChannelsInternal)
	dc.API_sampleRate = apiFsHz
	dc.internalSampleRate = internalFsHz
	dc.payloadSize_ms = opus_int(payloadSizeMs)

	var dec ec_dec
	ec_dec_init(&dec, packet, opus_uint32(len(packet)))

	nSamples := int(apiFsHz) * payloadSizeMs / 1000
	out := make([]opus_res, nSamples*nChannelsAPI)
	var nSamplesOut opus_int32

	rr := silk_Decode(&sd, &dc, opus_int(lostFlag), opus_int(firstFrame),
		&dec, out, &nSamplesOut, 0)
	pcm = make([]float32, len(out))
	for i, v := range out {
		pcm[i] = float32(v)
	}
	return pcm, uint32(dec.rng), int(rr)
}

// ExportTestEcDecRng returns the range-coder rng field. Used by
// external parity tests that need to compare the ec state after a
// decode.
func ExportTestEcDecRng(d *ec_dec) uint32 { return uint32(d.rng) }
