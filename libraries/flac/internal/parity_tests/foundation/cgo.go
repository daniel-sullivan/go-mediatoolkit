//go:build cgo

// Package foundation contains parity tests that compare the foundational
// helpers of the pure-Go libFLAC port (libraries/flac/internal/nativeflac)
// against the vendored libFLAC C reference. These tests gate at the
// foundation level — once they pass, downstream ports (bitreader,
// stream_decoder, …) can rely on the foundational primitives matching
// bit-for-bit.
//
// The C side links against the same vendored libFLAC the production
// libraries/flac path uses, configured for the scalar baseline (no
// SIMD intrinsics) — see libraries/flac/libflac/config.h.
package foundation

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
// Resolve include paths to the vendored libFLAC tree at
// libraries/flac/libflac. foundation_cgo_src.c includes the .c files
// via "src/libFLAC/*.c" so the SRCDIR-relative root path needs to land
// inside libflac/.
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
#include "private/crc.h"
#include "private/bitmath.h"
#include "private/md5.h"
#include "private/format.h"
#include "FLAC/format.h"

// Thin wrappers that surface libFLAC internal helpers to Go-cgo with
// stable, simple signatures. Each one is a verbatim call to the
// underlying libFLAC function — the wrapper adds nothing.

static uint8_t  fparity_crc8 (const uint8_t *data, uint32_t len)                 { return FLAC__crc8(data, len); }
static uint16_t fparity_crc16(const uint8_t *data, uint32_t len)                 { return FLAC__crc16(data, len); }
static uint16_t fparity_crc16_words32(const uint32_t *w, uint32_t n, uint16_t c) { return FLAC__crc16_update_words32(w, n, c); }
static uint16_t fparity_crc16_words64(const uint64_t *w, uint32_t n, uint16_t c) { return FLAC__crc16_update_words64(w, n, c); }

static uint32_t fparity_ilog2     (uint32_t v) { return FLAC__bitmath_ilog2(v);          }
static uint32_t fparity_ilog2_wide(uint64_t v) { return FLAC__bitmath_ilog2_wide(v);     }
static uint32_t fparity_silog2    (int64_t  v) { return FLAC__bitmath_silog2(v);          }
static uint32_t fparity_extra_mul (uint32_t v) { return FLAC__bitmath_extra_mulbits_unsigned(v); }

// format.c helpers
static int      fparity_sr_valid     (uint32_t sr) { return FLAC__format_sample_rate_is_valid(sr) ? 1 : 0; }
static int      fparity_blocksize_sub(uint32_t bs, uint32_t sr) { return FLAC__format_blocksize_is_subset(bs, sr) ? 1 : 0; }
static int      fparity_sr_subset    (uint32_t sr) { return FLAC__format_sample_rate_is_subset(sr) ? 1 : 0; }
static uint32_t fparity_max_rice_from_blocksize        (uint32_t bs)                        { return FLAC__format_get_max_rice_partition_order_from_blocksize(bs); }
static uint32_t fparity_max_rice_from_blocksize_limited(uint32_t lim, uint32_t bs, uint32_t po) { return FLAC__format_get_max_rice_partition_order_from_blocksize_limited_max_and_predictor_order(lim, bs, po); }
static int      fparity_vc_name_legal(const char *n)                                        { return FLAC__format_vorbiscomment_entry_name_is_legal(n) ? 1 : 0; }
static int      fparity_vc_value_legal(const uint8_t *v, uint32_t n)                        { return FLAC__format_vorbiscomment_entry_value_is_legal(v, n) ? 1 : 0; }
static int      fparity_vc_entry_legal(const uint8_t *v, uint32_t n)                        { return FLAC__format_vorbiscomment_entry_is_legal(v, n)  ? 1 : 0; }

// MD5 oracle wrappers. The implementations live in
// foundation_cgo_src.c because FLAC__MD5Update is `static` inside
// md5.c — it can only be referenced from a TU that #includes md5.c.
extern FLAC__MD5Context *fparity_md5_new_impl(void);
extern void              fparity_md5_free_impl(FLAC__MD5Context *c);
extern void              fparity_md5_update_impl(FLAC__MD5Context *c, const uint8_t *d, uint32_t n);
extern void              fparity_md5_final_impl(FLAC__MD5Context *c, uint8_t out[16]);
extern int               fparity_md5_accumulate_impl(FLAC__MD5Context *c, const int32_t * const *signal, uint32_t channels, uint32_t samples, uint32_t bytes_per_sample);

