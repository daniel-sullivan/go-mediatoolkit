// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/aacEnc_rom.cpp
 * — the encoder ROM tables aacEnc_tns.cpp reads, in particular the TNS-encode
 * reflection-coefficient tables FDKaacEnc_tnsEncCoeff3/4 and
 * FDKaacEnc_tnsCoeff3Borders/4Borders (aacEnc_rom.cpp:818-866) that the static
 * FDKaacEnc_Search3/Search4 / Index2Parcor reference, plus the autocorrelation
 * window / max-bands tables the (uncalled) TnsDetect path needs to link. Linking
 * the genuine TU keeps the oracle real_vendored (the Go side's enc_tns.go ROM is
 * verified against these). See bridge.cpp for the amalgamation-split rationale. */
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
