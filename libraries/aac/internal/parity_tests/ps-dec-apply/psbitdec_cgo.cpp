// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine vendored libSBRdec/src/psbitdec.cpp as its own TU: ReadPsData /
// DecodePs (the PS payload parse + delta decode + 34<->20 band mapping). See
// cgo.go.
#include "libfdk/libSBRdec/src/psbitdec.cpp"
