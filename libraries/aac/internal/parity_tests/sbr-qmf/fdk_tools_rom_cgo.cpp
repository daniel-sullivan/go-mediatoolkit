// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/FDK_tools_rom.cpp (defines SineTableXXX, SineWindowXXX,
// windowSlopes, FDKgetWindowSlope, and the QMF prototype/phaseshift ROM
// qmf_pfilt640 / qmf_phaseshift_cos64 / qmf_phaseshift_sin64), its own TU.
// See cgo.go.
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
