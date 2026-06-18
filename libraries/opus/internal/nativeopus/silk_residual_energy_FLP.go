package nativeopus

// 1:1 port of libopus/silk/float/residual_energy_FLP.c.
// Weighted residual energy plus per-subframe residual energy helper.

const (
	MAX_ITERATIONS_RESIDUAL_NRG            = 10
	REGULARIZATION_FACTOR       silk_float = 1e-8
)

// silk_residual_energy_covar_FLP — weighted residual energy
// nrg = wxx - 2*wXx'*c + c'*wXX*c
// wXX is symmetric, stored in both triangles; diagonal gets
// regularized if the first pass returns a non-positive energy.
func silk_residual_energy_covar_FLP(
	c []silk_float,
	wXX []silk_float,
	wXx []silk_float,
	wxx silk_float,
	D opus_int,
) silk_float {
	var tmp, nrg silk_float
	var k, i, j opus_int

	// regularization = REGULARIZATION_FACTOR * (wXX[0] + wXX[D*D-1])
	regularization := REGULARIZATION_FACTOR * (wXX[0] + wXX[D*D-1])

	for k = 0; k < MAX_ITERATIONS_RESIDUAL_NRG; k++ {
		nrg = wxx
		tmp = 0.0
		for i = 0; i < D; i++ {
			// tmp += wXx[i] * c[i]
			tmp = fma_add(tmp, wXx[i], c[i])
		}
		// nrg -= 2.0f * tmp   (left-to-right: nrg - (2*tmp)).
		nrg = fma_sub(nrg, 2.0, tmp)

		// nrg += sum over i of c[i] * ( 2*tmp + wXX[i,i]*c[i] )
		// with tmp = sum_{j>i} wXX[i,j] * c[j].
		for i = 0; i < D; i++ {
			tmp = 0.0
			for j = i + 1; j < D; j++ {
				// tmp += wXX_c[i,j] * c[j]  (column-major, matrix_c_ptr).
				tmp = fma_add(tmp, wXX[i+D*j], c[j])
			}
			// inner = 2.0f * tmp + wXX_c[i,i] * c[i]
			inner := fma_add(2.0*tmp, wXX[i+D*i], c[i])
			// nrg += c[i] * inner
			nrg = fma_add(nrg, c[i], inner)
		}
		if nrg > 0 {
			break
		}
		// Add white noise and retry.
		for i = 0; i < D; i++ {
			wXX[i+D*i] += regularization
		}
		regularization *= 2.0
	}
	if k == MAX_ITERATIONS_RESIDUAL_NRG {
		silk_assert(nrg == 0)
		nrg = 1.0
	}
	return nrg
}

// silk_residual_energy_FLP — residual energies of input subframes.
// Uses silk_LPC_analysis_filter_FLP to compute the LPC residual for
// each frame half, then silk_energy_FLP for the per-subframe energy,
// finally scales by gains[k]*gains[k].
func silk_residual_energy_FLP(
	nrgs []silk_float, // [MAX_NB_SUBFR]
	x []silk_float,
	a [2][MAX_LPC_ORDER]silk_float,
	gains []silk_float,
	subfr_length, nb_subfr, LPC_order opus_int,
) {
	const bufLen = (MAX_FRAME_LENGTH + MAX_NB_SUBFR*MAX_LPC_ORDER) / 2
	var LPC_res [bufLen]silk_float
	shift := LPC_order + subfr_length

	silk_LPC_analysis_filter_FLP(LPC_res[:], a[0][:], x[0*shift:], 2*shift, LPC_order)
	// nrgs[0] = gains[0] * gains[0] * silk_energy_FLP(...)
	//   left-to-right: ((g*g) * energy). gains are silk_float, energy is double.
	//   In C: (silk_float)( gains[0] * gains[0] * silk_energy_FLP(...) )
	//   The mul gains*gains is float32; * energy promotes float to double.
	g0 := gains[0] * gains[0]
	g1 := gains[1] * gains[1]
	nrgs[0] = silk_float(float64(g0) * silk_energy_FLP(LPC_res[LPC_order+0*shift:], subfr_length))
	nrgs[1] = silk_float(float64(g1) * silk_energy_FLP(LPC_res[LPC_order+1*shift:], subfr_length))

	if nb_subfr == MAX_NB_SUBFR {
		silk_LPC_analysis_filter_FLP(LPC_res[:], a[1][:], x[2*shift:], 2*shift, LPC_order)
		g2 := gains[2] * gains[2]
		g3 := gains[3] * gains[3]
		nrgs[2] = silk_float(float64(g2) * silk_energy_FLP(LPC_res[LPC_order+0*shift:], subfr_length))
		nrgs[3] = silk_float(float64(g3) * silk_energy_FLP(LPC_res[LPC_order+1*shift:], subfr_length))
	}
}
