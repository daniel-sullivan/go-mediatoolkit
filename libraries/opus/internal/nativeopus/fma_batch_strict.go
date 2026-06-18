//go:build opus_strict

package nativeopus

// Batched FMA helpers — strict (bit-exact parity) variant.
//
// Selected via `-tags=opus_strict`. Amortizes the per-element BL
// overhead of the non-fused `fma_*` wrappers by processing whole
// slices under one //go:noinline boundary.
//
// Strategy: each product goes through the existing //go:noinline
// `mul_f32` / `mul_f64` so it is an opaque CALL return value that Go's
// arm64 SSA cannot pattern-match back into `Add(x, Mul(y, z))`, so no
// FMADDS/FMADDD fusion is possible. The accumulator adds use the inline
// `+` operator — Go still emits a plain FADD because the adds are on
// call-return operands, not on visible Mul nodes.
//
// Rounding note: the `add_f32` comment in fma_strict.go flags a case
// where inline `a+b` on Go 1.26 arm64 produced a 1-ULP different
// last-bit rounding from the //go:noinline `add_f32` version. These
// batched helpers are gated by the three-way parity test — if the
// quirk manifests for any workload, the helper falls back to
// `add_f32`/`add_f64` for that specific accumulator at the cost of
// one extra BL per element.
//
// Cost model per element:
//   - pre-batching fma_add(): 2 BLs (mul_f32 + add_f32)
//   - batched with inline add: 1 BL (mul_f32 only) + amortized outer call
//   - batched with add_f32:    2 BLs (same as inline, no saving)

// celt_inner_prod_batch returns sum_i x[i]*y[i] as float32, with each
// product separately rounded via mul_f32 and the running sum
// accumulated via inline FADD. Matches the loop body of
// celt_inner_prod_c: `xy = fma_add(xy, x[i], y[i])`.
//
//go:noinline
func celt_inner_prod_batch(x, y []opus_val16, N int) opus_val32 {
	var xy opus_val32
	for i := 0; i < N; i++ {
		xy = xy + mul_f32(x[i], y[i])
	}
	return xy
}

// dual_inner_prod_batch returns (sum_i x[i]*y01[i], sum_i x[i]*y02[i]).
// Matches the two-parallel-accumulator loop in dual_inner_prod_c.
//
//go:noinline
func dual_inner_prod_batch(x, y01, y02 []opus_val16, N int) (xy01, xy02 opus_val32) {
	for i := 0; i < N; i++ {
		xy01 = xy01 + mul_f32(x[i], y01[i])
		xy02 = xy02 + mul_f32(x[i], y02[i])
	}
	return
}

// silk_inner_product_batch returns the f32-data / f64-accumulator dot
// product used by the SILK _FLP paths. Preserves the 4x-unrolled
// left-associative grouping `((p0+p1)+p2)+p3` of the C source; the
// unrolled tail uses element-at-a-time accumulation to match
// silk_inner_product_FLP_c.
//
//go:noinline
func silk_inner_product_batch(data1, data2 []silk_float, dataSize opus_int) float64 {
	var i opus_int
	var result float64
	for i = 0; i < dataSize-3; i += 4 {
		p0 := mul_f64(float64(data1[i+0]), float64(data2[i+0]))
		p1 := mul_f64(float64(data1[i+1]), float64(data2[i+1]))
		p2 := mul_f64(float64(data1[i+2]), float64(data2[i+2]))
		p3 := mul_f64(float64(data1[i+3]), float64(data2[i+3]))
		rhs := ((p0 + p1) + p2) + p3
		result = result + rhs
	}
	for ; i < dataSize; i++ {
		p := mul_f64(float64(data1[i]), float64(data2[i]))
		result = result + p
	}
	return result
}

// celt_autocorr_tail_batch computes `d = sum_{i=k+fastN..n-1} x[i]*x[i-k]`
// — the tail loop of `_celt_autocorr` past the celt_pitch_xcorr-covered
// prefix. Caller still adds `d` into ac[k].
//
//go:noinline
func celt_autocorr_tail_batch(xptr []opus_val16, k, fastN, n int) opus_val32 {
	var d opus_val32
	for i := k + fastN; i < n; i++ {
		d = d + mul_f32(xptr[i], xptr[i-k])
	}
	return d
}
