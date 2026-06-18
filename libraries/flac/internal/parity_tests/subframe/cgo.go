//go:build cgo

// Package subframe pins the Go ports of read_subframe_constant_,
// read_subframe_verbatim_, and read_residual_partitioned_rice_
// against equivalent C parsers built on libFLAC's bitreader. Test
// inputs are generated via libFLAC's own bitwriter so the bit
// layout exactly matches what stream_decoder.c expects.
package subframe

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

extern FLAC__bool fparity_sf_read_cb(FLAC__byte buf[], size_t *bytes, void *cd);

extern size_t fparity_encode_residual(uint8_t *out, size_t out_cap,
                                       uint32_t predictor_order,
                                       uint32_t partition_order,
                                       uint32_t blocksize,
                                       int is_extended,
                                       const int32_t *residual,
                                       const uint32_t *rice_params);
extern size_t fparity_encode_constant(uint8_t *out, size_t out_cap, int64_t value, uint32_t bps);
extern size_t fparity_encode_verbatim(uint8_t *out, size_t out_cap, const int32_t *samples,
                                       uint32_t blocksize, uint32_t bps);

extern int fparity_decode_constant(FLAC__BitReader *br, uint32_t bps, int64_t *out_value);
extern int fparity_decode_verbatim_int32(FLAC__BitReader *br, uint32_t blocksize, uint32_t bps, int32_t *out);
extern int fparity_decode_verbatim_int64(FLAC__BitReader *br, uint32_t blocksize, uint32_t bps, int64_t *out);
extern int fparity_decode_residual(FLAC__BitReader *br,
                                    uint32_t predictor_order,
                                    uint32_t partition_order,
                                    uint32_t blocksize,
                                    int is_extended,
                                    int32_t *residual,
                                    uint32_t *parameters,
                                    uint32_t *raw_bits);

extern size_t fparity_encode_subframe_fixed(uint8_t *out, size_t out_cap,
                                             uint32_t blocksize, uint32_t bps, uint32_t order,
                                             const int64_t *warmup,
                                             uint32_t partition_order,
                                             int is_extended,
                                             const int32_t *residual,
                                             const uint32_t *rice_params);
extern size_t fparity_encode_subframe_lpc(uint8_t *out, size_t out_cap,
                                           uint32_t blocksize, uint32_t bps, uint32_t order,
                                           const int64_t *warmup,
                                           uint32_t qlp_coeff_precision,
                                           int qlp_shift,
                                           const int32_t *qlp_coeff,
                                           uint32_t partition_order,
                                           int is_extended,
                                           const int32_t *residual,
                                           const uint32_t *rice_params);
*/
import "C"

import (
	"unsafe"
)

// Source registry — same shape used by the bitreader and frameheader
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

//export goSubframeRead
func goSubframeRead(buf *C.uchar, bytes *C.size_t, clientData unsafe.Pointer) C.int {
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

// EncodeResidual uses libFLAC's bitwriter to emit a partitioned-rice
// residual block matching stream_decoder.c's expectations.
func EncodeResidual(predictorOrder, partitionOrder, blocksize uint32, isExtended bool,
	residual []int32, riceParams []uint32) []byte {
	buf := make([]byte, 1<<20) // generous; we trim
	ext := C.int(0)
	if isExtended {
		ext = 1
	}
	var resPtr *C.int32_t
	if len(residual) > 0 {
		resPtr = (*C.int32_t)(unsafe.Pointer(&residual[0]))
	}
	var rpPtr *C.uint32_t
	if len(riceParams) > 0 {
		rpPtr = (*C.uint32_t)(unsafe.Pointer(&riceParams[0]))
	}
	n := C.fparity_encode_residual(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])),
		C.size_t(len(buf)),
		C.uint32_t(predictorOrder),
		C.uint32_t(partitionOrder),
		C.uint32_t(blocksize),
		ext, resPtr, rpPtr)
	return buf[:n]
}

func EncodeConstant(value int64, bps uint32) []byte {
	buf := make([]byte, 16)
	n := C.fparity_encode_constant((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)),
		C.int64_t(value), C.uint32_t(bps))
	return buf[:n]
}

func EncodeVerbatim(samples []int32, blocksize, bps uint32) []byte {
	buf := make([]byte, blocksize*((bps+7)/8)+16)
	var sp *C.int32_t
	if len(samples) > 0 {
		sp = (*C.int32_t)(unsafe.Pointer(&samples[0]))
	}
	n := C.fparity_encode_verbatim((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)),
		sp, C.uint32_t(blocksize), C.uint32_t(bps))
	return buf[:n]
}

