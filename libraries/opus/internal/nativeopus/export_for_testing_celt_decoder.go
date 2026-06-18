package nativeopus

// CELT decoder test shims.

type CeltDecoderHandle struct{ p *OpusCustomDecoder }

// NewCeltDecoder constructs a Go CELTDecoder attached to the given
// mode mirror.
func NewCeltDecoder(m CeltModeHandle, sampling_rate int32, channels int) (CeltDecoderHandle, int) {
	st := &OpusCustomDecoder{}
	ret := celt_decoder_init(st, m.p, opus_int32(sampling_rate), channels)
	return CeltDecoderHandle{p: st}, ret
}

// SetStartEnd sets the start/end band window (e.g. CELT_SET_START_BAND).
func (h CeltDecoderHandle) SetStartEnd(start, end int) {
	h.p.start = start
	h.p.end = end
}

func (h CeltDecoderHandle) Rng() uint32         { return uint32(h.p.rng) }
func (h CeltDecoderHandle) ResetState()         { celt_decoder_reset(h.p) }
func (h CeltDecoderHandle) SetSignalling(v int) { h.p.signalling = v }

// ExportTestCeltDecodeWithEc runs the Go celt_decode_with_ec on the
// provided data and writes `N * channels` float32 samples to pcm.
func ExportTestCeltDecodeWithEc(h CeltDecoderHandle, data []byte,
	pcm []float32, frame_size, accum int) int {
	return celt_decode_with_ec(h.p, data, len(data), pcm, frame_size, nil, accum)
}
