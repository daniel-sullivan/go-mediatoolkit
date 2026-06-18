// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

// Strict-mode mixed float/double helpers for LAME's per-channel bit allocation
// (on_pe / reduce_side, quantize_pvt.c:430/494).
//
// Both routines compute a few quantities partly in C `float` (FLOAT == float32)
// and partly in `double` (the 700.0 / .5 / .33 literals are double, so they
// promote their operands), then truncate to int. The cgo oracle compiles
// quantize_pvt.c with -ffp-contract=off, so each multiply/divide rounds
// separately; the //go:noinline wrappers below keep the products opaque so Go's
// backend cannot fuse them into an FMA, matching clang. int(float64) truncates
// toward zero exactly like C's assignment to int.
//
// The exact C MIXED-precision semantics, reproduced operand-for-operand:
//
//   on_pe:      add_bits[ch] = targ_bits[ch] * pe[gr][ch] / 700.0 - targ_bits[ch]
//               `targ_bits * pe` is int*float -> FLOAT (float32) product; `/700.0`
//               promotes that to double; `- targ_bits` is double; assign -> int.
//   reduce_side fac = .33 * (.5 - ms_ener_ratio) / .5
//               all-double (the literals are double; ms_ener_ratio FLOAT promotes).
//   reduce_side move_bits = fac * .5 * (targ_bits[0] + targ_bits[1])
//               all-double; assign -> int.

// maxBitsPerChannel / maxBitsPerGranule are util.h:85/86
// (#define MAX_BITS_PER_CHANNEL 4095, #define MAX_BITS_PER_GRANULE 7680): the
// hard per-channel / per-granule bit caps the allocator clamps to.
const (
	maxBitsPerChannel = 4095
	maxBitsPerGranule = 7680
)

// opAddBits returns int(float64(float32(targBits)*pe)/700.0 - float64(targBits)),
// on_pe's add_bits computation (quantize_pvt.c:450). The int*float product is a
// float32 (//go:noinline so it rounds before promoting to double); the /700.0
// and the -targBits run in double, separately rounded.
//
//go:noinline
func opAddBits(targBits int, pe float32) int {
	prod := opMulF32(float32(targBits), pe) // FLOAT product (int promoted to float)
	return int(float64(prod)/700.0 - float64(targBits))
}

// opMulF32 returns the separately rounded float32 product a*b (on_pe's
// targ_bits * pe before the double divide).
//
//go:noinline
func opMulF32(a, b float32) float32 { return a * b }

// opReduceFac returns float64(.33*(.5-msEnerRatio)/.5), reduce_side's fac
// (quantize_pvt.c:506). All-double: ms_ener_ratio (FLOAT) promotes to double.
//
//go:noinline
func opReduceFac(msEnerRatio float32) float64 {
	return 0.33 * (0.5 - float64(msEnerRatio)) / 0.5
}

// opMoveBits returns int(fac * .5 * float64(sumTarg)), reduce_side's move_bits
// (quantize_pvt.c:514). All-double, separately rounded by the //go:noinline
// boundary so the two multiplies do not fuse.
//
//go:noinline
func opMoveBits(fac float64, sumTarg int) int {
	return int(fac * 0.5 * float64(sumTarg))
}
