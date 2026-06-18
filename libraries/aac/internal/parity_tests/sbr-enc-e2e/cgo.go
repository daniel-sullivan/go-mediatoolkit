// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrence2e is the end-to-end HE-AAC v1 (AAC-LC core + SBR) ENCODE
// parity slice. It encodes real PCM with BOTH the genuine Fraunhofer FDK-AAC
// ENCODER (AOT_SBR == 5, raw transmux, afterburner) and the pure-Go
// internal/nativeaac/heaac encoder, and asserts the produced AOT-5 access-unit
// streams (every frame) plus the AudioSpecificConfig are BYTE-IDENTICAL —
// fdk-aac SBR is fixed-point, so the encode is reproducible bit-for-bit.
//
// This slice compiles its OWN copy of the needed fdk encoder C TUs (fdk_tu_*.cpp
// here, libAACenc + libSBRenc + the shared libFDK/libSYS/libMpegTPEnc/libPCMutils
// /libSACenc) and never imports libraries/aac; it MAY import internal/nativeaac
// + internal/nativeaac/heaac. Build with `-tags aacfdk`.
package sbrence2e

/*
#cgo CXXFLAGS: -std=c++11 -O2 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/src
#cgo LDFLAGS: -lm

#include <stdlib.h>
#include <string.h>
#include "aacenc_lib.h"

static int e2e_encode_heaac(int sampleRate, int channels, int bitrate,
                            short *pcm, int framesIn, int frameLen,
                            unsigned char *au, int auCap,
                            int *auLens, int *nAU,
                            unsigned char *asc, int *ascLen,
                            int *encDelay) {
    HANDLE_AACENCODER enc;
    if (aacEncOpen(&enc, 0, (UINT)channels) != AACENC_OK) return -1;
    int channelMode = (channels == 1) ? MODE_1 : MODE_2;
    if (aacEncoder_SetParam(enc, AACENC_AOT, 5) != AACENC_OK ||
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
    *encDelay = (int)info.nDelay;

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
*/
import "C"

import "unsafe"

// cEncodeHEAAC runs the genuine fdk HE-AAC encoder over framesIn frames and
// returns every produced access unit (in order) plus the ASC and the encoder delay.
func cEncodeHEAAC(sampleRate, channels, bitrate, frameLen int, pcm []int16) (aus [][]byte, asc []byte, encDelay int, ok bool) {
	framesIn := len(pcm) / (frameLen * channels)
	auBuf := make([]byte, framesIn*8192)
	auLens := make([]C.int, framesIn)
	var nAU C.int
	ascBuf := make([]byte, 64)
	ascLen := C.int(len(ascBuf))
	var cDelay C.int
	rc := C.e2e_encode_heaac(C.int(sampleRate), C.int(channels), C.int(bitrate),
		(*C.short)(unsafe.Pointer(&pcm[0])), C.int(framesIn), C.int(frameLen),
		(*C.uchar)(unsafe.Pointer(&auBuf[0])), C.int(len(auBuf)),
		(*C.int)(unsafe.Pointer(&auLens[0])), &nAU,
		(*C.uchar)(unsafe.Pointer(&ascBuf[0])), &ascLen, &cDelay)
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
	}
	asc = make([]byte, int(ascLen))
	copy(asc, ascBuf[:int(ascLen)])
	return aus, asc, int(cDelay), true
}
