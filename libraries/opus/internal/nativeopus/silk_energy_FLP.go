package nativeopus

// 1:1 port of libopus/silk/float/energy_FLP.c.
// Sum of squares of a silk_float array, returned as float64.

func silk_energy_FLP_c(data []silk_float, dataSize opus_int) float64 {
	var i opus_int
	var result float64
	// Same grouping as silk_inner_product_FLP_c: four squared terms
	// summed left-to-right before being added to `result`.
	for i = 0; i < dataSize-3; i += 4 {
		d0 := float64(data[i+0])
		d1 := float64(data[i+1])
		d2 := float64(data[i+2])
		d3 := float64(data[i+3])
		p0 := mul_f64(d0, d0)
		p1 := mul_f64(d1, d1)
		p2 := mul_f64(d2, d2)
		p3 := mul_f64(d3, d3)
		s01 := add_f64(p0, p1)
		s012 := add_f64(s01, p2)
		rhs := add_f64(s012, p3)
		result = add_f64(result, rhs)
	}
	for ; i < dataSize; i++ {
		d := float64(data[i])
		p := mul_f64(d, d)
		result = add_f64(result, p)
	}
	return result
}

// silk_energy_FLP — no arch-specific variant in this port.
func silk_energy_FLP(data []silk_float, dataSize opus_int) float64 {
	return silk_energy_FLP_c(data, dataSize)
}
