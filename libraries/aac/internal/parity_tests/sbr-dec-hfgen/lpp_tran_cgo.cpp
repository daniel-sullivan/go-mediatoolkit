// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libSBRdec/src/lpp_tran.cpp as its own TU for the sbr-dec-hfgen parity
// oracle: the LPP transposer (lppTransposer / createLppTransposer /
// resetLppTransposer). See cgo.go.
#include "libfdk/libSBRdec/src/lpp_tran.cpp"
