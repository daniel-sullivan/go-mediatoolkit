//go:build cgo

// Package frameheader pins the Go ReadFrameHeader port against
// libFLAC's identical parser. Because libFLAC's read_frame_header_ is
// a static function inside stream_decoder.c, the cgo side runs an
// equivalent parser implemented in C using the libFLAC bitreader's
// public API (frameheader_cgo_src.c::fparity_read_frame_header). The
// shared bitreader + CRC tables guarantee any divergence is in the
// parse table itself.
package frameheader

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

typedef struct {
    uint32_t blocksize;
    uint32_t sample_rate;
    uint32_t channels;
    uint8_t  channel_assignment;
    uint32_t bits_per_sample;
    uint8_t  number_type;
    uint64_t number;
    uint8_t  crc;
    uint32_t next_fixed_block_size;
    int      status;
} fparity_fh_result_t;

extern FLAC__bool fparity_fh_read_cb(FLAC__byte buf[], size_t *bytes, void *cd);

// fparity_fh_init wraps FLAC__bitreader_init so the integer handle id
// crosses the cgo boundary as a uintptr_t (not unsafe.Pointer of an
// integer, which trips -d=checkptr). The C side casts it to void*.
static inline FLAC__bool fparity_fh_init(FLAC__BitReader *br, FLAC__BitReaderReadCallback rcb, uintptr_t id) {
	return FLAC__bitreader_init(br, rcb, (void *)id);
}

extern void fparity_read_frame_header(FLAC__BitReader *br,
                                       uint8_t hdr0, uint8_t hdr1,
                                       int has_streaminfo,
                                       uint32_t si_sr, uint32_t si_bps,
                                       uint32_t si_minbs, uint32_t si_maxbs,
                                       uint32_t fixed_block_size,
                                       fparity_fh_result_t *out);
*/
import "C"

import (
	"unsafe"
)

// Active bitreaders, indexed by an integer ID stored in client_data.
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

//export goFrameHdrRead
func goFrameHdrRead(buf *C.uchar, bytes *C.size_t, clientData unsafe.Pointer) C.int {
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

// FHResult mirrors fparity_fh_result_t in plain Go.
type FHResult struct {
	Blocksize          uint32
	SampleRate         uint32
	Channels           uint32
	ChannelAssignment  uint8
	BitsPerSample      uint32
	NumberType         uint8
	Number             uint64
	CRC                uint8
	NextFixedBlockSize uint32
	Status             int
}

// CgoReadFrameHeader runs libFLAC's bitreader + the C parity parser on
// `body` (the bytes AFTER the first two header bytes, which are passed
// as hdr0 / hdr1 — matching the header_warmup pattern).
func CgoReadFrameHeader(body []byte, hdr0, hdr1 byte, hasStreamInfo bool, siSR, siBPS, siMinBS, siMaxBS, fixedBS uint32) FHResult {
	src := &cgoSource{source: body}
	id := registerSource(src)
	defer unregisterSource(id)

	br := C.FLAC__bitreader_new()
	defer func() {
		C.FLAC__bitreader_free(br)
		C.FLAC__bitreader_delete(br)
	}()
	C.fparity_fh_init(br, (C.FLAC__BitReaderReadCallback)(C.fparity_fh_read_cb), C.uintptr_t(id))

	hsi := C.int(0)
	if hasStreamInfo {
		hsi = 1
	}
	var out C.fparity_fh_result_t
	C.fparity_read_frame_header(br,
		C.uint8_t(hdr0), C.uint8_t(hdr1),
		hsi,
		C.uint32_t(siSR), C.uint32_t(siBPS),
		C.uint32_t(siMinBS), C.uint32_t(siMaxBS),
		C.uint32_t(fixedBS),
		&out)
	return FHResult{
		Blocksize:          uint32(out.blocksize),
		SampleRate:         uint32(out.sample_rate),
		Channels:           uint32(out.channels),
		ChannelAssignment:  uint8(out.channel_assignment),
		BitsPerSample:      uint32(out.bits_per_sample),
		NumberType:         uint8(out.number_type),
		Number:             uint64(out.number),
		CRC:                uint8(out.crc),
		NextFixedBlockSize: uint32(out.next_fixed_block_size),
		Status:             int(out.status),
	}
}