static FLAC__MD5Context *fparity_md5_new(void)                                    { return fparity_md5_new_impl(); }
static void              fparity_md5_free(FLAC__MD5Context *c)                    {        fparity_md5_free_impl(c); }
static void              fparity_md5_update(FLAC__MD5Context *c, const uint8_t *d, uint32_t n) { fparity_md5_update_impl(c, d, n); }
static void              fparity_md5_final(FLAC__MD5Context *c, uint8_t out[16])               { fparity_md5_final_impl(c, out); }
static int               fparity_md5_accumulate(FLAC__MD5Context *c, const int32_t * const *signal, uint32_t channels, uint32_t samples, uint32_t bytes_per_sample) {
    return fparity_md5_accumulate_impl(c, signal, channels, samples, bytes_per_sample);
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// cgoCRC8 calls libFLAC's FLAC__crc8 over data.
func cgoCRC8(data []byte) uint8 {
	if len(data) == 0 {
		return uint8(C.fparity_crc8(nil, 0))
	}
	return uint8(C.fparity_crc8((*C.uchar)(unsafe.Pointer(&data[0])), C.uint32_t(len(data))))
}

// cgoCRC16 calls libFLAC's FLAC__crc16 over data.
func cgoCRC16(data []byte) uint16 {
	if len(data) == 0 {
		return uint16(C.fparity_crc16(nil, 0))
	}
	return uint16(C.fparity_crc16((*C.uchar)(unsafe.Pointer(&data[0])), C.uint32_t(len(data))))
}

func cgoCRC16Words32(words []uint32, crc uint16) uint16 {
	if len(words) == 0 {
		return uint16(C.fparity_crc16_words32(nil, 0, C.uint16_t(crc)))
	}
	return uint16(C.fparity_crc16_words32((*C.uint32_t)(unsafe.Pointer(&words[0])), C.uint32_t(len(words)), C.uint16_t(crc)))
}

func cgoCRC16Words64(words []uint64, crc uint16) uint16 {
	if len(words) == 0 {
		return uint16(C.fparity_crc16_words64(nil, 0, C.uint16_t(crc)))
	}
	return uint16(C.fparity_crc16_words64((*C.uint64_t)(unsafe.Pointer(&words[0])), C.uint32_t(len(words)), C.uint16_t(crc)))
}

func cgoILog2(v uint32) uint32     { return uint32(C.fparity_ilog2(C.uint32_t(v))) }
func cgoILog2Wide(v uint64) uint32 { return uint32(C.fparity_ilog2_wide(C.uint64_t(v))) }
func cgoSILog2(v int64) uint32     { return uint32(C.fparity_silog2(C.int64_t(v))) }
func cgoExtraMulbitsUnsigned(v uint32) uint32 {
	return uint32(C.fparity_extra_mul(C.uint32_t(v)))
}

// format.c shims
func cgoFormatSampleRateIsValid(sr uint32) bool { return C.fparity_sr_valid(C.uint32_t(sr)) != 0 }
func cgoFormatBlocksizeIsSubset(bs, sr uint32) bool {
	return C.fparity_blocksize_sub(C.uint32_t(bs), C.uint32_t(sr)) != 0
}
func cgoFormatSampleRateIsSubset(sr uint32) bool {
	return C.fparity_sr_subset(C.uint32_t(sr)) != 0
}
func cgoMaxRicePartitionOrderFromBlocksize(bs uint32) uint32 {
	return uint32(C.fparity_max_rice_from_blocksize(C.uint32_t(bs)))
}
func cgoMaxRicePartitionOrderFromBlocksizeLimited(limit, bs, po uint32) uint32 {
	return uint32(C.fparity_max_rice_from_blocksize_limited(C.uint32_t(limit), C.uint32_t(bs), C.uint32_t(po)))
}
func cgoVCNameLegal(name string) bool {
	cn := C.CString(name)
	defer C.free(unsafe.Pointer(cn))
	return C.fparity_vc_name_legal(cn) != 0
}
func cgoVCValueLegal(value []byte) bool {
	if len(value) == 0 {
		return C.fparity_vc_value_legal(nil, 0) != 0
	}
	return C.fparity_vc_value_legal((*C.uchar)(unsafe.Pointer(&value[0])), C.uint32_t(len(value))) != 0
}
func cgoVCEntryLegal(entry []byte) bool {
	if len(entry) == 0 {
		return C.fparity_vc_entry_legal(nil, 0) != 0
	}
	return C.fparity_vc_entry_legal((*C.uchar)(unsafe.Pointer(&entry[0])), C.uint32_t(len(entry))) != 0
}

// cgoMD5 wraps a C-side FLAC__MD5Context. It is parity-only — never
// used outside tests — and so does no finalizer plumbing.
type cgoMD5 struct {
	ctx *C.FLAC__MD5Context
}

func newCgoMD5() *cgoMD5 { return &cgoMD5{ctx: C.fparity_md5_new()} }
func (m *cgoMD5) Update(data []byte) {
	if len(data) == 0 {
		return
	}
	C.fparity_md5_update(m.ctx, (*C.uchar)(unsafe.Pointer(&data[0])), C.uint32_t(len(data)))
}
func (m *cgoMD5) Final() (digest [16]byte) {
	C.fparity_md5_final(m.ctx, (*C.uchar)(unsafe.Pointer(&digest[0])))
	return digest
}
func (m *cgoMD5) Free() { C.fparity_md5_free(m.ctx); m.ctx = nil }

// cgoMD5Accumulate runs FLAC__MD5Accumulate through a fresh context
// and returns the resulting digest. Per-channel buffers are passed as
// `int32_t * const *` — we materialise the C array of pointers from
// Go-side slices.
func cgoMD5Accumulate(signal [][]int32, channels, samples, bytesPerSample uint32) [16]byte {
	ctx := C.fparity_md5_new()
	defer C.fparity_md5_free(ctx)

	// cgo's pointer-checking rejects Go-pointer-to-Go-pointer, so we
	// pin every backing slice and the pointer array itself before
	// crossing the boundary. This is parity-test-only code; pinning
	// is acceptable.
	var pinner runtime.Pinner
	defer pinner.Unpin()
	ptrs := make([]*C.int32_t, channels)
	for ch := uint32(0); ch < channels; ch++ {
		if len(signal[ch]) == 0 {
			ptrs[ch] = nil
			continue
		}
		pinner.Pin(&signal[ch][0])
		ptrs[ch] = (*C.int32_t)(unsafe.Pointer(&signal[ch][0]))
	}
	var arr **C.int32_t
	if len(ptrs) > 0 {
		pinner.Pin(&ptrs[0])
		arr = (**C.int32_t)(unsafe.Pointer(&ptrs[0]))
	}
	C.fparity_md5_accumulate(ctx, arr, C.uint32_t(channels), C.uint32_t(samples), C.uint32_t(bytesPerSample))

	var digest [16]byte
	C.fparity_md5_final(ctx, (*C.uchar)(unsafe.Pointer(&digest[0])))
	return digest
}
