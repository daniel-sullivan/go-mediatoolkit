// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package frame_sync_parse pins the Go port of the Fraunhofer FDK-AAC ADTS
// frame-sync-parse slice (nativeaac.findSyncword / decodeHeader /
// getRawDataBlockLength) against the vendored libMpegTPDec ADTS reader,
// compiled into this test binary via cgo. For a range of fabricated ADTS
// headers the C reference parse is run and its result (error code, every parsed
// header field, and the raw-data-block length) is compared field-for-field
// against the nativeaac port.
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (tpdec_adts.cpp plus the four translation units it links — tpdec_asc.cpp,
// FDK_crc.cpp, FDK_bitbuffer.cpp, genericStds.cpp; one go-test binary per
// package) and NEVER imports libraries/aac — importing it would link a second
// copy of the whole FDK reference and clash on static symbols (the same
// amalgamation-split reason the flac parity packages document). It MAY, and
// does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// # Oracle fidelity
//
// adtsRead_DecodeHeader and adtsRead_GetRawDataBlockLength are the genuine
// vendored functions — the oracle calls them directly. The syncword search,
// however, lives inside the file-static synchronization() in tpdec_lib.cpp,
// tangled with the full transport-decoder state machine; it cannot be called in
// isolation. Per the add-audio-format parity discipline ("otherwise reimplement
// the static C parser on the C public API inside the oracle TU; document the
// hand-twin"), fparity_find_syncword in oracle_adts_cgo.cpp lifts the
// TT_MP4_ADTS / numberOfRawDataBlocks==0 / !TPDEC_SYNCOK branch of that loop
// verbatim (tpdec_lib.cpp:1118) onto the public FDKreadBits/FDKgetValidBits API.
// The slice is an integer kernel (only bit reads and integer arithmetic), so it
// is bit-identical regardless of -ffp-contract / vectorization — no
// transcendental shim is needed here.
package frame_sync_parse

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/frame-sync-parse).
// libMpegTPDec/src is on the path because tpdec_adts.h / tpdec_lib.h include
// sibling sources by bare name from there.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libArithCoding/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libDRCdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/src
#cgo LDFLAGS: -lm

#include <stdint.h>

// fparity_adts_result and the two bridge entry points (fparity_find_syncword,
// fparity_adts_decode_header) are declared in oracle_bridge.h, shared verbatim
// with the oracle TU oracle_adts_cgo.cpp so the struct layout is identical on
// both sides.
#include "oracle_bridge.h"
*/
import "C"

import "unsafe"

// adtsResult is the Go-side mirror of the C fparity_adts_result, holding the
// reference parse for one fabricated ADTS header.
type adtsResult struct {
	err              int
	rdbLen0          int
	mpegID           uint8
	layer            uint8
	protectionAbsent uint8
	profile          uint8
	sampleFreqIndex  uint8
	privateBit       uint8
	channelConfig    uint8
	original         uint8
	home             uint8
	copyrightID      uint8
	copyrightStart   uint8
	frameLength      uint16
	adtsFullness     uint16
	numRawBlocks     uint8
	numPceBits       uint8
}

// cFindSyncword runs the vendored-lifted ADTS syncword search over buf and
// returns the raw TRANSPORTDEC_ERROR int and the bit position past the
// syncword (valid only when err == 0).
func cFindSyncword(buf []byte) (errCode, bitPos int) {
	var bp C.int
	var p *C.uchar
	if len(buf) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}
	e := C.fparity_find_syncword(p, C.int(len(buf)), &bp)
	return int(e), int(bp)
}

// cDecodeHeader runs the genuine vendored adtsRead_DecodeHeader over buf and
// returns the reference parse.
func cDecodeHeader(buf []byte, decoderCanDoMpeg4, bufferFullnessStartFlag, ignoreBufferFullness int) adtsResult {
	var r C.fparity_adts_result
	var p *C.uchar
	if len(buf) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}
	C.fparity_adts_decode_header(p, C.int(len(buf)),
		C.int(decoderCanDoMpeg4), C.int(bufferFullnessStartFlag),
		C.int(ignoreBufferFullness), &r)
	return adtsResult{
		err:              int(r.err),
		rdbLen0:          int(r.rdbLen0),
		mpegID:           uint8(r.mpeg_id),
		layer:            uint8(r.layer),
		protectionAbsent: uint8(r.protection_absent),
		profile:          uint8(r.profile),
		sampleFreqIndex:  uint8(r.sample_freq_index),
		privateBit:       uint8(r.private_bit),
		channelConfig:    uint8(r.channel_config),
		original:         uint8(r.original),
		home:             uint8(r.home),
		copyrightID:      uint8(r.copyright_id),
		copyrightStart:   uint8(r.copyright_start),
		frameLength:      uint16(r.frame_length),
		adtsFullness:     uint16(r.adts_fullness),
		numRawBlocks:     uint8(r.num_raw_blocks),
		numPceBits:       uint8(r.num_pce_bits),
	}
}
