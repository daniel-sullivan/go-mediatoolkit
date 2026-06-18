// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libfdk/libFDK/src/FDK_tools_rom.cpp so the
// invCount[] ROM that backs the inline GetInvInt() (fixpoint_math.h:948,
// referenced by FDKaacEnc_InitElementBits' LFE-bit split) resolves at link time.
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
