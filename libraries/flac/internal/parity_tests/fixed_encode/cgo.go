//go:build cgo

// Package fixed_encode pins the Go ports of FLAC's encoder-side fixed
// predictor order selection (FLAC__fixed_compute_best_predictor and
// FLAC__fixed_compute_best_predictor_wide) against libFLAC's reference.
// These functions choose the fixed order 0..4 for every fixed subframe
// the encoder emits and produce the residual-bits-per-sample estimates
// that gate the fixed-vs-LPC decision, so both the chosen order and the
// bits[] estimate array must be bit/exact-float identical to libFLAC.
package fixed_encode

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libflac
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/include
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/src/libFLAC/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable -Wno-static-in-inline

#ifdef HAVE_CONFIG_H
#  include <config.h>
#endif
#include <stdint.h>
#include "private/fixed.h"

// fparity_best_predictor{,_wide} advance the base pointer past the 4
// history samples on the C side. Doing the +4 in C avoids Go's -d=checkptr
// rejecting the equivalent pointer arithmetic: when dataLen==0 the Go slice
// has length exactly 4, so &data[4] (or unsafe.Add to that offset) is a
// one-past-the-end pointer that checkptr flags as "invalid allocation",
// even though C only reads data[-4..-1] (history) and data[0..dataLen-1].
static inline uint32_t fparity_best_predictor(const FLAC__int32 *base, uint32_t data_len, float bits[5]) {
	return FLAC__fixed_compute_best_predictor(base + 4, data_len, bits);
}
static inline uint32_t fparity_best_predictor_wide(const FLAC__int32 *base, uint32_t data_len, float bits[5]) {
	return FLAC__fixed_compute_best_predictor_wide(base + 4, data_len, bits);
}
*/
import "C"

import "unsafe"

// cgoBestPredictor wraps FLAC__fixed_compute_best_predictor. `data`
// holds FLAC__MAX_FIXED_ORDER (=4) history samples followed by dataLen
// signal samples; libFLAC is called with the pointer advanced past the
// history, matching the encoder call site (stream_encoder.c:4088). The
// +4 advance happens in the C wrapper (see fparity_best_predictor).
func cgoBestPredictor(data []int32, dataLen uint32) (uint32, [5]float32) {
	var bits [5]C.float
	order := C.fparity_best_predictor(
		(*C.FLAC__int32)(unsafe.Pointer(&data[0])),
		C.uint32_t(dataLen),
		&bits[0],
	)
	var out [5]float32
	for i := range out {
		out[i] = float32(bits[i])
	}
	return uint32(order), out
}

// cgoBestPredictorWide wraps FLAC__fixed_compute_best_predictor_wide.
func cgoBestPredictorWide(data []int32, dataLen uint32) (uint32, [5]float32) {
	var bits [5]C.float
	order := C.fparity_best_predictor_wide(
		(*C.FLAC__int32)(unsafe.Pointer(&data[0])),
		C.uint32_t(dataLen),
		&bits[0],
	)
	var out [5]float32
	for i := range out {
		out[i] = float32(bits[i])
	}
	return uint32(order), out
}
