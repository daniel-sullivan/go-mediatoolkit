//go:build cgo

// Package fvad_filterbank is the parity slice for libfvad's feature
// extraction (vad_filterbank.c): the six band log-energies, the
// approximate total energy, and the carried split/highpass filter
// states, checked bit-for-bit per frame against the vendored C oracle.
// Pure integer — exact equality, no strict tag, no CGO_CFLAGS (see the
// fvad_sp slice doc for the shared slice conventions).
package fvad_filterbank

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libfvad/src

#include <stdint.h>
#include <stddef.h>
#include "vad/vad_filterbank.h"
*/
import "C"

import "unsafe"

// cFilterbankInst wraps a C VadInstT with the fields
// WebRtcVad_CalculateFeatures touches initialized the way
// WebRtcVad_InitCore leaves them (all filter states zero).
type cFilterbankInst struct {
	inst C.VadInstT
}

func newCFilterbankInst() *cFilterbankInst {
	h := new(cFilterbankInst)
	for i := range h.inst.upper_state {
		h.inst.upper_state[i] = 0
		h.inst.lower_state[i] = 0
	}
	for i := range h.inst.hp_filter_state {
		h.inst.hp_filter_state[i] = 0
	}
	return h
}

// calculateFeatures runs the C oracle over one frame, returning the six
// Q4 band energies and the total-energy indicator.
func (h *cFilterbankInst) calculateFeatures(dataIn []int16) (features [6]int16, totalEnergy int16) {
	var cFeatures [6]C.int16_t
	total := C.WebRtcVad_CalculateFeatures(&h.inst,
		(*C.int16_t)(unsafe.Pointer(&dataIn[0])), C.size_t(len(dataIn)),
		&cFeatures[0])
	for i := 0; i < 6; i++ {
		features[i] = int16(cFeatures[i])
	}
	return features, int16(total)
}

// snapshot copies the carried filter states.
func (h *cFilterbankInst) snapshot() (upper, lower [5]int16, hp [4]int16) {
	for i := 0; i < 5; i++ {
		upper[i] = int16(h.inst.upper_state[i])
		lower[i] = int16(h.inst.lower_state[i])
	}
	for i := 0; i < 4; i++ {
		hp[i] = int16(h.inst.hp_filter_state[i])
	}
	return
}
