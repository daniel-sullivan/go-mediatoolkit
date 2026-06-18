// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package decodee2e is the end-to-end AAC-LC decode parity slice. It produces a
// real AAC-LC access unit with the vendored Fraunhofer FDK-AAC ENCODER, decodes
// it with BOTH the FDK cgo DECODER (PCM limiter disabled, so the output is the
// deterministic fixed-point decode chain) and the pure-Go internal/nativeaac
// decoder, and asserts the two int16 PCM frames are EXACTLY equal — fdk-aac is
// fixed-point, so the decode is reproducible bit-for-bit, not merely within
// tolerance.
//
// This slice compiles its OWN copy of the needed fdk C TUs (fdk_tu_*.cpp here);
// it never imports libraries/aac. It may import internal/nativeaac for the
// pure-Go decoder under test. Build with `-tags aacfdk`.
package decodee2e

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

// e2e_encode opens a raw-AAC (TRANSMUX 0) AAC-LC encoder, encodes framesIn
// interleaved int16 frames one at a time, and returns EVERY produced access
// unit concatenated in au[] with per-AU byte counts in auLens[0:*nAU], plus the
// ASC in asc[0:*ascLen]. Returning all AUs lets both decoders replay the same
// stateful sequence from a fresh handle. Returns 0 on success.
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

// e2e_decode opens a raw-config (TT_MP4_RAW) AAC decoder with the PCM limiter
// DISABLED (so the output is the bare fixed-point decode chain, with no
// stateful peak limiter), applies the ASC, and decodes the AU sequence from a
// fresh handle, writing EVERY decoded frame's interleaved int16 PCM back to
// back into out. Returns 0 on success and sets *samplesPerCh / *channels for
// the steady frames.
static int e2e_decode(unsigned char *asc, int ascLen,
                      unsigned char *auData, int *auLens, int nAU,
                      short *out, int outCap,
                      int *samplesPerCh, int *channels) {
    HANDLE_AACDECODER h = aacDecoder_Open(TT_MP4_RAW, 1);
    if (h == NULL) return -1;
    // Disable the PCM peak limiter: keep the output the deterministic
    // fixed-point decode result (the chain this slice's pure-Go port mirrors).
    if (aacDecoder_SetParam(h, AAC_PCM_LIMITER_ENABLE, 0) != AAC_DEC_OK) {
        aacDecoder_Close(h); return -2;
    }
    UCHAR *cfg = (UCHAR *)asc; UINT clen = (UINT)ascLen;
    if (aacDecoder_ConfigRaw(h, &cfg, &clen) != AAC_DEC_OK) { aacDecoder_Close(h); return -3; }

    int sp = 0, ch = 0, off = 0, auOff = 0;
    for (int i = 0; i < nAU; i++) {
        UCHAR *in = auData + auOff; UINT size = (UINT)auLens[i]; UINT valid = (UINT)auLens[i];
        auOff += auLens[i];
        if (aacDecoder_Fill(h, &in, &size, &valid) != AAC_DEC_OK) { aacDecoder_Close(h); return -4; }
        AAC_DECODER_ERROR e = aacDecoder_DecodeFrame(h, (INT_PCM *)(out + off), outCap - off, 0);
        if (e != AAC_DEC_OK) { aacDecoder_Close(h); return -10 - (int)e; }
        CStreamInfo *si = aacDecoder_GetStreamInfo(h);
        if (si == NULL) { aacDecoder_Close(h); return -5; }
        sp = (int)si->frameSize; ch = (int)si->numChannels;
        off += sp * ch;
    }
    *samplesPerCh = sp; *channels = ch;
    aacDecoder_Close(h);
    return 0;
}
*/
import "C"

import "unsafe"

// cEncode runs the fdk encoder over framesIn frames and returns every produced
// access unit (in order) plus the ASC. The full AU sequence lets both decoders
// replay the stateful overlap-add from a fresh handle.
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

// cDecode runs the fdk decoder (limiter disabled) over the full AU sequence and
// returns all decoded frames concatenated as interleaved int16 PCM, plus the
// steady samples-per-channel and channel count.
func cDecode(asc []byte, aus [][]byte, channels, frameLen int) (pcm []int16, samplesPerCh, chans int, ok bool) {
	out := make([]int16, frameLen*channels*(len(aus)+2))

	// Flatten the AUs into one contiguous buffer with a parallel lengths array
	// (cgo forbids passing a Go slice whose elements are themselves Go pointers).
	var flat []byte
	cLens := make([]C.int, len(aus))
	for i, a := range aus {
		flat = append(flat, a...)
		cLens[i] = C.int(len(a))
	}

	var sp, ch C.int
	rc := C.e2e_decode(
		(*C.uchar)(unsafe.Pointer(&asc[0])), C.int(len(asc)),
		(*C.uchar)(unsafe.Pointer(&flat[0])), (*C.int)(unsafe.Pointer(&cLens[0])), C.int(len(aus)),
		(*C.short)(unsafe.Pointer(&out[0])), C.int(len(out)),
		&sp, &ch)
	if rc != 0 {
		return nil, 0, 0, false
	}
	// Total decoded samples == sum over frames; with steady sp/ch and a possible
	// leading priming frame, just return the buffer trimmed to what was written.
	// The caller knows the per-frame size and the frame count it fed.
	return out, int(sp), int(ch), true
}
