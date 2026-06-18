//go:build cgo

// Package predictors pins the Go ports of FLAC's fixed and LPC
// predictor inverses against libFLAC's reference. These functions
// drive the entire decoded sample pipeline: a single off-by-one or
// sign mismatch propagates into every audio sample, so the parity
// suite drives them across a wide range of orders, qlpCoeff vectors,
// quantisations, and residual magnitudes.
package predictors

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
#include <stdlib.h>
#include "private/fixed.h"
#include "private/lpc.h"
*/
import "C"

import "unsafe"

// cgoFixedRestore wraps FLAC__fixed_restore_signal. Caller layout
// matches the Go side: data[0..order] is the warm-up, the function
// writes into data[order..order+len(residual)].
func cgoFixedRestore(residual []int32, order uint32, data []int32) {
	dataLen := uint32(len(data)) - order
	if dataLen == 0 {
		return
	}
	C.FLAC__fixed_restore_signal(
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
		C.uint32_t(dataLen),
		C.uint32_t(order),
		(*C.FLAC__int32)(unsafe.Pointer(&data[order])),
	)
}

func cgoFixedRestoreWide(residual []int32, order uint32, data []int32) {
	dataLen := uint32(len(data)) - order
	if dataLen == 0 {
		return
	}
	C.FLAC__fixed_restore_signal_wide(
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
		C.uint32_t(dataLen),
		C.uint32_t(order),
		(*C.FLAC__int32)(unsafe.Pointer(&data[order])),
	)
}

func cgoFixedRestoreWide33(residual []int32, order uint32, data []int64) {
	dataLen := uint32(len(data)) - order
	if dataLen == 0 {
		return
	}
	C.FLAC__fixed_restore_signal_wide_33bit(
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
		C.uint32_t(dataLen),
		C.uint32_t(order),
		(*C.FLAC__int64)(unsafe.Pointer(&data[order])),
	)
}

func cgoLPCRestore(residual []int32, qlpCoeff []int32, order uint32, lpQuant int, data []int32) {
	dataLen := uint32(len(data)) - order
	if dataLen == 0 {
		return
	}
	C.FLAC__lpc_restore_signal(
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
		C.uint32_t(dataLen),
		(*C.FLAC__int32)(unsafe.Pointer(&qlpCoeff[0])),
		C.uint32_t(order),
		C.int(lpQuant),
		(*C.FLAC__int32)(unsafe.Pointer(&data[order])),
	)
}

func cgoLPCRestoreWide(residual []int32, qlpCoeff []int32, order uint32, lpQuant int, data []int32) {
	dataLen := uint32(len(data)) - order
	if dataLen == 0 {
		return
	}
	C.FLAC__lpc_restore_signal_wide(
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
		C.uint32_t(dataLen),
		(*C.FLAC__int32)(unsafe.Pointer(&qlpCoeff[0])),
		C.uint32_t(order),
		C.int(lpQuant),
		(*C.FLAC__int32)(unsafe.Pointer(&data[order])),
	)
}

func cgoLPCRestoreWide33(residual []int32, qlpCoeff []int32, order uint32, lpQuant int, data []int64) {
	dataLen := uint32(len(data)) - order
	if dataLen == 0 {
		return
	}
	C.FLAC__lpc_restore_signal_wide_33bit(
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
		C.uint32_t(dataLen),
		(*C.FLAC__int32)(unsafe.Pointer(&qlpCoeff[0])),
		C.uint32_t(order),
		C.int(lpQuant),
		(*C.FLAC__int64)(unsafe.Pointer(&data[order])),
	)
}
