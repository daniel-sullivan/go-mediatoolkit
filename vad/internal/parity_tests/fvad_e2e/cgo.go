//go:build cgo

// Package fvad_e2e is the end-to-end parity slice for the public
// libfvad API (fvad.c): fvad_new / fvad_reset / fvad_set_mode /
// fvad_set_sample_rate / fvad_process against the Go port's New /
// Reset / SetMode / SetSampleRate / Process, frame-by-frame over the
// full {8,16,32,48} kHz × {10,20,30} ms × modes 0–3 matrix on ≥ 60 s
// streams, plus mid-stream reset and error-return parity. Pure integer
// — exact equality, no strict tag, no CGO_CFLAGS (see the fvad_sp slice
// doc for the shared slice conventions).
package fvad_e2e

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libfvad/include

#include <stdint.h>
#include <stddef.h>
#include "fvad.h"
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// cVAD wraps a heap-allocated C Fvad instance.
type cVAD struct {
	inst *C.Fvad
}

func newCVAD() *cVAD {
	v := &cVAD{inst: C.fvad_new()}
	if v.inst == nil {
		panic("fvad_new failed")
	}
	runtime.AddCleanup(v, func(p *C.Fvad) { C.fvad_free(p) }, v.inst)
	return v
}

func (v *cVAD) reset() { C.fvad_reset(v.inst) }

func (v *cVAD) setMode(mode int) int {
	return int(C.fvad_set_mode(v.inst, C.int(mode)))
}

func (v *cVAD) setSampleRate(rate int) int {
	return int(C.fvad_set_sample_rate(v.inst, C.int(rate)))
}

// process returns the raw C decision: 1 voice, 0 no voice, -1 invalid
// frame length.
func (v *cVAD) process(frame []int16) int {
	return int(C.fvad_process(v.inst, (*C.int16_t)(unsafe.Pointer(&frame[0])), C.size_t(len(frame))))
}
