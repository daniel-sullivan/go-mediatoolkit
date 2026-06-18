// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libSBRdec/src/HFgen_preFlat.cpp as its own TU: the pre-flattening gain
// vector (sbrDecoder_calculateGainVec + polyfit/polyval/Cholesky). See cgo.go.
#include "libfdk/libSBRdec/src/HFgen_preFlat.cpp"
