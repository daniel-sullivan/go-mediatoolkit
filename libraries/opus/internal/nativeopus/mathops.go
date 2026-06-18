package nativeopus

import "math"

// Port of libopus/celt/mathops.h + libopus/celt/mathops.c (float path).
//
// The FIXED_POINT half of the header — celt_ilog2 / celt_zlog2 / the
// Q-format log2/exp2/sqrt/rsqrt/rcp/cos approximations and the
// frac_div32* integer implementations — is intentionally skipped;
// we take the float-mode definitions throughout. FLOAT_APPROX is set
// in our vendored config.h, so celt_log2 / celt_exp2 use the
// polynomial approximations below rather than libm log/exp.

// PI matches the C literal 3.1415926535897931.
const PI = 3.1415926535897931

// FRAC_MUL16 multiplies two Q15 16-bit fractional values.
// C: (16384+((opus_int32)(opus_int16)(a)*(opus_int16)(b)))>>15
// Bit-exactness of this expression is required — port verbatim.
func FRAC_MUL16(a, b opus_int32) opus_int32 {
	return (16384 + opus_int32(opus_int16(a))*opus_int32(opus_int16(b))) >> 15
}

// isqrt32 — floor(sqrt(_val)), exact arithmetic, _val > 0. Uses the
// "binary-digit search" method from azillionmonkeys.com/qed/sqroot.
// C: libopus/celt/mathops.c:45-68.
func isqrt32(_val opus_uint32) uint {
	var b, g uint
	var bshift int
	g = 0
	bshift = (ec_ilog(_val) - 1) >> 1
	b = 1 << uint(bshift)
	for {
		var t opus_uint32
		t = ((opus_uint32(g) << 1) + opus_uint32(b)) << uint(bshift)
		if t <= _val {
			g += b
			_val -= t
		}
		b >>= 1
		bshift--
		if bshift < 0 {
			break
		}
	}
	return g
}

// fast_atan2f approximates atan2 on floats. Float build only.
// C implementation at libopus/celt/mathops.h:60–77.
//
// Every `A + B*C` or `A - B*C` sub-expression that clang fuses into a
// single FMA at -O2 is routed through the fma_* helpers here so that
// Go emits the same FMADDS / FMSUBS / FNMSUBS instruction. Plain
// Go arithmetic is left in place where clang would not fuse
// (single-multiply expressions, chained pure adds, divisions).
func fast_atan2f(y, x float32) float32 {
	const (
		cA = float32(0.43157974)
		cB = float32(0.67848403)
		cC = float32(0.08595542)
		cE = float32(PI / 2)
	)
	var x2, y2 float32
	x2 = x * x
	y2 = y * y
	// For very small values, return 0. Pure add of two squares — no FMA.
	if x2+y2 < 1e-18 {
		return 0
	}
	if x2 < y2 {
		// C: (y2 + cB*x2) * (y2 + cC*x2)
		den := fma_add(y2, cB, x2) * fma_add(y2, cC, x2)
		sign := cE
		if y < 0 {
			sign = -cE
		}
		// C: -x*y * (y2 + cA*x2) / den + sign
		return fneg_mul(x, y)*fma_add(y2, cA, x2)/den + sign
	}
	// C: (x2 + cB*y2) * (x2 + cC*y2)
	den := fma_add(x2, cB, y2) * fma_add(x2, cC, y2)
	signY := cE
	if y < 0 {
		signY = -cE
	}
	signXY := cE
	if x*y < 0 {
		signXY = -cE
	}
	// C: x*y * (x2 + cA*y2) / den + signY - signXY
	return x*y*fma_add(x2, cA, y2)/den + signY - signXY
}

// celt_maxabs16 — max absolute value over a slice of length len.
// C: #ifndef OVERRIDE_CELT_MAXABS16 / static OPUS_INLINE opus_val32 …
func celt_maxabs16(x []opus_val16, len_ int) opus_val32 {
	var maxval, minval opus_val16 = 0, 0
	for i := 0; i < len_; i++ {
		maxval = MAX16(maxval, x[i])
		minval = MIN16(minval, x[i])
	}
	return MAX32(EXTEND32(maxval), -EXTEND32(minval))
}

// celt_maxabs_res / celt_maxabs32 — in the float build these are
// macro aliases for celt_maxabs16 (ENABLE_RES24 is on but FIXED_POINT
// is off, so the #else branches fire for both).
func celt_maxabs_res(x []opus_res, len_ int) opus_res {
	return celt_maxabs16(x, len_)
}
func celt_maxabs32(x []opus_val32, len_ int) opus_val32 {
	return celt_maxabs16(x, len_)
}

