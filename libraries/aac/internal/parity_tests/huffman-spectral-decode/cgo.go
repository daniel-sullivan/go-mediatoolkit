// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package huffman_spectral_decode pins the Go port of the Fraunhofer
// FDK-AAC plain spectral Huffman decoder (the nativeaac functions
// decodeHuffmanWord / decodeHuffmanWordCB / getEscape and the non-HCR branch
// of readSpectralData) against the vendored libAACdec/src/block.cpp +
// block.h, compiled into this test binary via cgo. Random codeword bit
// streams are fabricated with the FDK library's own bit WRITER
// (FDKwriteBits, BS_WRITER) and decoded on both sides; the unpacked
// quantized MDCT spectrum (int32 / FIXP_DBL) is compared bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (block.cpp for CBlock_GetEscape, aac_rom.cpp for the codebook ROM tables +
// AACcodeBookDescriptionTable, FDK_bitbuffer.cpp + genericStds.cpp for the
// bit-buffer back-end, one go-test binary per package) and NEVER imports
// libraries/aac — importing it would link a second copy of the whole FDK
// reference and clash on static symbols (the same amalgamation-split reason
// the flac parity packages document). It MAY, and does, import the pure-Go
// internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the plain spectral Huffman path is a pure INTEGER kernel — the
// bitstream cache, the codebook tree walk, the sign bits, and the escape
// sequence are all integer/shift operations producing int32 coefficients. It
// is therefore bit-identical regardless of -ffp-contract / vectorization, so
// no transcendental shim is needed. The strict-gate on the Go assertion is
// kept only for convention (the area lives under the aac_strict parity
// discipline); the kernel itself matches in any build.
package huffman_spectral_decode

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at
// libraries/aac/internal/parity_tests/huffman-spectral-decode). Mirrors the
// set in libraries/aac/aac_cgo.go.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libArithCoding/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libDRCdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>

// fparity_decode_huffman_word_cb walks one CBlock_DecodeHuffmanWordCB codeword
// out of the bit buffer for codebook `cb` (1..11) and returns the index. The
// bit buffer is the random byte slab fabricated by the Go side; bitsConsumed
// is updated to the post-read bit position.
extern int fparity_decode_huffman_word_cb(const uint8_t *buf, int bufSize,
                                          int cb, unsigned *bitNdxInOut);

// fparity_decode_huffman_word walks one CBlock_DecodeHuffmanWord codeword (the
// non-CB tree-walker variant) for codebook `cb` and returns the index.
extern int fparity_decode_huffman_word(const uint8_t *buf, int bufSize, int cb,
                                       unsigned *bitNdxInOut);

// fparity_get_escape runs CBlock_GetEscape over a bit buffer for quantized
// coefficient q (only |q|==16 reads bits) and returns the resolved value;
// bitNdxInOut is advanced past the consumed escape bits.
extern int fparity_get_escape(const uint8_t *buf, int bufSize, int q,
                              unsigned *bitNdxInOut);

// fparity_read_spectral_data runs the full non-HCR plain-Huffman branch of
// CBlock_ReadSpectralData over a fabricated bit buffer, driving the genuine
// vendored CBlock_DecodeHuffmanWordCB / CBlock_GetEscape / FDKreadBit. It is a
// faithful in-place twin of the block.cpp:620 outer loop (the surrounding
// function takes a giant CAacDecoderChannelInfo that is impractical to
// fabricate; the inner decode it dispatches is the genuine vendored code).
// codeBook[bnds], bandOffsets, windowGroupLen are the fabricated inputs; the
// flat int32 spectrum is written to `spectrum`.
extern void fparity_read_spectral_data(const uint8_t *buf, int bufSize,
                                       uint8_t *codeBook,
                                       const int16_t *bandOffsets,
                                       int windowGroups,
                                       const int *windowGroupLen,
                                       int granuleLength, int transmittedBands,
                                       int32_t *spectrum, int spectrumLen);
*/
import "C"

import "unsafe"

// cDecodeHuffmanWordCB walks one codeword with the vendored
// CBlock_DecodeHuffmanWordCB over buf for codebook cb, returning the decoded
// index and the post-read bit index.
func cDecodeHuffmanWordCB(buf []byte, cb int, bitNdx uint32) (int, uint32) {
	ndx := C.uint(bitNdx)
	idx := C.fparity_decode_huffman_word_cb(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf)),
		C.int(cb), &ndx)
	return int(idx), uint32(ndx)
}

// cDecodeHuffmanWord walks one codeword with the vendored
// CBlock_DecodeHuffmanWord (non-CB tree-walker) over buf for codebook cb.
func cDecodeHuffmanWord(buf []byte, cb int, bitNdx uint32) (int, uint32) {
	ndx := C.uint(bitNdx)
	idx := C.fparity_decode_huffman_word(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf)),
		C.int(cb), &ndx)
	return int(idx), uint32(ndx)
}

// cGetEscape runs the vendored CBlock_GetEscape over buf for quantized
// coefficient q, returning the resolved value and the post-read bit index.
func cGetEscape(buf []byte, q int, bitNdx uint32) (int, uint32) {
	ndx := C.uint(bitNdx)
	v := C.fparity_get_escape(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf)),
		C.int(q), &ndx)
	return int(v), uint32(ndx)
}

// cReadSpectralData runs the vendored non-HCR plain-Huffman branch of
// CBlock_ReadSpectralData and returns the unpacked int32 spectrum.
func cReadSpectralData(buf []byte, codeBook []byte, bandOffsets []int16,
	windowGroupLen []int, granuleLength, transmittedBands, spectrumLen int) []int32 {
	spectrum := make([]int32, spectrumLen)
	wgl := make([]C.int, len(windowGroupLen))
	for i, v := range windowGroupLen {
		wgl[i] = C.int(v)
	}
	C.fparity_read_spectral_data(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf)),
		(*C.uint8_t)(unsafe.Pointer(&codeBook[0])),
		(*C.int16_t)(unsafe.Pointer(&bandOffsets[0])),
		C.int(len(windowGroupLen)),
		&wgl[0],
		C.int(granuleLength), C.int(transmittedBands),
		(*C.int32_t)(unsafe.Pointer(&spectrum[0])), C.int(spectrumLen))
	return spectrum
}
