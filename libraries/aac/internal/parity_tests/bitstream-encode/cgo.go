// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package bitstream_encode pins the Go port of the Fraunhofer FDK-AAC
// bitstream-encode area — the FDK bit WRITER (nativeaac.writeBits / fdkPut /
// syncCacheWrite / byteAlignWrite) and the encode-time spectral / scalefactor
// Huffman emitters (nativeaac.CodeValues / CodeScalefactorDelta) — against the
// vendored FDK reference, compiled into this test binary via cgo. For a range
// of fabricated write patterns, quantized-spectrum sections and scalefactor
// deltas the C reference is run and its produced bytes plus valid-bit count are
// compared bit-for-bit against the nativeaac port.
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (bit_cnt.cpp for the Huffman emitters, aacEnc_rom.cpp for the Huffman tables,
// FDK_bitbuffer.cpp for the FDK_put ring store, genericStds.cpp for the libSYS
// shims; one go-test binary per package) and NEVER imports libraries/aac —
// importing it would link a second copy of the whole FDK reference and clash on
// static symbols (the same amalgamation-split reason the flac parity packages
// document). It MAY, and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// # Oracle fidelity
//
// FDKaacEnc_codeValues and FDKaacEnc_codeScalefactorDelta are the genuine
// vendored functions — the oracle calls them directly. The FDK bit writer
// itself lives in inline header functions (FDK_bitstream.h: FDKwriteBits /
// FDKsyncCache / FDKbyteAlign); the oracle drives them through
// FDKinitBitStream so the produced ring buffer is the real reference store, not
// a hand-twin. The slice is a pure integer kernel (bit shifts, masks, table
// lookups), so it is bit-identical regardless of -ffp-contract / vectorization
// — no transcendental shim is needed here.
package bitstream_encode

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/bitstream-encode).
// libAACenc/src is on the path because bit_cnt.cpp and aacEnc_rom.cpp include
// sibling encoder headers by bare name from there; the libFDK / libSYS includes
// satisfy FDK_bitstream.h / FDK_bitbuffer.h / genericStds.h.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>

// Bridge entry points defined in oracle_bitenc_cgo.cpp.
extern int fparity_write_bits(const unsigned int *values,
                              const unsigned int *widths, int n,
                              unsigned char *out, int bufBytes, int *validBits);
extern int fparity_code_values(const short *values, int width, int codeBook,
                               unsigned char *out, int bufBytes, int *validBits);
extern int fparity_code_scalefactor_delta(int delta, unsigned char *out,
                                          int bufBytes, int *validBits,
                                          int *rangeErr);
*/
import "C"

import "unsafe"

// cWriteBits replays the (values[i], widths[i]) write sequence through the
// vendored FDKwriteBits into a fresh bufBytes-byte buffer, byte-aligns and
// flushes, and returns the produced bytes plus the pre-alignment valid-bit
// count. values and widths must have equal length.
func cWriteBits(values, widths []uint32, bufBytes int) (out []byte, validBits int) {
	buf := make([]byte, bufBytes)
	var vb C.int
	var vp *C.uint
	var wp *C.uint
	if len(values) > 0 {
		vp = (*C.uint)(unsafe.Pointer(&values[0]))
		wp = (*C.uint)(unsafe.Pointer(&widths[0]))
	}
	n := C.fparity_write_bits(vp, wp, C.int(len(values)),
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &vb)
	return buf[:int(n)], int(vb)
}

// cCodeValues Huffman-encodes width SHORT coefficients of one section with
// codeBook via the vendored FDKaacEnc_codeValues into a fresh bufBytes-byte
// buffer, byte-aligns and flushes, and returns the produced bytes plus the
// pre-alignment valid-bit count.
func cCodeValues(values []int16, width, codeBook, bufBytes int) (out []byte, validBits int) {
	buf := make([]byte, bufBytes)
	var vb C.int
	var vp *C.short
	if len(values) > 0 {
		vp = (*C.short)(unsafe.Pointer(&values[0]))
	}
	n := C.fparity_code_values(vp, C.int(width), C.int(codeBook),
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &vb)
	return buf[:int(n)], int(vb)
}

// cCodeScalefactorDelta DPCM-encodes a single scalefactor delta via the
// vendored FDKaacEnc_codeScalefactorDelta into a fresh bufBytes-byte buffer,
// byte-aligns and flushes, and returns the produced bytes, the pre-alignment
// valid-bit count and the C range-error return (1 when out of range, else 0).
func cCodeScalefactorDelta(delta, bufBytes int) (out []byte, validBits, rangeErr int) {
	buf := make([]byte, bufBytes)
	var vb, re C.int
	n := C.fparity_code_scalefactor_delta(C.int(delta),
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &vb, &re)
	return buf[:int(n)], int(vb), int(re)
}
