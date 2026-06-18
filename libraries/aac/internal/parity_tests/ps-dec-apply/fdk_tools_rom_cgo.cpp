// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine vendored libFDK/src/FDK_tools_rom.cpp as its own TU: the SineTable512
// (used by inline_fixp_cos_sin in the PS rotation) and the inv-sqrt / sqrt
// lookup tables (used by the PS ducker's invSqrtNorm2 / sqrtFixp). See cgo.go.
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
