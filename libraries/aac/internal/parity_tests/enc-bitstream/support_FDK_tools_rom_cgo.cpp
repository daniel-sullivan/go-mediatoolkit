// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/FDK_tools_rom.cpp
 * — supplies getBitstreamElementList referenced (but, by these shims, never
 * called) by the FDKaacEnc_ChannelElementWrite public function in the included
 * bitenc.cpp, plus the bitstream syntax / trig ROM tables. Genuine vendored
 * source -> oracle stays real_vendored. */
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
