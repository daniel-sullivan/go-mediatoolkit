// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psymainmultiframe is the MULTI-FRAME psyMain carried-state parity
// slice. It drives the GENUINE vendored fdk encoder (aacEncEncode) over a
// sequence of >= 3 real-signal input frames and, after each frame, snapshots
// the encoder's inter-frame carried state directly out of the live handle:
//
//   - PSY_STATIC pre-echo carry: psyStatic[ch]->sfbThresholdnm1[],
//     mdctScalenm1, calcPreEcho (psy_data.h:147-149) and the carried
//     block-switch window-sequence (block_switch.h).
//   - adj_thr ATS_ELEMENT carry: peLast, dynBitsLast, peCorrectionFactor_m/_e,
//     chaosMeasureOld (adj_thr_data.h:153-160).
//   - the qcKernel bit reservoir: bitResTot (qc_data.h:280).
//   - the element rate-control result: peData.pe, grantedDynBits, grantedPe
//     (qc_data.h) — the bits/PE that drive the next frame's threshold adaptation.
//
// The pure-Go nativeaac encoder is driven over the SAME frames (it applies the
// same one-frame block-switch input delay the genuine encoder does, so the two
// are frame-aligned) and its EncoderStateDump is compared field-for-field
// against the genuine snapshot after each frame. The test localizes the FIRST
// carried field that diverges, isolating a multi-frame encode bug the
// single-frame component slices cannot catch.
//
// This slice compiles its OWN copy of the needed fdk encoder TUs (the
// fdk_tu_AACenc_*/fdk_tu_FDK_*/... amalgamation split). It NEVER imports
// libraries/aac (that would link a second copy of the whole reference); it MAY,
// and does, import internal/nativeaac for the encoder under test. The genuine
// symbol driven is the real vendored aacEncEncode, reading the real
// psyKernel/qcKernel state it carries. Build with `-tags aacfdk`.
package psymainmultiframe

/*
// Only -I / -D / -Wno-* belong in-source; the scalar FP flags come from the
// mise task env (CGO_CFLAGS). The encode path is fixed-point integer
// arithmetic, so that flag set is belt-and-suspenders.
#cgo CXXFLAGS: -std=c++11 -O2 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
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

// Per-channel + per-element carried state mirror (see bridge.cpp for the field
// reads out of the live encoder handle). MAX_SFB == 51.
typedef struct {
    int   sfbThresholdNm1[51];
    int   mdctScaleNm1;
    int   calcPreEcho;
    int   lastWindowSequence;
    int   windowShape;
    int   lastWindowShape;
    int   noOfGroups;
    int   peLast;
    int   dynBitsLast;
    int   peCorrectionFactorM;
    int   peCorrectionFactorE;
    int   chaosMeasureOld;
    int   mdctScale;
} mf_chan_state;

typedef struct {
    int maxSfbPerGroup;
    int sfbCnt;
    int sfbPerGroup;
    int sfbThresholdLdData[60];
    int sfbEnergyLdData[60];
    int isBook[60];
} mf_psyout_chan;

typedef struct {
    mf_chan_state ch[2];
    int           bitResTot;
    int           pe;
    int           constPart;
    int           nActiveLines;
    int           grantedDynBits;
    int           grantedPe;
    mf_psyout_chan psyOut[2];
    int            msDigest;
    int            msMask[60];
} mf_frame_state;

// mf_encode opens a raw-AAC AAC-LC CBR encoder, encodes framesIn interleaved
// int16 frames one at a time, and after EACH frame writes the carried encoder
// state into states[f]. Defined in bridge.cpp (needs the internal headers).
int mf_encode(int sampleRate, int channels, int bitrate,
              short *pcm, int framesIn, int frameLen,
              mf_frame_state *states);
*/
import "C"

import "unsafe"

// chanState mirrors C mf_chan_state.
type chanState struct {
	SfbThresholdNm1     [51]int32
	MdctScaleNm1        int
	CalcPreEcho         int
	LastWindowSequence  int
	WindowShape         int
	LastWindowShape     int
	NoOfGroups          int
	PeLast              int
	DynBitsLast         int
	PeCorrectionFactorM int32
	PeCorrectionFactorE int
	ChaosMeasureOld     int32
	MdctScale           int
}

