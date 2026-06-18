package nativeopus

// Port of libopus/src/mapping_matrix.c and src/mapping_matrix.h.
//
// MappingMatrix carries a dense opus_int16 coefficient grid in
// column-major order. Callers multiply individual rows (out-channel)
// or columns (in-channel) against PCM frames. The float build uses
// float32 accumulators for the _in_float, _out_float, and _in_int24
// paths; the two _short variants of `_out` and int24 `_out` stay in
// integer arithmetic. Every float accumulator in the per-sample inner
// loop is wrapped in fma_add / add_f32 / mul_f32 helpers so the
// separate-round arithmetic matches the C oracle bit-for-bit under
// `-ffp-contract=off`.

// MappingMatrix mirrors `typedef struct MappingMatrix` in
// src/mapping_matrix.h. The matrix cell data is not a C flexible array
// in this port — data is stored in a side slice associated with the
// struct via mapping_matrix_init / mapping_matrix_get_data.
type MappingMatrix struct {
	rows int // number of channels output from the matrix
	cols int // number of channels input to the matrix
	gain int // S7.8-format dB gain

	// data holds the row-major-flat-but-column-wise-indexed coefficient
	// cells. In the C implementation the cells live in memory directly
	// after the struct; in Go we keep them alongside the struct. Index
	// arithmetic is identical: cell(row,col) = data[rows*col + row].
	data []opus_int16
}

// matrixIndex — C: `#define MATRIX_INDEX(nb_rows, row, col) (nb_rows * col + row)`.
func matrixIndex(nb_rows, row, col int) int { return nb_rows*col + row }

// mm_align mirrors the static-inline `align(i)` in src/opus_private.h.
// That helper computes `ceil(i / alignof(union{void*,int32,val32})) *
// alignof(...)`; on 64-bit platforms the pointer member forces
// alignment = 8. The mapping_matrix_get_size result must match that
// exactly for parity with C callers under the same pointer width.
func mm_align(x int) int {
	const alignment = 8
	return (x + alignment - 1) / alignment * alignment
}

// mapping_matrix_get_size — C: src/mapping_matrix.c:40.
func mapping_matrix_get_size(rows, cols int) opus_int32 {
	var size opus_int32

	// Mapping Matrix must only support up to 255 channels in or out.
	// Additionally, the total cell count must be <= 65004 octets in
	// order for the matrix to be stored in an OGG header.
	if rows > 255 || cols > 255 {
		return 0
	}
	size = opus_int32(rows) * opus_int32(cols) * 2 // sizeof(opus_int16)
	if size > 65004 {
		return 0
	}

	return opus_int32(mm_align(mappingMatrixSizeofStruct()) + mm_align(int(size)))
}

// mappingMatrixSizeofStruct — placeholder for `sizeof(MappingMatrix)` in
// C. The C struct is three ints — 12 bytes on every platform the
// port targets. This value is only consulted by
// mapping_matrix_get_size, which aligns it up to the pointer boundary.
func mappingMatrixSizeofStruct() int { return 12 }

// mapping_matrix_get_data — C: src/mapping_matrix.c:57.
// Returns the coefficient slice associated with the matrix.
func mapping_matrix_get_data(matrix *MappingMatrix) []opus_int16 { return matrix.data }

// mapping_matrix_init — C: src/mapping_matrix.c:63.
func mapping_matrix_init(matrix *MappingMatrix, rows, cols, gain int, data []opus_int16, data_size opus_int32) {
	_ = data_size
	celt_assert(mm_align(int(data_size)) == mm_align(rows*cols*2))

	matrix.rows = rows
	matrix.cols = cols
	matrix.gain = gain
	if matrix.data == nil || len(matrix.data) < rows*cols {
		matrix.data = make([]opus_int16, rows*cols)
	} else {
		matrix.data = matrix.data[:rows*cols]
	}
	ptr := mapping_matrix_get_data(matrix)
	for i := 0; i < rows*cols; i++ {
		ptr[i] = data[i]
	}
}

