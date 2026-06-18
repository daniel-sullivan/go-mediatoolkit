// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psdece2e is the end-to-end HE-AAC v2 (AAC-LC core + SBR + parametric
// stereo) DECODE parity slice. It produces real HE-AAC v2 access units with the
// vendored Fraunhofer FDK-AAC ENCODER at AOT_PS (29) over a MONO PCM input,
// decodes them with BOTH the genuine FDK cgo full DECODER (which performs the PS
// mono->stereo upmix internally, PCM limiter disabled) and the pure-Go
// internal/nativeaac/heaac PS decoder (AAC-LC mono core -> SBR -> PS upmix), and
// asserts the two int16 STEREO PCM frames are EXACTLY equal after removing fdk's
// one-frame full-decode output delay — fdk-aac SBR/PS is fixed-point, so the
// decode is reproducible bit-for-bit.
//
// This slice compiles its OWN copy of the needed fdk C TUs (fdk_tu_*.cpp here),
// it never imports libraries/aac. It may import internal/nativeaac and
// internal/nativeaac/sbr (via the heaac glue). Build with `-tags aacfdk`.
package psdece2e

/*
#cgo CXXFLAGS: -std=c++11 -O2 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/src
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

// sbr_direct_ps is implemented in sbr_direct_ps_bridge.cpp (a standalone TU so the
// FDK_INLINE bitstream helpers link cleanly). It drives the genuine fdk SBR+PS
// decoder over per-frame mono core int32 input + sbr_extension_data bit location,
// returning int32 STEREO output per frame (frame-immediate, no decoder delay).
extern int sbr_direct_ps(int coreRate, int outRate, int nf, const int *coreInputs,
                         const unsigned char *auFlat, const int *auLens,
                         const int *startBits, const int *countBits, const int *crcFlags,
                         const int *prevElements, int *sbrOut);

// ps_encode_heaac opens a raw-AAC (TRANSMUX 0) HE-AAC v2 (AOT_PS == 29) encoder
// over a STEREO input (PS is a stereo tool: the encoder downmixes the stereo
// input to a MONO AAC-LC core and codes the spatial image as ps_data in the SBR
// extension — GetCoreChannelMode maps MODE_2 -> MODE_1 core for AOT_PS,
// aacenc_lib.cpp:392-400). It encodes framesIn interleaved-stereo int16 frames one
// at a time and returns EVERY produced access unit concatenated in au[] with
// per-AU byte counts in auLens[0:*nAU], plus the AOT_PS ASC in asc[0:*ascLen].
// Returns 0 on success.
static int ps_encode_heaac(int sampleRate, int bitrate,
                           short *pcm, int framesIn, int frameLen,
                           unsigned char *au, int auCap,
                           int *auLens, int *nAU,
                           unsigned char *asc, int *ascLen,
                           int *encDelay) {
    HANDLE_AACENCODER enc;
    // AOT_PS takes a 2-channel (stereo) input and produces a mono core + PS.
    if (aacEncOpen(&enc, 0, 2) != AACENC_OK) return -1;
    if (aacEncoder_SetParam(enc, AACENC_AOT, 29) != AACENC_OK ||           // AOT_PS
        aacEncoder_SetParam(enc, AACENC_SAMPLERATE, (UINT)sampleRate) != AACENC_OK ||
        aacEncoder_SetParam(enc, AACENC_CHANNELMODE, MODE_2) != AACENC_OK ||
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

    int per = frameLen * 2; // stereo interleaved
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

// ps_decode_heaac opens a raw-config (TT_MP4_RAW) AAC decoder with the PCM limiter
// DISABLED, applies the HE-AAC v2 ASC, and decodes the AU sequence from a fresh
// handle, writing EVERY decoded frame's interleaved int16 PCM back to back into
// out. PS upmix is performed internally by the fdk full decoder, so the output is
// STEREO. The output channel count is pinned to 2 so fdk does no implicit
// up/down-mix. Returns 0 on success and sets *samplesPerCh / *channels /
// *sampleRate for the steady frames.
static int ps_decode_heaac_ch(unsigned char *asc, int ascLen,
                           unsigned char *auData, int *auLens, int nAU, int wantCh,
                           short *out, int outCap,
                           int *samplesPerCh, int *channels, int *sampleRate,
                           int *nFramesOut) {
    HANDLE_AACDECODER h = aacDecoder_Open(TT_MP4_RAW, 1);
    if (h == NULL) return -1;
    if (aacDecoder_SetParam(h, AAC_PCM_LIMITER_ENABLE, 0) != AAC_DEC_OK) {
        aacDecoder_Close(h); return -2;
    }
    // Pin the output channel count: 2 keeps the PS upmix, 1 disables PS (mono
    // core + SBR upsampled) so the core/SBR path can be isolated.
    aacDecoder_SetParam(h, AAC_PCM_MIN_OUTPUT_CHANNELS, wantCh);
    aacDecoder_SetParam(h, AAC_PCM_MAX_OUTPUT_CHANNELS, wantCh);
    UCHAR *cfg = (UCHAR *)asc; UINT clen = (UINT)ascLen;
    if (aacDecoder_ConfigRaw(h, &cfg, &clen) != AAC_DEC_OK) { aacDecoder_Close(h); return -3; }

    static const int kMaxFrame = 8 * 2048;
    INT_PCM frameBuf[kMaxFrame];
    int sp = 0, ch = 0, sr = 0, off = 0, auOff = 0, nf = 0;
    for (int i = 0; i < nAU; i++) {
        UCHAR *in = auData + auOff; UINT size = (UINT)auLens[i]; UINT valid = (UINT)auLens[i];
        auOff += auLens[i];
        if (aacDecoder_Fill(h, &in, &size, &valid) != AAC_DEC_OK) { aacDecoder_Close(h); return -4; }
        AAC_DECODER_ERROR e = aacDecoder_DecodeFrame(h, frameBuf, kMaxFrame, 0);
        if (e == AAC_DEC_NOT_ENOUGH_BITS) continue;
        if (e != AAC_DEC_OK) { aacDecoder_Close(h); return -10 - (int)e; }
        CStreamInfo *si = aacDecoder_GetStreamInfo(h);
        if (si == NULL) { aacDecoder_Close(h); return -5; }
        sp = (int)si->frameSize; ch = (int)si->numChannels; sr = (int)si->sampleRate;
        int nn = sp * ch;
        if (off + nn > outCap) { aacDecoder_Close(h); return -6; }
        for (int k = 0; k < nn; k++) out[off + k] = (short)frameBuf[k];
        off += nn;
        nf++;
    }
    *samplesPerCh = sp; *channels = ch; *sampleRate = sr; *nFramesOut = nf;
    aacDecoder_Close(h);
    return 0;
}

// ps_decode_heaac is the stereo (PS-on) decode used by the parity gate.
static int ps_decode_heaac(unsigned char *asc, int ascLen,
                           unsigned char *auData, int *auLens, int nAU,
                           short *out, int outCap,
                           int *samplesPerCh, int *channels, int *sampleRate,
                           int *nFramesOut) {
    return ps_decode_heaac_ch(asc, ascLen, auData, auLens, nAU, 2, out, outCap,
                              samplesPerCh, channels, sampleRate, nFramesOut);
}
*/
import "C"

