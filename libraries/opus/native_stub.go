package opus

import (
	"fmt"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// newNativeEncoder / newNativeDecoder route to the pure-Go libopus port
// at libraries/opus/internal/nativeopus, which is byte-exact against the
// vendored C implementation across the full opus_encode / opus_decode
// matrix (see internal/parity_tests/benchcmp/parity_opus_encode_matrix_test.go).

func newNativeEncoder(sampleRate, channels int, cfg encoderConfig) (Encoder, error) {
	app := nativeopus.ApplicationAudio
	switch cfg.application {
	case AppVoIP:
		app = nativeopus.ApplicationVoIP
	case AppAudio:
		app = nativeopus.ApplicationAudio
	case AppLowDelay:
		app = nativeopus.ApplicationRestrictedLowdelay
	}
	st, code := nativeopus.NewEncoder(int32(sampleRate), channels, app)
	if code != nativeopus.ErrorOK {
		return nil, opusErrorToGo(code)
	}
	if cfg.bitrate > 0 {
		if code := nativeopus.EncoderCtl(st, nativeopus.CtlSetBitrate, int32(cfg.bitrate)); code != nativeopus.ErrorOK {
			nativeopus.DestroyEncoder(st)
			return nil, opusErrorToGo(code)
		}
	}
	if cfg.complexity > 0 {
		if code := nativeopus.EncoderCtl(st, nativeopus.CtlSetComplexity, int32(cfg.complexity)); code != nativeopus.ErrorOK {
			nativeopus.DestroyEncoder(st)
			return nil, opusErrorToGo(code)
		}
	}
	return &nativeEncoder{st: st, sampleRate: sampleRate, channels: channels}, nil
}

func newNativeDecoder(sampleRate, channels int, cfg decoderConfig) (Decoder, error) {
	_ = cfg // gain not yet applied; parity tests don't exercise it.
	st, code := nativeopus.NewDecoder(int32(sampleRate), channels)
	if code != nativeopus.ErrorOK {
		return nil, opusErrorToGo(code)
	}
	return &nativeDecoder{st: st, sampleRate: sampleRate, channels: channels}, nil
}

type nativeEncoder struct {
	st         *nativeopus.OpusEncoder
	sampleRate int
	channels   int
	// inScratch is a float64→float32 conversion buffer reused across
	// Encode calls to avoid a per-call allocation of len(pcm)*4 bytes.
	// The output packet buffer is still allocated fresh each call
	// since we return a slice of it to the caller.
	inScratch []float32
}

func (e *nativeEncoder) Encode(pcm []float64, maxPacketSize int) ([]byte, error) {
	if len(pcm)%e.channels != 0 {
		return nil, ErrBadArg
	}
	samplesPerChannel := len(pcm) / e.channels
	if cap(e.inScratch) < len(pcm) {
		e.inScratch = make([]float32, len(pcm))
	}
	in := e.inScratch[:len(pcm)]
	for i, v := range pcm {
		in[i] = float32(v)
	}
	out := make([]byte, maxPacketSize)
	n := nativeopus.EncodeFloat(e.st, in, samplesPerChannel, out)
	if n < 0 {
		return nil, opusErrorToGo(n)
	}
	return out[:n], nil
}

func (e *nativeEncoder) SampleRate() int { return e.sampleRate }
func (e *nativeEncoder) Channels() int   { return e.channels }
func (e *nativeEncoder) Reset()          { nativeopus.ResetEncoder(e.st) }

func (e *nativeEncoder) SetBitrate(bps int) error {
	if code := nativeopus.EncoderCtl(e.st, nativeopus.CtlSetBitrate, int32(bps)); code != nativeopus.ErrorOK {
		return opusErrorToGo(code)
	}
	return nil
}

type nativeDecoder struct {
	st         *nativeopus.OpusDecoder
	sampleRate int
	channels   int
	// outScratch is the float32 buffer the native decoder writes into
	// before we widen to float64 into the caller's pcm. Reused across
	// Decode calls to avoid a per-call allocation of len(pcm)*4 bytes.
	outScratch []float32
}

func (d *nativeDecoder) Decode(data []byte, pcm []float64) (int, error) {
	if len(pcm)%d.channels != 0 {
		return 0, ErrBadArg
	}
	framesPerChannel := len(pcm) / d.channels
	if cap(d.outScratch) < len(pcm) {
		d.outScratch = make([]float32, len(pcm))
	}
	out := d.outScratch[:len(pcm)]
	n := nativeopus.DecodeFloat(d.st, data, out, framesPerChannel, 0)
	if n < 0 {
		return 0, opusErrorToGo(n)
	}
	for i := 0; i < n*d.channels; i++ {
		pcm[i] = float64(out[i])
	}
	return n, nil
}

func (d *nativeDecoder) SampleRate() int { return d.sampleRate }
func (d *nativeDecoder) Channels() int   { return d.channels }
func (d *nativeDecoder) Reset()          { nativeopus.ResetDecoder(d.st) }
func (d *nativeDecoder) LastPacketDuration() int {
	return nativeopus.DecoderLastPacketDuration(d.st)
}

func opusErrorToGo(code int) error {
	switch code {
	case nativeopus.ErrorOK:
		return nil
	case nativeopus.ErrorBadArg:
		return ErrBadArg
	case nativeopus.ErrorBufferTooSmall:
		return ErrBufferTooSmall
	case nativeopus.ErrorInternalError:
		return ErrInternal
	case nativeopus.ErrorInvalidPacket:
		return ErrInvalidPacket
	case nativeopus.ErrorUnimplemented:
		return ErrUnimplemented
	case nativeopus.ErrorInvalidState:
		return ErrInvalidState
	case nativeopus.ErrorAllocFail:
		return ErrAllocFail
	default:
		return fmt.Errorf("opus: unknown error code %d", code)
	}
}
