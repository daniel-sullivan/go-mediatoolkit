// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// cgo_fdk.go supplies the vendored Fraunhofer FDK-AAC cgo reference (the "cgo"
// column of the AAC benchmarks). It compiles its OWN copy of the needed fdk C
// TUs (the fdk_tu_*.cpp files alongside) and never imports libraries/aac — the
// same amalgamation-split discipline the decode-e2e / encode-e2e parity slices
// use, so the AAC benchcmp suite can link both the native (internal/nativeaac)
// and the cgo (FDK) paths in one test binary without a duplicate-symbol clash.
//
// The C encoder is the genuine production-style fdk encoder (TRANSMUX 0, raw
// AAC-LC, afterburner on, -O2); the C decoder is the production fdk decoder
// with the PCM peak limiter DISABLED so the output is the bare fixed-point
// decode chain (the chain the pure-Go port mirrors). Build with `-tags aacfdk`.
package benchcmp

/*
#cgo CXXFLAGS: -std=c++11 -O2 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libArithCoding/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libDRCdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/src
#cgo LDFLAGS: -lm

#include <stdlib.h>
#include <string.h>
#include "aacenc_lib.h"
#include "aacdecoder_lib.h"

// bench_encode opens a raw-AAC (TRANSMUX 0) AAC-LC encoder, encodes framesIn
// interleaved int16 frames one at a time, and returns EVERY produced access
// unit concatenated in au[] with per-AU byte counts in auLens[0:*nAU], plus the
// ASC in asc[0:*ascLen]. Returns 0 on success. Mirrors decode-e2e's e2e_encode.
static int bench_encode(int sampleRate, int channels, int bitrate,
                        short *pcm, int framesIn, int frameLen,
                        unsigned char *au, int auCap,
                        int *auLens, int *nAU,
                        unsigned char *asc, int *ascLen) {
    HANDLE_AACENCODER enc;
    if (aacEncOpen(&enc, 0, (UINT)channels) != AACENC_OK) return -1;
    int channelMode = (channels == 1) ? MODE_1 : MODE_2;
    if (aacEncoder_SetParam(enc, AACENC_AOT, 2) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_SAMPLERATE, (UINT)sampleRate) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_CHANNELMODE, (UINT)channelMode) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_BITRATE, (UINT)bitrate) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_TRANSMUX, 0) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_AFTERBURNER, 1) != AACENC_OK) {
        aacEncClose(&enc); return -2;
    }
    if (aacEncEncode(enc, NULL, NULL, NULL, NULL) != AACENC_OK) { aacEncClose(&enc); return -3; }
    AACENC_InfoStruct info; memset(&info, 0, sizeof(info));
    if (aacEncInfo(enc, &info) != AACENC_OK) { aacEncClose(&enc); return -4; }
    int n = (int)info.confSize; if (n > *ascLen) n = *ascLen;
    memcpy(asc, info.confBuf, (size_t)n); *ascLen = n;

    int per = frameLen * channels;
    int off = 0, count = 0;
    for (int f = 0; f < framesIn; f++) {
        AACENC_BufDesc inDesc;  memset(&inDesc, 0, sizeof(inDesc));
        AACENC_BufDesc outDesc; memset(&outDesc, 0, sizeof(outDesc));
        AACENC_InArgs inArgs;   memset(&inArgs, 0, sizeof(inArgs));
        AACENC_OutArgs outArgs; memset(&outArgs, 0, sizeof(outArgs));

        void *inPtr = pcm + (size_t)f * per;
        INT inId = IN_AUDIO_DATA, inSize = per * (INT)sizeof(short), inElem = (INT)sizeof(short);
        inDesc.numBufs = 1; inDesc.bufs = &inPtr; inDesc.bufferIdentifiers = &inId;
        inDesc.bufSizes = &inSize; inDesc.bufElSizes = &inElem;

        void *outPtr = au + off;
        INT outId = OUT_BITSTREAM_DATA, outSize = auCap - off, outElem = 1;
        outDesc.numBufs = 1; outDesc.bufs = &outPtr; outDesc.bufferIdentifiers = &outId;
        outDesc.bufSizes = &outSize; outDesc.bufElSizes = &outElem;

        inArgs.numInSamples = per;
        AACENC_ERROR e = aacEncEncode(enc, &inDesc, &outDesc, &inArgs, &outArgs);
        if (e != AACENC_OK) { aacEncClose(&enc); return -10 - (int)e; }
        if (outArgs.numOutBytes > 0) {
            auLens[count++] = (int)outArgs.numOutBytes;
            off += (int)outArgs.numOutBytes;
        }
    }
    aacEncClose(&enc);
    *nAU = count;
    return count > 0 ? 0 : -5;
}

// A persistent decoder handle so the per-op benchmark loop measures decode work,
// not aacDecoder_Open/Close churn. bench_decode_open configures it from the ASC,
// bench_decode_au decodes one AU into out, bench_decode_close tears it down.
static HANDLE_AACDECODER bench_decode_open(unsigned char *asc, int ascLen) {
    HANDLE_AACDECODER h = aacDecoder_Open(TT_MP4_RAW, 1);
    if (h == NULL) return NULL;
    if (aacDecoder_SetParam(h, AAC_PCM_LIMITER_ENABLE, 0) != AAC_DEC_OK) {
        aacDecoder_Close(h); return NULL;
    }
    UCHAR *cfg = (UCHAR *)asc; UINT clen = (UINT)ascLen;
    if (aacDecoder_ConfigRaw(h, &cfg, &clen) != AAC_DEC_OK) { aacDecoder_Close(h); return NULL; }
    return h;
}

static int bench_decode_au(HANDLE_AACDECODER h, unsigned char *au, int auLen,
                           short *out, int outCap, int *samplesPerCh, int *channels) {
    UCHAR *in = au; UINT size = (UINT)auLen; UINT valid = (UINT)auLen;
    if (aacDecoder_Fill(h, &in, &size, &valid) != AAC_DEC_OK) return -1;
    AAC_DECODER_ERROR e = aacDecoder_DecodeFrame(h, (INT_PCM *)out, outCap, 0);
    if (e != AAC_DEC_OK) return -10 - (int)e;
    CStreamInfo *si = aacDecoder_GetStreamInfo(h);
    if (si == NULL) return -2;
    *samplesPerCh = (int)si->frameSize; *channels = (int)si->numChannels;
    return 0;
}

static void bench_decode_close(HANDLE_AACDECODER h) { if (h) aacDecoder_Close(h); }
*/
import "C"

