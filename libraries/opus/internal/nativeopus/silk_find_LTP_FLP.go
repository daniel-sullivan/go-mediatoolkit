package nativeopus

// 1:1 port of libopus/silk/float/find_LTP_FLP.c.
// LTP correlation & weighting for each subframe. Builds per-subframe
// correlation matrix XX' and correlation vector xX', then scales by
// temp = 1 / max(xx, LTP_CORR_INV_MAX*0.5*(XX[0]+XX[24]) + 1).
// All float32 arithmetic; each a+b*c / a-b*c is separately rounded in
// the C oracle (-ffp-contract=off), so use fma helpers on the Go side.
//
// C takes `const silk_float r_ptr[]` as a raw pointer into a larger
// buffer so that `r_ptr - (lag[k] + LTP_ORDER/2)` is a valid address.
// The Go port takes the full backing slice plus `rOff`, the absolute
// starting index; rOff - max(lag) - LTP_ORDER/2 must be non-negative.

func silk_find_LTP_FLP(
	XX []silk_float, // [MAX_NB_SUBFR * LTP_ORDER * LTP_ORDER]
	xX []silk_float, // [MAX_NB_SUBFR * LTP_ORDER]
	rBuf []silk_float, // I   LPC residual (backing slice)
	rOff opus_int, // I   absolute start index into rBuf for r_ptr
	lag []opus_int, // I   [MAX_NB_SUBFR] LTP lags
	subfr_length opus_int,
	nb_subfr opus_int,
	arch int,
) {
	var k opus_int
	var xX_off opus_int
	var XX_off opus_int

	var xx, temp silk_float

	for k = 0; k < nb_subfr; k++ {
		// lag_ptr = r_ptr - ( lag[ k ] + LTP_ORDER / 2 )
		lagOff := rOff - (lag[k] + LTP_ORDER/2)

		silk_corrMatrix_FLP(rBuf[lagOff:], subfr_length, LTP_ORDER, XX[XX_off:], arch)
		silk_corrVector_FLP(rBuf[lagOff:], rBuf[rOff:], subfr_length, LTP_ORDER, xX[xX_off:], arch)

		// xx = (silk_float)silk_energy_FLP( r_ptr, subfr_length + LTP_ORDER )
		xx = silk_float(silk_energy_FLP(rBuf[rOff:], subfr_length+LTP_ORDER))

		// temp = 1.0f / silk_max( xx, LTP_CORR_INV_MAX * 0.5f * ( XX_ptr[0] + XX_ptr[24] ) + 1.0f )
		// LTP_CORR_INV_MAX is `#define ... 0.03f` so the entire RHS is
		// float32 throughout. Each op is separately rounded under
		// -ffp-contract=off. Left-to-right grouping for the mul chain:
		//   m   = (LTP_CORR_INV_MAX * 0.5f) * (XX[0] + XX[24])
		// and the cap = m + 1.0f.
		s := add_f32(XX[XX_off+0], XX[XX_off+24])
		// (LTP_CORR_INV_MAX * 0.5f) is a compile-time float32 constant
		// 0.015f in C. Mirror by pre-folding as a typed float32.
		const ltpHalfInvMax silk_float = silk_float(LTP_CORR_INV_MAX) * silk_float(0.5)
		prod := mul_f32(ltpHalfInvMax, s)
		cap_ := add_f32(prod, 1.0)
		var maxF silk_float
		if xx > cap_ {
			maxF = xx
		} else {
			maxF = cap_
		}
		// 1.0f / maxF — float32 division, one round, then assign.
		temp = 1.0 / maxF

		silk_scale_vector_FLP(XX[XX_off:], temp, LTP_ORDER*LTP_ORDER)
		silk_scale_vector_FLP(xX[xX_off:], temp, LTP_ORDER)

		rOff += subfr_length
		XX_off += LTP_ORDER * LTP_ORDER
		xX_off += LTP_ORDER
	}
}
