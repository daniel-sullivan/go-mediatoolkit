package nativeflac

import "math"

// 1:1 port of libflac/src/libFLAC/lpc.c, encoder analysis side.
//
// This file mirrors the four encoder-facing routines libFLAC uses to
// turn a windowed signal into a quantised LPC predictor + residual:
//
//   - LPCComputeAutocorrelation       (FLAC__lpc_compute_autocorrelation)
//   - LPCComputeLPCoefficients        (FLAC__lpc_compute_lp_coefficients)
//   - LPCQuantizeCoefficients         (FLAC__lpc_quantize_coefficients)
//   - LPCComputeResidualFromQLPCoefficients      (+ _wide)
//
// The autocorrelation and Levinson-Durbin steps are float64-heavy; for
// bit-exact parity with the cgo oracle the [StrictMode] build must
// preserve libFLAC's exact scalar multiply/add ordering with FMA
// disabled. Go's spec forbids the compiler from contracting a*b+c into
// an FMA across separate statements/expressions unless explicitly fused,
// so the straightforward translation already matches libFLAC's scalar
// path; the strict tag exists to route around any SIMD/FMA fast path a
// future optimisation might add. The quantise + residual steps are pure
// integer arithmetic and are bit-exact unconditionally.
//
// Note on FLAC__real: libFLAC types the windowed signal as `float`
// (float32), with the autocorrelation accumulators and Levinson state in
// `double` (float64). The Go port carries the windowed signal as
// []float32 to reproduce the float32 rounding of the windowing step, and
// promotes to float64 exactly where the C does.

// LPCComputeAutocorrelation — port of FLAC__lpc_compute_autocorrelation
// (lpc.c:110). Computes autoc[0..lag-1] = Σ data[i]·data[i-coeff] over
// the float32 windowed signal, accumulating in float64.
//
// libFLAC selects between two loop structures based on data_len and lag.
// The two structures accumulate the same products in a different order,
// which is observable in the float64 result, so the port reproduces the
// branch selection exactly. The lag<=16 SIMD include
// (deduplication/lpc_compute_autocorrelation_intrin.c) is the scalar
// reference body of the asm variants; the parity oracle links the
// scalar FLAC__lpc_compute_autocorrelation symbol, so that body is what
// we match.
//
// autoc must have length >= lag; data must have length data_len. lag>0
// and lag<=data_len (FLAC__ASSERT in the C).
func LPCComputeAutocorrelation(data []float32, dataLen uint32, lag uint32, autoc []float64) {
	if dataLen < MaxLPCOrder || lag > 16 {
		// Locality path (lpc.c:138-156): better data locality because
		// data_len is usually much larger than lag.
		limit := dataLen - lag
		for coeff := uint32(0); coeff < lag; coeff++ {
			autoc[coeff] = 0.0
		}
		var sample uint32
		for sample = 0; sample <= limit; sample++ {
			d := float64(data[sample])
			for coeff := uint32(0); coeff < lag; coeff++ {
				autoc[coeff] = f64add(autoc[coeff], f64mul(d, float64(data[sample+coeff])))
			}
		}
		for ; sample < dataLen; sample++ {
			d := float64(data[sample])
			for coeff := uint32(0); coeff < dataLen-sample; coeff++ {
				autoc[coeff] = f64add(autoc[coeff], f64mul(d, float64(data[sample+coeff])))
			}
		}
		return
	}
	// MAX_LAG include paths (lpc.c:158-172). The body
	// (lpc_compute_autocorrelation_intrin.c) is identical for
	// MAX_LAG ∈ {8,12,16}; only the compile-time MAX_LAG differs. We
	// reproduce it with maxLag = the bucket lag falls into.
	var maxLag uint32
	switch {
	case lag <= 8:
		maxLag = 8
	case lag <= 12:
		maxLag = 12
	default: // lag <= 16
		maxLag = 16
	}
	lpcAutocorrelationMaxLag(data, dataLen, maxLag, autoc)
}

