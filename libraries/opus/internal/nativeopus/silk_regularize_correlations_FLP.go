package nativeopus

// 1:1 port of libopus/silk/float/regularize_correlations_FLP.c.
// Adds noise to the diagonal of the correlation matrix and to the
// first element of the correlation vector.

func silk_regularize_correlations_FLP(XX, xx []silk_float, noise silk_float, D opus_int) {
	// Use add_f32 (//go:noinline) to defeat Go's arm64 inline-add
	// anomaly that produces a non-IEEE rounding on certain bit
	// patterns. See fma.go.
	for i := opus_int(0); i < D; i++ {
		XX[i*D+i] = add_f32(XX[i*D+i], noise)
	}
	xx[0] = add_f32(xx[0], noise)
}
