// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libAACenc/src/aacEnc_ram.cpp so the
// GetRam_aacEnc_AdjustThreshold / GetRam_aacEnc_AdjThrStateElement allocators
// (referenced by FDKaacEnc_AdjThrNew/Close in the adj_thr.cpp TU pinned by
// adj_thr_oracle_cgo.cpp) resolve at link time. The bridge never calls the
// New/Close path; this only satisfies the linker for the whole-TU compile.
#include "libfdk/libAACenc/src/aacEnc_ram.cpp"
