package mp3

import (
	"io"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// This file wires the pure-Go MP3 port (libraries/mp3/internal/nativemp3, the
// 1:1 minimp3 translation) into the public Decoder interface. It mirrors the
// cgo path in decoder_cgo.go byte-for-byte in behavior: it buffers compressed
// input from the io.Reader, hands the buffered bytes to the port's
// mp3dec_decode_frame translation (nativemp3.DecodeFrame), advances by the
// reported frame_bytes, and surfaces (0, nil) on skipped junk / io.EOF on
// exhaustion. The two paths share StreamInfo / version derivation so a caller
// gets identical results whether or not cgo is available.
//
// The pure-Go encoder adapter (newNativeEncoder / nativeEncoder) is a seam for
// the LAME-derived 1:1 port, so it is fenced behind the mp3lame build tag:
// native_encoder.go (//go:build mp3lame) provides it, and
// native_encoder_disabled.go (//go:build !mp3lame) returns
// ErrEncoderRequiresLAME. Only the decoder (minimp3, MIT/public-domain) lives
// here unfenced.

// newNativeDecoder builds a Decoder backed by the pure-Go nativemp3 decoder.
func newNativeDecoder(r io.Reader, cfg decoderConfig) (Decoder, error) {
	d := &nativeDecoder{r: r, cfg: cfg}
	nativemp3.Mp3decInit(&d.dec)
	return d, nil
}

// nativeDecoder adapts the pure-Go MP3 port to the public Decoder interface.
// It buffers compressed input and decodes one MP3 frame per DecodeFrame call.
// Not safe for concurrent use.
type nativeDecoder struct {
	r      io.Reader
	cfg    decoderConfig
	dec    nativemp3.Decoder
	in     []byte // unconsumed compressed bytes
	eof    bool   // underlying reader returned io.EOF
	info   StreamInfo
	closed bool
}

// refill grows the input buffer from the underlying reader until it holds at
// least want bytes or the reader is exhausted.
func (d *nativeDecoder) refill(want int) error {
	for !d.eof && len(d.in) < want {
		tmp := make([]byte, 16*1024)
		n, err := d.r.Read(tmp)
		if n > 0 {
			d.in = append(d.in, tmp[:n]...)
		}
		if err == io.EOF {
			d.eof = true
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// DecodeFrame decodes the next MP3 frame into buf (interleaved int16) and
// returns the samples-per-channel produced. A (0, nil) return means a
// non-audio region (e.g. ID3 padding) was skipped and the caller should call
// again. Returns io.EOF when the stream is exhausted.
func (d *nativeDecoder) DecodeFrame(buf []int16) (int, error) {
	if d.closed {
		return 0, ErrClosed
	}
	// Keep a generous lookahead so the port can resync past leading junk.
	if err := d.refill(16 * 1024); err != nil {
		return 0, err
	}
	if len(d.in) == 0 {
		return 0, io.EOF
	}
	if len(buf) < MaxSamplesPerFrame*MaxChannels {
		return 0, ErrBadArg
	}

	var info nativemp3.FrameInfo
	samples := nativemp3.DecodeFrame(&d.dec, d.in, len(d.in), buf, &info)

	consumed := info.FrameBytes
	if consumed > 0 {
		d.in = d.in[consumed:]
	}

	if samples == 0 {
		if consumed == 0 {
			// No frame was found and nothing was consumed: either the reader
			// is exhausted, or the buffered bytes are junk shorter than a
			// frame with no more input coming. Either way the stream is done.
			return 0, io.EOF
		}
		// Skipped ID3 / junk bytes; caller retries.
		return 0, nil
	}

	d.info = StreamInfo{
		Version:         versionFromHz(info.Hz),
		SampleRate:      info.Hz,
		Channels:        info.Channels,
		BitRate:         info.BitrateKbps * 1000,
		SamplesPerFrame: samples,
	}
	return samples, nil
}

// versionFromHz infers the MPEG version from the decoded sample rate, which is
// unambiguous across the MPEG-1 / MPEG-2 / MPEG-2.5 rate tables.
func versionFromHz(hz int) MPEGVersion {
	switch hz {
	case 32000, 44100, 48000:
		return MPEGVersion1
	case 16000, 22050, 24000:
		return MPEGVersion2
	case 8000, 11025, 12000:
		return MPEGVersion25
	default:
		return MPEGVersionUnknown
	}
}

// StreamInfo returns the most recently decoded frame's parameters.
func (d *nativeDecoder) StreamInfo() StreamInfo { return d.info }

// SampleRate returns the stream sample rate in Hz.
func (d *nativeDecoder) SampleRate() int { return d.info.SampleRate }

// Channels returns the channel count.
func (d *nativeDecoder) Channels() int { return d.info.Channels }

// Close releases resources held by the decoder. The port holds no heap
// resources, so this only marks the decoder unusable.
func (d *nativeDecoder) Close() error {
	if d.closed {
		return ErrClosed
	}
	d.closed = true
	return nil
}
