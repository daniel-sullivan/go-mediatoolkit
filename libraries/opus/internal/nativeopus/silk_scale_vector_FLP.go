package nativeopus

// 1:1 port of libopus/silk/float/scale_vector_FLP.c.
// Multiplies a vector in place by a constant.

func silk_scale_vector_FLP(data1 []silk_float, gain silk_float, dataSize opus_int) {
	var i opus_int
	dataSize4 := dataSize & 0xFFFC

	// 4x unrolled loop.
	for i = 0; i < dataSize4; i += 4 {
		data1[i+0] *= gain
		data1[i+1] *= gain
		data1[i+2] *= gain
		data1[i+3] *= gain
	}
	for ; i < dataSize; i++ {
		data1[i] *= gain
	}
}
