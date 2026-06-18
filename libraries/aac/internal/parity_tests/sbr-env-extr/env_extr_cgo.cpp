// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compiles the GENUINE vendored libSBRdec/src/env_extr.cpp as its own TU: the
// SBR bitstream extraction (sbrGetHeaderData, sbrGetChannelElement,
// extractFrameInfo, sbrGetEnvelope, sbrGetNoiseFloorData,
// sbrGetSyntheticCodedData, checkFrameInfo, ...). Its statics stay file-local;
// the oracle is the real reference. See cgo.go.
#include "libfdk/libSBRdec/src/env_extr.cpp"