// lpcAutocorrelationMaxLag — port of
// deduplication/lpc_compute_autocorrelation_intrin.c. Scalar reference
// body shared by the SSE2/FMA/NEON autocorrelation variants; here it is
// the body the scalar FLAC__lpc_compute_autocorrelation dispatches to
// for lag<=16. Casts both operands to double exactly as the C does.
//
// Implementations are build-tag split:
//   - flac_strict / non-arm64: lpc_autoc_strict.go keeps libFLAC's exact
//     scalar multiply/add ordering (FMA-free via the f64 //go:noinline
//     helpers) so the strict parity gate stays bit-exact against the
//     -ffp-contract=off oracle.
//   - arm64 && !flac_strict: lpc_autoc_default.go uses a float64x2 NEON
//     kernel with multiple independent accumulators (breaking the FMADDD
//     latency chain), mirroring
//     lpc_compute_autocorrelation_intrin_neon.c. This reassociates the
//     float64 reduction, so it is NOT bit-exact vs the oracle — only the
//     default (non-strict) build, which is verified by lossless
//     round-trip rather than byte-identical output.

// LPCComputeLPCoefficients — port of FLAC__lpc_compute_lp_coefficients
// (lpc.c:176). Runs Levinson-Durbin recursion over autoc, emitting LP
// coefficients for every order 1..maxOrder in one pass plus the
// per-order prediction error.
//
// lpCoeff is laid out [MaxLPCOrder][MaxLPCOrder] row-major: row (i)
// holds the order-(i+1) coefficients in lpCoeff[i][0..i]. error has
// length >= maxOrder, indexed error[i] for order i+1. Coefficients are
// negated FIR taps (predictor coeffs), stored as float32 (FLAC__real),
// matching lpc.c:209.
//
// Returns the (possibly reduced) maxOrder: if the recursion hits zero
// error early (SF bug 234, lpc.c:212-216) it stops and reports the
// order reached. autoc[0] must be != 0 (FLAC__ASSERT).
func LPCComputeLPCoefficients(autoc []float64, maxOrder uint32, lpCoeff [][]float32, errOut []float64) uint32 {
	var lpc [MaxLPCOrder]float64
	err := autoc[0]

	for i := uint32(0); i < maxOrder; i++ {
		// Sum up this iteration's reflection coefficient.
		//
		// FP-fusion parity: the cgo oracle compiles lpc.c with
		// `-ffp-contract=off` (see the parity packages' #cgo CFLAGS), so
		// clang does NOT contract any `a*b+c` in this recursion — every
		// multiply and add is a separately rounded double operation. Go's
		// arm64 backend, by contrast, would fuse `r - lpc[j]*autoc[i-j]`
		// into FNMSUB and the `lpc[k] += r*…` updates into FMADD. The f64
		// helpers (lpc_fp_strict.go) route each multiply/add/subtract
		// through a //go:noinline boundary so Go's SSA cannot fuse them,
		// reproducing the un-contracted oracle bit-for-bit. f64fma is the
		// separately-rounded a*b+c (NOT a fused FMA) in the strict build;
		// the default build (lpc_fp_default.go) uses a real fused math.FMA
		// and is not a parity target.
		r := -autoc[i+1]
		for j := uint32(0); j < i; j++ {
			r = f64sub(r, f64mul(lpc[j], autoc[i-j]))
		}
		r /= err

		// Update LPC coefficients and total error (lpc.c:199-203):
		// `lpc[k] += r * …`, a multiply then add, separately rounded.
		lpc[i] = r
		var j uint32
		for j = 0; j < (i >> 1); j++ {
			tmp := lpc[j]
			lpc[j] = f64fma(r, lpc[i-1-j], lpc[j])
			lpc[i-1-j] = f64fma(r, tmp, lpc[i-1-j])
		}
		if i&1 != 0 {
			lpc[j] = f64fma(lpc[j], r, lpc[j])
		}

		// err *= (1.0 - r*r) (lpc.c:205). The inner term is the
		// separately-rounded a*b+c form fma(-r, r, 1.0) = 1.0 + (-(r*r)).
		err = err * f64fma(-r, r, 1.0)

		// save this order
		for j = 0; j <= i; j++ {
			lpCoeff[i][j] = float32(-lpc[j]) // negate FIR filter coeff to get predictor coeff
		}
		errOut[i] = err

		// see SF bug https://sourceforge.net/p/flac/bugs/234/
		if err == 0.0 {
			return i + 1
		}
	}
	return maxOrder
}

