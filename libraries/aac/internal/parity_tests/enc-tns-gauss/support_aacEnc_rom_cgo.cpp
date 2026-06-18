// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Sibling TU compiling the genuine vendored libfdk/libAACenc/src/aacEnc_rom.cpp into this parity test binary
// so the aacenc_tns.cpp Gauss-window oracle links the real kernels. This package
// owns its own copy of the needed C TUs and never imports libraries/aac.
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
