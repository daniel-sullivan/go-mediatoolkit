//go:build cgo

package mp3

/*
#include <stdlib.h>
#include "minimp3.h"
*/
import "C"

import (
	"io"
	"unsafe"
)

// newDecoder is the cgo-backed entry point: it decodes via the vendored
// minimp3 (libraries/mp3/libminimp3). The decoder reads from r lazily,
// refilling an internal byte buffer as minimp3 consumes frames.
func newDecoder(r io.Reader, cfg decoderConfig) (Decoder, error) {
	d := &cgoDecoder{r: r, cfg: cfg}
	C.mp3dec_init(&d.dec)
	return d, nil
}

// cgoDecoder wraps a minimp3 mp3dec_t. It buffers compressed input and decodes
// one MP3 frame per DecodeFrame call. Not safe for concurrent use.
type cgoDecoder struct {
	r      io.Reader
	cfg    decoderConfig
	dec    C.mp3dec_t
	in     []byte // unconsumed compressed bytes
	eof    bool   // underlying reader returned io.EOF
	info   StreamInfo
	closed bool
}

// refill grows the input buffer from the underlying reader until it holds at
// least want bytes or the reader is exhausted.
func (d *cgoDecoder) refill(want int) error {
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
func (d *cgoDecoder) DecodeFrame(buf []int16) (int, error) {
	if d.closed {
		return 0, ErrClosed
	}
	// Keep a generous lookahead so minimp3 can resync past leading junk.
	if err := d.refill(16 * 1024); err != nil {
		return 0, err
	}
	if len(d.in) == 0 {
		return 0, io.EOF
	}
	if len(buf) < MaxSamplesPerFrame*MaxChannels {
		return 0, ErrBadArg
	}

	var info C.mp3dec_frame_info_t
	samples := int(C.mp3dec_decode_frame(
		&d.dec,
		(*C.uint8_t)(unsafe.Pointer(&d.in[0])),
		C.int(len(d.in)),
		(*C.mp3d_sample_t)(unsafe.Pointer(&buf[0])),
		&info,
	))

	consumed := int(info.frame_bytes)
	if consumed > 0 {
		d.in = d.in[consumed:]
	}

	if samples == 0 {
		if consumed == 0 {
			// minimp3 found no frame and consumed nothing: either the reader
			// is exhausted, or the buffered bytes are junk shorter than a
			// frame with no more input coming. Either way the stream is done.
			return 0, io.EOF
		}
		// Skipped ID3 / junk bytes; caller retries.
		return 0, nil
	}

	d.info = StreamInfo{
		Version:         versionFromHz(int(info.hz)),
		SampleRate:      int(info.hz),
		Channels:        int(info.channels),
		BitRate:         int(info.bitrate_kbps) * 1000,
		SamplesPerFrame: samples,
	}
	return samples, nil
}

// StreamInfo returns the most recently decoded frame's parameters.
func (d *cgoDecoder) StreamInfo() StreamInfo { return d.info }

// SampleRate returns the stream sample rate in Hz, or zero before the first
// frame is decoded.
func (d *cgoDecoder) SampleRate() int { return d.info.SampleRate }

// Channels returns the channel count, or zero before the first frame is
// decoded.
func (d *cgoDecoder) Channels() int { return d.info.Channels }

// Close releases the decoder. minimp3 holds no heap resources, so this only
// marks the decoder unusable.
func (d *cgoDecoder) Close() error {
	if d.closed {
		return ErrClosed
	}
	d.closed = true
	return nil
}