// EncodeSubframeFixed builds a complete FIXED subframe body — warm-up
// + entropy header + residual.
func EncodeSubframeFixed(blocksize, bps, order uint32, warmup []int64,
	partitionOrder uint32, isExtended bool, residual []int32, riceParams []uint32) []byte {
	buf := make([]byte, 1<<20)
	ext := C.int(0)
	if isExtended {
		ext = 1
	}
	var wp *C.int64_t
	if len(warmup) > 0 {
		wp = (*C.int64_t)(unsafe.Pointer(&warmup[0]))
	}
	var rp *C.int32_t
	if len(residual) > 0 {
		rp = (*C.int32_t)(unsafe.Pointer(&residual[0]))
	}
	var pp *C.uint32_t
	if len(riceParams) > 0 {
		pp = (*C.uint32_t)(unsafe.Pointer(&riceParams[0]))
	}
	n := C.fparity_encode_subframe_fixed(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)),
		C.uint32_t(blocksize), C.uint32_t(bps), C.uint32_t(order),
		wp, C.uint32_t(partitionOrder), ext, rp, pp)
	return buf[:n]
}

// EncodeSubframeLPC builds a complete LPC subframe body.
func EncodeSubframeLPC(blocksize, bps, order uint32, warmup []int64,
	qlpCoeffPrecision uint32, qlpShift int, qlpCoeff []int32,
	partitionOrder uint32, isExtended bool, residual []int32, riceParams []uint32) []byte {
	buf := make([]byte, 1<<20)
	ext := C.int(0)
	if isExtended {
		ext = 1
	}
	var wp *C.int64_t
	if len(warmup) > 0 {
		wp = (*C.int64_t)(unsafe.Pointer(&warmup[0]))
	}
	var qp *C.int32_t
	if len(qlpCoeff) > 0 {
		qp = (*C.int32_t)(unsafe.Pointer(&qlpCoeff[0]))
	}
	var rp *C.int32_t
	if len(residual) > 0 {
		rp = (*C.int32_t)(unsafe.Pointer(&residual[0]))
	}
	var pp *C.uint32_t
	if len(riceParams) > 0 {
		pp = (*C.uint32_t)(unsafe.Pointer(&riceParams[0]))
	}
	n := C.fparity_encode_subframe_lpc(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)),
		C.uint32_t(blocksize), C.uint32_t(bps), C.uint32_t(order), wp,
		C.uint32_t(qlpCoeffPrecision), C.int(qlpShift), qp,
		C.uint32_t(partitionOrder), ext, rp, pp)
	return buf[:n]
}

// CgoDecodeConstant runs the C-side parity reader; returns (value, status).
func CgoDecodeConstant(body []byte, bps uint32) (int64, int) {
	src := &cgoSource{source: body}
	id := registerSource(src)
	defer unregisterSource(id)

	br := C.FLAC__bitreader_new()
	defer func() {
		C.FLAC__bitreader_free(br)
		C.FLAC__bitreader_delete(br)
	}()
	C.FLAC__bitreader_init(br, (C.FLAC__BitReaderReadCallback)(C.fparity_sf_read_cb), unsafe.Pointer(uintptr(id)))

	var v C.int64_t
	st := C.fparity_decode_constant(br, C.uint32_t(bps), &v)
	return int64(v), int(st)
}

func CgoDecodeVerbatim32(body []byte, blocksize, bps uint32) ([]int32, int) {
	src := &cgoSource{source: body}
	id := registerSource(src)
	defer unregisterSource(id)

	br := C.FLAC__bitreader_new()
	defer func() {
		C.FLAC__bitreader_free(br)
		C.FLAC__bitreader_delete(br)
	}()
	C.FLAC__bitreader_init(br, (C.FLAC__BitReaderReadCallback)(C.fparity_sf_read_cb), unsafe.Pointer(uintptr(id)))

	out := make([]int32, blocksize)
	st := C.fparity_decode_verbatim_int32(br, C.uint32_t(blocksize), C.uint32_t(bps),
		(*C.int32_t)(unsafe.Pointer(&out[0])))
	return out, int(st)
}

func CgoDecodeResidual(body []byte, predictorOrder, partitionOrder, blocksize uint32, isExtended bool) (residual []int32, params []uint32, rawBits []uint32, status int) {
	src := &cgoSource{source: body}
	id := registerSource(src)
	defer unregisterSource(id)

	br := C.FLAC__bitreader_new()
	defer func() {
		C.FLAC__bitreader_free(br)
		C.FLAC__bitreader_delete(br)
	}()
	C.FLAC__bitreader_init(br, (C.FLAC__BitReaderReadCallback)(C.fparity_sf_read_cb), unsafe.Pointer(uintptr(id)))

	residual = make([]int32, blocksize-predictorOrder)
	partitions := uint32(1) << partitionOrder
	if partitions < 64 {
		partitions = 64
	}
	params = make([]uint32, partitions)
	rawBits = make([]uint32, partitions)
	ext := C.int(0)
	if isExtended {
		ext = 1
	}
	var resPtr *C.int32_t
	if len(residual) > 0 {
		resPtr = (*C.int32_t)(unsafe.Pointer(&residual[0]))
	}
	st := C.fparity_decode_residual(br,
		C.uint32_t(predictorOrder),
		C.uint32_t(partitionOrder),
		C.uint32_t(blocksize),
		ext,
		resPtr,
		(*C.uint32_t)(unsafe.Pointer(&params[0])),
		(*C.uint32_t)(unsafe.Pointer(&rawBits[0])))
	return residual, params, rawBits, int(st)
}
