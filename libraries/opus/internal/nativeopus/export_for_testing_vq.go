package nativeopus

// VQ test shims.

func ExportTestExpRotation(X []float32, length, dir, stride, K, spread int) {
	exp_rotation(X, length, dir, stride, K, spread)
}

func ExportTestOpPvqSearchC(X []float32, iy []int, K, N int) float32 {
	return float32(op_pvq_search_c(X, iy, K, N, 0))
}

func ExportTestRenormaliseVector(X []float32, N int, gain float32) {
	renormalise_vector(X, N, opus_val32(gain), 0)
}

func ExportTestStereoItheta(X, Y []float32, stereo, N int) int32 {
	return int32(stereo_itheta(X, Y, stereo, N, 0))
}

func ExportTestAlgQuant(X []float32, N, K, spread, B int, enc EcCtxHandle,
	gain float32, resynth int) uint {
	return alg_quant(X, N, K, spread, B, enc.p, opus_val32(gain), resynth, 0, make([]int, N+3))
}

func ExportTestAlgUnquant(X []float32, N, K, spread, B int, dec EcCtxHandle,
	gain float32) uint {
	return alg_unquant(X, N, K, spread, B, dec.p, opus_val32(gain), make([]int, N))
}

func ExportTestExtractCollapseMask(iy []int, N, B int) uint {
	return extract_collapse_mask(iy, N, B)
}

func ExportTestNormaliseResidual(iy []int, X []float32, N int, Ryy, gain float32) {
	normalise_residual(iy, X, N, opus_val32(Ryy), opus_val32(gain), 0)
}
