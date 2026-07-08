//go:build cgo

// Package fvad_gmm is the parity slice for libfvad's Gaussian
// probability kernel (vad_gmm.c): the Q20 probability and the Q11
// model-update delta, checked bit-for-bit over dense Q-format grids
// against the vendored C oracle. Pure integer — exact equality, no
// strict tag, no CGO_CFLAGS (see the fvad_sp slice doc for the shared
// slice conventions).
package fvad_gmm

/*
#cgo CFLAGS: -I${SRCDIR}/../../../libfvad/src

#include <stdint.h>
#include "vad/vad_gmm.h"
*/
import "C"

func cGaussianProbability(input, mean, std int16) (int32, int16) {
	var delta C.int16_t
	prob := C.WebRtcVad_GaussianProbability(C.int16_t(input), C.int16_t(mean),
		C.int16_t(std), &delta)
	return int32(prob), int16(delta)
}
