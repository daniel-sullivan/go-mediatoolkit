//go:build cgo

// Package framing pins the Go port of stream_encoder_framing.c
// (encode_framing.go) against libFLAC's real framing functions. Each
// Cgo* wrapper builds the equivalent C struct from flat arguments and
// frames it through FLAC__add_metadata_block / FLAC__frame_add_header /
// FLAC__subframe_add_*; the test then frames the same object with the
// Go port and asserts byte-identical output.
package framing

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DFLAC__NO_DLL
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../libflac
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/include
#cgo CFLAGS: -I${SRCDIR}/../../../libflac/src/libFLAC/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable -Wno-static-in-inline -Wno-incompatible-pointer-types-discards-qualifiers

#include <stdint.h>
#include <stdlib.h>

extern size_t fparity_frame_header(uint8_t *out, size_t out_cap,
    uint32_t blocksize, uint32_t sample_rate, uint32_t channels,
    uint32_t channel_assignment, uint32_t bits_per_sample, int is_variable,
    uint64_t number);

extern size_t fparity_subframe_constant(uint8_t *out, size_t out_cap,
    int64_t value, uint32_t subframe_bps, uint32_t wasted_bits);
extern size_t fparity_subframe_verbatim32(uint8_t *out, size_t out_cap,
    const int32_t *signal, uint32_t samples, uint32_t subframe_bps, uint32_t wasted_bits);
extern size_t fparity_subframe_verbatim64(uint8_t *out, size_t out_cap,
    const int64_t *signal, uint32_t samples, uint32_t subframe_bps, uint32_t wasted_bits);
extern size_t fparity_subframe_fixed(uint8_t *out, size_t out_cap,
    uint32_t order, uint32_t residual_samples, uint32_t subframe_bps, uint32_t wasted_bits,
    const int64_t *warmup, uint32_t partition_order, int is_extended,
    const int32_t *residual, const uint32_t *params, const uint32_t *raw_bits);
extern size_t fparity_subframe_lpc(uint8_t *out, size_t out_cap,
    uint32_t order, uint32_t residual_samples, uint32_t subframe_bps, uint32_t wasted_bits,
    const int64_t *warmup, uint32_t qlp_coeff_precision, int quantization_level,
    const int32_t *qlp_coeff, uint32_t partition_order, int is_extended,
    const int32_t *residual, const uint32_t *params, const uint32_t *raw_bits);

extern size_t fparity_metadata_streaminfo(uint8_t *out, size_t out_cap,
    int is_last, uint32_t length, uint32_t min_bs, uint32_t max_bs,
    uint32_t min_fs, uint32_t max_fs, uint32_t sample_rate, uint32_t channels,
    uint32_t bps, uint64_t total_samples, const uint8_t *md5);
extern size_t fparity_metadata_padding(uint8_t *out, size_t out_cap, int is_last, uint32_t length);
extern size_t fparity_metadata_application(uint8_t *out, size_t out_cap,
    int is_last, uint32_t length, const uint8_t *id, const uint8_t *data);
extern size_t fparity_metadata_seektable(uint8_t *out, size_t out_cap,
    int is_last, uint32_t length, uint32_t num_points,
    const uint64_t *sample_numbers, const uint64_t *stream_offsets, const uint32_t *frame_samples);
extern size_t fparity_metadata_vorbiscomment(uint8_t *out, size_t out_cap,
    int is_last, uint32_t length, int update_vendor,
    const uint8_t *vendor, uint32_t vendor_len, uint32_t num_comments,
    const uint8_t *flat, const uint32_t *offsets, const uint32_t *lengths);
extern size_t fparity_metadata_picture(uint8_t *out, size_t out_cap,
    int is_last, uint32_t length, uint32_t type,
    const char *mime, const uint8_t *desc,
    uint32_t width, uint32_t height, uint32_t depth, uint32_t colors,
    uint32_t data_length, const uint8_t *data);
