//go:build cgo

// Package lpc_encode pins the Go ports of FLAC's encoder-side LPC
// analysis (FLAC__lpc_compute_autocorrelation, _compute_lp_coefficients,
// _quantize_coefficients, _compute_residual_from_qlp_coefficients[_wide])
// against libFLAC's reference. The autocorrelation + Levinson-Durbin
// steps are float64-heavy, so parity is asserted under the flac_strict
// build (bit-exact FP ordering); the quantise + residual steps are
// integer and bit-exact unconditionally.
package lpc_encode

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
#include "private/lpc.h"

// fparity_compute_lp_coefficients flattens lp_coeff[][FLAC__MAX_LPC_ORDER]
// into a contiguous row-major buffer the Go side can read back, and
// returns the (possibly reduced) max_order via *out_max_order.
static void fparity_compute_lp_coefficients(const double *autoc, uint32_t max_order,
                                            FLAC__real *lp_coeff_flat, double *error,
                                            uint32_t *out_max_order)
{
	FLAC__real lp_coeff[FLAC__MAX_LPC_ORDER][FLAC__MAX_LPC_ORDER];
	uint32_t mo = max_order;
	uint32_t i, j;
	FLAC__lpc_compute_lp_coefficients(autoc, &mo, lp_coeff, error);
	*out_max_order = mo;
	for(i = 0; i < max_order; i++)
		for(j = 0; j < FLAC__MAX_LPC_ORDER; j++)
			lp_coeff_flat[i*FLAC__MAX_LPC_ORDER + j] = lp_coeff[i][j];
}
*/
import "C"

import "unsafe"

// MaxLPCOrder mirrors FLAC__MAX_LPC_ORDER; used to size the flattened
// lp_coeff row stride shared with the C wrapper.
const MaxLPCOrder = 32

func cgoComputeAutocorrelation(data []float32, dataLen, lag uint32) []float64 {
	// FLAC__lpc_compute_autocorrelation's lag<=16 MAX_LAG path writes
	// autoc[0..MAX_LAG-1] (MAX_LAG ∈ {8,12,16}, lag rounded up to its
	// bucket) regardless of the requested lag, exactly as the real
	// encoder relies on: stream_encoder.c declares autoc[FLAC__MAX_LPC_
	// ORDER+1]. Size the buffer the same way so the C path can't run past
	// it, then return only the first `lag` entries (the meaningful output).
	autoc := make([]float64, MaxLPCOrder+1)
	C.FLAC__lpc_compute_autocorrelation(
		(*C.FLAC__real)(unsafe.Pointer(&data[0])),
		C.uint32_t(dataLen),
		C.uint32_t(lag),
		(*C.double)(unsafe.Pointer(&autoc[0])),
	)
	return autoc[:lag]
}

// cgoComputeLPCoefficients returns the flattened lp_coeff (row stride
// MaxLPCOrder), the per-order error, and the (possibly reduced) max
// order.
func cgoComputeLPCoefficients(autoc []float64, maxOrder uint32) (lpCoeffFlat []float32, errOut []float64, outMaxOrder uint32) {
	lpCoeffFlat = make([]float32, int(maxOrder)*MaxLPCOrder)
	errOut = make([]float64, maxOrder)
	var mo C.uint32_t
	C.fparity_compute_lp_coefficients(
		(*C.double)(unsafe.Pointer(&autoc[0])),
		C.uint32_t(maxOrder),
		(*C.FLAC__real)(unsafe.Pointer(&lpCoeffFlat[0])),
		(*C.double)(unsafe.Pointer(&errOut[0])),
		&mo,
	)
	return lpCoeffFlat, errOut, uint32(mo)
}

