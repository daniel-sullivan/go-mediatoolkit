// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hooks for the bitstream-format parity oracle
// (internal/parity_tests/bitstream-format, the LAME-encoder format_bitstream
// slice). The bitstream.c frame-assembly functions are unexported methods on
// LameInternalFlags (1:1 translations of file-static / module-internal C
// routines); the pass-throughs below surface exactly the ones the parity suite
// drives, without widening the production API. Each adds no behaviour beyond
// calling the method it shadows. The reservoir.c entry points (ResvFrameBegin /
// ResvMaxBits / ResvAdjust / ResvFrameEnd) are already exported (they are
// non-static in reservoir.c), so they need no hook.
//
// The context sub-structs and consts these tests prime/read (Cfg / OvEnc /
// SvEnc / L3Side / Bs and MaxHeaderBuf / MaxHeaderLen) are exported fields on
// LameInternalFlags, so the oracle fabricates and inspects state directly.

// FormatBitstream exposes formatBitstream for the parity oracle.
func (gfc *LameInternalFlags) FormatBitstream() int { return gfc.formatBitstream() }

// EncodeSideInfo2 exposes encodeSideInfo2 for the parity oracle.
func (gfc *LameInternalFlags) EncodeSideInfo2(bitsPerFrame int) { gfc.encodeSideInfo2(bitsPerFrame) }

// WriteMainData exposes writeMainData for the parity oracle.
func (gfc *LameInternalFlags) WriteMainData() int { return gfc.writeMainData() }

// WriteHeader exposes writeheader for the parity oracle.
func (gfc *LameInternalFlags) WriteHeader(val, j int) { gfc.writeheader(val, j) }

// CRCWriteHeader exposes crcWriteheader for the parity oracle.
func (gfc *LameInternalFlags) CRCWriteHeader(header []byte) { gfc.crcWriteheader(header) }

// CRCUpdate exposes crcUpdate for the parity oracle.
func CRCUpdate(value, crc int) int { return crcUpdate(value, crc) }

// DrainIntoAncillary exposes drainIntoAncillary for the parity oracle.
func (gfc *LameInternalFlags) DrainIntoAncillary(remainingBits int) {
	gfc.drainIntoAncillary(remainingBits)
}

// ComputeFlushbits exposes computeFlushbits for the parity oracle; it returns
// both the flushbits result and the totalBytesOutput the C writes through its
// out-pointer.
func (gfc *LameInternalFlags) ComputeFlushbits() (flushbits, totalBytesOutput int) {
	flushbits = gfc.computeFlushbits(&totalBytesOutput)
	return flushbits, totalBytesOutput
}

// FlushBitstream exposes flushBitstream for the parity oracle.
func (gfc *LameInternalFlags) FlushBitstream() { gfc.flushBitstream() }

// AddDummyByte exposes addDummyByte for the parity oracle.
func (gfc *LameInternalFlags) AddDummyByte(val byte, n uint) { gfc.addDummyByte(val, n) }

// DoCopyBuffer exposes doCopyBuffer for the parity oracle.
func (gfc *LameInternalFlags) DoCopyBuffer(buffer []byte, size int) int {
	return gfc.doCopyBuffer(buffer, size)
}

// CopyBuffer exposes copyBuffer for the parity oracle.
func (gfc *LameInternalFlags) CopyBuffer(buffer []byte, size, mp3data int) int {
	return gfc.copyBuffer(buffer, size, mp3data)
}

// GetFrameBits exposes getframebits for the parity oracle.
func (gfc *LameInternalFlags) GetFrameBits() int { return gfc.getframebits() }

// CalcFrameLength exposes calcFrameLength for the parity oracle.
func (gfc *LameInternalFlags) CalcFrameLength(kbps, pad int) int {
	return gfc.calcFrameLength(kbps, pad)
}

// GetMaxFrameBufferSizeByConstraint exposes getMaxFrameBufferSizeByConstraint
// for the parity oracle.
func (gfc *LameInternalFlags) GetMaxFrameBufferSizeByConstraint(constraint int) int {
	return gfc.getMaxFrameBufferSizeByConstraint(constraint)
}
