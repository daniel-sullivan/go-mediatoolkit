// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compiles the GENUINE vendored libSBRdec/src/env_calc.cpp as its own TU: the
// SBR envelope-gain calculation (calculateSbrEnvelope, ResetLimiterBands,
// maxSubbandSample, rescaleSubbandSamples + the calcNrg/calcSubbandGain/
// calcAvgGain/adjustTimeSlot*/apply_inter_tes statics). Its statics stay
// file-local; the oracle is the real reference. See cgo.go.
#include "libfdk/libSBRdec/src/env_calc.cpp"
