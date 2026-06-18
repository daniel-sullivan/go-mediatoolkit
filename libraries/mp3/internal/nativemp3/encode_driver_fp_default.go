// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

// Default-build counterpart of the PCM encode driver's pcm_transform helper
// (lame_copy_inbuffer). Same float32 mul/add as the strict build but WITHOUT
// //go:noinline, so Go's backend may fuse / vectorize the downmix. The default
// build is within PSNR noise of the reference but is NOT a bit-exact target;
// the mp3_strict build (encode_driver_fp_strict.go) is the only bit-exact claim.

// drvTransform returns xl*ml + xr*mr (fusable in the default build).
func drvTransform(xl, ml, xr, mr float32) float32 { return xl*ml + xr*mr }
