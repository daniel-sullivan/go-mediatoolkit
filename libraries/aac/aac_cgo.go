// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// This file carries the shared cgo preamble for the Fraunhofer FDK-AAC
// backend: the include/compile flags for the vendored libfdk tree and the
// AAC encoder/decoder public headers. The per-translation-unit wrappers
// (fdk_tu_*.cpp) each compile one vendored FDK-AAC source so file-static
// helpers never collide across modules; the include paths below must point
// at the actual vendored module include directories.
//
// The whole FDK-AAC island is fenced behind the opt-in `aacfdk` build tag:
// a default `go build ./...` (cgo or not) links zero FDK-AAC. See
// libfdk/COPYING for the (non-FOSS-but-permissive) Fraunhofer FDK-AAC
// license; the repository LICENSE stays MIT and applies to the pure-Go
// code only. AAC-LC patents expired in 2017, so the AAC-LC target carries
// no live patent concern.

package aac

/*
// Include search paths. They must be visible to BOTH the C compilation of
// the import "C" preambles in the *_cgo.go files (CFLAGS) and the C++
// compilation of the per-TU vendored sources fdk_tu_*.cpp (CXXFLAGS), so
// every -I is listed under both. CPPFLAGS would cover both, but Go's cgo
// flag handling routes the preamble through CFLAGS and the .cpp TUs
// through CXXFLAGS, so they are duplicated explicitly.
#cgo CXXFLAGS: -std=c++11 -O2 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libArithCoding/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libDRCdec/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libSACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libSACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/libfdk/libSACenc/src
#cgo LDFLAGS: -lm

#include <stdlib.h>
#include "aacenc_lib.h"
#include "aacdecoder_lib.h"
*/
import "C"