// mapping_matrix_multiply_channel_in_float — C: src/mapping_matrix.c:85.
//
// Float build: `tmp` is a float32 accumulator; `matrix_data[r,c]` is
// opus_int16 and `input[...]` is float32. C evaluates each term as
// int16→int→float32 multiply then separately-rounded add (-ffp-contract=off).
// The Go port wraps the per-col add in fma_add.
func mapping_matrix_multiply_channel_in_float(
	matrix *MappingMatrix,
	input []float32,
	input_rows int,
	output []opus_res,
	output_row int,
	output_rows int,
	frame_size int,
) {
	celt_assert(input_rows <= matrix.cols && output_rows <= matrix.rows)
	matrix_data := mapping_matrix_get_data(matrix)

	for i := 0; i < frame_size; i++ {
		var tmp float32 = 0
		for col := 0; col < input_rows; col++ {
			// C: tmp += matrix_data[r,c] * input[c,i];
			tmp = fma_add(tmp,
				float32(matrix_data[matrixIndex(matrix.rows, output_row, col)]),
				input[matrixIndex(input_rows, col, i)])
		}
		// C: output[output_rows * i] = FLOAT2RES((1/32768.f)*tmp);
		output[output_rows*i] = FLOAT2RES(mul_f32(1.0/32768.0, tmp))
	}
}

// mapping_matrix_multiply_channel_out_float — C: src/mapping_matrix.c:115.
//
// Float build per-row: `tmp = (1/32768.f) * matrix_data[r,input_row] * input_sample`.
// Under -ffp-contract=off and LTR associativity the product is
// `mul_f32(mul_f32(1/32768, matrix_data), input_sample)`. Then
// `output[r,i] = add_f32(output[r,i], tmp)`.
func mapping_matrix_multiply_channel_out_float(
	matrix *MappingMatrix,
	input []opus_res,
	input_row int,
	input_rows int,
	output []float32,
	output_rows int,
	frame_size int,
) {
	celt_assert(input_rows <= matrix.cols && output_rows <= matrix.rows)
	matrix_data := mapping_matrix_get_data(matrix)

	for i := 0; i < frame_size; i++ {
		input_sample := RES2FLOAT(input[input_rows*i])
		for row := 0; row < output_rows; row++ {
			// C: float tmp = (1/32768.f)*matrix_data[r,input_row] * input_sample;
			tmp := mul_f32(
				mul_f32(1.0/32768.0, float32(matrix_data[matrixIndex(matrix.rows, row, input_row)])),
				input_sample,
			)
			// C: output[r,i] += tmp;
			output[matrixIndex(output_rows, row, i)] = add_f32(output[matrixIndex(output_rows, row, i)], tmp)
		}
	}
}

// mapping_matrix_multiply_channel_in_short — C: src/mapping_matrix.c:148.
//
// Float build (FIXED_POINT undef): `tmp` is opus_val32 (float32). Each
// inner-loop term `matrix_data[r,c] * input[c,i]` is int16*int16; C
// promotes both to int, computes the int product, then converts to
// float32 to add to tmp. `add_f32` wraps the per-col accumulation.
// Post-loop: `output = (1/(32768*32768)) * tmp`.
func mapping_matrix_multiply_channel_in_short(
	matrix *MappingMatrix,
	input []opus_int16,
	input_rows int,
	output []opus_res,
	output_row int,
	output_rows int,
	frame_size int,
) {
	celt_assert(input_rows <= matrix.cols && output_rows <= matrix.rows)
	matrix_data := mapping_matrix_get_data(matrix)

	for i := 0; i < frame_size; i++ {
		var tmp opus_val32 = 0
		for col := 0; col < input_rows; col++ {
			// C (float build): tmp += matrix_data[r,c] * input[c,i];
			// int16*int16 → int → float32 → +=.
			prod := opus_int32(matrix_data[matrixIndex(matrix.rows, output_row, col)]) *
				opus_int32(input[matrixIndex(input_rows, col, i)])
			tmp = add_f32(tmp, float32(prod))
		}
		// C: output[...] = (1/(32768.f*32768.f))*tmp;
		output[output_rows*i] = mul_f32(1.0/(32768.0*32768.0), tmp)
	}
}

