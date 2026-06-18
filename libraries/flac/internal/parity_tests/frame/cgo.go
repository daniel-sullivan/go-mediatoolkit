//go:build cgo

// Package frame pins the Go ports of read_subframe_ (the subframe-type
// dispatch + wasted-bits shift) and read_frame_ (header + per-channel
// subframe loop + undo_channel_coding + footer CRC-16) against libFLAC.
//
// Whole frames are fabricated with libFLAC's OWN framing writers
// (FLAC__frame_add_header / FLAC__subframe_add_*) plus the bitwriter's
// CRC-16 tracker for the footer, so every byte — including the CRC-8
// header trailer and the CRC-16 footer — is produced by libFLAC itself.
// The frame is then decoded by a faithful C re-implementation of
// read_frame_ (the real one is static) and by the Go port; both must
// yield identical interleaved samples and identical CRC acceptance.
package frame

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
    int      type;
    uint32_t subframe_bps;
    uint32_t wasted_bits;
    int64_t  constant_value;
    uint32_t order;
    int64_t  warmup[32];
    uint32_t qlp_coeff_precision;
    int      quantization_level;
    int32_t  qlp_coeff[32];
    uint32_t partition_order;
    int      is_extended;
    const int32_t  *residual;
    const uint32_t *rice_params;
    const int32_t  *verbatim;
} fparity_sf_desc_t;

extern FLAC__bool fparity_frame_read_cb(FLAC__byte buf[], size_t *bytes, void *cd);

// fparity_frame_init wraps FLAC__bitreader_init so the integer handle id
// crosses the cgo boundary as a uintptr_t (not unsafe.Pointer of an
// integer, which trips -d=checkptr). The C side casts it to void*.
static inline FLAC__bool fparity_frame_init(FLAC__BitReader *br, FLAC__BitReaderReadCallback rcb, uintptr_t id) {
	return FLAC__bitreader_init(br, rcb, (void *)id);
}

extern size_t fparity_assemble_frame(uint8_t *out, size_t out_cap,
                                     uint32_t blocksize, uint32_t sample_rate,
                                     uint32_t channels, uint32_t bits_per_sample,
                                     uint32_t channel_assignment,
                                     uint64_t sample_number,
                                     const fparity_sf_desc_t *subframes);

extern int fparity_decode_frame(FLAC__BitReader *br,
                                uint8_t hdr0, uint8_t hdr1,
                                uint32_t si_sr, uint32_t si_bps,
                                uint32_t si_minbs, uint32_t si_maxbs,
                                int32_t *interleaved,
                                uint32_t *out_blocksize, uint32_t *out_channels,
                                uint32_t *out_bps, uint32_t *out_channel_assignment,
                                uint64_t *out_sample_number);
*/
import "C"

import (
	"unsafe"
)

// Source registry — same shape used by the sibling parity packages.
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

