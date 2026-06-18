// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package mp3

/*
#include <stdlib.h>
#include "liblame/include/lame.h"

// lameNew configures a LAME encoder for interleaved int16 PCM. Returns NULL on
// any setup failure (bad params, lame_init_params error). The caller owns the
// returned handle and must lame_close it.
static lame_global_flags *lameNew(int sampleRate, int channels, int brate, int quality, int vbr) {
    lame_global_flags *gfp = lame_init();
    if (gfp == NULL) {
        return NULL;
    }
    lame_set_in_samplerate(gfp, sampleRate);
    lame_set_num_channels(gfp, channels);
    lame_set_quality(gfp, quality);
    if (channels == 1) {
        lame_set_mode(gfp, MONO);
    } else {
        lame_set_mode(gfp, JOINT_STEREO);
    }
    if (vbr) {
        lame_set_VBR(gfp, vbr_default);
        lame_set_VBR_q(gfp, quality);
    } else {
        lame_set_VBR(gfp, vbr_off);
        lame_set_brate(gfp, brate / 1000);
    }
    if (lame_init_params(gfp) < 0) {
        lame_close(gfp);
        return NULL;
    }
    return gfp;
}
*/
import "C"

import (
	"io"
	"unsafe"
)

// newEncoder is the cgo-backed entry point: it encodes via the vendored LAME
// 3.100 (libraries/mp3/liblame). Output frames are written to w as LAME
// produces them.
func newEncoder(w io.Writer, info StreamInfo, cfg encoderConfig) (Encoder, error) {
	var vbr C.int
	if cfg.vbr {
		vbr = 1
	}
	gfp := C.lameNew(C.int(info.SampleRate), C.int(info.Channels),
		C.int(cfg.bitRate), C.int(cfg.quality), vbr)
	if gfp == nil {
		return nil, ErrInternal
	}
	return &cgoEncoder{w: w, info: info, gfp: gfp}, nil
}

// cgoEncoder wraps a LAME lame_global_flags handle. Not safe for concurrent
// use.
type cgoEncoder struct {
	w      io.Writer
	info   StreamInfo
	gfp    *C.lame_global_flags
	closed bool
}

// mp3BufSize returns a worst-case output buffer size for nSamples per channel,
// per the LAME documentation: 1.25*num_samples + 7200.
func mp3BufSize(nSamples int) int { return nSamples + nSamples/4 + 7200 }

// EncodeFrame compresses a block of interleaved int16 samples via LAME and
// writes any produced MP3 bytes to the underlying writer.
func (e *cgoEncoder) EncodeFrame(buf []int16) error {
	if e.closed {
		return ErrClosed
	}
	if e.info.Channels == 0 || len(buf)%e.info.Channels != 0 {
		return ErrBadArg
	}
	if len(buf) == 0 {
		return nil
	}
	nPerChan := len(buf) / e.info.Channels
	out := make([]byte, mp3BufSize(nPerChan))
	var n C.int
	if e.info.Channels == 1 {
		// lame_encode_buffer_interleaved always reads two interleaved
		// channels, so a mono buffer must go through lame_encode_buffer with
		// the single channel supplied as both left and right.
		n = C.lame_encode_buffer(
			e.gfp,
			(*C.short)(unsafe.Pointer(&buf[0])),
			(*C.short)(unsafe.Pointer(&buf[0])),
			C.int(nPerChan),
			(*C.uchar)(unsafe.Pointer(&out[0])),
			C.int(len(out)),
		)
	} else {
		n = C.lame_encode_buffer_interleaved(
			e.gfp,
			(*C.short)(unsafe.Pointer(&buf[0])),
			C.int(nPerChan),
			(*C.uchar)(unsafe.Pointer(&out[0])),
			C.int(len(out)),
		)
	}
	if n < 0 {
		return ErrInternal
	}
	if n == 0 {
		return nil
	}
	_, err := e.w.Write(out[:n])
	return err
}

// Close flushes LAME's internal buffers (emitting any final frame) and
// releases the encoder handle.
func (e *cgoEncoder) Close() error {
	if e.closed {
		return ErrClosed
	}
	e.closed = true
	out := make([]byte, mp3BufSize(0))
	n := C.lame_encode_flush(e.gfp, (*C.uchar)(unsafe.Pointer(&out[0])), C.int(len(out)))
	C.lame_close(e.gfp)
	e.gfp = nil
	if n < 0 {
		return ErrInternal
	}
	if n == 0 {
		return nil
	}
	_, err := e.w.Write(out[:n])
	return err
}
