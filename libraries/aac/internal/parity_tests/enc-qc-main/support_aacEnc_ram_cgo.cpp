// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Sibling TU compiling the genuine vendored libAACenc/src/aacEnc_ram.cpp so that
// FDKaacEnc_BCNew / FDKaacEnc_BCClose (linked in from dyn_bits.cpp, which this
// slice does not call but which shares the dyn_bits.cpp TU with
// FDKaacEnc_dynBitCount that it does call) resolve their GetRam_aacEnc_*
// allocator references. The bridge allocates the BITCNTR_STATE scratch directly
// with calloc, so these pools are link-time only. Genuine vendored source -> the
// oracle stays real_vendored.
#include "libfdk/libAACenc/src/aacEnc_ram.cpp"
