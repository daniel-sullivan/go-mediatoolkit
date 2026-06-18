// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine vendored libFDK/src/FDK_decorrelate.cpp as its own TU: the PS
// decorrelator (FDKdecorrelateOpen/Init/Apply, the INDEP_CPLX_PS allpass cascade
// + the PS ducker). See cgo.go.
#include "libfdk/libFDK/src/FDK_decorrelate.cpp"
