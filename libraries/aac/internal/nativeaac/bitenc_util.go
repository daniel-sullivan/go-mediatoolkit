// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// Bitstream-encode area: small shared integer helpers and the transport
// encoder seam.
//
// fixMin / fixpAbsInt are the libfdk fixminmax.h / common_fix.h integer
// primitives the bitstream-encode functions use. TransportEnc is the Go seam
// for the opaque C HANDLE_TRANSPORTENC: the bitstream-encode functions only
// ever fetch its bit stream and start/end CRC regions and the access unit, so
// the interface exposes exactly those operations. A nil TransportEnc mirrors
// the C `hTpEnc == NULL` (count-static-bits-only) calling convention.

package nativeaac

// fixMin returns the smaller of two ints (fixminmax.h, fixMin).
func fixMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// fixMax returns the larger of two ints (fixminmax.h, fixMax).
func fixMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// fixpAbsInt returns the absolute value of an int (common_fix.h fixp_abs on an
// INT operand).
func fixpAbsInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TransportEnc is the Go counterpart of the opaque C HANDLE_TRANSPORTENC
// (tpenc_lib.h). The bitstream-encode area calls only these methods; the
// transport framing itself is a separate area.
type TransportEnc interface {
	// GetBitstream returns the transport encoder's output bit stream
	// (tpenc_lib.h:236, transportEnc_GetBitstream).
	GetBitstream() *bitStream
	// CrcStartReg opens a CRC region of mBits bits and returns its id
	// (tpenc_lib.h:305, transportEnc_CrcStartReg).
	CrcStartReg(mBits int) int
	// CrcEndReg closes the CRC region reg (tpenc_lib.h:313,
	// transportEnc_CrcEndReg).
	CrcEndReg(reg int)
	// EndAccessUnit finalises the access unit, updating *frameBits
	// (tpenc_lib.h:281, transportEnc_EndAccessUnit).
	EndAccessUnit(frameBits *int)
}
