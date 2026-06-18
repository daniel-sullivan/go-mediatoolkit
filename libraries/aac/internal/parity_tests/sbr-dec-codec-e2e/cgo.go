// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrdece2e is the end-to-end HE-AAC v1 (AAC-LC core + SBR) decode parity
// slice. It produces real HE-AAC access units with the vendored Fraunhofer
// FDK-AAC ENCODER (AOT_SBR == 5), decodes them with BOTH the FDK cgo DECODER
// (PCM limiter disabled, so the output is the deterministic fixed-point decode
// chain) and the pure-Go internal/nativeaac/heaac decoder (AAC-LC core -> SBR
// upsample), and asserts the two int16 PCM frames are EXACTLY equal — fdk-aac SBR
// is fixed-point, so the decode is reproducible bit-for-bit.
//
// This slice compiles its OWN copy of the needed fdk C TUs (fdk_tu_*.cpp here),
// it never imports libraries/aac. It may import internal/nativeaac and
// internal/nativeaac/sbr (via the heaac glue). Build with `-tags aacfdk`.
package sbrdeccodece2e

/*
#cgo CXXFLAGS: -std=c++11 -O2 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
// The parity-local tapped copy of aacdecoder_lib.cpp resolves its relative
// "aac_ram.h"/"aacdecoder.h"/... includes from the vendored libAACdec/src dir.
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

// sbr_direct is implemented in sbr_direct_bridge.cpp (a standalone TU so the
// FDK_INLINE bitstream helpers link cleanly). It drives the genuine fdk SBR
// decoder over per-frame (core int32 input, sbr_extension_data bit location).
extern int sbr_direct(int coreRate, int outRate, int ch, int nf,
                      const int *coreInputs, const unsigned char *auFlat,
                      const int *auLens, const int *startBits, const int *countBits,
                      const int *crcFlags, const int *prevElements, int *sbrOut);


// e2e_encode_heaac opens a raw-AAC (TRANSMUX 0) HE-AAC (AOT_SBR == 5) encoder,
// encodes framesIn interleaved int16 frames one at a time, and returns EVERY
// produced access unit concatenated in au[] with per-AU byte counts in
// auLens[0:*nAU], plus the ASC in asc[0:*ascLen]. SBR signalling (the AOT_SBR
// ASC + the per-AU SBR fill element) is produced by the encoder. Returns 0 on
// success.
static int e2e_encode_heaac(int sampleRate, int channels, int bitrate,
                            short *pcm, int framesIn, int frameLen,
                            unsigned char *au, int auCap,
                            int *auLens, int *nAU,
                            unsigned char *asc, int *ascLen,
                            int *encDelay) {
    HANDLE_AACENCODER enc;
    if (aacEncOpen(&enc, 0, (UINT)channels) != AACENC_OK) return -1;
    int channelMode = (channels == 1) ? MODE_1 : MODE_2;
    if (aacEncoder_SetParam(enc, AACENC_AOT, 5) != AACENC_OK ||           // AOT_SBR
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

// fdk_core_tap_reset / fdk_core_tap_info are implemented in core_tap_bridge.cpp
// and drive the PARITY-LOCAL pre-SBR core tap inside the tapped copy of
// aacDecoder_DecodeFrame (_fdk_local_aacdecoder_lib_tapped.cpp). reset arms the
// tap with a destination buffer (planar int32 core PCM, channel c at c*frameSize,
// every frame appended); info reports how many frames were captured and the core
// geometry. The shared vendored libfdk is never modified.
extern void fdk_core_tap_reset(int *buf, long capSamples);
extern void fdk_core_tap_info(int *frames, int *frameSize, int *numCh);

// e2e_decode_heaac opens a raw-config (TT_MP4_RAW) AAC decoder with the PCM
// limiter DISABLED, applies the HE-AAC ASC, and decodes the AU sequence from a
// fresh handle, writing EVERY decoded frame's interleaved int16 PCM back to back
// into out. Returns 0 on success and sets *samplesPerCh / *channels /
// *sampleRate for the steady (SBR-upsampled) frames.
static int e2e_decode_heaac(unsigned char *asc, int ascLen,
                            unsigned char *auData, int *auLens, int nAU,
                            int wantChannels,
                            short *out, int outCap,
                            int *samplesPerCh, int *channels, int *sampleRate,
                            int *nFramesOut) {
    HANDLE_AACDECODER h = aacDecoder_Open(TT_MP4_RAW, 1);
    if (h == NULL) return -1;
    if (aacDecoder_SetParam(h, AAC_PCM_LIMITER_ENABLE, 0) != AAC_DEC_OK) {
        aacDecoder_Close(h); return -2;
    }
    // Pin the output channel count to the stream's so fdk performs no implicit
    // up/down-mix (the default would up-mix mono HE-AAC to a stereo pair).
    aacDecoder_SetParam(h, AAC_PCM_MIN_OUTPUT_CHANNELS, wantChannels);
    aacDecoder_SetParam(h, AAC_PCM_MAX_OUTPUT_CHANNELS, wantChannels);
    UCHAR *cfg = (UCHAR *)asc; UINT clen = (UINT)ascLen;
    if (aacDecoder_ConfigRaw(h, &cfg, &clen) != AAC_DEC_OK) { aacDecoder_Close(h); return -3; }

    // The fdk decoder validates the output buffer against its internal maximum
    // (up to 8 channels * 2048 samples) regardless of the actual channel count,
    // so decode each frame into a worst-case-sized scratch buffer and copy only
    // the produced samples into the caller's contiguous output.
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
        int n = sp * ch;
        if (off + n > outCap) { aacDecoder_Close(h); return -6; }
        for (int k = 0; k < n; k++) out[off + k] = (short)frameBuf[k];
        off += n;
        nf++;
    }
    *samplesPerCh = sp; *channels = ch; *sampleRate = sr; *nFramesOut = nf;
    aacDecoder_Close(h);
    return 0;
}
*/
import "C"

