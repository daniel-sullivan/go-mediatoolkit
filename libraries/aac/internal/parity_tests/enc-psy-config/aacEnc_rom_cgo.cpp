// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/aacEnc_rom.cpp
 * — supplies the p_FDKaacEnc_<rate>_long_1024 / _short_128 SFB_PARAM ROM tables
 * (aacEnc_rom.cpp:735-809) that the sfbInfoTab[] in psy_configuration.cpp points
 * at and FDKaacEnc_initSfbTable indexes. Compiling it as its own TU resolves
 * these without duplicate symbols. */
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