extern size_t fparity_metadata_cuesheet(uint8_t *out, size_t out_cap,
    int is_last, uint32_t length, const char *mcn, uint64_t lead_in, int is_cd,
    uint32_t num_tracks, const uint64_t *t_offset, const uint8_t *t_number,
    const char *t_isrc, const uint32_t *t_type, const uint32_t *t_preemph,
    const uint8_t *t_num_indices, const uint32_t *t_index_base,
    const uint64_t *idx_offset, const uint8_t *idx_number);
*/
import "C"

import "unsafe"

const outCap = 1 << 21

func b2c(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// CgoFrameHeader frames a frame header via libFLAC. isVariable selects
// SAMPLE_NUMBER (true) vs FRAME_NUMBER (false).
func CgoFrameHeader(blocksize, sampleRate, channels, channelAssignment, bitsPerSample uint32, isVariable bool, number uint64) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_frame_header((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		C.uint32_t(blocksize), C.uint32_t(sampleRate), C.uint32_t(channels),
		C.uint32_t(channelAssignment), C.uint32_t(bitsPerSample), b2c(isVariable),
		C.uint64_t(number))
	return buf[:n]
}

func CgoSubframeConstant(value int64, subframeBps, wastedBits uint32) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_subframe_constant((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		C.int64_t(value), C.uint32_t(subframeBps), C.uint32_t(wastedBits))
	return buf[:n]
}

func CgoSubframeVerbatim32(signal []int32, samples, subframeBps, wastedBits uint32) []byte {
	buf := make([]byte, outCap)
	var sp *C.int32_t
	if len(signal) > 0 {
		sp = (*C.int32_t)(unsafe.Pointer(&signal[0]))
	}
	n := C.fparity_subframe_verbatim32((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		sp, C.uint32_t(samples), C.uint32_t(subframeBps), C.uint32_t(wastedBits))
	return buf[:n]
}

func CgoSubframeVerbatim64(signal []int64, samples, subframeBps, wastedBits uint32) []byte {
	buf := make([]byte, outCap)
	var sp *C.int64_t
	if len(signal) > 0 {
		sp = (*C.int64_t)(unsafe.Pointer(&signal[0]))
	}
	n := C.fparity_subframe_verbatim64((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		sp, C.uint32_t(samples), C.uint32_t(subframeBps), C.uint32_t(wastedBits))
	return buf[:n]
}

func CgoSubframeFixed(order, residualSamples, subframeBps, wastedBits uint32, warmup []int64,
	partitionOrder uint32, isExtended bool, residual []int32, params, rawBits []uint32) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_subframe_fixed((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		C.uint32_t(order), C.uint32_t(residualSamples), C.uint32_t(subframeBps), C.uint32_t(wastedBits),
		ptr64(warmup), C.uint32_t(partitionOrder), b2c(isExtended),
		ptr32(residual), ptrU32(params), ptrU32(rawBits))
	return buf[:n]
}

func CgoSubframeLPC(order, residualSamples, subframeBps, wastedBits uint32, warmup []int64,
	qlpCoeffPrecision uint32, quantizationLevel int, qlpCoeff []int32,
	partitionOrder uint32, isExtended bool, residual []int32, params, rawBits []uint32) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_subframe_lpc((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		C.uint32_t(order), C.uint32_t(residualSamples), C.uint32_t(subframeBps), C.uint32_t(wastedBits),
		ptr64(warmup), C.uint32_t(qlpCoeffPrecision), C.int(quantizationLevel),
		ptr32(qlpCoeff), C.uint32_t(partitionOrder), b2c(isExtended),
		ptr32(residual), ptrU32(params), ptrU32(rawBits))
	return buf[:n]
}

func CgoMetadataStreamInfo(isLast bool, length, minBS, maxBS, minFS, maxFS, sampleRate, channels, bps uint32,
	totalSamples uint64, md5 [16]byte) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_metadata_streaminfo((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		b2c(isLast), C.uint32_t(length), C.uint32_t(minBS), C.uint32_t(maxBS),
		C.uint32_t(minFS), C.uint32_t(maxFS), C.uint32_t(sampleRate), C.uint32_t(channels),
		C.uint32_t(bps), C.uint64_t(totalSamples), (*C.uint8_t)(unsafe.Pointer(&md5[0])))
	return buf[:n]
}

func CgoMetadataPadding(isLast bool, length uint32) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_metadata_padding((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		b2c(isLast), C.uint32_t(length))
	return buf[:n]
}

func CgoMetadataApplication(isLast bool, length uint32, id [4]byte, data []byte) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_metadata_application((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		b2c(isLast), C.uint32_t(length), (*C.uint8_t)(unsafe.Pointer(&id[0])), ptrBytes(data))
	return buf[:n]
}

func CgoMetadataSeekTable(isLast bool, length, numPoints uint32, sampleNumbers, streamOffsets []uint64, frameSamples []uint32) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_metadata_seektable((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		b2c(isLast), C.uint32_t(length), C.uint32_t(numPoints),
		ptrU64(sampleNumbers), ptrU64(streamOffsets), ptrU32(frameSamples))
	return buf[:n]
}

func CgoMetadataVorbisComment(isLast bool, length uint32, updateVendor bool,
	vendor []byte, numComments uint32, flat []byte, offsets, lengths []uint32) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_metadata_vorbiscomment((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		b2c(isLast), C.uint32_t(length), b2c(updateVendor),
		ptrBytes(vendor), C.uint32_t(len(vendor)), C.uint32_t(numComments),
		ptrBytes(flat), ptrU32(offsets), ptrU32(lengths))
	return buf[:n]
}

func CgoMetadataPicture(isLast bool, length, typ uint32, mime []byte, desc []byte,
	width, height, depth, colors, dataLength uint32, data []byte) []byte {
	buf := make([]byte, outCap)
	// mime is a NUL-terminated C string.
	mimeC := C.CString(string(mime))
	defer C.free(unsafe.Pointer(mimeC))
	// desc is NUL-terminated in the C struct (strlen()).
	descC := append(append([]byte(nil), desc...), 0)
	n := C.fparity_metadata_picture((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		b2c(isLast), C.uint32_t(length), C.uint32_t(typ),
		mimeC, (*C.uint8_t)(unsafe.Pointer(&descC[0])),
		C.uint32_t(width), C.uint32_t(height), C.uint32_t(depth), C.uint32_t(colors),
		C.uint32_t(dataLength), ptrBytes(data))
	return buf[:n]
}

func CgoMetadataCueSheet(isLast bool, length uint32, mcn [129]byte, leadIn uint64, isCD bool,
	numTracks uint32, tOffset []uint64, tNumber []byte, tISRC []byte /* numTracks*13 */, tType, tPreEmph []uint32,
	tNumIndices []byte, tIndexBase []uint32, idxOffset []uint64, idxNumber []byte) []byte {
	buf := make([]byte, outCap)
	n := C.fparity_metadata_cuesheet((*C.uint8_t)(unsafe.Pointer(&buf[0])), C.size_t(outCap),
		b2c(isLast), C.uint32_t(length), (*C.char)(unsafe.Pointer(&mcn[0])), C.uint64_t(leadIn), b2c(isCD),
		C.uint32_t(numTracks), ptrU64(tOffset), ptrBytes(tNumber),
		(*C.char)(unsafe.Pointer(&tISRC[0])), ptrU32(tType), ptrU32(tPreEmph),
		ptrBytes(tNumIndices), ptrU32(tIndexBase), ptrU64(idxOffset), ptrBytes(idxNumber))
	return buf[:n]
}

// pointer helpers (nil-safe).
func ptr32(s []int32) *C.int32_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int32_t)(unsafe.Pointer(&s[0]))
}
func ptr64(s []int64) *C.int64_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int64_t)(unsafe.Pointer(&s[0]))
}
func ptrU32(s []uint32) *C.uint32_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.uint32_t)(unsafe.Pointer(&s[0]))
}
func ptrU64(s []uint64) *C.uint64_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.uint64_t)(unsafe.Pointer(&s[0]))
}
func ptrBytes(s []byte) *C.uint8_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.uint8_t)(unsafe.Pointer(&s[0]))
}
