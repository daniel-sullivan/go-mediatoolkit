//go:build cgo

// Package channel pins the Go ports of undo_channel_coding and the
// read_frame_ frame-footer CRC-16 verification against equivalent C
// logic built on libFLAC's primitives.
//
// undo_channel_coding is static in stream_decoder.c, so the oracle
// re-implements its body line-for-line on plain C arrays (the parity
// strategy used throughout parity_tests/). The footer CRC path drives
// libFLAC's real bitreader CRC plumbing (FLAC__bitreader_reset_read_crc16
// / FLAC__bitreader_get_read_crc16) over a bitwriter-fabricated buffer.
package channel

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libflac
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/include
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/src/libFLAC/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable -Wno-static-in-inline

#include <stdint.h>
#include <stdlib.h>
#include "private/bitreader.h"

extern FLAC__bool fparity_ch_read_cb(FLAC__byte buf[], size_t *bytes, void *cd);

extern void fparity_undo_channel_coding(int assignment,
                                        int32_t *output0,
                                        int32_t *output1,
                                        const int64_t *side,
                                        int side_in_use,
                                        uint32_t blocksize);

extern size_t fparity_build_footer(uint8_t *out, size_t out_cap,
                                    const uint8_t *payload, size_t payload_len,
                                    uint8_t warmup0, uint8_t warmup1,
                                    int corrupt);

extern int fparity_verify_footer(FLAC__BitReader *br,
                                  uint8_t warmup0, uint8_t warmup1,
                                  size_t payload_len,
                                  int *out_match);
*/
import "C"

import (
	"unsafe"
)

// Source registry — same shape used by the subframe/bitreader parity
// packages.
type cgoSource struct {
	source []byte
	off    int
}

var (
	sources               = make(map[C.uintptr_t]*cgoSource)
	nextSrcID C.uintptr_t = 1
)

func registerSource(s *cgoSource) C.uintptr_t {
	id := nextSrcID
	nextSrcID++
	sources[id] = s
	return id
}

func unregisterSource(id C.uintptr_t) { delete(sources, id) }

//export goChannelRead
func goChannelRead(buf *C.uchar, bytes *C.size_t, clientData unsafe.Pointer) C.int {
	id := C.uintptr_t(uintptr(clientData))
	src, ok := sources[id]
	if !ok {
		return 0
	}
	want := int(*bytes)
	avail := len(src.source) - src.off
	if avail <= 0 {
		*bytes = 0
		return 0
	}
	n := want
	if n > avail {
		n = avail
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(buf)), n)
	copy(dst, src.source[src.off:src.off+n])
	src.off += n
	*bytes = C.size_t(n)
	return 1
}

// CgoUndoChannelCoding runs libFLAC's undo_channel_coding body
// (re-implemented in the oracle TU) over the supplied buffers in place
// and returns the mutated channel buffers.
func CgoUndoChannelCoding(assignment int, output0, output1 []int32, side []int64, sideInUse bool, blocksize uint32) (out0, out1 []int32) {
	o0 := append([]int32(nil), output0...)
	o1 := append([]int32(nil), output1...)
	var sp *C.int64_t
	if len(side) > 0 {
		sp = (*C.int64_t)(unsafe.Pointer(&side[0]))
	}
	siu := C.int(0)
	if sideInUse {
		siu = 1
	}
	var p0, p1 *C.int32_t
	if len(o0) > 0 {
		p0 = (*C.int32_t)(unsafe.Pointer(&o0[0]))
	}
	if len(o1) > 0 {
		p1 = (*C.int32_t)(unsafe.Pointer(&o1[0]))
	}
	C.fparity_undo_channel_coding(C.int(assignment), p0, p1, sp, siu, C.uint32_t(blocksize))
	return o0, o1
}

// BuildFooter fabricates a frame body: `payload` arbitrary bytes
// followed by a 16-bit CRC computed over (warmup0, warmup1, payload).
// When corrupt is true the stored CRC is flipped so verification must
// fail.
func BuildFooter(payload []byte, warmup0, warmup1 byte, corrupt bool) []byte {
	buf := make([]byte, len(payload)+8)
	var pp *C.uint8_t
	if len(payload) > 0 {
		pp = (*C.uint8_t)(unsafe.Pointer(&payload[0]))
	}
	c := C.int(0)
	if corrupt {
		c = 1
	}
	n := C.fparity_build_footer(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)),
		pp, C.size_t(len(payload)),
		C.uint8_t(warmup0), C.uint8_t(warmup1), c)
	return buf[:n]
}

// CgoVerifyFooter drives libFLAC's bitreader CRC plumbing exactly as
// read_frame_ does: seed with the two warmup bytes, consume payloadLen
// payload bytes, then read + compare the 16-bit footer. Returns
// (match, status) where status==0 means the reads succeeded.
func CgoVerifyFooter(body []byte, warmup0, warmup1 byte, payloadLen int) (match bool, status int) {
	src := &cgoSource{source: body}
	id := registerSource(src)
	defer unregisterSource(id)

	br := C.FLAC__bitreader_new()
	defer func() {
		C.FLAC__bitreader_free(br)
		C.FLAC__bitreader_delete(br)
	}()
	C.FLAC__bitreader_init(br, (C.FLAC__BitReaderReadCallback)(C.fparity_ch_read_cb), unsafe.Pointer(uintptr(id)))

	var m C.int
	st := C.fparity_verify_footer(br, C.uint8_t(warmup0), C.uint8_t(warmup1), C.size_t(payloadLen), &m)
	return m != 0, int(st)
}