// mapping_matrix_multiply_channel_out_short — C: src/mapping_matrix.c:192.
//
// Both float and fixed builds operate on integer math here; no float
// arithmetic appears in the inner loop.
func mapping_matrix_multiply_channel_out_short(
	matrix *MappingMatrix,
	input []opus_res,
	input_row int,
	input_rows int,
	output []opus_int16,
	output_rows int,
	frame_size int,
) {
	celt_assert(input_rows <= matrix.cols && output_rows <= matrix.rows)
	matrix_data := mapping_matrix_get_data(matrix)

	for i := 0; i < frame_size; i++ {
		// C: input_sample = RES2INT16(input[input_rows * i]);
		// Float build: RES2INT16 = FLOAT2INT16.
		input_sample := opus_int32(FLOAT2INT16(input[input_rows*i]))
		for row := 0; row < output_rows; row++ {
			tmp := opus_int32(matrix_data[matrixIndex(matrix.rows, row, input_row)]) * input_sample
			output[matrixIndex(output_rows, row, i)] += opus_int16((tmp + 16384) >> 15)
		}
	}
}

// mapping_matrix_multiply_channel_in_int24 — C: src/mapping_matrix.c:223.
//
// Float build: `opus_val64` is float32. `tmp += matrix_data * (float32)input`.
// Each inner add wraps in fma_add. Post-loop: `output = INT24TORES((1/32768.f)*tmp)`.
func mapping_matrix_multiply_channel_in_int24(
	matrix *MappingMatrix,
	input []opus_int32,
	input_rows int,
	output []opus_res,
	output_row int,
	output_rows int,
	frame_size int,
) {
	celt_assert(input_rows <= matrix.cols && output_rows <= matrix.rows)
	matrix_data := mapping_matrix_get_data(matrix)

	for i := 0; i < frame_size; i++ {
		var tmp opus_val64 = 0
		for col := 0; col < input_rows; col++ {
			// C: tmp += matrix_data[r,c] * (opus_val64)input[c,i];
			// Float build: opus_val64 = float32. The C product is
			// int16-promoted-to-float32 * (float32)(opus_int32 cast).
			tmp = fma_add(tmp,
				float32(matrix_data[matrixIndex(matrix.rows, output_row, col)]),
				float32(input[matrixIndex(input_rows, col, i)]))
		}
		// C: output[...] = INT24TORES((1/(32768.f))*tmp);
		output[output_rows*i] = INT24TORES_from_float32(mul_f32(1.0/32768.0, tmp))
	}
}

// INT24TORES_from_float32 — INT24TORES called with a float argument in
// the float build is `(1.f/32768.f/256.f) * (float)a`. Here `a` is
// already the post-`(1/32768)*tmp` float32, which INT24TORES in turn
// scales by `1/(32768*256)`. Note the float-build macro takes an
// opus_int32 when called from C; when called with a float via the
// macro expansion the argument is implicitly converted. This helper
// keeps the call site literal.
func INT24TORES_from_float32(a float32) opus_res {
	return opus_res(mul_f32(1.0/32768.0/256.0, a))
}

// mapping_matrix_multiply_channel_out_int24 — C: src/mapping_matrix.c:257.
//
// Float build: `input_sample = RES2INT24(input[...])` which is
// `float2int(32768*256*x)`. Remainder is int64 integer arithmetic.
func mapping_matrix_multiply_channel_out_int24(
	matrix *MappingMatrix,
	input []opus_res,
	input_row int,
	input_rows int,
	output []opus_int32,
	output_rows int,
	frame_size int,
) {
	celt_assert(input_rows <= matrix.cols && output_rows <= matrix.rows)
	matrix_data := mapping_matrix_get_data(matrix)

	for i := 0; i < frame_size; i++ {
		// C: input_sample = RES2INT24(input[...]);
		// Float build: float2int(32768.f*256.f*(a)), no clamping.
		input_sample := float2int(mul_f32(32768.0*256.0, float32(input[input_rows*i])))
		for row := 0; row < output_rows; row++ {
			tmp := int64(matrix_data[matrixIndex(matrix.rows, row, input_row)]) * int64(input_sample)
			output[matrixIndex(output_rows, row, i)] += opus_int32((tmp + 16384) >> 15)
		}
	}
}
