package nativeopus

func ExportTestXcorrKernelC(x, y []float32, sum []float32, ln int) {
	sum32 := make([]opus_val32, len(sum))
	for i, v := range sum {
		sum32[i] = opus_val32(v)
	}
	xcorr_kernel_c(x, y, sum32, ln)
	for i, v := range sum32 {
		sum[i] = float32(v)
	}
}
func ExportTestCeltInnerProdC(x, y []float32, N int) float32 {
	return float32(celt_inner_prod_c(x, y, N))
}
func ExportTestDualInnerProdC(x, y01, y02 []float32, N int) (float32, float32) {
	var a, b opus_val32
	dual_inner_prod_c(x, y01, y02, N, &a, &b)
	return float32(a), float32(b)
}

// ExportTestCeltInnerProdArch exercises the arch-aware dispatch —
// i.e. the SIMD path when compiled in, the scalar path otherwise.
func ExportTestCeltInnerProdArch(x, y []float32, N int) float32 {
	return float32(celt_inner_prod(x, y, N, 0))
}
func ExportTestDualInnerProdArch(x, y01, y02 []float32, N int) (float32, float32) {
	var a, b opus_val32
	dual_inner_prod(x, y01, y02, N, &a, &b, 0)
	return float32(a), float32(b)
}
func ExportTestInnerProdSIMDAvailable() bool { return innerProdSIMDAvailable }
func ExportTestCeltPitchXcorrC(x, y []float32, xcorr []float32, ln, max int) {
	xc := make([]opus_val32, len(xcorr))
	celt_pitch_xcorr_c(x, y, xc, ln, max, 0)
	for i, v := range xc {
		xcorr[i] = float32(v)
	}
}
func ExportTestCeltLPC(lpc []float32, ac []float32, p int) {
	ac32 := make([]opus_val32, len(ac))
	for i, v := range ac {
		ac32[i] = opus_val32(v)
	}
	_celt_lpc(lpc, ac32, p)
}
func ExportTestCeltFirC(x, num, y []float32, N, ord int) {
	celt_fir_c(x, num, y, N, ord, 0)
}
func ExportTestCeltIir(x []float32, den []float32, y []float32,
	N, ord int, mem []float32) {
	x32 := make([]opus_val32, len(x))
	for i, v := range x {
		x32[i] = opus_val32(v)
	}
	y32 := make([]opus_val32, len(y))
	celt_iir(x32, den, y32, N, ord, mem, 0)
	for i, v := range y32 {
		y[i] = float32(v)
	}
}
func ExportTestCeltAutocorr(x []float32, ac []float32, window []float32,
	overlap, lag, n int) {
	ac32 := make([]opus_val32, len(ac))
	_celt_autocorr(x, ac32, window, overlap, lag, n, 0)
	for i, v := range ac32 {
		ac[i] = float32(v)
	}
}
func ExportTestPitchDownsample(x [][]float32, xlp []float32, ln, C, factor int) {
	pitch_downsample(x, xlp, ln, C, factor, 0)
}
func ExportTestPitchSearch(xlp, y []float32, ln, maxp int) int {
	var p int
	pitch_search(xlp, y, ln, maxp, &p, 0)
	return p
}
func ExportTestRemoveDoubling(x []float32, maxperiod, minperiod, N int,
	T0 *int, prevPeriod int, prevGain float32) float32 {
	return float32(remove_doubling(x, maxperiod, minperiod, N, T0,
		prevPeriod, prevGain, 0))
}
