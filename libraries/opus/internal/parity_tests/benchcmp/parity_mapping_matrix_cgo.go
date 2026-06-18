//go:build cgo

package benchcmp

/*
#include "config.h"

#include "arch.h"
#include "mapping_matrix.h"
#include "opus_private.h"

// mapping_matrix_init needs a properly sized byte buffer. We allocate
// max(16 + data_bytes, sizeof(MappingMatrix) + data_bytes) + extra
// alignment slack on the Go side and hand a pointer in.

// For the per-channel multiply helpers, we construct a MappingMatrix
// inside the caller's buffer via mapping_matrix_init, then invoke the
// multiply helper. Returning output via a pointer parameter is fine.

static opus_int32 c_mapping_matrix_get_size(int rows, int cols) {
    return mapping_matrix_get_size(rows, cols);
}

// c_mm_init initializes an in-buffer MappingMatrix and copies the
// data into its trailing cell array. The caller supplies a buffer
// large enough (>= mapping_matrix_get_size(rows, cols)).
static void c_mm_init(void *buf, int rows, int cols, int gain,
                      const opus_int16 *data, opus_int32 data_size) {
    mapping_matrix_init((MappingMatrix *)buf, rows, cols, gain, data, data_size);
}

// c_mm_get_data returns a pointer to the cell array inside the
// initialized buffer.
static const opus_int16 *c_mm_get_data(const void *buf) {
    return mapping_matrix_get_data((const MappingMatrix *)buf);
}

static int c_mm_rows(const void *buf) { return ((const MappingMatrix*)buf)->rows; }
static int c_mm_cols(const void *buf) { return ((const MappingMatrix*)buf)->cols; }
static int c_mm_gain(const void *buf) { return ((const MappingMatrix*)buf)->gain; }

#ifndef DISABLE_FLOAT_API
static void c_mm_multiply_channel_in_float(const void *buf,
    const float *input, int input_rows,
    float *output, int output_row, int output_rows, int frame_size) {
    mapping_matrix_multiply_channel_in_float((const MappingMatrix*)buf,
        input, input_rows, output, output_row, output_rows, frame_size);
}

static void c_mm_multiply_channel_out_float(const void *buf,
    const float *input, int input_row, int input_rows,
    float *output, int output_rows, int frame_size) {
    mapping_matrix_multiply_channel_out_float((const MappingMatrix*)buf,
        input, input_row, input_rows, output, output_rows, frame_size);
}
#endif

static void c_mm_multiply_channel_in_short(const void *buf,
    const opus_int16 *input, int input_rows,
    float *output, int output_row, int output_rows, int frame_size) {
    mapping_matrix_multiply_channel_in_short((const MappingMatrix*)buf,
        input, input_rows, output, output_row, output_rows, frame_size);
}

static void c_mm_multiply_channel_out_short(const void *buf,
    const float *input, int input_row, int input_rows,
    opus_int16 *output, int output_rows, int frame_size) {
    mapping_matrix_multiply_channel_out_short((const MappingMatrix*)buf,
        input, input_row, input_rows, output, output_rows, frame_size);
}

// opus_multistream.c helpers.
static int c_validate_layout(const ChannelLayout *l) { return validate_layout(l); }
static int c_get_left_channel(const ChannelLayout *l, int s, int p) { return get_left_channel(l, s, p); }
static int c_get_right_channel(const ChannelLayout *l, int s, int p) { return get_right_channel(l, s, p); }
static int c_get_mono_channel(const ChannelLayout *l, int s, int p) { return get_mono_channel(l, s, p); }
*/
import "C"

import (
	"unsafe"
)

// cMappingMatrixGetSize mirrors the C entry.
func cMappingMatrixGetSize(rows, cols int) int32 {
	return int32(C.c_mapping_matrix_get_size(C.int(rows), C.int(cols)))
}

// cMappingMatrixBuffer returns an allocated buffer big enough for a
// (rows, cols, data) matrix plus alignment slack.
func cMappingMatrixBuffer(rows, cols int) []byte {
	sz := int(C.c_mapping_matrix_get_size(C.int(rows), C.int(cols)))
	if sz == 0 {
		sz = 64 + rows*cols*2
	}
	// Add generous slack so sizeof(MappingMatrix) differences between
	// Go-side guess and C-side actual cannot cause overruns.
	return make([]byte, sz+128)
}

// cMappingMatrixInit runs the C init, returning a populated buffer and
// the pointer to its contents (for passing to multiply helpers).
func cMappingMatrixInit(rows, cols, gain int, data []int16) ([]byte, unsafe.Pointer) {
	buf := cMappingMatrixBuffer(rows, cols)
	var dataPtr *C.opus_int16
	if len(data) > 0 {
		dataPtr = (*C.opus_int16)(unsafe.Pointer(&data[0]))
	}
	ptr := unsafe.Pointer(&buf[0])
	C.c_mm_init(ptr, C.int(rows), C.int(cols), C.int(gain), dataPtr, C.opus_int32(len(data)*2))
	return buf, ptr
}

