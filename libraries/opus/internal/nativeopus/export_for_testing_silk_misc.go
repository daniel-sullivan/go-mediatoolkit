package nativeopus

// Exports for SILK gain_quant / decode_pitch / stereo parity tests.

func ExportTestSilkGainsQuant(gainQ16 []int32, prevInd int8, conditional int) ([]int8, []int32, int8) {
	gain := append([]int32(nil), gainQ16...)
	ind := make([]int8, len(gain))
	ind8 := make([]opus_int8, len(gain))
	p := opus_int8(prevInd)
	g32 := make([]opus_int32, len(gain))
	for i, v := range gain {
		g32[i] = v
	}
	silk_gains_quant(ind8, g32, &p, opus_int(conditional), opus_int(len(gain)))
	for i, v := range ind8 {
		ind[i] = int8(v)
		gain[i] = int32(g32[i])
	}
	return ind, gain, int8(p)
}

func ExportTestSilkGainsDequant(ind []int8, prevInd int8, conditional int) ([]int32, int8) {
	p := opus_int8(prevInd)
	gain := make([]opus_int32, len(ind))
	ind8 := make([]opus_int8, len(ind))
	for i, v := range ind {
		ind8[i] = opus_int8(v)
	}
	silk_gains_dequant(gain, ind8, &p, opus_int(conditional), opus_int(len(ind)))
	out := make([]int32, len(gain))
	for i, v := range gain {
		out[i] = int32(v)
	}
	return out, int8(p)
}

func ExportTestSilkGainsID(ind []int8) int32 {
	in := make([]opus_int8, len(ind))
	for i, v := range ind {
		in[i] = opus_int8(v)
	}
	return silk_gains_ID(in, opus_int(len(in)))
}

func ExportTestSilkDecodePitch(lagIndex int16, contourIndex int8, Fs_kHz, nb_subfr int) []int {
	lags := make([]opus_int, nb_subfr)
	silk_decode_pitch(opus_int16(lagIndex), opus_int8(contourIndex), lags, opus_int(Fs_kHz), opus_int(nb_subfr))
	out := make([]int, nb_subfr)
	for i, v := range lags {
		out[i] = int(v)
	}
	return out
}

// ExportTestSilkStereoPredRoundtrip encodes ix via silk_stereo_encode_pred,
// decodes via silk_stereo_decode_pred, and returns the packet and predictors.
func ExportTestSilkStereoPredRoundtrip(ix [2][3]int8, bufSize int) ([]byte, []int32) {
	buf := make([]byte, bufSize)
	var enc ec_enc
	ec_enc_init(&enc, buf, opus_uint32(bufSize))
	var ix8 [2][3]opus_int8
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			ix8[i][j] = opus_int8(ix[i][j])
		}
	}
	silk_stereo_encode_pred(&enc, ix8)
	ec_enc_done(&enc)
	pkt := append([]byte(nil), buf...)
	var dec ec_dec
	ec_dec_init(&dec, pkt, opus_uint32(bufSize))
	pred := make([]opus_int32, 2)
	silk_stereo_decode_pred(&dec, pred)
	out := []int32{int32(pred[0]), int32(pred[1])}
	return pkt, out
}

func ExportTestSilkStereoMidOnlyRoundtrip(flag int8, bufSize int) ([]byte, int) {
	buf := make([]byte, bufSize)
	var enc ec_enc
	ec_enc_init(&enc, buf, opus_uint32(bufSize))
	silk_stereo_encode_mid_only(&enc, opus_int8(flag))
	ec_enc_done(&enc)
	pkt := append([]byte(nil), buf...)
	var dec ec_dec
	ec_dec_init(&dec, pkt, opus_uint32(bufSize))
	var v opus_int
	silk_stereo_decode_mid_only(&dec, &v)
	return pkt, int(v)
}

func ExportTestSilkStereoQuantPred(pred []int32) ([]int32, [2][3]int8) {
	in := make([]opus_int32, len(pred))
	for i, v := range pred {
		in[i] = opus_int32(v)
	}
	var ix [2][3]opus_int8
	silk_stereo_quant_pred(in, &ix)
	out := []int32{int32(in[0]), int32(in[1])}
	var outIdx [2][3]int8
	for i := 0; i < 2; i++ {
		for j := 0; j < 3; j++ {
			outIdx[i][j] = int8(ix[i][j])
		}
	}
	return out, outIdx
}

func ExportTestSilkStereoFindPredictor(x, y []int16, midResAmpQ0 []int32, smoothQ16 int) (int32, int32, []int32) {
	xin := make([]opus_int16, len(x))
	yin := make([]opus_int16, len(y))
	amp := make([]opus_int32, len(midResAmpQ0))
	for i, v := range x {
		xin[i] = opus_int16(v)
	}
	for i, v := range y {
		yin[i] = opus_int16(v)
	}
	for i, v := range midResAmpQ0 {
		amp[i] = opus_int32(v)
	}
	var ratio opus_int32
	p := silk_stereo_find_predictor(&ratio, xin, yin, amp, opus_int(len(x)), opus_int(smoothQ16))
	outAmp := make([]int32, len(amp))
	for i, v := range amp {
		outAmp[i] = int32(v)
	}
	return int32(p), int32(ratio), outAmp
}

func ExportTestSilkStereoMSToLR(pred []int32, x1, x2 []int16, predPrev, sMid, sSide [2]int16, fsKhz, frameLen int) ([]int16, []int16, [2]int16, [2]int16, [2]int16) {
	var state stereo_dec_state
	for i := 0; i < 2; i++ {
		state.pred_prev_Q13[i] = opus_int16(predPrev[i])
		state.sMid[i] = opus_int16(sMid[i])
		state.sSide[i] = opus_int16(sSide[i])
	}
	in1 := make([]opus_int16, len(x1))
	in2 := make([]opus_int16, len(x2))
	for i, v := range x1 {
		in1[i] = opus_int16(v)
	}
	for i, v := range x2 {
		in2[i] = opus_int16(v)
	}
	predIn := make([]opus_int32, len(pred))
	for i, v := range pred {
		predIn[i] = opus_int32(v)
	}
	silk_stereo_MS_to_LR(&state, in1, in2, predIn, opus_int(fsKhz), opus_int(frameLen))
	out1 := make([]int16, len(in1))
	out2 := make([]int16, len(in2))
	for i, v := range in1 {
		out1[i] = int16(v)
	}
	for i, v := range in2 {
		out2[i] = int16(v)
	}
	var pp, sM, sS [2]int16
	for i := 0; i < 2; i++ {
		pp[i] = int16(state.pred_prev_Q13[i])
		sM[i] = int16(state.sMid[i])
		sS[i] = int16(state.sSide[i])
	}
	return out1, out2, pp, sM, sS
}
