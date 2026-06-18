//go:build cgo

// Package bitreader contains parity tests that pin the Go bitreader
// port at libraries/flac/internal/nativeflac against libFLAC's
// FLAC__BitReader. Every reader operation is run on both sides over
// the same byte stream and the (value, ok) outcome is compared.
package bitreader

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libflac
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/include
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/src/libFLAC/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable -Wno-static-in-inline

#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "private/bitreader.h"

// Read callback shim. The Go side stashes a *cgoBitReader pointer in
// client_data and feeds bytes from a Go-owned []byte slab. The
// callback signature has to match FLAC__BitReaderReadCallback exactly.
extern FLAC__bool fparity_br_read_cb(FLAC__byte buf[], size_t *bytes, void *cd);
*/
import "C"

import (
	"unsafe"
)

// cgoBitReader wraps a libFLAC FLAC__BitReader and a Go-owned byte
// slab the read callback drains.
type cgoBitReader struct {
	br     *C.FLAC__BitReader
	source []byte
	off    int
	id     C.uintptr_t // index into the global handle table
}

// We track active cgoBitReaders by an integer ID so the C-side read
// callback can look them up via client_data without dereferencing a
// Go pointer (cgo forbids storing Go pointers in C memory).
var (
	cgoBitReaders             = make(map[C.uintptr_t]*cgoBitReader)
	nextCgoID     C.uintptr_t = 1
)

func newCgoBitReader(source []byte) *cgoBitReader {
	br := &cgoBitReader{
		br:     C.FLAC__bitreader_new(),
		source: source,
		id:     nextCgoID,
	}
	nextCgoID++
	cgoBitReaders[br.id] = br
	C.FLAC__bitreader_init(br.br, (C.FLAC__BitReaderReadCallback)(C.fparity_br_read_cb), unsafe.Pointer(uintptr(br.id)))
	return br
}

func (b *cgoBitReader) free() {
	C.FLAC__bitreader_free(b.br)
	C.FLAC__bitreader_delete(b.br)
	delete(cgoBitReaders, b.id)
}

//export goBitreaderRead
func goBitreaderRead(buf *C.uchar, bytes *C.size_t, clientData unsafe.Pointer) C.int {
	id := C.uintptr_t(uintptr(clientData))
	br, ok := cgoBitReaders[id]
	if !ok {
		return 0
	}
	want := int(*bytes)
	avail := len(br.source) - br.off
	if avail <= 0 {
		// libFLAC interprets *bytes == 0 plus return true as EOF;
		// signal that path so the reader doesn't spin.
		*bytes = 0
		return 0
	}
	n := want
	if n > avail {
		n = avail
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(buf)), n)
	copy(dst, br.source[br.off:br.off+n])
	br.off += n
	*bytes = C.size_t(n)
	return 1
}

func (b *cgoBitReader) ReadRawUint32(bits uint32) (uint32, bool) {
	var v C.uint32_t
	ok := C.FLAC__bitreader_read_raw_uint32(b.br, &v, C.uint32_t(bits)) != 0
	return uint32(v), ok
}

func (b *cgoBitReader) ReadRawInt32(bits uint32) (int32, bool) {
	var v C.int32_t
	ok := C.FLAC__bitreader_read_raw_int32(b.br, &v, C.uint32_t(bits)) != 0
	return int32(v), ok
}

func (b *cgoBitReader) ReadRawUint64(bits uint32) (uint64, bool) {
	var v C.uint64_t
	ok := C.FLAC__bitreader_read_raw_uint64(b.br, &v, C.uint32_t(bits)) != 0
	return uint64(v), ok
}

func (b *cgoBitReader) ReadRawInt64(bits uint32) (int64, bool) {
	var v C.int64_t
	ok := C.FLAC__bitreader_read_raw_int64(b.br, &v, C.uint32_t(bits)) != 0
	return int64(v), ok
}

func (b *cgoBitReader) ReadUint32LittleEndian() (uint32, bool) {
	var v C.uint32_t
	ok := C.FLAC__bitreader_read_uint32_little_endian(b.br, &v) != 0
	return uint32(v), ok
}

func (b *cgoBitReader) ReadUnaryUnsigned() (uint32, bool) {
	var v C.uint32_t
	ok := C.FLAC__bitreader_read_unary_unsigned(b.br, &v) != 0
	return uint32(v), ok
}

func (b *cgoBitReader) ReadRiceSignedBlock(out []int32, parameter uint32) bool {
	if len(out) == 0 {
		return true
	}
	return C.FLAC__bitreader_read_rice_signed_block(b.br, (*C.int)(unsafe.Pointer(&out[0])), C.uint32_t(len(out)), C.uint32_t(parameter)) != 0
}

func (b *cgoBitReader) SkipBitsNoCRC(bits uint32) bool {
	return C.FLAC__bitreader_skip_bits_no_crc(b.br, C.uint32_t(bits)) != 0
}

func (b *cgoBitReader) ReadByteBlockAlignedNoCRC(out []byte) bool {
	if len(out) == 0 {
		return true
	}
	return C.FLAC__bitreader_read_byte_block_aligned_no_crc(b.br, (*C.uchar)(unsafe.Pointer(&out[0])), C.uint32_t(len(out))) != 0
}

func (b *cgoBitReader) ReadUTF8Uint32() (val uint32, rawLen int, ok bool) {
	var v C.uint32_t
	var raw [7]C.uchar
	var rawLenC C.uint32_t
	r := C.FLAC__bitreader_read_utf8_uint32(b.br, &v, &raw[0], &rawLenC) != 0
	return uint32(v), int(rawLenC), r
}

func (b *cgoBitReader) ReadUTF8Uint64() (val uint64, rawLen int, ok bool) {
	var v C.uint64_t
	var raw [7]C.uchar
	var rawLenC C.uint32_t
	r := C.FLAC__bitreader_read_utf8_uint64(b.br, &v, &raw[0], &rawLenC) != 0
	return uint64(v), int(rawLenC), r
}

func (b *cgoBitReader) ResetReadCRC16(seed uint16) {
	C.FLAC__bitreader_reset_read_crc16(b.br, C.uint16_t(seed))
}

func (b *cgoBitReader) GetReadCRC16() uint16 {
	return uint16(C.FLAC__bitreader_get_read_crc16(b.br))
}

func (b *cgoBitReader) IsConsumedByteAligned() bool {
	return C.FLAC__bitreader_is_consumed_byte_aligned(b.br) != 0
}

func (b *cgoBitReader) BitsLeftForByteAlignment() uint32 {
	return uint32(C.FLAC__bitreader_bits_left_for_byte_alignment(b.br))
}
