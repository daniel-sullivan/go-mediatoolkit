// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compiles the GENUINE vendored libSBRdec/src/env_extr.cpp as its own TU: the
// SBR bitstream extraction (sbrGetHeaderData, sbrGetChannelElement,
// extractFrameInfo, sbrGetEnvelope, sbrGetNoiseFloorData, ...) plus the
// sbrdec_mapToStdSampleRate definition. The env_dec parity oracle drives the
// genuine decodeSbrData (env_dec.cpp) on top of this genuine parse path. Statics
// stay file-local. See cgo.go.
#include "libfdk/libSBRdec/src/env_extr.cpp"
