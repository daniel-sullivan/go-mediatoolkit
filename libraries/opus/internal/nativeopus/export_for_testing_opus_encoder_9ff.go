package nativeopus

// 9f-F test shims for opus_encode / opus_encode_float / opus_encode_native.
//
// These exposed entry points wrap the ported top-level encoder so the
// benchcmp package can drive byte-exact parity tests against the C
// oracle's opus_encode / opus_encode_float.

// ExportOpusEncodeFloat wraps opus_encode_float.
func ExportOpusEncodeFloat(st *OpusEncoder, pcm []float32, analysisFrameSize int,
	data []byte, outDataBytes int32) int32 {
	return int32(opus_encode_float(st, pcm, analysisFrameSize, data, opus_int32(outDataBytes)))
}

// ExportOpusEncodeInt16 wraps opus_encode.
func ExportOpusEncodeInt16(st *OpusEncoder, pcm []int16, analysisFrameSize int,
	data []byte, maxDataBytes int32) int32 {
	p := make([]opus_int16, len(pcm))
	for i, v := range pcm {
		p[i] = opus_int16(v)
	}
	return int32(opus_encode(st, p, analysisFrameSize, data, opus_int32(maxDataBytes)))
}

// ExportOpusEncoderCreate creates a Go OpusEncoder with full initialisation
// including installation of the built-in CELT mode — mirrors what
// opus_encoder_create does in C.
func ExportOpusEncoderCreate(Fs int32, channels, application int) (*OpusEncoder, int) {
	var err int
	st := opus_encoder_create(opus_int32(Fs), channels, application, &err)
	return st, err
}

// ExportOpusEncoderCtl forwards a CTL request.
func ExportOpusEncoderCtl(st *OpusEncoder, request int, args ...interface{}) int {
	return opus_encoder_ctl(st, request, args...)
}
