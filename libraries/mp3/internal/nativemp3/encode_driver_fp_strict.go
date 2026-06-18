// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

// Strict-mode float32 helper for the PCM encode driver's pcm_transform
// (lame_copy_inbuffer, lame.c:1846/1847).
//
// The transform widens int16 PCM to sample_t (float32) and applies the
// scale/downmix matrix: u = xl*m00 + xr*m01. Both products and the sum are
// float32; the cgo oracle compiles lame.c with -ffp-contract=off, so they round
// separately. Go's backend would otherwise fuse `xl*m00 + xr*m01` into one FMA.
// drvTransform routes each multiply through the //go:noinline drvMul so the SSA
// cannot pattern-match the fused form, and the add through drvAdd, matching
// clang. The `drv` prefix keeps it distinct from the other slices' helpers.

//go:noinline
func drvMul(a, b float32) float32 { return a * b }

//go:noinline
func drvAdd(a, b float32) float32 { return a + b }

// drvTransform returns xl*ml + xr*mr as two separately rounded float32 products
// followed by a rounded add, matching lame_copy_inbuffer's
// `xl*m[i][0] + xr*m[i][1]` under -ffp-contract=off.
func drvTransform(xl, ml, xr, mr float32) float32 {
	return drvAdd(drvMul(xl, ml), drvMul(xr, mr))
}