// LPCQuantizeCoefficients — port of FLAC__lpc_quantize_coefficients
// (lpc.c:220). Quantises the float32 LP coefficients lpCoeff[0..order-1]
// to qlpCoeff[0..order-1] at the given precision, returning the shift.
//
// Return codes match the C:
//   - 0: success
//   - 1: shift fell below the representable minimum (caller must fall
//     back to a fixed predictor / verbatim)
//   - 2: all coefficients were zero (constant-detect should have caught
//     this)
//
// precision is the qlp coeff precision in bits (>= FLAC__MIN_QLP_COEFF_
// PRECISION). The error-feedback rounding (lround on the running error)
// is reproduced exactly, including the negative-shift fallback path
// (lpc.c:287-311) that scales coefficients down and forces shift=0.
func LPCQuantizeCoefficients(lpCoeff []float32, order uint32, precision uint32, qlpCoeff []int32, shift *int) int {
	// drop one bit for the sign; from here on out we consider only |lp_coeff[i]|
	precision--
	qmax := int32(1) << precision
	qmin := -qmax
	qmax--

	// calc cmax = max( |lp_coeff[i]| )
	cmax := 0.0
	for i := uint32(0); i < order; i++ {
		d := math.Abs(float64(lpCoeff[i]))
		if d > cmax {
			cmax = d
		}
	}

	if cmax <= 0.0 {
		// => coefficients are all 0, which means our constant-detect didn't work
		return 2
	}

	const maxShiftLimit = (1 << (SubframeLPCQLPShiftLen - 1)) - 1
	const minShiftLimit = -maxShiftLimit - 1

	_, log2cmax := math.Frexp(cmax)
	log2cmax--
	*shift = int(precision) - log2cmax - 1

	if *shift > maxShiftLimit {
		*shift = maxShiftLimit
	} else if *shift < minShiftLimit {
		return 1
	}

	if *shift >= 0 {
		errAcc := 0.0
		for i := uint32(0); i < order; i++ {
			errAcc = f64add(errAcc, f64mul(float64(lpCoeff[i]), float64(int32(1)<<uint(*shift))))
			q := lpcLround(errAcc)
			if q > qmax {
				q = qmax
			} else if q < qmin {
				q = qmin
			}
			errAcc -= float64(q)
			qlpCoeff[i] = q
		}
	} else {
		// negative shift is very rare but due to design flaw, negative shift is
		// not allowed in the decoder, so it must be handled specially by scaling
		// down coeffs
		nshift := -(*shift)
		errAcc := 0.0
		for i := uint32(0); i < order; i++ {
			errAcc = f64add(errAcc, float64(lpCoeff[i])/float64(int32(1)<<uint(nshift)))
			q := lpcLround(errAcc)
			if q > qmax {
				q = qmax
			} else if q < qmin {
				q = qmin
			}
			errAcc -= float64(q)
			qlpCoeff[i] = q
		}
		*shift = 0
	}

	return 0
}

// lpcLround reproduces C lround for the values produced here: round to
// nearest, ties away from zero (lpc.c:58-64 fallback definition,
// matching glibc lround). The quantiser only ever feeds finite values in
// the int32 range, so the int32 result is exact.
func lpcLround(x float64) int32 {
	return int32(math.Round(x))
}