import "unsafe"

// cgoEncodeAll runs the fdk encoder over the interleaved int16 PCM, one frame at
// a time, and returns every produced access unit plus the ASC and total byte
// count. It is the cgo counterpart of the native EncodeOneFrame loop.
func cgoEncodeAll(sampleRate, channels, bitrate, frameLen int, pcm []int16) (aus [][]byte, asc []byte, total int, ok bool) {
	per := frameLen * channels
	framesIn := len(pcm) / per
	if framesIn == 0 {
		return nil, nil, 0, false
	}

	auBuf := make([]byte, framesIn*8192)
	auLens := make([]C.int, framesIn)
	var nAU C.int
	ascBuf := make([]byte, 64)
	ascLen := C.int(len(ascBuf))

	rc := C.bench_encode(C.int(sampleRate), C.int(channels), C.int(bitrate),
		(*C.short)(unsafe.Pointer(&pcm[0])), C.int(framesIn), C.int(frameLen),
		(*C.uchar)(unsafe.Pointer(&auBuf[0])), C.int(len(auBuf)),
		(*C.int)(unsafe.Pointer(&auLens[0])), &nAU,
		(*C.uchar)(unsafe.Pointer(&ascBuf[0])), &ascLen)
	if rc != 0 {
		return nil, nil, 0, false
	}

	off := 0
	for i := 0; i < int(nAU); i++ {
		l := int(auLens[i])
		a := make([]byte, l)
		copy(a, auBuf[off:off+l])
		aus = append(aus, a)
		off += l
		total += l
	}
	asc = make([]byte, int(ascLen))
	copy(asc, ascBuf[:int(ascLen)])
	return aus, asc, total, true
}

// cgoDecoder wraps a persistent fdk decoder handle so the decode benchmark loop
// measures per-AU decode cost without Open/Close churn.
type cgoDecoder struct {
	h   C.HANDLE_AACDECODER
	out []int16
}

// newCgoDecoder opens and configures an fdk decoder from the ASC.
func newCgoDecoder(asc []byte, frameLen, channels int) *cgoDecoder {
	h := C.bench_decode_open((*C.uchar)(unsafe.Pointer(&asc[0])), C.int(len(asc)))
	if h == nil {
		return nil
	}
	return &cgoDecoder{h: h, out: make([]int16, frameLen*channels*2)}
}

// decode decodes one access unit, returning samples-per-channel produced.
func (d *cgoDecoder) decode(au []byte) (samplesPerCh int, ok bool) {
	var sp, ch C.int
	rc := C.bench_decode_au(d.h,
		(*C.uchar)(unsafe.Pointer(&au[0])), C.int(len(au)),
		(*C.short)(unsafe.Pointer(&d.out[0])), C.int(len(d.out)), &sp, &ch)
	if rc != 0 {
		return 0, false
	}
	return int(sp), true
}

func (d *cgoDecoder) close() { C.bench_decode_close(d.h); d.h = nil }
