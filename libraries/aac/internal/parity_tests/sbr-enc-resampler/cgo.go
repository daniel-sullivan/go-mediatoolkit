// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrencresampler pins the Go port of the SBR-encoder 2:1 time-domain
// downsampler (internal/nativeaac/sbr/enc_resampler.go) against the vendored
// Fraunhofer FDK-AAC C (resampler.cpp) via cgo. A deterministic int16 signal is
// streamed through both, block by block, and every output sample is compared
// bit-for-bit. fixed-point => EXACT int equality.
package sbrencresampler

/*
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo LDFLAGS: -lm

#include <stdint.h>

extern int resample_run(int wc, int ratio, const short *in, int nIn, int blocks,
                        short *out, int *delayOut);
*/
import "C"

import "unsafe"

func cResample(wc, ratio int, in []int16, blocks int) (out []int16, delay int) {
	outBuf := make([]int16, len(in)/ratio+blocks)
	var cDelay C.int
	n := C.resample_run(C.int(wc), C.int(ratio),
		(*C.short)(unsafe.Pointer(&in[0])), C.int(len(in)), C.int(blocks),
		(*C.short)(unsafe.Pointer(&outBuf[0])), &cDelay)
	return outBuf[:int(n)], int(cDelay)
}
