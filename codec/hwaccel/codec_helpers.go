//go:build linux || darwin

// Small codec-geometry helpers shared by the VAAPI (linux) and VideoToolbox
// (darwin) VP9/AV1 paths.

package hwaccel

// alignUp rounds v up to the next multiple of a. Used for coding-block /
// superblock alignment when sizing decode surfaces (16 for H.264 macroblocks,
// 64 for VP9 superblocks, 8 for AV1 frame size).
func alignUp(v, a int) int { return (v + a - 1) / a * a }