// cMappingMatrixExtract returns rows/cols/gain and a copy of the cell
// slice from an initialized buffer.
func cMappingMatrixExtract(buf []byte) (rows, cols, gain int, cells []int16) {
	ptr := unsafe.Pointer(&buf[0])
	rows = int(C.c_mm_rows(ptr))
	cols = int(C.c_mm_cols(ptr))
	gain = int(C.c_mm_gain(ptr))
	n := rows * cols
	dataPtr := C.c_mm_get_data(ptr)
	cells = make([]int16, n)
	for i := 0; i < n; i++ {
		cells[i] = int16(*(*C.opus_int16)(unsafe.Add(unsafe.Pointer(dataPtr), uintptr(i)*unsafe.Sizeof(C.opus_int16(0)))))
	}
	return
}

// cMappingMatrixMultiplyChannelInFloat runs the C multiply helper.
func cMappingMatrixMultiplyChannelInFloat(
	rows, cols int, data []int16,
	input []float32, input_rows int,
	output []float32, output_row, output_rows, frame_size int,
) {
	_, ptr := cMappingMatrixInit(rows, cols, 0, data)
	var inPtr *C.float
	if len(input) > 0 {
		inPtr = (*C.float)(unsafe.Pointer(&input[0]))
	}
	var outPtr *C.float
	if len(output) > 0 {
		outPtr = (*C.float)(unsafe.Pointer(&output[0]))
	}
	C.c_mm_multiply_channel_in_float(ptr, inPtr, C.int(input_rows),
		outPtr, C.int(output_row), C.int(output_rows), C.int(frame_size))
}

// cMappingMatrixMultiplyChannelOutFloat runs the C multiply helper.
func cMappingMatrixMultiplyChannelOutFloat(
	rows, cols int, data []int16,
	input []float32, input_row, input_rows int,
	output []float32, output_rows, frame_size int,
) {
	_, ptr := cMappingMatrixInit(rows, cols, 0, data)
	var inPtr *C.float
	if len(input) > 0 {
		inPtr = (*C.float)(unsafe.Pointer(&input[0]))
	}
	var outPtr *C.float
	if len(output) > 0 {
		outPtr = (*C.float)(unsafe.Pointer(&output[0]))
	}
	C.c_mm_multiply_channel_out_float(ptr, inPtr, C.int(input_row), C.int(input_rows),
		outPtr, C.int(output_rows), C.int(frame_size))
}

// cMappingMatrixMultiplyChannelInShort runs the C multiply helper.
func cMappingMatrixMultiplyChannelInShort(
	rows, cols int, data []int16,
	input []int16, input_rows int,
	output []float32, output_row, output_rows, frame_size int,
) {
	_, ptr := cMappingMatrixInit(rows, cols, 0, data)
	var inPtr *C.opus_int16
	if len(input) > 0 {
		inPtr = (*C.opus_int16)(unsafe.Pointer(&input[0]))
	}
	var outPtr *C.float
	if len(output) > 0 {
		outPtr = (*C.float)(unsafe.Pointer(&output[0]))
	}
	C.c_mm_multiply_channel_in_short(ptr, inPtr, C.int(input_rows),
		outPtr, C.int(output_row), C.int(output_rows), C.int(frame_size))
}

// cMappingMatrixMultiplyChannelOutShort runs the C multiply helper.
func cMappingMatrixMultiplyChannelOutShort(
	rows, cols int, data []int16,
	input []float32, input_row, input_rows int,
	output []int16, output_rows, frame_size int,
) {
	_, ptr := cMappingMatrixInit(rows, cols, 0, data)
	var inPtr *C.float
	if len(input) > 0 {
		inPtr = (*C.float)(unsafe.Pointer(&input[0]))
	}
	var outPtr *C.opus_int16
	if len(output) > 0 {
		outPtr = (*C.opus_int16)(unsafe.Pointer(&output[0]))
	}
	C.c_mm_multiply_channel_out_short(ptr, inPtr, C.int(input_row), C.int(input_rows),
		outPtr, C.int(output_rows), C.int(frame_size))
}

// Multistream layout helpers.

// cChannelLayout is a Go-side mirror of ChannelLayout used as the
// argument pack for the four layout helpers.
type cChannelLayout struct {
	NbChannels       int
	NbStreams        int
	NbCoupledStreams int
	Mapping          [256]byte
}

func (l cChannelLayout) asC() C.ChannelLayout {
	var cl C.ChannelLayout
	cl.nb_channels = C.int(l.NbChannels)
	cl.nb_streams = C.int(l.NbStreams)
	cl.nb_coupled_streams = C.int(l.NbCoupledStreams)
	for i := 0; i < 256; i++ {
		cl.mapping[i] = C.uchar(l.Mapping[i])
	}
	return cl
}

func cValidateLayout(l cChannelLayout) int {
	cl := l.asC()
	return int(C.c_validate_layout(&cl))
}

func cGetLeftChannel(l cChannelLayout, streamID, prev int) int {
	cl := l.asC()
	return int(C.c_get_left_channel(&cl, C.int(streamID), C.int(prev)))
}

func cGetRightChannel(l cChannelLayout, streamID, prev int) int {
	cl := l.asC()
	return int(C.c_get_right_channel(&cl, C.int(streamID), C.int(prev)))
}

func cGetMonoChannel(l cChannelLayout, streamID, prev int) int {
	cl := l.asC()
	return int(C.c_get_mono_channel(&cl, C.int(streamID), C.int(prev)))
}