// psyOutChan mirrors C mf_psyout_chan: the POST-psyMain per-SFB outputs that
// feed peData.pe (ld-domain threshold/energy + post-IS intensity book).
type psyOutChan struct {
	MaxSfbPerGroup     int
	SfbCnt             int
	SfbPerGroup        int
	SfbThresholdLdData [60]int32
	SfbEnergyLdData    [60]int32
	IsBook             [60]int32
}

// frameState mirrors C mf_frame_state.
type frameState struct {
	Ch             [2]chanState
	BitResTot      int
	Pe             int
	ConstPart      int
	NActiveLines   int
	GrantedDynBits int
	GrantedPe      int
	PsyOut         [2]psyOutChan
	MsDigest       int
	MsMask         [60]int32
}

// cEncodeStates runs the genuine fdk encoder over framesIn frames and returns
// the per-frame carried state snapshot taken after each frame.
func cEncodeStates(sampleRate, channels, bitrate, frameLen int, pcm []int16) ([]frameState, bool) {
	framesIn := len(pcm) / (frameLen * channels)
	cStates := make([]C.mf_frame_state, framesIn)

	rc := C.mf_encode(C.int(sampleRate), C.int(channels), C.int(bitrate),
		(*C.short)(unsafe.Pointer(&pcm[0])), C.int(framesIn), C.int(frameLen),
		(*C.mf_frame_state)(unsafe.Pointer(&cStates[0])))
	if rc != 0 {
		return nil, false
	}

	out := make([]frameState, framesIn)
	for f := 0; f < framesIn; f++ {
		cs := &cStates[f]
		out[f].BitResTot = int(cs.bitResTot)
		out[f].Pe = int(cs.pe)
		out[f].ConstPart = int(cs.constPart)
		out[f].NActiveLines = int(cs.nActiveLines)
		out[f].GrantedDynBits = int(cs.grantedDynBits)
		out[f].GrantedPe = int(cs.grantedPe)
		for ch := 0; ch < channels; ch++ {
			cc := &cs.ch[ch]
			gc := &out[f].Ch[ch]
			for i := 0; i < 51; i++ {
				gc.SfbThresholdNm1[i] = int32(cc.sfbThresholdNm1[i])
			}
			gc.MdctScaleNm1 = int(cc.mdctScaleNm1)
			gc.CalcPreEcho = int(cc.calcPreEcho)
			gc.LastWindowSequence = int(cc.lastWindowSequence)
			gc.WindowShape = int(cc.windowShape)
			gc.LastWindowShape = int(cc.lastWindowShape)
			gc.NoOfGroups = int(cc.noOfGroups)
			gc.PeLast = int(cc.peLast)
			gc.DynBitsLast = int(cc.dynBitsLast)
			gc.PeCorrectionFactorM = int32(cc.peCorrectionFactorM)
			gc.PeCorrectionFactorE = int(cc.peCorrectionFactorE)
			gc.ChaosMeasureOld = int32(cc.chaosMeasureOld)
			gc.MdctScale = int(cc.mdctScale)
		}
		out[f].MsDigest = int(cs.msDigest)
		for i := 0; i < 60; i++ {
			out[f].MsMask[i] = int32(cs.msMask[i])
		}
		for ch := 0; ch < channels; ch++ {
			cp := &cs.psyOut[ch]
			gp := &out[f].PsyOut[ch]
			gp.MaxSfbPerGroup = int(cp.maxSfbPerGroup)
			gp.SfbCnt = int(cp.sfbCnt)
			gp.SfbPerGroup = int(cp.sfbPerGroup)
			for i := 0; i < 60; i++ {
				gp.SfbThresholdLdData[i] = int32(cp.sfbThresholdLdData[i])
				gp.SfbEnergyLdData[i] = int32(cp.sfbEnergyLdData[i])
				gp.IsBook[i] = int32(cp.isBook[i])
			}
		}
	}
	return out, true
}
