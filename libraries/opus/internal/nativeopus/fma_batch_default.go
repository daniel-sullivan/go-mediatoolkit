//go:build !opus_strict

package nativeopus

// Batched FMA helpers — default FMA-fused variant.
//
// Built by default. All ops are inline so Go's compiler is free to
// fuse `acc + x*y` into FMADDS (arm64) or VFMADD (amd64). Bit-exact
// parity with the C oracle is NOT expected under this build — use
// `-tags=opus_strict` for that, at the cost of ~2× on encode paths.
// See README for the full quality matrix.

func celt_inner_prod_batch(x, y []opus_val16, N int) opus_val32 {
	var xy opus_val32
	for i := 0; i < N; i++ {
		xy += x[i] * y[i]
	}
	return xy
}

func dual_inner_prod_batch(x, y01, y02 []opus_val16, N int) (xy01, xy02 opus_val32) {
	for i := 0; i < N; i++ {
		xy01 += x[i] * y01[i]
		xy02 += x[i] * y02[i]
	}
	return
}

func silk_inner_product_batch(data1, data2 []silk_float, dataSize opus_int) float64 {
	var i opus_int
	var result float64
	for i = 0; i < dataSize-3; i += 4 {
		p0 := float64(data1[i+0]) * float64(data2[i+0])
		p1 := float64(data1[i+1]) * float64(data2[i+1])
		p2 := float64(data1[i+2]) * float64(data2[i+2])
		p3 := float64(data1[i+3]) * float64(data2[i+3])
		result += ((p0 + p1) + p2) + p3
	}
	for ; i < dataSize; i++ {
		result += float64(data1[i]) * float64(data2[i])
	}
	return result
}

func celt_autocorr_tail_batch(xptr []opus_val16, k, fastN, n int) opus_val32 {
	var d opus_val32
	for i := k + fastN; i < n; i++ {
		d += xptr[i] * xptr[i-k]
	}
	return d
}
