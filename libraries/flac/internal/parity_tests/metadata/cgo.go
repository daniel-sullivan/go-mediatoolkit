//go:build cgo

// Package metadata pins the Go ports of find_metadata_,
// skip_id3v2_tag_, read_metadata_, and read_metadata_streaminfo_
// against the equivalent static stream_decoder.c logic, re-implemented
// here on libFLAC's PUBLIC bitreader API. Test inputs are fabricated
// with libFLAC's own bitwriter so the bit layout exactly matches what
// stream_decoder.c expects, and both decoders are driven over the same
// bytes. Parity asserts identical parsed fields AND identical post-read
// stream position.
package metadata

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

extern FLAC__bool fparity_md_read_cb(FLAC__byte buf[], size_t *bytes, void *cd);

// fparity_md_init wraps FLAC__bitreader_init so the integer handle id
// crosses the cgo boundary as a uintptr_t (not unsafe.Pointer of an
// integer, which trips -d=checkptr). The C side casts it to void*.
static inline FLAC__bool fparity_md_init(FLAC__BitReader *br, FLAC__BitReaderReadCallback rcb, uintptr_t id) {
	return FLAC__bitreader_init(br, rcb, (void *)id);
}

extern size_t fparity_encode_streaminfo(uint8_t *out, size_t out_cap,
                                        int is_last, uint32_t type,
                                        uint32_t min_blocksize, uint32_t max_blocksize,
                                        uint32_t min_framesize, uint32_t max_framesize,
                                        uint32_t sample_rate, uint32_t channels,
                                        uint32_t bits_per_sample, uint64_t total_samples,
                                        const uint8_t md5[16], uint32_t extra_pad);
extern size_t fparity_encode_generic(uint8_t *out, size_t out_cap,
                                      int is_last, uint32_t type, uint32_t length,
                                      const uint8_t *body);
extern size_t fparity_make_prefix(uint8_t *out, size_t out_cap, const uint8_t *bytes, size_t n);

extern int fparity_find_metadata(FLAC__BitReader *br,
                                 int *cached, uint8_t *lookahead,
                                 uint8_t warmup[2], int *lost_sync);
extern int fparity_read_metadata(FLAC__BitReader *br,
                                 int *out_is_last, uint32_t *out_type, uint32_t *out_length,
                                 int *has_stream_info,
                                 uint32_t *min_bs, uint32_t *max_bs,
                                 uint32_t *min_fs, uint32_t *max_fs,
                                 uint32_t *sr, uint32_t *ch, uint32_t *bps,
                                 uint64_t *total, uint8_t md5[16], int *md5_is_zero);
extern uint32_t fparity_bytes_consumed(FLAC__BitReader *br, uint32_t total_bytes);
*/
import "C"

import (
	"unsafe"
)

// Source registry — same shape used by the subframe / bitreader
// parity packages.
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

//export goMetadataRead
func goMetadataRead(buf *C.uchar, bytes *C.size_t, clientData unsafe.Pointer) C.int {
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

// EncodeStreamInfo fabricates a complete STREAMINFO metadata block
// (32-bit header + 34-byte body + extraPad trailing bytes) using
// libFLAC's bitwriter.
func EncodeStreamInfo(isLast bool, typ uint32,
	minBS, maxBS, minFS, maxFS, sampleRate, channels, bps uint32,
	totalSamples uint64, md5 [16]byte, extraPad uint32) []byte {
	buf := make([]byte, 64+int(extraPad))
	il := C.int(0)
	if isLast {
		il = 1
	}
	n := C.fparity_encode_streaminfo(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)),
		il, C.uint32_t(typ),
		C.uint32_t(minBS), C.uint32_t(maxBS), C.uint32_t(minFS), C.uint32_t(maxFS),
		C.uint32_t(sampleRate), C.uint32_t(channels), C.uint32_t(bps),
		C.uint64_t(totalSamples),
		(*C.uint8_t)(unsafe.Pointer(&md5[0])), C.uint32_t(extraPad))
	return buf[:n]
}

