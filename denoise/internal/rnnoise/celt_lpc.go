package rnnoise

// LPC analysis, ported 1:1 from librnnoise/src/celt_lpc.c (float build).
// The float-mode arch.h macros collapse the shifts to identity and every
// MULT*/MAC to a plain multiply / multiply-add, routed here through the
// strict primitives to match the -ffp-contract=off oracle.

// rnnLpc is celt_lpc.c rnn_lpc: Levinson-Durbin recursion turning the
// autocorrelation ac[0..p] into LPC coefficients lpc[0..p-1].
func rnnLpc(lpc, ac []float32, p int) {
	error := ac[0]
	for i := 0; i < p; i++ {
		lpc[i] = 0
	}
	if ac[0] != 0 {
		for i := 0; i < p; i++ {
			var rr float32
			for j := 0; j < i; j++ {
				rr = mla(rr, lpc[j], ac[i-j]) // rr += lpc[j]*ac[i-j]
			}
			rr = add32(rr, ac[i+1]) // rr += ac[i+1]
			r := (-rr) / error
			lpc[i] = r
			for j := 0; j < (i+1)>>1; j++ {
				tmp1 := lpc[j]
				tmp2 := lpc[i-1-j]
				lpc[j] = mla(tmp1, r, tmp2)     // tmp1 + r*tmp2
				lpc[i-1-j] = mla(tmp2, r, tmp1) // tmp2 + r*tmp1
			}
			error = sub32(error, mul32(mul32(r, r), error)) // error - (r*r)*error
			if error < mul32(0.001, ac[0]) {
				break
			}
		}
	}
}

// rnnAutocorr is celt_lpc.c rnn_autocorr for the no-window (overlap==0)
// path used by pitch downsampling: ac[0..lag] of x[0..n-1]. The FIXED
// scaling logic is absent in the float build; it returns shift==0.
func rnnAutocorr(x, ac []float32, lag, n int) {
	fastN := n - lag
	pitchXcorr(x, x, ac, fastN, lag+1)
	for k := 0; k <= lag; k++ {
		var d float32
		for i := k + fastN; i < n; i++ {
			d = mla(d, x[i], x[i-k]) // d += x[i]*x[i-k]
		}
		ac[k] = add32(ac[k], d)
	}
}
