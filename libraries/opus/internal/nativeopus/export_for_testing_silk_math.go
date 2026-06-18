package nativeopus

// Thin exports for SILK math-primitive parity tests.

func ExportTestSilkLin2Log(x int32) int32 { return silk_lin2log(x) }
func ExportTestSilkLog2Lin(x int32) int32 { return silk_log2lin(x) }
func ExportTestSilkSigmQ15(x int) int     { return silk_sigm_Q15(x) }

func ExportTestSilkSumSqrShift(x []int16) (energy int32, shift int) {
	silk_sum_sqr_shift(&energy, &shift, x, opus_int(len(x)))
	return
}

func ExportTestSilkInnerProdAlignedScale(a, b []int16, scale int) int32 {
	if len(a) != len(b) {
		panic("length mismatch")
	}
	return silk_inner_prod_aligned_scale(a, b, scale, opus_int(len(a)))
}
func ExportTestSilkInnerProd16(a, b []int16) int64 {
	if len(a) != len(b) {
		panic("length mismatch")
	}
	return silk_inner_prod16_c(a, b, opus_int(len(a)))
}

func ExportTestSilkInsertionSortIncreasing(a []int32, K int) ([]int32, []int) {
	idx := make([]int, K)
	silk_insertion_sort_increasing(a, idx, opus_int(len(a)), opus_int(K))
	return a, idx
}
func ExportTestSilkInsertionSortDecreasingInt16(a []int16, K int) ([]int16, []int) {
	idx := make([]int, K)
	silk_insertion_sort_decreasing_int16(a, idx, opus_int(len(a)), opus_int(K))
	return a, idx
}
func ExportTestSilkInsertionSortIncreasingAllValuesInt16(a []int16) []int16 {
	silk_insertion_sort_increasing_all_values_int16(a, opus_int(len(a)))
	return a
}

func ExportTestSilkLSFCosTabFIXQ12() []int16 {
	out := make([]int16, len(silk_LSFCosTab_FIX_Q12))
	for i, v := range silk_LSFCosTab_FIX_Q12 {
		out[i] = v
	}
	return out
}
