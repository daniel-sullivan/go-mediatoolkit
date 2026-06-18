// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// adtsBitReader is a pure-Go, MSB-first bit reader providing the subset of the
// FDK bitstream API that the ADTS frame-sync-parse slice consumes: FDKreadBits,
// FDKgetValidBits, FDKpushBack and FDKpushFor (see
// libfdk/libFDK/include/FDK_bitstream.h). The FDK reference reads big-endian and
// consumes the most-significant bit first; this twin reproduces that bit order
// and the read-past-end / push semantics behaviourally rather than reproducing
// the CacheWord/BitsInCache machinery of the bit-buffer subsystem (a separate
// area, ported in bitstream.go). The slice is an integer kernel, so field
// extraction is bit-identical.
type adtsBitReader struct {
	buf     []byte
	bitPos  int // absolute bit offset from the start of buf, MSB-first
	bitSize int // total number of readable bits in buf
}

// newAdtsBitReader returns an adtsBitReader over buf positioned at the first bit.
func newAdtsBitReader(buf []byte) *adtsBitReader {
	return &adtsBitReader{buf: buf, bitPos: 0, bitSize: len(buf) * 8}
}

// readBits reads numberOfBits (0..32) MSB-first and returns them right-aligned.
// It mirrors FDKreadBits in libfdk/libFDK/include/FDK_bitstream.h:210. Reading
// past the end yields zero bits, matching the reference behaviour of a depleted
// cache returning padding.
func (r *adtsBitReader) readBits(numberOfBits uint) uint32 {
	if numberOfBits == 0 {
		return 0
	}
	var value uint32
	for i := uint(0); i < numberOfBits; i++ {
		value <<= 1
		if r.bitPos < r.bitSize {
			byteIdx := r.bitPos >> 3
			bitIdx := uint(7 - (r.bitPos & 7))
			value |= uint32((r.buf[byteIdx] >> bitIdx) & 1)
		}
		r.bitPos++
	}
	return value
}

// getValidBits returns the number of bits remaining before the end of the
// buffer. It mirrors FDKgetValidBits in
// libfdk/libFDK/include/FDK_bitstream.h.
func (r *adtsBitReader) getValidBits() int {
	if r.bitPos >= r.bitSize {
		return 0
	}
	return r.bitSize - r.bitPos
}

// pushBack rewinds the read position by numberOfBits, mirroring FDKpushBack in
// libfdk/libFDK/include/FDK_bitstream.h:538.
func (r *adtsBitReader) pushBack(numberOfBits int) {
	r.bitPos -= numberOfBits
}

// pushFor advances the read position by numberOfBits, mirroring FDKpushFor in
// libfdk/libFDK/include/FDK_bitstream.h:550.
func (r *adtsBitReader) pushFor(numberOfBits int) {
	r.bitPos += numberOfBits
}
