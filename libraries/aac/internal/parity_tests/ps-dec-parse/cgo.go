// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psdecparse pins the Go port of the Fraunhofer FDK-AAC HE-AAC v2
// parametric-stereo bitstream parse (ReadPsData + DecodePs, psbitdec.cpp) —
// internal/nativeaac/sbr ps_bitdec.go — against the vendored C, compiled into
// this test binary via cgo.
//
// This package compiles its OWN copy of the needed vendored C source (psbitdec +
// sbr_rom + huff_dec + FDK_bitbuffer + genericStds + fixpoint_math) and NEVER
// imports libraries/aac — importing it would link a second copy of the FDK
// reference and clash on static symbols. It MAY, and does, import the pure-Go
// internal/nativeaac/sbr.
//
// Integer parity: the PS parse is a pure integer subsystem (Huffman codeword
// walk, SCHAR delta decode, UCHAR border arithmetic, integer 34->20 averaging),
// bit-identical regardless of -ffp-contract / vectorization — so the slice
// asserts EXACT integer equality unconditionally.
package psdecparse

/*
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/src
#cgo LDFLAGS: -lm

#include <stdint.h>

typedef struct {
  int psProcessFlag;
  int bitsRead;
  uint8_t noEnv;
  uint8_t freqResIid;
  uint8_t freqResIcc;
  uint8_t bFineIidQ;
  uint8_t envStartStop[6];
  int8_t iidMapped[5 * 34];
  int8_t iccMapped[5 * 34];
} psParseOut;

extern void qparity_psParse(const uint8_t *payload, int payloadBytes, int validBits,
                            int noSubSamples, int prevDecoded, int frameError,
                            psParseOut *out);
*/
import "C"

import "unsafe"

// psParseC is the genuine PS parse result.
type psParseC struct {
	psProcessFlag int
	bitsRead      int
	noEnv         uint8
	freqResIid    uint8
	freqResIcc    uint8
	bFineIidQ     uint8
	envStartStop  [6]uint8
	iidMapped     [5 * 34]int8
	iccMapped     [5 * 34]int8
}

// cPsParse runs the genuine ReadPsData + DecodePs over payload[:payloadBytes]
// (validBits valid bits) and returns the flat result.
func cPsParse(payload []byte, validBits, noSubSamples, prevDecoded, frameError int) psParseC {
	var out C.psParseOut
	var pp *C.uint8_t
	if len(payload) > 0 {
		pp = (*C.uint8_t)(unsafe.Pointer(&payload[0]))
	}
	C.qparity_psParse(pp, C.int(len(payload)), C.int(validBits),
		C.int(noSubSamples), C.int(prevDecoded), C.int(frameError), &out)

	var r psParseC
	r.psProcessFlag = int(out.psProcessFlag)
	r.bitsRead = int(out.bitsRead)
	r.noEnv = uint8(out.noEnv)
	r.freqResIid = uint8(out.freqResIid)
	r.freqResIcc = uint8(out.freqResIcc)
	r.bFineIidQ = uint8(out.bFineIidQ)
	for i := 0; i < 6; i++ {
		r.envStartStop[i] = uint8(out.envStartStop[i])
	}
	for i := 0; i < 5*34; i++ {
		r.iidMapped[i] = int8(out.iidMapped[i])
		r.iccMapped[i] = int8(out.iccMapped[i])
	}
	return r
}
