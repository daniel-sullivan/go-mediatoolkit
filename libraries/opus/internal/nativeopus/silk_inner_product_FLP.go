package nativeopus

// 1:1 port of libopus/silk/float/inner_product_FLP.c.
// Inner product of two silk_float arrays, returned as float64.
//
// Each product `data1[i] * (double)data2[i]` is evaluated as a
// float64 multiply (via promotion of the float32 operand through a
// separate cast step) and accumulated into a float64 running sum.
// The C source, compiled with -ffp-contract=off, performs each
// multiply and each add as an independently-rounded IEEE op; use
// the non-fusing fma_add64 helper to mirror this exactly on arm64
// Go (which would otherwise emit FMADDD).

func silk_inner_product_FLP_c(data1, data2 []silk_float, dataSize opus_int) float64 {
	// 4x unrolled, left-associative grouping ((p0+p1)+p2)+p3; see
	// silk_inner_product_batch in fma_batch_strict.go / fma_batch_default.go.
	return silk_inner_product_batch(data1, data2, dataSize)
}

// silk_inner_product_FLP — default dispatch to the C oracle (no
// arch-specific variant in this port). Matches the #define in
// float/SigProc_FLP.h that aliases silk_inner_product_FLP(d1,d2,n,arch)
// to silk_inner_product_FLP_c.
func silk_inner_product_FLP(data1, data2 []silk_float, dataSize opus_int, arch int) float64 {
	_ = arch
	return silk_inner_product_FLP_c(data1, data2, dataSize)
}
