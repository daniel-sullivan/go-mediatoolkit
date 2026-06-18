package nativeopus

// 1:1 port of libopus/silk/float/corrMatrix_FLP.c.
// Builds the correlation vector X'*t and the correlation matrix X'*X
// used by the least-squares LPC estimator. Diagonal and lag-0 energies
// are accumulated in double; inner products use silk_inner_product_FLP.

// silk_corrVector_FLP — Calculates correlation vector X'*t.
func silk_corrVector_FLP(
	x, t []silk_float,
	L, Order opus_int,
	Xt []silk_float,
	arch int,
) {
	ptr1 := Order - 1 // x[Order-1] is first sample of column 0 of X.
	for lag := opus_int(0); lag < Order; lag++ {
		Xt[lag] = silk_float(silk_inner_product_FLP(x[ptr1:], t, L, arch))
		ptr1-- // next column of X
	}
}

// silk_corrMatrix_FLP — Calculates correlation matrix X'*X.
func silk_corrMatrix_FLP(
	x []silk_float,
	L, Order opus_int,
	XX []silk_float,
	arch int,
) {
	ptr1 := Order - 1
	energy := silk_energy_FLP(x[ptr1:], L) // X[:,0]'*X[:,0]
	XX[0*Order+0] = silk_float(energy)
	for j := opus_int(1); j < Order; j++ {
		// C: energy += ptr1[-j]*ptr1[-j] - ptr1[L-j]*ptr1[L-j];
		// ptr1 is silk_float*; the whole RHS is computed in float32
		// (mul, mul, sub), THEN promoted to double for the += on
		// `energy`. Preserve this precision-sequence exactly.
		a := x[ptr1-j]
		b := x[ptr1+L-j]
		rhs := sub_f32(mul_f32(a, a), mul_f32(b, b))
		energy = add_f64(energy, float64(rhs))
		XX[j*Order+j] = silk_float(energy)
	}

	ptr2 := Order - 2 // first sample of column 1 of X
	for lag := opus_int(1); lag < Order; lag++ {
		// X[:,0]' * X[:,lag].
		energy = silk_inner_product_FLP(x[ptr1:], x[ptr2:], L, arch)
		XX[lag*Order+0] = silk_float(energy)
		XX[0*Order+lag] = silk_float(energy)
		for j := opus_int(1); j < Order-lag; j++ {
			// C: energy += ptr1[-j]*ptr2[-j] - ptr1[L-j]*ptr2[L-j];
			// RHS computed entirely in float32, then promoted for the
			// += on the double `energy`.
			a1 := x[ptr1-j]
			a2 := x[ptr2-j]
			b1 := x[ptr1+L-j]
			b2 := x[ptr2+L-j]
			rhs := sub_f32(mul_f32(a1, a2), mul_f32(b1, b2))
			energy = add_f64(energy, float64(rhs))
			XX[(lag+j)*Order+j] = silk_float(energy)
			XX[j*Order+(lag+j)] = silk_float(energy)
		}
		ptr2-- // next column
	}
}