// LPCComputeResidualFromQLPCoefficients — port of
// FLAC__lpc_compute_residual_from_qlp_coefficients (lpc.c:321). 32-bit
// accumulator path:
//
//	residual[i] = data[i] - (Σ qlpCoeff[j]·data[i-j-1]) >> lpQuantization
//
// data holds the signal with at least `order` warm-up samples preceding
// index 0; the port indexes data with a base offset of `order` so the
// negative C indices data[i-j-1] map to valid Go indices. residual has
// length dataLen. The sum is an int32 with C wraparound semantics, which
// Go int32 arithmetic reproduces exactly.
//
// The C production build (FLAC__LPC_UNROLLED_FILTER_LOOPS) hand-unrolls a
// distinct loop body per order up to 12 (the subset limit) so the inner
// tap loop and its per-tap bounds checks vanish from the hot path; orders
// 13..32 fall through to a single generic loop. The Go port mirrors that:
// each unrolled case hoists the slices into locals and indexes only
// provably in-range elements (qlpCoeff[0..order-1] and data[o+i-1..o+i-order],
// with o+i-order >= 0 and o+i <= o+dataLen-1 <= len(data)-1), so the
// compiler can elide the per-tap bounds checks. The int32 wraparound of the
// accumulating sum is identical to the documented equivalent loop
// (lpc.c:350-356) regardless of the addition order, because int32 addition
// is associative under two's-complement wraparound.
func LPCComputeResidualFromQLPCoefficients(data []int32, dataLen uint32, qlpCoeff []int32, order uint32, lpQuantization int, residual []int32) {
	o := int(order)
	n := int(dataLen)
	// On arm64 the NEON kernel computes the bulk four samples at a time;
	// it is integer-exact (int32 two's-complement) so it runs in both the
	// default and flac_strict builds. The kernel returns the number of
	// samples it consumed (a multiple of 4); the remaining tail is handled
	// by the scalar unrolled path on a shifted sub-problem. data[consumed]
	// onward keeps its order-length warm-up history at data[consumed:],
	// so the shifted call reproduces the same arithmetic exactly.
	if lpcMACAvailable && o >= 1 && n >= 4 {
		consumed := lpcResidualMACNEON(&data[0], n, &qlpCoeff[0], o, lpQuantization, &residual[0])
		if consumed >= n {
			return
		}
		data = data[consumed:]
		residual = residual[consumed:]
		n -= consumed
		dataLen = uint32(n)
	}
	lpcResidualScalar(data, uint32(n), qlpCoeff, order, lpQuantization, residual)
}

// lpcResidualScalar is the pure-Go unrolled scalar body of
// LPCComputeResidualFromQLPCoefficients (orders 1..12 specialized, 13..32
// generic). It is the fallback on non-arm64 and the tail handler after the
// NEON bulk on arm64. Layout matches the public function: data carries the
// order-length warm-up history preceding the dataLen output samples.
func lpcResidualScalar(data []int32, dataLen uint32, qlpCoeff []int32, order uint32, lpQuantization int, residual []int32) {
	o := int(order)
	n := int(dataLen)
	q := qlpCoeff[:o]
	// d aliases data with the warm-up history at d[0..o-1]; d[o+i] is the
	// current sample and d[o+i-1..o+i-o] are the predictor taps.
	d := data
	switch order {
	case 1:
		c0 := q[0]
		for i := 0; i < n; i++ {
			sum := c0 * d[o+i-1]
			residual[i] = d[o+i] - (sum >> lpQuantization)
		}
	case 2:
		c0, c1 := q[0], q[1]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c1*b[0] + c0*b[1]
			residual[i] = b[2] - (sum >> lpQuantization)
		}
	case 3:
		c0, c1, c2 := q[0], q[1], q[2]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c2*b[0] + c1*b[1] + c0*b[2]
			residual[i] = b[3] - (sum >> lpQuantization)
		}
	case 4:
		c0, c1, c2, c3 := q[0], q[1], q[2], q[3]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c3*b[0] + c2*b[1] + c1*b[2] + c0*b[3]
			residual[i] = b[4] - (sum >> lpQuantization)
		}
	case 5:
		c0, c1, c2, c3, c4 := q[0], q[1], q[2], q[3], q[4]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c4*b[0] + c3*b[1] + c2*b[2] + c1*b[3] + c0*b[4]
			residual[i] = b[5] - (sum >> lpQuantization)
		}
	case 6:
		c0, c1, c2, c3, c4, c5 := q[0], q[1], q[2], q[3], q[4], q[5]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c5*b[0] + c4*b[1] + c3*b[2] + c2*b[3] + c1*b[4] + c0*b[5]
			residual[i] = b[6] - (sum >> lpQuantization)
		}
	case 7:
		c0, c1, c2, c3, c4, c5, c6 := q[0], q[1], q[2], q[3], q[4], q[5], q[6]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c6*b[0] + c5*b[1] + c4*b[2] + c3*b[3] + c2*b[4] + c1*b[5] + c0*b[6]
			residual[i] = b[7] - (sum >> lpQuantization)
		}
	case 8:
		c0, c1, c2, c3, c4, c5, c6, c7 := q[0], q[1], q[2], q[3], q[4], q[5], q[6], q[7]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c7*b[0] + c6*b[1] + c5*b[2] + c4*b[3] + c3*b[4] + c2*b[5] + c1*b[6] + c0*b[7]
			residual[i] = b[8] - (sum >> lpQuantization)
		}
	case 9:
		c0, c1, c2, c3, c4, c5, c6, c7, c8 := q[0], q[1], q[2], q[3], q[4], q[5], q[6], q[7], q[8]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c8*b[0] + c7*b[1] + c6*b[2] + c5*b[3] + c4*b[4] + c3*b[5] + c2*b[6] + c1*b[7] + c0*b[8]
			residual[i] = b[9] - (sum >> lpQuantization)
		}
	case 10:
		c0, c1, c2, c3, c4, c5, c6, c7, c8, c9 := q[0], q[1], q[2], q[3], q[4], q[5], q[6], q[7], q[8], q[9]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c9*b[0] + c8*b[1] + c7*b[2] + c6*b[3] + c5*b[4] + c4*b[5] + c3*b[6] + c2*b[7] + c1*b[8] + c0*b[9]
			residual[i] = b[10] - (sum >> lpQuantization)
		}
	case 11:
		c0, c1, c2, c3, c4, c5, c6, c7, c8, c9, c10 := q[0], q[1], q[2], q[3], q[4], q[5], q[6], q[7], q[8], q[9], q[10]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c10*b[0] + c9*b[1] + c8*b[2] + c7*b[3] + c6*b[4] + c5*b[5] + c4*b[6] + c3*b[7] + c2*b[8] + c1*b[9] + c0*b[10]
			residual[i] = b[11] - (sum >> lpQuantization)
		}
	case 12:
		c0, c1, c2, c3, c4, c5, c6, c7, c8, c9, c10, c11 := q[0], q[1], q[2], q[3], q[4], q[5], q[6], q[7], q[8], q[9], q[10], q[11]
		for i := 0; i < n; i++ {
			b := d[i : o+i+1]
			sum := c11*b[0] + c10*b[1] + c9*b[2] + c8*b[3] + c7*b[4] + c6*b[5] + c5*b[6] + c4*b[7] + c3*b[8] + c2*b[9] + c1*b[10] + c0*b[11]
			residual[i] = b[12] - (sum >> lpQuantization)
		}
	default:
		// Orders 13..32: generic loop (lpc.c:350-356).
		for i := 0; i < n; i++ {
			var sum int32
			for j := 0; j < o; j++ {
				sum += q[j] * d[o+i-j-1]
			}
			residual[i] = d[o+i] - (sum >> lpQuantization)
		}
	}
}

