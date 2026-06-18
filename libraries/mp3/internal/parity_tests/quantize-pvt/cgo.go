// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

// Package quantizepvt holds the quantize-pvt parity slice: it pins the pure-Go
// nativemp3 port of LAME 3.100's quantizer-support floating-point kernels — the
// ATH shaping (athAdjust quantize_pvt.c:555, ATHmdct :211, compute_ath :231),
// the per-band allowed-distortion budget calc_xmin (:590) and the
// quantization-noise measure calc_noise (:816) / calc_noise_core_c (:751) —
// against the vendored C LAME reference compiled inline via cgo.
//
// Per the parity discipline in CONTRIBUTING.md this
// package compiles its OWN copy of the C reference (oracle.c, which #includes
// the committed libmp3lame/quantize_pvt.c + util.c) so each go-test binary is
// symbol-self-contained, and it NEVER imports libraries/mp3 (which would
// duplicate the LAME symbols at link time) — only the pure-Go
// internal/nativemp3 port.
//
// ATHmdct / compute_ath / calc_noise_core_c are file-static in quantize_pvt.c;
// oracle.c re-exports them through thin oracle_* trampolines in the same
// translation unit so the C side of every assertion is the genuine vendored
// code (see oracle.h). athAdjust / calc_xmin / calc_noise are public and called
// directly through the trampolines.
//
// This slice IS floating-point-bearing: every energy sum / masking ratio / ATH
// product is a separately rounded float term, so the result is only bit-exact
// under the mp3_strict build (FMA-free Go) against the -ffp-contract=off cgo
// oracle. The strict gate lives in parity_test.go.
//
// Build tags: gated by `mp3lame` (in addition to `cgo`) because quantize_pvt.c /
// util.c are LGPL LAME source and the Go port slice it pins is itself
// mp3lame-gated; the canonical strict run is `-tags=mp3_strict,mp3lame` with the
// FP CGO env (the //libraries/mp3:parity-lame mise task).
package quantizepvt

/*
#cgo CFLAGS: -I${SRCDIR}/../../../liblame
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/libmp3lame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/mpglib
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/include
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable
#cgo CFLAGS: -Wno-shift-negative-value -Wno-absolute-value -Wno-tautological-pointer-compare
#cgo CFLAGS: -Wno-missing-field-initializers -Wno-parentheses

#include "oracle.h"
*/
import "C"

// cints converts an int slice to a fresh []C.int (a C-owned copy), so passing
// &out[0] to a C trampoline satisfies cgo's pointer rules. cfloats / goFloats
// are the float32 analogs.
func cints(xs []int) []C.int {
	out := make([]C.int, len(xs))
	for i, v := range xs {
		out[i] = C.int(v)
	}
	return out
}

func cfloats(xs []float32) []C.float {
	out := make([]C.float, len(xs))
	for i, v := range xs {
		out[i] = C.float(v)
	}
	return out
}

func goFloats(xs []C.float) []float32 {
	out := make([]float32, len(xs))
	for i, v := range xs {
		out[i] = float32(v)
	}
	return out
}

// cgoFillTables drives the genuine iteration_init table-fill so the oracle's
// pow43[]/pow20[] file globals are populated before any calc_noise call.
func cgoFillTables(samplerateOut, athType int, athCurve, athOffsetDb, athFixpoint float32, noath int,
	sbL, sbS, psfb21, psfb12 []int, adjustLong, adjustShort []float32) {
	cSbL := cints(sbL)
	cSbS := cints(sbS)
	cPsfb21 := cints(psfb21)
	cPsfb12 := cints(psfb12)
	cAL := cfloats(adjustLong)
	cAS := cfloats(adjustShort)
	C.oracle_fill_tables(C.int(samplerateOut), C.int(athType), C.float(athCurve),
		C.float(athOffsetDb), C.float(athFixpoint), C.int(noath),
		&cSbL[0], &cSbS[0], &cPsfb21[0], &cPsfb12[0], &cAL[0], &cAS[0])
}

func cgoAthAdjust(a, x, athFloor, athFixpoint float32) float32 {
	return float32(C.oracle_athadjust(C.float(a), C.float(x), C.float(athFloor), C.float(athFixpoint)))
}

func cgoAthmdct(athType int, athCurve, athOffsetDb, athFixpoint, f float32) float32 {
	return float32(C.oracle_athmdct(C.int(athType), C.float(athCurve),
		C.float(athOffsetDb), C.float(athFixpoint), C.float(f)))
}

