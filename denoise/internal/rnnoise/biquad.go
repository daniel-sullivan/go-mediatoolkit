package rnnoise

// biquad ports rnn_biquad (librnnoise/src/denoise.c). It is the input
// high-pass pre-emphasis. The trap: the filter STATE (mem[2],
// DenoiseState.mem_hp_x) is stored as float32, but each update's
// multiply-accumulate is computed in float64 — C casts the samples to
// (double) inside the parenthesised product-difference:
//
//	yi     = x[i] + mem[0];                                 // float32
//	mem[0] = mem[1] + (b[0]*(double)xi - a[0]*(double)yi);  // double->float32
//	mem[1] =          (b[1]*(double)xi - a[1]*(double)yi);  // double->float32
//	y[i]   = yi;
//
// The two double multiplies feed a subtract, so under the
// -ffp-contract=off oracle they do not fuse; mulsub64 preserves that.
// The `mem[1] + (...)` outer add is a plain double add (add64). Old
// mem[1] is read before mem[1] is overwritten, so mem[0] is computed
// first.
func biquad(y []float32, mem *[2]float32, x []float32, b, a [2]float32, n int) {
	for i := 0; i < n; i++ {
		xi := x[i]
		yi := x[i] + mem[0]
		newMem0 := float32(add64(float64(mem[1]),
			mulsub64(float64(b[0]), float64(xi), float64(a[0]), float64(yi))))
		newMem1 := float32(mulsub64(float64(b[1]), float64(xi), float64(a[1]), float64(yi)))
		mem[0] = newMem0
		mem[1] = newMem1
		y[i] = yi
	}
}

// hpB / hpA are the fixed high-pass biquad coefficients from
// rnnoise_process_frame (denoise.c): static const float b_hp/a_hp.
var (
	hpB = [2]float32{-2, 1}
	hpA = [2]float32{-1.99599, 0.99600}
)
