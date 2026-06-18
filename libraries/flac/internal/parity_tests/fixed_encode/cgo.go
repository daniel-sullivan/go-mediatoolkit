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

#include <stdint.h>
#include "private/fixed.h"
*/
import "C"

import "unsafe"

// cgoBestPredictor wraps FLAC__fixed_compute_best_predictor. `data`
// holds FLAC__MAX_FIXED_ORDER (=4) history samples followed by dataLen
// signal samples; libFLAC is called with the pointer advanced past the
// history, matching the encoder call site (stream_encoder.c:4088).
func cgoBestPredictor(data []int32, dataLen uint32) (uint32, [5]float32) {
	var bits [5]C.float
	// Advance the base pointer past the 4 history samples via pointer
	// arithmetic rather than &data[4]: when dataLen==0 the slice has
	// exactly length 4, so &data[4] would panic (one-past-the-end index),
	// yet C only reads data[-4..-1] (history) and data[0..dataLen-1].
	base := unsafe.Pointer(&data[0])
	adv := unsafe.Pointer(uintptr(base) + 4*unsafe.Sizeof(data[0]))
	order := C.FLAC__fixed_compute_best_predictor(
		(*C.FLAC__int32)(adv),
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
	base := unsafe.Pointer(&data[0])
	adv := unsafe.Pointer(uintptr(base) + 4*unsafe.Sizeof(data[0]))
	order := C.FLAC__fixed_compute_best_predictor_wide(
		(*C.FLAC__int32)(adv),
		C.uint32_t(dataLen),
		&bits[0],
	)
	var out [5]float32
	for i := range out {
		out[i] = float32(bits[i])
	}
	return uint32(order), out
}