func cgoComputeATH(samplerateOut, athType int, athCurve, athOffsetDb, athFixpoint float32, noath int,
	sbL, sbS, psfb21, psfb12 []int) (l, p21, s, p12 []float32, floor float32) {
	cSbL := cints(sbL)
	cSbS := cints(sbS)
	cPsfb21 := cints(psfb21)
	cPsfb12 := cints(psfb12)
	outL := make([]C.float, 22)
	outP21 := make([]C.float, 6)
	outS := make([]C.float, 13)
	outP12 := make([]C.float, 6)
	var outFloor C.float
	C.oracle_compute_ath(C.int(samplerateOut), C.int(athType), C.float(athCurve),
		C.float(athOffsetDb), C.float(athFixpoint), C.int(noath),
		&cSbL[0], &cSbS[0], &cPsfb21[0], &cPsfb12[0],
		&outL[0], &outP21[0], &outS[0], &outP12[0], &outFloor)
	return goFloats(outL), goFloats(outP21), goFloats(outS), goFloats(outP12), float32(outFloor)
}

// xminResult bundles calc_xmin's outputs.
type xminResult struct {
	xmin       []float32
	eac        []byte
	maxNonzero int
	athOver    int
}

func cgoCalcXmin(
	samplerateOut int, athFixpoint float32, sfb21Extra, useTemporal int,
	sbL, sbS []int,
	athAdjustFactor, athFloor float32, athL, athS []float32,
	longfact, shortfact []float32, decay float32,
	xr []float32, width []int, psyLmax, psymax, sfbSmin, blockType int,
	enL, thmL, enS, thmS []float32) xminResult {
	cSbL := cints(sbL)
	cSbS := cints(sbS)
	cAthL := cfloats(athL)
	cAthS := cfloats(athS)
	cLong := cfloats(longfact)
	cShort := cfloats(shortfact)
	cXr := cfloats(xr)
	cWidth := cints(width)
	cEnL := cfloats(enL)
	cThmL := cfloats(thmL)
	cEnS := cfloats(enS)
	cThmS := cfloats(thmS)

	outXmin := make([]C.float, 39)
	outEac := make([]C.schar, 39)
	var outMaxNonzero C.int

	ret := C.oracle_calc_xmin(
		C.int(samplerateOut), C.float(athFixpoint), C.int(sfb21Extra), C.int(useTemporal),
		&cSbL[0], &cSbS[0],
		C.float(athAdjustFactor), C.float(athFloor), &cAthL[0], &cAthS[0],
		&cLong[0], &cShort[0], C.float(decay),
		&cXr[0], &cWidth[0],
		C.int(psyLmax), C.int(psymax), C.int(sfbSmin), C.int(blockType),
		&cEnL[0], &cThmL[0], &cEnS[0], &cThmS[0],
		&outXmin[0], &outEac[0], &outMaxNonzero)

	eac := make([]byte, 39)
	for i, v := range outEac {
		eac[i] = byte(v)
	}
	return xminResult{
		xmin:       goFloats(outXmin),
		eac:        eac,
		maxNonzero: int(outMaxNonzero),
		athOver:    int(ret),
	}
}

// noiseResult bundles calc_noise's outputs.
type noiseResult struct {
	distort   []float32
	overNoise float32
	totNoise  float32
	maxNoise  float32
	overCount int
	overSSD   int
	over      int
}

func cgoCalcNoise(
	xr []float32, l3enc, scalefac, width, window, subblockGain []int,
	globalGain, scalefacScale, preflag, psymax, maxNonzeroCoeff, count1, bigValues int,
	l3xmin []float32) noiseResult {
	cXr := cfloats(xr)
	cL3enc := cints(l3enc)
	cScalefac := cints(scalefac)
	cWidth := cints(width)
	cWindow := cints(window)
	cSbg := cints(subblockGain)
	cXmin := cfloats(l3xmin)

	outDistort := make([]C.float, 39)
	var outOver, outTot, outMax C.float
	var outOverCount, outOverSSD C.int

	ret := C.oracle_calc_noise(
		&cXr[0], &cL3enc[0], &cScalefac[0], &cWidth[0], &cWindow[0], &cSbg[0],
		C.int(globalGain), C.int(scalefacScale), C.int(preflag), C.int(psymax),
		C.int(maxNonzeroCoeff), C.int(count1), C.int(bigValues),
		&cXmin[0], &outDistort[0],
		&outOver, &outTot, &outMax, &outOverCount, &outOverSSD)

	return noiseResult{
		distort:   goFloats(outDistort),
		overNoise: float32(outOver),
		totNoise:  float32(outTot),
		maxNoise:  float32(outMax),
		overCount: int(outOverCount),
		overSSD:   int(outOverSSD),
		over:      int(ret),
	}
}
