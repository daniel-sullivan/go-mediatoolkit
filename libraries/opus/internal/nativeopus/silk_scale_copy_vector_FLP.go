package nativeopus

// 1:1 port of libopus/silk/float/scale_copy_vector_FLP.c.
// Multiplies a vector by a constant and copies to a separate output.

func silk_scale_copy_vector_FLP(data_out, data_in []silk_float, gain silk_float, dataSize opus_int) {
	var i opus_int
	dataSize4 := dataSize & 0xFFFC

	// 4x unrolled loop.
	for i = 0; i < dataSize4; i += 4 {
		data_out[i+0] = gain * data_in[i+0]
		data_out[i+1] = gain * data_in[i+1]
		data_out[i+2] = gain * data_in[i+2]
		data_out[i+3] = gain * data_in[i+3]
	}
	// Remaining elements.
	for ; i < dataSize; i++ {
		data_out[i] = gain * data_in[i]
	}
}
