// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package aac

/*
#include <stdlib.h>
#include "aacdecoder_lib.h"

// aacGoDecOpenRaw opens a raw-AAC (TT_MP4_RAW) decoder and applies the
// AudioSpecificConfig in asc[0:ascLen] via aacDecoder_ConfigRaw. It returns
// the handle, or NULL on failure.
static HANDLE_AACDECODER aacGoDecOpenRaw(unsigned char *asc, unsigned int ascLen) {
    HANDLE_AACDECODER h = aacDecoder_Open(TT_MP4_RAW, 1);
    if (h == NULL) {
        return NULL;
    }
    UCHAR *cfg = (UCHAR *)asc;
    UINT len = (UINT)ascLen;
    if (aacDecoder_ConfigRaw(h, &cfg, &len) != AAC_DEC_OK) {
        aacDecoder_Close(h);
        return NULL;
    }
    return h;
}

// aacGoDecodeFrame feeds one access unit (pkt[0:pktLen]) and decodes one
// frame into out (capacity outCap INT_PCM samples). It returns the
// AAC_DECODER_ERROR; on success *outSamplesPerCh / *outChannels / *outRate
// describe the produced frame.
static int aacGoDecodeFrame(HANDLE_AACDECODER h,
                            unsigned char *pkt, unsigned int pktLen,
                            short *out, int outCap,
                            int *outSamplesPerCh, int *outChannels, int *outRate) {
    UCHAR *in = (UCHAR *)pkt;
    UINT size = (UINT)pktLen;
    UINT valid = (UINT)pktLen;
    AAC_DECODER_ERROR err = aacDecoder_Fill(h, &in, &size, &valid);
    if (err != AAC_DEC_OK) {
        return (int)err;
    }
    err = aacDecoder_DecodeFrame(h, (INT_PCM *)out, outCap, 0);
    if (err != AAC_DEC_OK) {
        return (int)err;
    }
    CStreamInfo *si = aacDecoder_GetStreamInfo(h);
    if (si == NULL) {
        return (int)AAC_DEC_UNKNOWN;
    }
    *outSamplesPerCh = (int)si->frameSize;
    *outChannels = (int)si->numChannels;
    *outRate = (int)si->sampleRate;
    return (int)AAC_DEC_OK;
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// fdkDecoder is the cgo [Decoder] backed by the vendored Fraunhofer
// FDK-AAC reference. It consumes raw AAC access units (TT_MP4_RAW) and
// produces interleaved float64 PCM. Not safe for concurrent use.
type fdkDecoder struct {
	handle     C.HANDLE_AACDECODER
	asc        AudioSpecificConfig
	sampleRate int
	channels   int
	frame      int
	pcm16      []C.short // scratch int16 PCM, reused across Decode calls
}

// newDecoder routes [NewDecoder] to the vendored FDK-AAC backend in the
// aacfdk build.
func newDecoder(asc AudioSpecificConfig, cfg decoderConfig) (Decoder, error) {
	if len(asc.Raw) == 0 {
		return nil, ErrInvalidConfig
	}
	raw := C.CBytes(asc.Raw)
	defer C.free(raw)
	h := C.aacGoDecOpenRaw((*C.uchar)(raw), C.uint(len(asc.Raw)))
	if h == nil {
		return nil, ErrInvalidConfig
	}
	frame := asc.FrameSamples
	if frame == 0 {
		frame = FrameSamplesLong
	}
	d := &fdkDecoder{
		handle:     h,
		asc:        asc,
		sampleRate: asc.SampleRate,
		channels:   asc.Channels,
		frame:      frame,
		pcm16:      make([]C.short, FrameSamplesLong*MaxChannels),
	}
	runtime.SetFinalizer(d, (*fdkDecoder).close)
	return d, nil
}

// Decode decodes one AAC access unit into interleaved float64 pcm and
// returns the samples-per-channel produced.
func (d *fdkDecoder) Decode(pkt []byte, pcm []float64) (int, error) {
	if d.handle == nil {
		return 0, ErrInternal
	}
	if len(pkt) == 0 {
		return 0, ErrInvalidPacket
	}
	if len(pcm) < d.frame*d.channels {
		return 0, ErrBufferTooSmall
	}

	var samplesPerCh, channels, rate C.int
	err := C.aacGoDecodeFrame(
		d.handle,
		(*C.uchar)(unsafe.Pointer(&pkt[0])), C.uint(len(pkt)),
		&d.pcm16[0], C.int(len(d.pcm16)),
		&samplesPerCh, &channels, &rate,
	)
	if err != 0 {
		return 0, ErrInvalidPacket
	}
	d.channels = int(channels)
	d.sampleRate = int(rate)

	n := int(samplesPerCh)
	total := n * int(channels)
	if total > len(pcm) {
		return 0, ErrBufferTooSmall
	}
	// FDK emits interleaved int16; normalise to [-1, 1].
	for i := 0; i < total; i++ {
		pcm[i] = float64(int16(d.pcm16[i])) / 32768.0
	}
	return n, nil
}

func (d *fdkDecoder) SampleRate() int             { return d.sampleRate }
func (d *fdkDecoder) Channels() int               { return d.channels }
func (d *fdkDecoder) Config() AudioSpecificConfig { return d.asc }

// Reset clears decoder state. FDK exposes no in-place stream reset for a
// raw-config decoder, so it reopens the handle from the same ASC.
func (d *fdkDecoder) Reset() {
	if d.handle != nil {
		C.aacDecoder_Close(d.handle)
		d.handle = nil
	}
	if len(d.asc.Raw) == 0 {
		return
	}
	raw := C.CBytes(d.asc.Raw)
	defer C.free(raw)
	d.handle = C.aacGoDecOpenRaw((*C.uchar)(raw), C.uint(len(d.asc.Raw)))
}

func (d *fdkDecoder) close() {
	if d.handle != nil {
		C.aacDecoder_Close(d.handle)
		d.handle = nil
	}
}