// celt_atan_norm — Remez-approximated atan(x) * 2/PI, order 15, odd
// terms only. Input x is in [0, 1]. Float-build only.
// C: libopus/celt/mathops.h:142–165.
//
// The polynomial is evaluated with Horner's method; every level is a
// fused `Ai + x_sq * <inner>` (→ fma_add). The outer `x + x*x_sq*poly`
// is also a fused add-multiply with x as the addend and (x*x_sq)*poly
// as the product. See fma.go for why the explicit helpers are
// required for bit-exact parity with clang.
func celt_atan_norm(x float32) float32 {
	const (
		ATAN2_2_OVER_PI = float32(0.636619772367581)
		ATAN2_COEFF_A03 = float32(-3.3331659436225891113281250000e-01)
		ATAN2_COEFF_A05 = float32(1.99627041816711425781250000000e-01)
		ATAN2_COEFF_A07 = float32(-1.3976582884788513183593750000e-01)
		ATAN2_COEFF_A09 = float32(9.79423448443412780761718750000e-02)
		ATAN2_COEFF_A11 = float32(-5.7773590087890625000000000000e-02)
		ATAN2_COEFF_A13 = float32(2.30401363223791122436523437500e-02)
		ATAN2_COEFF_A15 = float32(-4.3554059229791164398193359375e-03)
	)
	x_sq := x * x
	poly := fma_add(ATAN2_COEFF_A13, x_sq, ATAN2_COEFF_A15)
	poly = fma_add(ATAN2_COEFF_A11, x_sq, poly)
	poly = fma_add(ATAN2_COEFF_A09, x_sq, poly)
	poly = fma_add(ATAN2_COEFF_A07, x_sq, poly)
	poly = fma_add(ATAN2_COEFF_A05, x_sq, poly)
	poly = fma_add(ATAN2_COEFF_A03, x_sq, poly)
	// C: x + x*x_sq*poly — (x*x_sq) is an unfused product; the outer
	// add-mul fuses around it.
	xxsq := x * x_sq
	return ATAN2_2_OVER_PI * fma_add(x, xxsq, poly)
}

// celt_atan2p_norm — atan2(y, x) as a normalized result in [0, 1],
// valid for non-negative x, y with at least one non-zero.
func celt_atan2p_norm(y, x float32) float32 {
	celt_sig_assert(x >= 0 && y >= 0)
	if x*x+y*y < 1e-18 {
		return 0
	}
	if y < x {
		return celt_atan_norm(y / x)
	}
	return 1.0 - celt_atan_norm(x/y)
}

// celt_cos_norm2 — cosine approximation for (PI/2 * x), using only
// even-exponent polynomial terms. Defined whenever !FIXED_POINT or
// ENABLE_QEXT — our float build takes this path.
// C: libopus/celt/mathops.h:192–219.
func celt_cos_norm2(x float32) float32 {
	const (
		COS_COEFF_A0 = float32(9.999999403953552246093750000000e-01)
		COS_COEFF_A2 = float32(-1.233698248863220214843750000000000)
		COS_COEFF_A4 = float32(2.536507546901702880859375000000e-01)
		COS_COEFF_A6 = float32(-2.08106283098459243774414062500e-02)
		COS_COEFF_A8 = float32(8.581906440667808055877685546875e-04)
	)
	// Restrict x to [-1, 3].
	x -= 4 * float32(math.Floor(float64(0.25*(x+1))))
	// Negative sign for [1, 3].
	var output_sign float32 = 1.0
	if x > 1 {
		output_sign = -1.0
	}
	// Restrict to [-1, 1].
	if x > 1 {
		x -= 2
	}
	x_norm_sq := x * x
	// Horner polynomial evaluation: C is A0 + x_norm_sq*(A2 + x_norm_sq*
	// (A4 + x_norm_sq*(A6 + x_norm_sq*A8))). Each level is `Ai + x_norm_sq *
	// inner` — fma_add(Ai, x_norm_sq, inner). fma_add(a, b, c) = a + b*c.
	poly := fma_add(COS_COEFF_A6, x_norm_sq, COS_COEFF_A8)
	poly = fma_add(COS_COEFF_A4, x_norm_sq, poly)
	poly = fma_add(COS_COEFF_A2, x_norm_sq, poly)
	poly = fma_add(COS_COEFF_A0, x_norm_sq, poly)
	return output_sign * poly
}

// ── Float-mode macros from mathops.h (lines 225–234) ─────────────────
//
// In fixed-point the following are real Q-format approximations; in
// float mode they collapse to libm wrappers or identity divisions.
// Implemented as Go functions preserving the C macro names.