// LPCComputeResidualFromQLPCoefficientsWide — port of
// FLAC__lpc_compute_residual_from_qlp_coefficients_wide (lpc.c:582).
// int64 accumulator; the residual is truncated to int32 (matching the
// C cast at lpc.c:606). The C overflow-detect build breaks on a >32-bit
// residual, but the production unrolled path does not — the port mirrors
// the production path and always writes the truncated residual.
func LPCComputeResidualFromQLPCoefficientsWide(data []int32, dataLen uint32, qlpCoeff []int32, order uint32, lpQuantization int, residual []int32) {
	o := int(order)
	for i := 0; i < int(dataLen); i++ {
		var sum int64
		for j := 0; j < o; j++ {
			sum += int64(qlpCoeff[j]) * int64(data[o+i-j-1])
		}
		residual[i] = int32(int64(data[o+i]) - (sum >> lpQuantization))
	}
}

// FP-ordering: LPCComputeExpectedBitsPerResidualSample and LPCComputeBestOrder
// are pure double-precision chains with no FMA-fusable a*b+c spanning a single
// multiply; Go's straightforward translation matches libFLAC's scalar path, so
// no flac_strict split is required here. The window-data multiply
// (LPCWindowData family) routes its single float32 multiply through f32mul so
// the strict build (window_fp_strict.go) controls fusing exactly as the rest
// of window.c does.

