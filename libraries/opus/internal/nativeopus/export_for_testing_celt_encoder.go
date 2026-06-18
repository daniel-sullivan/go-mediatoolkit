package nativeopus

// CELT encoder test shims.

type CeltEncoderHandle struct{ p *OpusCustomEncoder }

func NewCeltEncoder(m CeltModeHandle, sampling_rate int32, channels int) (CeltEncoderHandle, int) {
	st := &OpusCustomEncoder{}
	ret := celt_encoder_init(st, m.p, opus_int32(sampling_rate), channels, 0)
	return CeltEncoderHandle{p: st}, ret
}

func (h CeltEncoderHandle) SetStartEnd(start, end int) {
	h.p.start = start
	h.p.end = end
}
func (h CeltEncoderHandle) SetSignalling(v int) { h.p.signalling = v }
func (h CeltEncoderHandle) SetBitrate(v int32)  { h.p.bitrate = opus_int32(v) }
func (h CeltEncoderHandle) SetComplexity(v int) { h.p.complexity = v }
func (h CeltEncoderHandle) Rng() uint32         { return uint32(h.p.rng) }
func (h CeltEncoderHandle) ResetState()         { celt_encoder_reset(h.p) }

// ExportTestCeltEncodeWithEc runs celt_encode_with_ec on the Go
// encoder state. Returns the number of compressed bytes produced.
func ExportTestCeltEncodeWithEc(h CeltEncoderHandle, pcm []float32,
	frame_size int, compressed []byte) int {
	return celt_encode_with_ec(h.p, pcm, frame_size, compressed,
		len(compressed), nil)
}
