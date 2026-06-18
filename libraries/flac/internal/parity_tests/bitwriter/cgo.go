//go:build cgo

// Package bitwriter contains parity tests that pin the Go bitwriter
// port at libraries/flac/internal/nativeflac against libFLAC's
// FLAC__BitWriter. Every writer entry point is exercised on both sides
// over the same input sequence and the emitted byte stream (plus the
// running CRC8/CRC16) is compared for bit-exact equality.
package bitwriter

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libflac
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/include
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/src/libFLAC/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable -Wno-static-in-inline

#ifdef HAVE_CONFIG_H
#  include <config.h>
#endif
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "private/bitwriter.h"
*/
import "C"

import (
	"unsafe"
)

// cgoBitWriter wraps a libFLAC FLAC__BitWriter.
type cgoBitWriter struct {
	bw *C.FLAC__BitWriter
}

func newCgoBitWriter() *cgoBitWriter {
	b := &cgoBitWriter{bw: C.FLAC__bitwriter_new()}
	C.FLAC__bitwriter_init(b.bw)
	return b
}

func (b *cgoBitWriter) free() {
	C.FLAC__bitwriter_free(b.bw)
	C.FLAC__bitwriter_delete(b.bw)
}

func (b *cgoBitWriter) clear() { C.FLAC__bitwriter_clear(b.bw) }

func (b *cgoBitWriter) WriteZeroes(bits uint32) bool {
	return C.FLAC__bitwriter_write_zeroes(b.bw, C.uint32_t(bits)) != 0
}

func (b *cgoBitWriter) WriteRawUint32(val, bits uint32) bool {
	return C.FLAC__bitwriter_write_raw_uint32(b.bw, C.uint32_t(val), C.uint32_t(bits)) != 0
}

func (b *cgoBitWriter) WriteRawInt32(val int32, bits uint32) bool {
	return C.FLAC__bitwriter_write_raw_int32(b.bw, C.int32_t(val), C.uint32_t(bits)) != 0
}

func (b *cgoBitWriter) WriteRawUint64(val uint64, bits uint32) bool {
	return C.FLAC__bitwriter_write_raw_uint64(b.bw, C.uint64_t(val), C.uint32_t(bits)) != 0
}

func (b *cgoBitWriter) WriteRawInt64(val int64, bits uint32) bool {
	return C.FLAC__bitwriter_write_raw_int64(b.bw, C.int64_t(val), C.uint32_t(bits)) != 0
}

func (b *cgoBitWriter) WriteRawUint32LittleEndian(val uint32) bool {
	return C.FLAC__bitwriter_write_raw_uint32_little_endian(b.bw, C.uint32_t(val)) != 0
}

func (b *cgoBitWriter) WriteByteBlock(vals []byte) bool {
	if len(vals) == 0 {
		return C.FLAC__bitwriter_write_byte_block(b.bw, nil, 0) != 0
	}
	return C.FLAC__bitwriter_write_byte_block(b.bw, (*C.FLAC__byte)(unsafe.Pointer(&vals[0])), C.uint32_t(len(vals))) != 0
}

func (b *cgoBitWriter) WriteUnaryUnsigned(val uint32) bool {
	return C.FLAC__bitwriter_write_unary_unsigned(b.bw, C.uint32_t(val)) != 0
}

func (b *cgoBitWriter) WriteRiceSignedBlock(vals []int32, parameter uint32) bool {
	if len(vals) == 0 {
		return true
	}
	return C.FLAC__bitwriter_write_rice_signed_block(b.bw, (*C.FLAC__int32)(unsafe.Pointer(&vals[0])), C.uint32_t(len(vals)), C.uint32_t(parameter)) != 0
}

func (b *cgoBitWriter) WriteUTF8Uint32(val uint32) bool {
	return C.FLAC__bitwriter_write_utf8_uint32(b.bw, C.uint32_t(val)) != 0
}

func (b *cgoBitWriter) WriteUTF8Uint64(val uint64) bool {
	return C.FLAC__bitwriter_write_utf8_uint64(b.bw, C.uint64_t(val)) != 0
}

func (b *cgoBitWriter) ZeroPadToByteBoundary() bool {
	return C.FLAC__bitwriter_zero_pad_to_byte_boundary(b.bw) != 0
}

func (b *cgoBitWriter) IsByteAligned() bool {
	return C.FLAC__bitwriter_is_byte_aligned(b.bw) != 0
}

func (b *cgoBitWriter) GetInputBitsUnconsumed() uint32 {
	return uint32(C.FLAC__bitwriter_get_input_bits_unconsumed(b.bw))
}

// GetBuffer flushes any partial-but-aligned bits and returns a copy of
// the emitted byte stream.
func (b *cgoBitWriter) GetBuffer() ([]byte, bool) {
	var buf *C.FLAC__byte
	var n C.size_t
	if C.FLAC__bitwriter_get_buffer(b.bw, &buf, &n) == 0 {
		return nil, false
	}
	out := C.GoBytes(unsafe.Pointer(buf), C.int(n))
	C.FLAC__bitwriter_release_buffer(b.bw)
	return out, true
}

func (b *cgoBitWriter) GetWriteCRC16() (uint16, bool) {
	var crc C.FLAC__uint16
	ok := C.FLAC__bitwriter_get_write_crc16(b.bw, &crc) != 0
	return uint16(crc), ok
}

func (b *cgoBitWriter) GetWriteCRC8() (byte, bool) {
	var crc C.FLAC__byte
	ok := C.FLAC__bitwriter_get_write_crc8(b.bw, &crc) != 0
	return byte(crc), ok
}