//export goFrameRead
func goFrameRead(buf *C.uchar, bytes *C.size_t, clientData unsafe.Pointer) C.int {
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

// SubframeDesc describes one subframe to assemble into a frame. Mirrors
// the C fparity_sf_desc_t; the slices must outlive AssembleFrame.
type SubframeDesc struct {
	Type              int // 0=CONSTANT, 1=VERBATIM, 2=FIXED, 3=LPC
	SubframeBPS       uint32
	WastedBits        uint32
	ConstantValue     int64
	Order             uint32
	Warmup            []int64
	QLPCoeffPrecision uint32
	QuantizationLevel int
	QLPCoeff          []int32
	PartitionOrder    uint32
	IsExtended        bool
	Residual          []int32
	RiceParams        []uint32
	Verbatim          []int32
}

// AssembleFrame fabricates a complete FLAC frame (header + subframes +
// footer CRC-16) via libFLAC's own writers. Returns the byte buffer.
func AssembleFrame(blocksize, sampleRate, channels, bitsPerSample, channelAssignment uint32,
	sampleNumber uint64, subs []SubframeDesc) []byte {
	// The descriptor array and every slice it points at must live in C
	// memory: cgo forbids passing a Go pointer that itself contains Go
	// pointers. Allocate the descs and the residual/rice/verbatim arrays
	// with C.malloc and copy the Go data in, freeing everything afterwards.
	descSize := C.size_t(unsafe.Sizeof(C.fparity_sf_desc_t{}))
	descMem := C.malloc(descSize * C.size_t(len(subs)))
	defer C.free(descMem)
	descs := unsafe.Slice((*C.fparity_sf_desc_t)(descMem), len(subs))

	var toFree []unsafe.Pointer
	defer func() {
		for _, p := range toFree {
			C.free(p)
		}
	}()
	cInt32Array := func(src []int32) *C.int32_t {
		if len(src) == 0 {
			return nil
		}
		p := C.malloc(C.size_t(len(src)) * C.size_t(unsafe.Sizeof(C.int32_t(0))))
		toFree = append(toFree, p)
		dst := unsafe.Slice((*C.int32_t)(p), len(src))
		for i, v := range src {
			dst[i] = C.int32_t(v)
		}
		return (*C.int32_t)(p)
	}
	cUint32Array := func(src []uint32) *C.uint32_t {
		if len(src) == 0 {
			return nil
		}
		p := C.malloc(C.size_t(len(src)) * C.size_t(unsafe.Sizeof(C.uint32_t(0))))
		toFree = append(toFree, p)
		dst := unsafe.Slice((*C.uint32_t)(p), len(src))
		for i, v := range src {
			dst[i] = C.uint32_t(v)
		}
		return (*C.uint32_t)(p)
	}

	for i := range subs {
		s := &subs[i]
		d := &descs[i]
		*d = C.fparity_sf_desc_t{}
		d._type = C.int(s.Type)
		d.subframe_bps = C.uint32_t(s.SubframeBPS)
		d.wasted_bits = C.uint32_t(s.WastedBits)
		d.constant_value = C.int64_t(s.ConstantValue)
		d.order = C.uint32_t(s.Order)
		for j := 0; j < int(s.Order) && j < len(s.Warmup); j++ {
			d.warmup[j] = C.int64_t(s.Warmup[j])
		}
		d.qlp_coeff_precision = C.uint32_t(s.QLPCoeffPrecision)
		d.quantization_level = C.int(s.QuantizationLevel)
		for j := 0; j < int(s.Order) && j < len(s.QLPCoeff); j++ {
			d.qlp_coeff[j] = C.int32_t(s.QLPCoeff[j])
		}
		d.partition_order = C.uint32_t(s.PartitionOrder)
		if s.IsExtended {
			d.is_extended = 1
		}
		d.residual = cInt32Array(s.Residual)
		d.rice_params = cUint32Array(s.RiceParams)
		d.verbatim = cInt32Array(s.Verbatim)
	}

	buf := make([]byte, 1<<21)
	n := C.fparity_assemble_frame(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)),
		C.uint32_t(blocksize), C.uint32_t(sampleRate), C.uint32_t(channels),
		C.uint32_t(bitsPerSample), C.uint32_t(channelAssignment),
		C.uint64_t(sampleNumber), (*C.fparity_sf_desc_t)(descMem))
	return buf[:n]
}

// CgoDecodeFrame runs the faithful C read_frame_ oracle over body and
// returns the decoded interleaved samples plus the parsed header fields
// and status (0=OK, 1=READ_ERROR, 2=LOST_SYNC, 3=BAD_HEADER). hdr0/hdr1
// are the two header-warmup bytes (which the caller skips before handing
// the rest of body to the bitreader — matching frame_sync_).
func CgoDecodeFrame(body []byte, siSR, siBPS, siMinBS, siMaxBS uint32) (
	interleaved []int32, blocksize, channels, bps, channelAssignment uint32,
	sampleNumber uint64, status int) {

	hdr0 := body[0]
	hdr1 := body[1]
	// The bitreader is fed the bytes AFTER the two warmup bytes, exactly
	// as stream_decoder.c does (frame_sync_ has consumed them).
	src := &cgoSource{source: body[2:]}
	id := registerSource(src)
	defer unregisterSource(id)

	br := C.FLAC__bitreader_new()
	defer func() {
		C.FLAC__bitreader_free(br)
		C.FLAC__bitreader_delete(br)
	}()
	C.fparity_frame_init(br, (C.FLAC__BitReaderReadCallback)(C.fparity_frame_read_cb), C.uintptr_t(id))

	out := make([]int32, MaxBlocksize*MaxChannels)
	var cBS, cCh, cBps, cCa C.uint32_t
	var cSN C.uint64_t
	st := C.fparity_decode_frame(br, C.uint8_t(hdr0), C.uint8_t(hdr1),
		C.uint32_t(siSR), C.uint32_t(siBPS), C.uint32_t(siMinBS), C.uint32_t(siMaxBS),
		(*C.int32_t)(unsafe.Pointer(&out[0])),
		&cBS, &cCh, &cBps, &cCa, &cSN)

	blocksize = uint32(cBS)
	channels = uint32(cCh)
	bps = uint32(cBps)
	channelAssignment = uint32(cCa)
	sampleNumber = uint64(cSN)
	if int(st) == 0 {
		interleaved = out[:blocksize*channels]
	}
	return interleaved, blocksize, channels, bps, channelAssignment, sampleNumber, int(st)
}

// MaxBlocksize / MaxChannels mirror the FLAC limits for buffer sizing.
const (
	MaxBlocksize = 65535
	MaxChannels  = 8
)
