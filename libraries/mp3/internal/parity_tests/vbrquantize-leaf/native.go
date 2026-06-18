// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrquantizeleaf

import "go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 port the same way oracle.c drives the
// vendored C: each wrapper forwards the identical flat inputs into the
// nativemp3 parity hook (parityhooks_vbrquantize.go). Keeping the Go drivers
// beside the cgo bridge mirrors the C oracle structure so the two sides of each
// assertion are visibly symmetric. These import only internal/nativemp3 (never
// libraries/mp3).

func goFillTables() { nativemp3.FillVbrQuantizeTables() }

func goVecMaxC(xr34 []float32, bw int) float32 { return nativemp3.VecMaxC(xr34, bw) }

func goFindLowestScalefac(xr34 float32) uint8 { return nativemp3.FindLowestScalefac(xr34) }

func goK344(x [4]float64) [4]int { return nativemp3.K344(x) }

func goCalcSfbNoiseX34(xr, xr34 []float32, bw int, sf uint8) float32 {
	return nativemp3.CalcSfbNoiseX34(xr, xr34, bw, sf)
}

func goTriCalcSfbNoiseX34(xr, xr34 []float32, l3Xmin float32, bw int, sf uint8) uint8 {
	return nativemp3.TriCalcSfbNoiseX34(xr, xr34, l3Xmin, bw, sf)
}

func goCalcScalefac(l3Xmin float32, bw int) int { return nativemp3.CalcScalefac(l3Xmin, bw) }

func goGuessScalefacX34(xr, xr34 []float32, l3Xmin float32, bw int, sfMin uint8) uint8 {
	return nativemp3.GuessScalefacX34(xr, xr34, l3Xmin, bw, sfMin)
}

func goFindScalefacX34(xr, xr34 []float32, l3Xmin float32, bw int, sfMin uint8) uint8 {
	return nativemp3.FindScalefacX34(xr, xr34, l3Xmin, bw, sfMin)
}
