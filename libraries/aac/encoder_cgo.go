// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package aac

/*
#include <stdlib.h>
#include <string.h>
#include "aacenc_lib.h"

// aacGoEncOpen opens and initialises a raw-AAC (TRANSMUX 0) encoder for the
// given AOT, sample rate, channel count and bitrate (CBR) or VBR quality mode.
// When vbrMode is 0 it configures CBR at bitrate; when vbrMode is 1..5 it
// configures that VBR quality mode (AACENC_BITRATEMODE) and lets the lib derive
// the bitrate. On success it returns AACENC_OK and fills *frameLength with the
// per-channel frame size and asc[0:*ascLen] with the AudioSpecificConfig the
// container must emit.
static int aacGoEncOpen(int aot, int sampleRate, int channels, int bitrate,
                        int vbrMode, HANDLE_AACENCODER *outEnc,
                        int *frameLength,
                        unsigned char *asc, int *ascLen) {
    HANDLE_AACENCODER enc;
    if (aacEncOpen(&enc, 0, (UINT)channels) != AACENC_OK) {
        return -1;
    }
    int channelMode = (channels == 1) ? MODE_1 : MODE_2;
    if (aacEncoder_SetParam(enc, AACENC_AOT, (UINT)aot) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_SAMPLERATE, (UINT)sampleRate) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_CHANNELMODE, (UINT)channelMode) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_TRANSMUX, 0) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_AFTERBURNER, 1) != AACENC_OK) {
        aacEncClose(&enc);
        return -2;
    }
    if (vbrMode != 0) {
        if (aacEncoder_SetParam(enc, AACENC_BITRATEMODE, (UINT)vbrMode) != AACENC_OK) {
            aacEncClose(&enc);
            return -2;
        }
    } else {
        if (aacEncoder_SetParam(enc, AACENC_BITRATE, (UINT)bitrate) != AACENC_OK) {
            aacEncClose(&enc);
            return -2;
        }
    }
    if (aacEncEncode(enc, NULL, NULL, NULL, NULL) != AACENC_OK) {
        aacEncClose(&enc);
        return -3;
    }
    AACENC_InfoStruct info;
    memset(&info, 0, sizeof(info));
    if (aacEncInfo(enc, &info) != AACENC_OK) {
        aacEncClose(&enc);
        return -4;
    }
    *frameLength = (int)info.frameLength;
    int n = (int)info.confSize;
    if (n > *ascLen) {
        n = *ascLen;
    }
    memcpy(asc, info.confBuf, (size_t)n);
    *ascLen = n;
    *outEnc = enc;
    return 0;
}

// aacGoEncodeFrame encodes one frame of interleaved int16 PCM (in[0:nIn])
// into out (capacity outCap). It returns AACENC_OK on success and sets
// *outBytes to the produced access-unit length.
static int aacGoEncodeFrame(HANDLE_AACENCODER enc,
                            short *in, int nIn,
                            unsigned char *out, int outCap,
                            int *outBytes) {
    AACENC_BufDesc inDesc;  memset(&inDesc, 0, sizeof(inDesc));
    AACENC_BufDesc outDesc; memset(&outDesc, 0, sizeof(outDesc));
    AACENC_InArgs inArgs;   memset(&inArgs, 0, sizeof(inArgs));
    AACENC_OutArgs outArgs; memset(&outArgs, 0, sizeof(outArgs));

    void *inPtr = in;
    INT inId = IN_AUDIO_DATA;
    INT inSize = nIn * (INT)sizeof(short);
    INT inElem = (INT)sizeof(short);
    inDesc.numBufs = 1;
    inDesc.bufs = &inPtr;
    inDesc.bufferIdentifiers = &inId;
    inDesc.bufSizes = &inSize;
    inDesc.bufElSizes = &inElem;

    void *outPtr = out;
    INT outId = OUT_BITSTREAM_DATA;
    INT outSize = outCap;
    INT outElem = 1;
    outDesc.numBufs = 1;
    outDesc.bufs = &outPtr;
    outDesc.bufferIdentifiers = &outId;
    outDesc.bufSizes = &outSize;
    outDesc.bufElSizes = &outElem;

    inArgs.numInSamples = nIn;

    AACENC_ERROR e = aacEncEncode(enc, &inDesc, &outDesc, &inArgs, &outArgs);
    if (e != AACENC_OK) {
        return (int)e;
    }
    *outBytes = (int)outArgs.numOutBytes;
    return 0;
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// fdkEncoder is the cgo [Encoder] backed by the vendored Fraunhofer
// FDK-AAC reference. It consumes one frame of interleaved float64 PCM and
// produces one raw AAC access unit (TRANSMUX 0). Not safe for concurrent
// use.
type fdkEncoder struct {
	handle     C.HANDLE_AACENCODER
	asc        AudioSpecificConfig
	sampleRate int
	channels   int
	frame      int
	bitrate    int       // CBR bitrate (used by Reset; ignored when vbrMode != 0)
	vbrMode    int       // 0 = CBR, 1..5 = VBR quality mode (used by Reset)
	pcm16      []C.short // scratch interleaved int16, reused across Encode
	out        []byte    // scratch access-unit buffer, reused across Encode
}

// newEncoder routes [NewEncoder] to the vendored FDK-AAC backend in the
// aacfdk build.
func newEncoder(sampleRate, channels int, cfg encoderConfig) (Encoder, error) {
	aot := aotToFDK(cfg.objectType)
	bitrate := cfg.bitrate
	if bitrate <= 0 {
		bitrate = 128000
	}

	var handle C.HANDLE_AACENCODER
	var frameLen C.int
	ascBuf := make([]byte, 64)
	ascLen := C.int(len(ascBuf))
	rc := C.aacGoEncOpen(
		C.int(aot), C.int(sampleRate), C.int(channels), C.int(bitrate),
		C.int(cfg.vbrMode), &handle, &frameLen,
		(*C.uchar)(unsafe.Pointer(&ascBuf[0])), &ascLen,
	)
	if rc != 0 {
		return nil, ErrBadArg
	}

	raw := make([]byte, int(ascLen))
	copy(raw, ascBuf[:int(ascLen)])

	e := &fdkEncoder{
		handle:     handle,
		sampleRate: sampleRate,
		channels:   channels,
		frame:      int(frameLen),
		bitrate:    bitrate,
		vbrMode:    cfg.vbrMode,
		pcm16:      make([]C.short, int(frameLen)*channels),
		out:        make([]byte, MaxFrameBytes),
		asc: AudioSpecificConfig{
			ObjectType:   cfg.objectType,
			SampleRate:   sampleRate,
			Channels:     channels,
			FrameSamples: int(frameLen),
			Raw:          raw,
		},
	}
	runtime.SetFinalizer(e, (*fdkEncoder).close)
	return e, nil
}

// Encode encodes one frame of interleaved float64 PCM into a single AAC
// access unit. pcm must hold FrameSamples × Channels() samples.
func (e *fdkEncoder) Encode(pcm []float64) ([]byte, error) {
	if e.handle == nil {
		return nil, ErrInternal
	}
	want := e.frame * e.channels
	if len(pcm) < want {
		return nil, ErrBadArg
	}
	for i := 0; i < want; i++ {
		v := pcm[i]
		if v > 1.0 {
			v = 1.0
		} else if v < -1.0 {
			v = -1.0
		}
		s := int32(v * 32767.0)
		if s > 32767 {
			s = 32767
		} else if s < -32768 {
			s = -32768
		}
		e.pcm16[i] = C.short(s)
	}

	var outBytes C.int
	rc := C.aacGoEncodeFrame(
		e.handle,
		&e.pcm16[0], C.int(want),
		(*C.uchar)(unsafe.Pointer(&e.out[0])), C.int(len(e.out)),
		&outBytes,
	)
	if rc != 0 {
		return nil, ErrInternal
	}
	pkt := make([]byte, int(outBytes))
	copy(pkt, e.out[:int(outBytes)])
	return pkt, nil
}

func (e *fdkEncoder) Config() AudioSpecificConfig { return e.asc }
func (e *fdkEncoder) SampleRate() int             { return e.sampleRate }
func (e *fdkEncoder) Channels() int               { return e.channels }

// Reset reopens the encoder with the same parameters; FDK exposes no
// in-place reset that clears the look-ahead/bit-reservoir state.
func (e *fdkEncoder) Reset() {
	if e.handle == nil {
		return
	}
	C.aacEncClose(&e.handle)
	e.handle = nil

	var handle C.HANDLE_AACENCODER
	var frameLen C.int
	ascBuf := make([]byte, 64)
	ascLen := C.int(len(ascBuf))
	rc := C.aacGoEncOpen(
		C.int(aotToFDK(e.asc.ObjectType)), C.int(e.sampleRate), C.int(e.channels),
		C.int(e.bitrate), C.int(e.vbrMode), &handle, &frameLen,
		(*C.uchar)(unsafe.Pointer(&ascBuf[0])), &ascLen,
	)
	if rc == 0 {
		e.handle = handle
	}
}

func (e *fdkEncoder) close() {
	if e.handle != nil {
		C.aacEncClose(&e.handle)
		e.handle = nil
	}
}

// aotToFDK maps a public [AudioObjectType] to the MPEG-4 AOT index FDK
// expects for AACENC_AOT. Only the AAC-LC target is exercised; other
// values pass through as their index.
func aotToFDK(t AudioObjectType) int {
	switch t {
	case AOTAACMain:
		return 1
	case AOTAACLC:
		return 2
	case AOTAACSSR:
		return 3
	case AOTAACLTP:
		return 4
	case AOTSBR:
		return 5
	case AOTPS:
		return 29
	default:
		return 2 // default to AAC-LC
	}
}