import "unsafe"

// cEncodePSHEAAC runs the fdk HE-AAC v2 (AOT_PS) encoder over framesIn frames of
// interleaved-STEREO int16 PCM (frameLen samples per channel) and returns every
// produced access unit (in order) plus the AOT_PS ASC and the encoder delay.
func cEncodePSHEAAC(sampleRate, bitrate, frameLen int, pcm []int16) (aus [][]byte, asc []byte, encDelay int, ok bool) {
	framesIn := len(pcm) / (frameLen * 2)

	auBuf := make([]byte, framesIn*8192)
	auLens := make([]C.int, framesIn)
	var nAU C.int
	ascBuf := make([]byte, 64)
	ascLen := C.int(len(ascBuf))
	var cDelay C.int

	rc := C.ps_encode_heaac(C.int(sampleRate), C.int(bitrate),
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

// cDecodePSHEAAC runs the genuine fdk HE-AAC v2 full decoder (limiter disabled,
// stereo-pinned) over the AU sequence and returns all decoded frames concatenated
// as interleaved int16 STEREO PCM, plus the steady samples-per-channel, channel
// count, sample rate, and the number of frames actually emitted. The PS upmix is
// performed inside the fdk decoder.
func cDecodePSHEAAC(asc []byte, aus [][]byte, outFrameLen int) (pcm []int16, samplesPerCh, chans, sampleRate, nFrames int, ok bool) {
	out := make([]int16, outFrameLen*2*(len(aus)+8)*2)

	var flat []byte
	cLens := make([]C.int, len(aus))
	for i, a := range aus {
		flat = append(flat, a...)
		cLens[i] = C.int(len(a))
	}

	var sp, ch, sr, nf C.int
	rc := C.ps_decode_heaac(
		(*C.uchar)(unsafe.Pointer(&asc[0])), C.int(len(asc)),
		(*C.uchar)(unsafe.Pointer(&flat[0])), (*C.int)(unsafe.Pointer(&cLens[0])), C.int(len(aus)),
		(*C.short)(unsafe.Pointer(&out[0])), C.int(len(out)),
		&sp, &ch, &sr, &nf)
	if rc != 0 {
		return nil, 0, 0, 0, 0, false
	}
	return out, int(sp), int(ch), int(sr), int(nf), true
}

// cDecodeMonoHEAAC decodes the AOT_PS stream with PS DISABLED (output pinned to
// mono), isolating fdk's mono AAC-LC core + SBR upsample path. The gate uses it to
// prove the startup transient at frames 3-4 is the inherited fdk full-decoder
// state-seeding effect (present in the unchanged mono v1 path too), not the PS
// integration.
func cDecodeMonoHEAAC(asc []byte, aus [][]byte, outFrameLen int) (pcm []int16, samplesPerCh, chans, sampleRate, nFrames int, ok bool) {
	out := make([]int16, outFrameLen*(len(aus)+8)*2)
	var flat []byte
	cLens := make([]C.int, len(aus))
	for i, a := range aus {
		flat = append(flat, a...)
		cLens[i] = C.int(len(a))
	}
	var sp, ch, sr, nf C.int
	rc := C.ps_decode_heaac_ch(
		(*C.uchar)(unsafe.Pointer(&asc[0])), C.int(len(asc)),
		(*C.uchar)(unsafe.Pointer(&flat[0])), (*C.int)(unsafe.Pointer(&cLens[0])), C.int(len(aus)), 1,
		(*C.short)(unsafe.Pointer(&out[0])), C.int(len(out)),
		&sp, &ch, &sr, &nf)
	if rc != 0 {
		return nil, 0, 0, 0, 0, false
	}
	return out, int(sp), int(ch), int(sr), int(nf), true
}

// cSbrDirectPS drives the genuine fdk SBR+PS decoder frame-immediate over per-frame
// mono core int32 input + SBR payload bit locations, returning int32 STEREO output
// per frame (2*2048). This is the int32 PS oracle (pre-int16-narrowing).
func cSbrDirectPS(coreRate, outRate, nf int, coreInputs []int32, auFlat []byte,
	auLens, startBits, countBits, crcFlags, prevElements []int32) ([]int32, bool) {
	sbrOut := make([]int32, nf*2*2048)
	rc := C.sbr_direct_ps(C.int(coreRate), C.int(outRate), C.int(nf),
		(*C.int)(unsafe.Pointer(&coreInputs[0])),
		(*C.uchar)(unsafe.Pointer(&auFlat[0])), (*C.int)(unsafe.Pointer(&auLens[0])),
		(*C.int)(unsafe.Pointer(&startBits[0])), (*C.int)(unsafe.Pointer(&countBits[0])),
		(*C.int)(unsafe.Pointer(&crcFlags[0])), (*C.int)(unsafe.Pointer(&prevElements[0])),
		(*C.int)(unsafe.Pointer(&sbrOut[0])))
	return sbrOut, rc == 0
}
