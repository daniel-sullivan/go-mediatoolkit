//go:build cgo

// Package fvad_core is the parity slice for libfvad's core VAD
// (vad_core.c): InitCore, set_mode_core, and the CalcVad8/16/32/48khz
// paths — GMM hypothesis test, model adaptation, overhang smoothing and
// the internal downsampling chains — checked bit-for-bit against the
// vendored C oracle with a COMPLETE VadInstT state snapshot compared
// after every processed frame. Pure integer — exact equality, no strict
// tag, no CGO_CFLAGS (see the fvad_sp slice doc for the shared slice
// conventions).
package fvad_core

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libfvad/src

#include <stdint.h>
#include <stddef.h>
#include "vad/vad_core.h"
*/
import "C"

import "unsafe"

// cCoreInst wraps a C VadInstT driven through the real InitCore /
// set_mode_core / CalcVad entry points.
type cCoreInst struct {
	inst C.VadInstT
}

func newCCoreInst() *cCoreInst {
	h := new(cCoreInst)
	if C.WebRtcVad_InitCore(&h.inst) != 0 {
		panic("WebRtcVad_InitCore failed")
	}
	return h
}

func (h *cCoreInst) setMode(mode int) int {
	return int(C.WebRtcVad_set_mode_core(&h.inst, C.int(mode)))
}

func (h *cCoreInst) calcVad(rate int, frame []int16) int {
	p := (*C.int16_t)(unsafe.Pointer(&frame[0]))
	n := C.size_t(len(frame))
	switch rate {
	case 8000:
		return int(C.WebRtcVad_CalcVad8khz(&h.inst, p, n))
	case 16000:
		return int(C.WebRtcVad_CalcVad16khz(&h.inst, p, n))
	case 32000:
		return int(C.WebRtcVad_CalcVad32khz(&h.inst, p, n))
	case 48000:
		return int(C.WebRtcVad_CalcVad48khz(&h.inst, p, n))
	}
	panic("bad rate")
}

// cCoreSnapshot is a complete copy of the C VadInstT state, in Go
// types, for exact comparison against the Go port's Inst.
type cCoreSnapshot struct {
	Vad                      int
	DownsamplingFilterStates [4]int32
	S48To24                  [8]int32
	S24To24                  [16]int32
	S24To16                  [8]int32
	S16To8                   [8]int32
	NoiseMeans               [12]int16
	SpeechMeans              [12]int16
	NoiseStds                [12]int16
	SpeechStds               [12]int16
	FrameCounter             int32
	OverHang                 int16
	NumOfSpeech              int16
	IndexVector              [96]int16
	LowValueVector           [96]int16
	MeanValue                [6]int16
	UpperState               [5]int16
	LowerState               [5]int16
	HpFilterState            [4]int16
	OverHangMax1             [3]int16
	OverHangMax2             [3]int16
	Individual               [3]int16
	Total                    [3]int16
	FeatureVector            [6]int16
	TotalPower               int16
}

func copy16(dst []int16, src []C.int16_t) {
	for i := range src {
		dst[i] = int16(src[i])
	}
}

func copy32(dst []int32, src []C.int32_t) {
	for i := range src {
		dst[i] = int32(src[i])
	}
}

func (h *cCoreInst) snapshot() cCoreSnapshot {
	var s cCoreSnapshot
	s.Vad = int(h.inst.vad)
	copy32(s.DownsamplingFilterStates[:], h.inst.downsampling_filter_states[:])
	copy32(s.S48To24[:], h.inst.state_48_to_8.S_48_24[:])
	copy32(s.S24To24[:], h.inst.state_48_to_8.S_24_24[:])
	copy32(s.S24To16[:], h.inst.state_48_to_8.S_24_16[:])
	copy32(s.S16To8[:], h.inst.state_48_to_8.S_16_8[:])
	copy16(s.NoiseMeans[:], h.inst.noise_means[:])
	copy16(s.SpeechMeans[:], h.inst.speech_means[:])
	copy16(s.NoiseStds[:], h.inst.noise_stds[:])
	copy16(s.SpeechStds[:], h.inst.speech_stds[:])
	s.FrameCounter = int32(h.inst.frame_counter)
	s.OverHang = int16(h.inst.over_hang)
	s.NumOfSpeech = int16(h.inst.num_of_speech)
	copy16(s.IndexVector[:], h.inst.index_vector[:])
	copy16(s.LowValueVector[:], h.inst.low_value_vector[:])
	copy16(s.MeanValue[:], h.inst.mean_value[:])
	copy16(s.UpperState[:], h.inst.upper_state[:])
	copy16(s.LowerState[:], h.inst.lower_state[:])
	copy16(s.HpFilterState[:], h.inst.hp_filter_state[:])
	copy16(s.OverHangMax1[:], h.inst.over_hang_max_1[:])
	copy16(s.OverHangMax2[:], h.inst.over_hang_max_2[:])
	copy16(s.Individual[:], h.inst.individual[:])
	copy16(s.Total[:], h.inst.total[:])
	copy16(s.FeatureVector[:], h.inst.feature_vector[:])
	s.TotalPower = int16(h.inst.total_power)
	return s
}