func cgoQuantizeCoefficients(lpCoeff []float32, order, precision uint32) (qlp []int32, shift int, status int) {
	qlp = make([]int32, order)
	var sh C.int
	st := C.FLAC__lpc_quantize_coefficients(
		(*C.FLAC__real)(unsafe.Pointer(&lpCoeff[0])),
		C.uint32_t(order),
		C.uint32_t(precision),
		(*C.FLAC__int32)(unsafe.Pointer(&qlp[0])),
		&sh,
	)
	return qlp, int(sh), int(st)
}

// cgoComputeResidual wraps the 32-bit accumulator path. data follows the
// same layout as the restore functions: data[0..order] is warm-up, the
// function reads it via a pointer advanced to index order.
func cgoComputeResidual(data []int32, dataLen uint32, qlpCoeff []int32, order uint32, lpQuant int) []int32 {
	residual := make([]int32, dataLen)
	if dataLen == 0 {
		return residual
	}
	C.FLAC__lpc_compute_residual_from_qlp_coefficients(
		(*C.FLAC__int32)(unsafe.Pointer(&data[order])),
		C.uint32_t(dataLen),
		(*C.FLAC__int32)(unsafe.Pointer(&qlpCoeff[0])),
		C.uint32_t(order),
		C.int(lpQuant),
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
	)
	return residual
}

func cgoComputeResidualWide(data []int32, dataLen uint32, qlpCoeff []int32, order uint32, lpQuant int) []int32 {
	residual := make([]int32, dataLen)
	if dataLen == 0 {
		return residual
	}
	C.FLAC__lpc_compute_residual_from_qlp_coefficients_wide(
		(*C.FLAC__int32)(unsafe.Pointer(&data[order])),
		C.uint32_t(dataLen),
		(*C.FLAC__int32)(unsafe.Pointer(&qlpCoeff[0])),
		C.uint32_t(order),
		C.int(lpQuant),
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
	)
	return residual
}

// cgoComputeResidualLimitResidual wraps
// FLAC__lpc_compute_residual_from_qlp_coefficients_limit_residual: the
// int64-accumulator path with overflow detection. Returns the populated
// residual and the C bool (true == every residual fit in int32). data
// follows the warm-up layout: data[0..order) is warm-up, the function is
// handed a pointer advanced to index order. residual is allocated full
// length; on a false return the C code stops writing at the offending
// index, so only entries before the failure are meaningful — the Go side
// compares the full slice but the boolean is the load-bearing assertion.
func cgoComputeResidualLimitResidual(data []int32, dataLen uint32, qlpCoeff []int32, order uint32, lpQuant int) ([]int32, bool) {
	residual := make([]int32, dataLen)
	if dataLen == 0 {
		return residual, true
	}
	ok := C.FLAC__lpc_compute_residual_from_qlp_coefficients_limit_residual(
		(*C.FLAC__int32)(unsafe.Pointer(&data[order])),
		C.uint32_t(dataLen),
		(*C.FLAC__int32)(unsafe.Pointer(&qlpCoeff[0])),
		C.uint32_t(order),
		C.int(lpQuant),
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
	)
	return residual, ok != 0
}

// cgoComputeResidualLimitResidual33Bit wraps
// FLAC__lpc_compute_residual_from_qlp_coefficients_limit_residual_33bit:
// the int64-input (33-bit side channel) variant with the same overflow
// detection. data is int64 with order warm-up samples before index 0.
func cgoComputeResidualLimitResidual33Bit(data []int64, dataLen uint32, qlpCoeff []int32, order uint32, lpQuant int) ([]int32, bool) {
	residual := make([]int32, dataLen)
	if dataLen == 0 {
		return residual, true
	}
	ok := C.FLAC__lpc_compute_residual_from_qlp_coefficients_limit_residual_33bit(
		(*C.FLAC__int64)(unsafe.Pointer(&data[order])),
		C.uint32_t(dataLen),
		(*C.FLAC__int32)(unsafe.Pointer(&qlpCoeff[0])),
		C.uint32_t(order),
		C.int(lpQuant),
		(*C.FLAC__int32)(unsafe.Pointer(&residual[0])),
	)
	return residual, ok != 0
}
