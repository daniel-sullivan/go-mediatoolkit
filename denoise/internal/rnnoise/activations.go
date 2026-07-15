package rnnoise

// Activation functions, ported 1:1 from librnnoise/src/vec.h GENERIC
// branch (tanh_approx, sigmoid_approx). These are rational-polynomial
// approximations, NOT libm tanh/sigmoid — the oracle is forced onto this
// same generic branch (-DDISABLE_NEON), so porting the polynomial
// verbatim is exact. vec.h's `fmadd(a,b,c) = (a)*(b)+(c)` is a plain
// (non-fused, under -ffp-contract=off) multiply-add: fmadd(a,b,c) ==
// mla(c, a, b) == c + a*b.

// tanhApprox is vec.h tanh_approx.
func tanhApprox(x float32) float32 {
	const (
		n0 float32 = 952.52801514
		n1 float32 = 96.39235687
		n2 float32 = 0.60863042
		d0 float32 = 952.72399902
		d1 float32 = 413.36801147
		d2 float32 = 11.88600922
	)
	x2 := mul32(x, x)
	num := mla(n0, mla(n1, n2, x2), x2) // ((n2*x2+n1)*x2+n0)
	den := mla(d0, mla(d1, d2, x2), x2)
	num = mul32(num, x) / den
	return maxf(-1, minf(1, num))
}

// sigmoidApprox is vec.h sigmoid_approx: .5 + .5*tanh_approx(.5*x).
func sigmoidApprox(x float32) float32 {
	return add32(0.5, mul32(0.5, tanhApprox(mul32(0.5, x))))
}