// EncodeGeneric fabricates a non-STREAMINFO metadata block: 32-bit
// header plus `length` body bytes (body may be nil for zero-fill).
func EncodeGeneric(isLast bool, typ, length uint32, body []byte) []byte {
	buf := make([]byte, 4+int(length)+8)
	il := C.int(0)
	if isLast {
		il = 1
	}
	var bp *C.uint8_t
	if len(body) > 0 {
		bp = (*C.uint8_t)(unsafe.Pointer(&body[0]))
	}
	n := C.fparity_encode_generic(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)),
		il, C.uint32_t(typ), C.uint32_t(length), bp)
	return buf[:n]
}

// CgoFindMetadata runs the C oracle find_metadata_ over body, with the
// given initial cache state. Returns the status plus the post-call
// cache/warmup/lost-sync state and bytes consumed.
func CgoFindMetadata(body []byte, cached bool, lookahead byte) (status int,
	outCached bool, outLookahead byte, warmup [2]byte, lostSync bool, consumed uint32) {
	src := &cgoSource{source: body}
	id := registerSource(src)
	defer unregisterSource(id)

	br := C.FLAC__bitreader_new()
	defer func() {
		C.FLAC__bitreader_free(br)
		C.FLAC__bitreader_delete(br)
	}()
	C.fparity_md_init(br, (C.FLAC__BitReaderReadCallback)(C.fparity_md_read_cb), C.uintptr_t(id))

	cCached := C.int(0)
	if cached {
		cCached = 1
	}
	cLook := C.uint8_t(lookahead)
	var cWarm [2]C.uint8_t
	var cLost C.int
	st := C.fparity_find_metadata(br, &cCached, &cLook, &cWarm[0], &cLost)
	consumed = uint32(C.fparity_bytes_consumed(br, C.uint32_t(len(body))))
	return int(st), cCached != 0, byte(cLook),
		[2]byte{byte(cWarm[0]), byte(cWarm[1])}, cLost != 0, consumed
}

// CgoStreamInfo holds the STREAMINFO fields plus header that the C
// oracle read_metadata_ parsed.
type CgoStreamInfo struct {
	IsLast        bool
	Type          uint32
	Length        uint32
	HasStreamInfo bool
	MinBlockSize  uint32
	MaxBlockSize  uint32
	MinFrameSize  uint32
	MaxFrameSize  uint32
	SampleRate    uint32
	Channels      uint32
	BitsPerSample uint32
	TotalSamples  uint64
	MD5Sum        [16]byte
	MD5IsZero     bool
}

// CgoReadMetadata runs the C oracle read_metadata_ over body and
// returns the parsed result, status, and bytes consumed.
func CgoReadMetadata(body []byte) (info CgoStreamInfo, status int, consumed uint32) {
	src := &cgoSource{source: body}
	id := registerSource(src)
	defer unregisterSource(id)

	br := C.FLAC__bitreader_new()
	defer func() {
		C.FLAC__bitreader_free(br)
		C.FLAC__bitreader_delete(br)
	}()
	C.fparity_md_init(br, (C.FLAC__BitReaderReadCallback)(C.fparity_md_read_cb), C.uintptr_t(id))

	var (
		isLast, hasSI, md5Zero     C.int
		typ, length                C.uint32_t
		minBS, maxBS, minFS, maxFS C.uint32_t
		sr, ch, bps                C.uint32_t
		total                      C.uint64_t
		md5                        [16]C.uint8_t
	)
	st := C.fparity_read_metadata(br,
		&isLast, &typ, &length, &hasSI,
		&minBS, &maxBS, &minFS, &maxFS, &sr, &ch, &bps, &total, &md5[0], &md5Zero)
	consumed = uint32(C.fparity_bytes_consumed(br, C.uint32_t(len(body))))

	info = CgoStreamInfo{
		IsLast:        isLast != 0,
		Type:          uint32(typ),
		Length:        uint32(length),
		HasStreamInfo: hasSI != 0,
		MinBlockSize:  uint32(minBS),
		MaxBlockSize:  uint32(maxBS),
		MinFrameSize:  uint32(minFS),
		MaxFrameSize:  uint32(maxFS),
		SampleRate:    uint32(sr),
		Channels:      uint32(ch),
		BitsPerSample: uint32(bps),
		TotalSamples:  uint64(total),
		MD5IsZero:     md5Zero != 0,
	}
	for i := 0; i < 16; i++ {
		info.MD5Sum[i] = byte(md5[i])
	}
	return info, int(st), consumed
}
