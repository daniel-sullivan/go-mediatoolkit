// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling one vendored Fraunhofer FDK-AAC source
// file as its own translation unit, so file-static helpers never collide
// across modules. Generated for libraries/aac; see libfdk/COPYING.
#include "libfdk/libSBRenc/src/env_bit.cpp"