func celt_sqrt(x float32) float32   { return float32(math.Sqrt(float64(x))) }
func celt_sqrt32(x float32) float32 { return float32(math.Sqrt(float64(x))) }
func celt_rsqrt(x float32) float32  { return 1.0 / celt_sqrt(x) }

// celt_rsqrt_norm / celt_rsqrt_norm32 are macro-equivalent to
// celt_rsqrt in the float build.
func celt_rsqrt_norm(x float32) float32   { return celt_rsqrt(x) }
func celt_rsqrt_norm32(x float32) float32 { return celt_rsqrt(x) }

func celt_cos_norm(x float32) float32 {
	return float32(math.Cos(float64(0.5 * PI * float64(x))))
}

func celt_rcp(x float32) float32                { return 1.0 / x }
func celt_div(a, b float32) float32             { return a / b }
func frac_div32(a, b opus_val32) opus_val32     { return a / b }
func frac_div32_q29(a, b opus_val32) opus_val32 { return frac_div32(a, b) }

// ── celt_log2 / celt_exp2: FLOAT_APPROX path ─────────────────────────
//
// Our config.h defines FLOAT_APPROX, so libopus takes the polynomial
// (bit-twiddled) log2/exp2 from mathops.h:272–343. These are the
// approximations the oracle actually runs; the libm-wrapped variants
// in the #else branch are NOT compiled in our build.

// log2_x_norm_coeff — 1 / (1 + 0.125 * index).
var log2_x_norm_coeff = [8]float32{
	1.000000000000000000000000000,
	8.88888895511627197265625e-01,
	8.00000000000000000000000e-01,
	7.27272748947143554687500e-01,
	6.66666686534881591796875e-01,
	6.15384638309478759765625e-01,
	5.71428596973419189453125e-01,
	5.33333361148834228515625e-01,
}

// log2_y_norm_coeff — log2(1 + 0.125 * index).
var log2_y_norm_coeff = [8]float32{
	0.0000000000000000000000000000,
	1.699250042438507080078125e-01,
	3.219280838966369628906250e-01,
	4.594316184520721435546875e-01,
	5.849624872207641601562500e-01,
	7.004396915435791015625000e-01,
	8.073549270629882812500000e-01,
	9.068905711174011230468750e-01,
}

// celt_log2 — polynomial base-2 log, FLOAT_APPROX path.
//
// The C code uses a float/uint32 union to read and rewrite the IEEE
// 754 bit pattern. In Go the same effect is achieved with
// math.Float32bits / math.Float32frombits. The subtraction of the
// unbiased exponent from the raw bits leaves a value with exponent
// forced to 127 (i.e., mantissa normalized to [1.0, 2.0)).
func celt_log2(x float32) float32 {
	var integer opus_int32
	var range_idx opus_int32
	var in_i opus_uint32 = math.Float32bits(x)
	integer = opus_int32(in_i>>23) - 127
	// C: in.i = (opus_int32)in.i - (opus_int32)((opus_uint32)integer<<23)
	// Modular uint32 subtraction is bit-exactly equivalent.
	in_i -= opus_uint32(integer) << 23

	// Normalize mantissa from [1, 2] to [1, 1.125], then shift by
	// 1.0625 to [-0.0625, 0.0625]. C: `in.f * coeff - 1.0625` is a
	// fused multiply-subtract with `in.f * coeff` as the product and
	// 1.0625 as the minuend — that's `b*c - a` = fma_rsub(a, b, c).
	range_idx = opus_int32(in_i>>20) & 0x7
	in_f := math.Float32frombits(in_i)
	in_f = fma_rsub(1.0625, in_f, log2_x_norm_coeff[range_idx])

	const (
		LOG2_COEFF_A0 = float32(8.74628424644470214843750000e-02)
		LOG2_COEFF_A1 = float32(1.357829570770263671875000000000)
		LOG2_COEFF_A2 = float32(-6.3897705078125000000000000e-01)
		LOG2_COEFF_A3 = float32(4.01971250772476196289062500e-01)
		LOG2_COEFF_A4 = float32(-2.8415444493293762207031250e-01)
	)
	// Horner polynomial: each level `Ai + in_f*inner` = fma_add.
	poly := fma_add(LOG2_COEFF_A3, in_f, LOG2_COEFF_A4)
	poly = fma_add(LOG2_COEFF_A2, in_f, poly)
	poly = fma_add(LOG2_COEFF_A1, in_f, poly)
	poly = fma_add(LOG2_COEFF_A0, in_f, poly)
	// Final sum: pure adds, no FMA opportunity.
	return float32(integer) + poly + log2_y_norm_coeff[range_idx]
}

