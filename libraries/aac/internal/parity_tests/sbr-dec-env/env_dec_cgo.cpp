// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compiles the GENUINE vendored libSBRdec/src/env_dec.cpp as its own TU: the SBR
// envelope + noise-floor dequantization (decodeSbrData, decodeEnvelope,
// decodeNoiseFloorlevels, sbr_envelope_unmapping, requantizeEnvelopeData,
// deltaToLinearPcmEnvelopeDecoding + the concealment statics). Its statics stay
// file-local; the oracle is the real reference. See cgo.go.
#include "libfdk/libSBRdec/src/env_dec.cpp"
