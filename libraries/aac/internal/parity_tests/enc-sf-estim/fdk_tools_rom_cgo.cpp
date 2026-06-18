// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libFDK/src/FDK_tools_rom.cpp so the invSqrtTab ROM
// (used by sqrtFixp / invSqrtNorm2 in the form-factor calculation) resolves.
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