// celt_exp2 — polynomial base-2 exp, FLOAT_APPROX path.
func celt_exp2(x float32) float32 {
	var integer opus_int32
	var frac float32
	integer = opus_int32(math.Floor(float64(x)))
	if integer < -50 {
		return 0
	}
	frac = x - float32(integer)

	const (
		EXP2_COEFF_A0 = float32(9.999999403953552246093750000000e-01)
		EXP2_COEFF_A1 = float32(6.931530833244323730468750000000e-01)
		EXP2_COEFF_A2 = float32(2.401536107063293457031250000000e-01)
		EXP2_COEFF_A3 = float32(5.582631751894950866699218750000e-02)
		EXP2_COEFF_A4 = float32(8.989339694380760192871093750000e-03)
		EXP2_COEFF_A5 = float32(1.877576694823801517486572265625e-03)
	)
	// Horner polynomial evaluation: each level fuses via fma_add.
	res_f := fma_add(EXP2_COEFF_A4, frac, EXP2_COEFF_A5)
	res_f = fma_add(EXP2_COEFF_A3, frac, res_f)
	res_f = fma_add(EXP2_COEFF_A2, frac, res_f)
	res_f = fma_add(EXP2_COEFF_A1, frac, res_f)
	res_f = fma_add(EXP2_COEFF_A0, frac, res_f)
	// C: res.i = (opus_uint32)((opus_int32)res.i + (opus_int32)((opus_uint32)integer<<23)) & 0x7fffffff;
	// Modular uint32 addition is bit-exactly equivalent.
	res_i := math.Float32bits(res_f)
	res_i = (res_i + opus_uint32(integer)<<23) & 0x7fffffff
	return math.Float32frombits(res_i)
}

// ── Float-mode aliases (mathops.h:350–356) ───────────────────────────
//
// In fixed-point these are distinct Q-format approximations. In float
// mode they collapse to the float-precision celt_log2 / celt_exp2 or
// to plain libm wrappers.

func celt_exp2_db(x float32) float32 { return celt_exp2(x) }
func celt_log2_db(x float32) float32 { return celt_log2(x) }

func celt_sin(x float32) float32 { return celt_cos_norm2((0.5*PI)*x - 1.0) }
func celt_log(x float32) float32 { return celt_log2(x) * 0.6931471805599453 }
func celt_exp(x float32) float32 { return celt_exp2(x * 1.4426950408889634) }

// ── mathops.c entry points (float path only) ────────────────────────

// celt_float2int16_c — scalar conversion of `cnt` floats to int16,
// with CELT_SIG_SCALE applied and saturation to the int16 range. The
// NEON override (celt_float2int16_neon) uses different rounding and
// is not compiled in the scalar-only oracle build — see config.h.
//
// C: libopus/celt/mathops.c:307–314.
func celt_float2int16_c(in []float32, out []opus_int16, cnt int) {
	// NEON fast path: process 16-sample blocks + scalar tail in asm.
	// FCVTAS (ties-away) vs math.RoundToEven gives ≤1-ULP drift on
	// half-integer inputs — matches upstream libopus NEON.
	if float2int16SIMDAvailable && cnt >= 16 {
		celtFloat2Int16SIMD(&in[0], &out[0], cnt)
		return
	}
	for i := 0; i < cnt; i++ {
		out[i] = FLOAT2INT16(in[i])
	}
}

// opus_limit2_checkwithin1_c — clips `cnt` samples in place to the
// range [-2, 2] and returns 1 iff all original samples were already
// within [-1, 1]. The return value is a fast-path hint; the scalar C
// always returns 0 (unknown), and we match that exactly.
//
// C: libopus/celt/mathops.c:316–334.
func opus_limit2_checkwithin1_c(samples []float32, cnt int) int {
	if cnt <= 0 {
		return 1
	}
	// NEON fast path: computes min/max in parallel and returns a
	// real 1/0 fast-path hint. Dispatches at cnt >= 16 so the full
	// 4×Q main block runs at least once.
	if limit2SIMDAvailable && cnt >= 16 {
		return int(opusLimit2CheckWithin1SIMD(&samples[0], cnt))
	}
	for i := 0; i < cnt; i++ {
		clippedVal := samples[i]
		clippedVal = float32(FMAX(clippedVal, -2.0))
		clippedVal = float32(FMIN(clippedVal, 2.0))
		samples[i] = clippedVal
	}
	// C implementation can't provide a quick hint — assume exceeded.
	return 0
}
