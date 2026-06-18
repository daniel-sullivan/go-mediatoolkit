// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

// Default-build counterparts of the per-channel bit-allocation float helpers
// (on_pe / reduce_side, quantize_pvt.c). Same arithmetic as the strict build
// but WITHOUT //go:noinline, so Go's backend may fuse / vectorize the mixed
// float/double products. The default build is within PSNR noise of the
// reference but is NOT a bit-exact target; the mp3_strict build
// (quantize_pvt_bits_fp_strict.go) is the only bit-exact claim. The integer
// clamping in on_pe / reduce_side is bit-identical in both builds.

// maxBitsPerChannel / maxBitsPerGranule are util.h:85/86 (the per-channel /
// per-granule bit caps); defined here for the default build.
const (
	maxBitsPerChannel = 4095
	maxBitsPerGranule = 7680
)

// opAddBits returns on_pe's add_bits = targ_bits*pe/700.0 - targ_bits.
func opAddBits(targBits int, pe float32) int {
	return int(float64(float32(targBits)*pe)/700.0 - float64(targBits))
}

// opReduceFac returns reduce_side's fac = .33*(.5-ms_ener_ratio)/.5.
func opReduceFac(msEnerRatio float32) float64 {
	return 0.33 * (0.5 - float64(msEnerRatio)) / 0.5
}

// opMoveBits returns reduce_side's move_bits = fac*.5*(targ_bits[0]+targ_bits[1]).
func opMoveBits(fac float64, sumTarg int) int {
	return int(fac * 0.5 * float64(sumTarg))
}
