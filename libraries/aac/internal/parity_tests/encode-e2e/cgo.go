// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encodee2e is the end-to-end AAC-LC CBR encode parity slice. It feeds
// identical int16 PCM frames and an identical AAC-LC CBR config (sample rate,
// channels, bitrate, AOT=2) to BOTH the genuine vendored Fraunhofer FDK-AAC
// encoder (aacEncEncode, TRANSMUX 0 raw access units) and the pure-Go
// internal/nativeaac encoder (nativeEncoder.Encode -> EncodeOneFrame), and
// asserts the emitted access units are BYTE-IDENTICAL. fdk-aac encode is
// fixed-point (int32 Q-format), so a fixed CBR config reproduces the bitstream
// bit-for-bit — the comparison is exact (no tolerance), as it must be.
//
// This slice compiles its OWN copy of the needed fdk C TUs (fdk_tu_*.cpp here);
// it NEVER imports libraries/aac. It may import internal/nativeaac for the
// pure-Go encoder under test. The genuine fdk symbol driven here is the real
// vendored aacEncEncode (libAACenc/src/aacenc_lib.cpp), not a re-derivation of
// the Go port. Build with `-tags aacfdk`.
//
// Scope: AAC-LC (AOT 2), CBR, TRANSMUX 0 raw AUs, 1024-sample frames, mono/
// stereo. EXCLUDED (and noted): VBR/two-pass (AACENC_BITRATEMODE 0 only),
// HE-AAC/SBR, ELD, and DRC/metadata — none are configured here.
package encodee2e

/*
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags come from the
// mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"); the encode path is
// fixed-point integer arithmetic, so that flag set is belt-and-suspenders.
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

// e2e_encode opens a raw-AAC (TRANSMUX 0) AAC-LC CBR encoder for the given
// sample rate / channels / bitrate, encodes framesIn interleaved int16 frames
// one at a time, and returns EVERY produced access unit concatenated in au[]
// with per-AU byte counts in auLens[0:*nAU], plus the ASC in asc[0:*ascLen].
//
// This drives the GENUINE vendored aacEncEncode. AACENC_AFTERBURNER is left at
// its default 0 so the byte-stream is the deterministic single-pass CBR encode
// the pure-Go FDKaacEnc_EncodeFrame port mirrors (the afterburner is a separate
// trellis-refinement search the 1:1 driver port does not run). Returns 0 on
// success.
static int e2e_encode(int sampleRate, int channels, int bitrate,
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
        aacEncoder_SetParam(enc, AACENC_BITRATEMODE, 0) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_TRANSMUX, 0) != AACENC_OK) {
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
*/
import "C"

import "unsafe"

// cEncode runs the genuine fdk encoder over framesIn frames and returns every
// produced access unit (in order) plus the ASC. The native encoder under test
// is replayed against the same PCM frame-for-frame.
func cEncode(sampleRate, channels, bitrate, frameLen int, pcm []int16) (aus [][]byte, asc []byte, ok bool) {
	framesIn := len(pcm) / (frameLen * channels)

	auBuf := make([]byte, framesIn*8192)
	auLens := make([]C.int, framesIn)
	var nAU C.int
	ascBuf := make([]byte, 64)
	ascLen := C.int(len(ascBuf))

	rc := C.e2e_encode(C.int(sampleRate), C.int(channels), C.int(bitrate),
		(*C.short)(unsafe.Pointer(&pcm[0])), C.int(framesIn), C.int(frameLen),
		(*C.uchar)(unsafe.Pointer(&auBuf[0])), C.int(len(auBuf)),
		(*C.int)(unsafe.Pointer(&auLens[0])), &nAU,
		(*C.uchar)(unsafe.Pointer(&ascBuf[0])), &ascLen)
	if rc != 0 {
		return nil, nil, false
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
	return aus, asc, true
}