// LPCWindowData — port of FLAC__lpc_window_data (lpc.c:68). Multiplies the
// int32 signal by the precomputed float32 window, storing the float32 product.
// out[i] = (float)in[i] * window[i]. In C, int*float promotes the int to
// float and multiplies in single precision; the Go port mirrors that with
// f32mul(float32(in[i]), window[i]).
func LPCWindowData(in []int32, window []float32, out []float32, dataLen uint32) {
	n := int(dataLen)
	var i int
	// The arm64 NEON kernel (default build) converts int32->float32 and
	// applies a single per-lane FMUL.4S, four samples at a time. It matches
	// the default-build scalar f32mul rounding (plain multiply, no FMA) and
	// is absent in flac_strict, where the //go:noinline scalar path runs.
	if windowMulNEONAvailable && n >= 4 {
		i = windowDataMulNEON(&in[0], &window[0], &out[0], n)
	}
	for ; i < n; i++ {
		out[i] = f32mul(float32(in[i]), window[i])
	}
}

// LPCWindowDataWide — port of FLAC__lpc_window_data_wide (lpc.c:75). Identical
// to LPCWindowData but the input is int64 (33-bit side channel). int64*float
// converts the int64 to float (single precision) then multiplies.
func LPCWindowDataWide(in []int64, window []float32, out []float32, dataLen uint32) {
	for i := uint32(0); i < dataLen; i++ {
		out[i] = f32mul(float32(in[i]), window[i])
	}
}

// LPCWindowDataPartial — port of FLAC__lpc_window_data_partial (lpc.c:82).
// Windows a sub-block: part_size samples taken from in[data_shift..], using
// window[0..part_size-1] for the leading taper and the window's trailing
// part_size coefficients for the trailing taper, zero-padding one sample if a
// gap remains. The faithful loop structure (and its flac_min clamp on i) is
// reproduced verbatim.
func LPCWindowDataPartial(in []int32, window []float32, out []float32, dataLen, partSize, dataShift uint32) {
	if (partSize + dataShift) < dataLen {
		var i, j uint32
		// Leading taper: out[i] = float32(in[dataShift+i]) * window[i] for
		// i in [0, partSize). The arm64 NEON kernel (default build) handles
		// the bulk four at a time; the scalar f32mul finishes the tail and
		// runs entirely in flac_strict / non-arm64.
		if windowMulNEONAvailable && partSize >= 4 {
			i = uint32(windowDataMulNEON(&in[dataShift], &window[0], &out[0], int(partSize)))
		}
		for ; i < partSize; i++ {
			out[i] = f32mul(float32(in[dataShift+i]), window[i])
		}
		if v := dataLen - partSize - dataShift; v < i {
			i = v
		}
		for j = dataLen - partSize; j < dataLen; i, j = i+1, j+1 {
			out[i] = f32mul(float32(in[dataShift+i]), window[j])
		}
		if i < dataLen {
			out[i] = 0.0
		}
	}
}

// LPCWindowDataPartialWide — port of FLAC__lpc_window_data_partial_wide
// (lpc.c:96). int64 input variant of LPCWindowDataPartial.
func LPCWindowDataPartialWide(in []int64, window []float32, out []float32, dataLen, partSize, dataShift uint32) {
	if (partSize + dataShift) < dataLen {
		var i, j uint32
		for i = 0; i < partSize; i++ {
			out[i] = f32mul(float32(in[dataShift+i]), window[i])
		}
		if v := dataLen - partSize - dataShift; v < i {
			i = v
		}
		for j = dataLen - partSize; j < dataLen; i, j = i+1, j+1 {
			out[i] = f32mul(float32(in[dataShift+i]), window[j])
		}
		if i < dataLen {
			out[i] = 0.0
		}
	}
}

// LPCComputeExpectedBitsPerResidualSample — port of
// FLAC__lpc_compute_expected_bits_per_residual_sample (lpc.c:1580) and its
// _with_error_scale helper (lpc.c:1591). total_samples must be > 0.
func LPCComputeExpectedBitsPerResidualSample(lpcError float64, totalSamples uint32) float64 {
	errorScale := 0.5 / float64(totalSamples)
	return lpcComputeExpectedBitsPerResidualSampleWithErrorScale(lpcError, errorScale)
}

