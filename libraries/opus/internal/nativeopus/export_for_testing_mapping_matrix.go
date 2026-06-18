package nativeopus

// Exported accessors for parity testing of mapping_matrix.go and
// opus_multistream.go. These wrap the unexported ports so benchcmp can
// drive them against the Cgo oracle.

// ExportTestMappingMatrix is a struct-shaped mirror of MappingMatrix
// that lets tests construct and inspect a matrix without reaching into
// unexported fields. Parity tests only need to round-trip data through
// the init/multiply entry points, so a minimal view suffices.
type ExportTestMappingMatrix struct {
	Rows int
	Cols int
	Gain int
	Data []int16 // opus_int16
}

// ExportTestMappingMatrixGetSize — mapping_matrix_get_size.
func ExportTestMappingMatrixGetSize(rows, cols int) int32 {
	return int32(mapping_matrix_get_size(rows, cols))
}

// ExportTestMappingMatrixInit runs mapping_matrix_init on a fresh
// MappingMatrix and returns the populated view.
func ExportTestMappingMatrixInit(rows, cols, gain int, data []int16) ExportTestMappingMatrix {
	m := &MappingMatrix{}
	mapping_matrix_init(m, rows, cols, gain, data, int32(len(data)*2))
	d := make([]int16, len(m.data))
	copy(d, m.data)
	return ExportTestMappingMatrix{Rows: m.rows, Cols: m.cols, Gain: m.gain, Data: d}
}

// makeMappingMatrixForTest constructs an in-package MappingMatrix for
// the multiply-path tests below.
func makeMappingMatrixForTest(rows, cols int, data []int16) *MappingMatrix {
	m := &MappingMatrix{}
	mapping_matrix_init(m, rows, cols, 0, data, int32(len(data)*2))
	return m
}

// ExportTestMappingMatrixMultiplyChannelInFloat mirrors
// mapping_matrix_multiply_channel_in_float.
func ExportTestMappingMatrixMultiplyChannelInFloat(
	rows, cols int, matrixData []int16,
	input []float32, input_rows int,
	output []float32, output_row, output_rows, frame_size int,
) {
	m := makeMappingMatrixForTest(rows, cols, matrixData)
	mapping_matrix_multiply_channel_in_float(m, input, input_rows, output, output_row, output_rows, frame_size)
}

// ExportTestMappingMatrixMultiplyChannelOutFloat mirrors
// mapping_matrix_multiply_channel_out_float.
func ExportTestMappingMatrixMultiplyChannelOutFloat(
	rows, cols int, matrixData []int16,
	input []float32, input_row, input_rows int,
	output []float32, output_rows, frame_size int,
) {
	m := makeMappingMatrixForTest(rows, cols, matrixData)
	mapping_matrix_multiply_channel_out_float(m, input, input_row, input_rows, output, output_rows, frame_size)
}

// ExportTestMappingMatrixMultiplyChannelInShort mirrors
// mapping_matrix_multiply_channel_in_short.
func ExportTestMappingMatrixMultiplyChannelInShort(
	rows, cols int, matrixData []int16,
	input []int16, input_rows int,
	output []float32, output_row, output_rows, frame_size int,
) {
	m := makeMappingMatrixForTest(rows, cols, matrixData)
	mapping_matrix_multiply_channel_in_short(m, input, input_rows, output, output_row, output_rows, frame_size)
}

// ExportTestMappingMatrixMultiplyChannelOutShort mirrors
// mapping_matrix_multiply_channel_out_short.
func ExportTestMappingMatrixMultiplyChannelOutShort(
	rows, cols int, matrixData []int16,
	input []float32, input_row, input_rows int,
	output []int16, output_rows, frame_size int,
) {
	m := makeMappingMatrixForTest(rows, cols, matrixData)
	mapping_matrix_multiply_channel_out_short(m, input, input_row, input_rows, output, output_rows, frame_size)
}

// Opus multistream foundation exports.

// ExportChannelLayout is a mirror of ChannelLayout with exported
// fields, so tests can construct one from outside the package.
type ExportChannelLayout struct {
	NbChannels       int
	NbStreams        int
	NbCoupledStreams int
	Mapping          [256]byte
}

func (e ExportChannelLayout) toInternal() ChannelLayout {
	return ChannelLayout{
		nb_channels:        e.NbChannels,
		nb_streams:         e.NbStreams,
		nb_coupled_streams: e.NbCoupledStreams,
		mapping:            e.Mapping,
	}
}

// ExportTestValidateLayout — validate_layout.
func ExportTestValidateLayout(layout ExportChannelLayout) int {
	l := layout.toInternal()
	return validate_layout(&l)
}

// ExportTestGetLeftChannel — get_left_channel.
func ExportTestGetLeftChannel(layout ExportChannelLayout, stream_id, prev int) int {
	l := layout.toInternal()
	return get_left_channel(&l, stream_id, prev)
}

// ExportTestGetRightChannel — get_right_channel.
func ExportTestGetRightChannel(layout ExportChannelLayout, stream_id, prev int) int {
	l := layout.toInternal()
	return get_right_channel(&l, stream_id, prev)
}

// ExportTestGetMonoChannel — get_mono_channel.
func ExportTestGetMonoChannel(layout ExportChannelLayout, stream_id, prev int) int {
	l := layout.toInternal()
	return get_mono_channel(&l, stream_id, prev)
}
