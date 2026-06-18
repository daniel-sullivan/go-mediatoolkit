// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling one vendored Fraunhofer FDK-AAC source as its
// own translation unit for the decode-e2e parity slice. This slice compiles its
// OWN copy of the fdk TUs (it never imports libraries/aac); see libfdk/COPYING.
#include "../../../libfdk/libMpegTPDec/src/tpdec_lib.cpp"