// lpcComputeExpectedBitsPerResidualSampleWithErrorScale — port of
// FLAC__lpc_compute_expected_bits_per_residual_sample_with_error_scale
// (lpc.c:1591).
func lpcComputeExpectedBitsPerResidualSampleWithErrorScale(lpcError, errorScale float64) float64 {
	if lpcError > 0.0 {
		bps := 0.5 * math.Log(errorScale*lpcError) / mLn2
		if bps >= 0.0 {
			return bps
		}
		return 0.0
	} else if lpcError < 0.0 {
		// Error should not be negative but can happen due to inadequate
		// floating-point resolution.
		return 1e32
	}
	return 0.0
}

// LPCComputeBestOrder — port of FLAC__lpc_compute_best_order (lpc.c:1608).
// Picks the LPC order minimising estimated total bits = expected residual bits
// per sample * (total_samples - order) + order * overhead_bits_per_order.
// lpcError[indx] is the error for order indx+1. max_order and total_samples
// must be > 0. Returns the chosen order (1-based).
func LPCComputeBestOrder(lpcError []float64, maxOrder uint32, totalSamples uint32, overheadBitsPerOrder uint32) uint32 {
	errorScale := 0.5 / float64(totalSamples)

	bestIndex := uint32(0)
	// best_bits is initialised to (uint32_t)(-1) then immediately compared as a
	// double; (uint32_t)(-1) == 4294967295, promoted to double.
	bestBits := float64(uint32(0xFFFFFFFF))

	order := uint32(1)
	for indx := uint32(0); indx < maxOrder; indx, order = indx+1, order+1 {
		bits := lpcComputeExpectedBitsPerResidualSampleWithErrorScale(lpcError[indx], errorScale)*float64(totalSamples-order) + float64(order*overheadBitsPerOrder)
		if bits < bestBits {
			bestIndex = indx
			bestBits = bits
		}
	}

	return bestIndex + 1 // +1 since indx of lpcError[] is order-1
}

// LPCComputeResidualFromQLPCoefficientsLimitResidual — port of
// FLAC__lpc_compute_residual_from_qlp_coefficients_limit_residual (lpc.c:832).
// int64 accumulator with overflow detection: if any residual would not fit in
// int32 (residual_to_check <= INT32_MIN || > INT32_MAX) the function returns
// false, telling the caller to fall back. data holds `order` warm-up samples
// preceding index 0; the port indexes with a base offset of `order`.
//
// Note the C multiply qlp_coeff[j] * (FLAC__int64)data[i-j-1]: qlp_coeff is
// int32, data is widened to int64, so the product is int64. Reproduced with
// int64(qlpCoeff[j]) * int64(data[...]).
func LPCComputeResidualFromQLPCoefficientsLimitResidual(data []int32, dataLen uint32, qlpCoeff []int32, order uint32, lpQuantization int, residual []int32) bool {
	o := int(order)
	for i := 0; i < int(dataLen); i++ {
		var sum int64
		for j := 0; j < o; j++ {
			sum += int64(qlpCoeff[j]) * int64(data[o+i-j-1])
		}
		residualToCheck := int64(data[o+i]) - (sum >> lpQuantization)
		// Residual must not be INT32_MIN because abs(INT32_MIN) is undefined.
		if residualToCheck <= math.MinInt32 || residualToCheck > math.MaxInt32 {
			return false
		}
		residual[i] = int32(residualToCheck)
	}
	return true
}

// LPCComputeResidualFromQLPCoefficientsLimitResidual33Bit — port of
// FLAC__lpc_compute_residual_from_qlp_coefficients_limit_residual_33bit
// (lpc.c:886). int64 input (33-bit side channel) variant. Here the C multiply
// is qlp_coeff[j] * data[i-j-1] where data is already int64, so qlp_coeff
// (int32) is promoted to int64 first.
func LPCComputeResidualFromQLPCoefficientsLimitResidual33Bit(data []int64, dataLen uint32, qlpCoeff []int32, order uint32, lpQuantization int, residual []int32) bool {
	o := int(order)
	for i := 0; i < int(dataLen); i++ {
		var sum int64
		for j := 0; j < o; j++ {
			sum += int64(qlpCoeff[j]) * data[o+i-j-1]
		}
		residualToCheck := data[o+i] - (sum >> lpQuantization)
		if residualToCheck <= math.MinInt32 || residualToCheck > math.MaxInt32 {
			return false
		}
		residual[i] = int32(residualToCheck)
	}
	return true
}
