// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compiles the GENUINE vendored libFDK/src/qmf.cpp as its own translation unit
// for the sbr-qmf parity oracle (static symbols stay file-local; the oracle is
// the real reference, never a hand-twin). qmf.cpp #includes qmf_pcm.h twice to
// instantiate the synthesis output variants plus the 32-bit analysis input
// variant — the SBR decoder uses the 32-bit (LONG) ones. See cgo.go.
#include "libfdk/libFDK/src/qmf.cpp"
