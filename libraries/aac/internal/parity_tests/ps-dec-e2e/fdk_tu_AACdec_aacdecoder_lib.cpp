// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper for the aacdecoder_lib translation unit of the ps-dec-e2e
// parity slice. This slice compiles its OWN copy of the fdk TUs (it never
// imports libraries/aac); see libfdk/COPYING.
//
// The ps-dec-e2e slice drives the GENUINE fdk full HE-AAC v2 decoder (which
// performs the parametric-stereo upmix internally), so it compiles the SHARED
// vendored aacdecoder_lib.cpp unmodified — no pre-SBR core tap is needed here.
#include "../../../libfdk/libAACdec/src/aacdecoder_lib.cpp"