import "unsafe"

// cEncodeHEAAC runs the fdk HE-AAC encoder over framesIn frames and returns
// every produced access unit (in order) plus the ASC and the encoder delay.
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

// cSbrDirect drives the genuine fdk SBR decoder over per-frame core int32 input +
// SBR payload bit locations, returning int32 SBR output per frame (2048*ch).
func cSbrDirect(coreRate, outRate, ch, nf int, coreInputs []int32, auFlat []byte,
	auLens, startBits, countBits, crcFlags, prevElements []int32) ([]int32, bool) {
	sbrOut := make([]int32, nf*ch*2048)
	rc := C.sbr_direct(C.int(coreRate), C.int(outRate), C.int(ch), C.int(nf),
		(*C.int)(unsafe.Pointer(&coreInputs[0])),
		(*C.uchar)(unsafe.Pointer(&auFlat[0])), (*C.int)(unsafe.Pointer(&auLens[0])),
		(*C.int)(unsafe.Pointer(&startBits[0])), (*C.int)(unsafe.Pointer(&countBits[0])),
		(*C.int)(unsafe.Pointer(&crcFlags[0])), (*C.int)(unsafe.Pointer(&prevElements[0])),
		(*C.int)(unsafe.Pointer(&sbrOut[0])))
	return sbrOut, rc == 0
}

// cDecodeHEAAC runs the fdk HE-AAC decoder (limiter disabled) over the full AU
// sequence and returns all decoded frames concatenated as interleaved int16 PCM,
// plus the steady samples-per-channel, channel count, sample rate, and the number
// of frames actually emitted.
func cDecodeHEAAC(asc []byte, aus [][]byte, channels, outFrameLen int) (pcm []int16, samplesPerCh, chans, sampleRate, nFrames int, ok bool) {
	out := make([]int16, outFrameLen*channels*(len(aus)+8)*2)

	var flat []byte
	cLens := make([]C.int, len(aus))
	for i, a := range aus {
		flat = append(flat, a...)
		cLens[i] = C.int(len(a))
	}

	var sp, ch, sr, nf C.int
	rc := C.e2e_decode_heaac(
		(*C.uchar)(unsafe.Pointer(&asc[0])), C.int(len(asc)),
		(*C.uchar)(unsafe.Pointer(&flat[0])), (*C.int)(unsafe.Pointer(&cLens[0])), C.int(len(aus)),
		C.int(channels),
		(*C.short)(unsafe.Pointer(&out[0])), C.int(len(out)),
		&sp, &ch, &sr, &nf)
	if rc != 0 {
		return nil, 0, 0, 0, 0, false
	}
	return out, int(sp), int(ch), int(sr), int(nf), true
}

// cDecodeHEAACCore drives the GENUINE fdk full HE-AAC decoder (limiter disabled)
// over the AU sequence with the parity-local pre-SBR core tap armed, returning
// the fdk AAC-LC core PCM at the CORE rate, captured exactly as it is handed to
// sbrDecoder_Apply: planar per frame (channel c at c*coreFrameLen, int32 at
// aacOutDataHeadroom). This is the fdk-core oracle the half-rate core divergence
// is diffed against. coreFrameLen == 1024 (== outFrameLen/2). Returns one
// []int32 per captured frame plus the captured frame count and core channel
// count.
func cDecodeHEAACCore(asc []byte, aus [][]byte, channels, coreFrameLen int) (coreFrames [][]int32, nCoreFrames, coreCh int, ok bool) {
	// Worst-case capture: every AU yields at most one core frame.
	capSamples := (len(aus) + 8) * channels * coreFrameLen
	coreBuf := make([]int32, capSamples)
	C.fdk_core_tap_reset((*C.int)(unsafe.Pointer(&coreBuf[0])), C.long(capSamples))

	// Drive the full decoder; we only need its side effect (the tap), not the
	// int16 SBR output here, but reuse the same path so the core capture matches
	// the exact decode the int16 gate compares against.
	out := make([]int16, 2*coreFrameLen*channels*(len(aus)+8)*2)
	var flat []byte
	cLens := make([]C.int, len(aus))
	for i, a := range aus {
		flat = append(flat, a...)
		cLens[i] = C.int(len(a))
	}
	var sp, ch, sr, nf C.int
	rc := C.e2e_decode_heaac(
		(*C.uchar)(unsafe.Pointer(&asc[0])), C.int(len(asc)),
		(*C.uchar)(unsafe.Pointer(&flat[0])), (*C.int)(unsafe.Pointer(&cLens[0])), C.int(len(aus)),
		C.int(channels),
		(*C.short)(unsafe.Pointer(&out[0])), C.int(len(out)),
		&sp, &ch, &sr, &nf)
	if rc != 0 {
		return nil, 0, 0, false
	}

	var cFrames, cFrameSize, cNumCh C.int
	C.fdk_core_tap_info(&cFrames, &cFrameSize, &cNumCh)
	if int(cFrameSize) != coreFrameLen || int(cNumCh) != channels {
		return nil, 0, 0, false
	}
	per := channels * coreFrameLen
	frames := make([][]int32, int(cFrames))
	for f := 0; f < int(cFrames); f++ {
		fr := make([]int32, per)
		copy(fr, coreBuf[f*per:(f+1)*per])
		frames[f] = fr
	}
	return frames, int(cFrames), int(cNumCh), true
}
