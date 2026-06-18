package nativeflac

// 1:1 port of libflac/src/libFLAC/fixed.c, encoder analysis side:
// FLAC__fixed_compute_best_predictor and
// FLAC__fixed_compute_best_predictor_wide. These pick the best fixed
// order (0..4) for a block by summing the absolute value of the
// finite-difference residual at each order, then estimate the residual
// bits-per-sample for each order so the encoder can compare against the
// LPC path.
//
// The decode-side restore_signal trio lives in fixed.go; the residual
// (forward) helpers FLAC__fixed_compute_residual* are not ported here
// because the encoder state machine that consumes them is not yet
// ported (see nativeflac.go status).
//
// # Caller layout
//
// libFLAC invokes these with `data` already advanced past the warm-up
// history: stream_encoder.c:4088 passes
// `((FLAC__int32 *)integer_signal)+FLAC__MAX_FIXED_ORDER` so that the
// in-loop reads data[i-1]..data[i-4] (for i==0) reach into four valid
// history samples preceding the signal. The Go port mirrors this with a
// single slice: `data` holds FLAC__MAX_FIXED_ORDER (=4) history samples
// followed by `dataLen` signal samples, and the analysis runs over
// data[4 : 4+dataLen]. Pass `dataLen = len(data) - 4`.
//
// # Float estimate
//
// The bits estimate uses libFLAC's exact double-precision chain
//
//	(float)(log(M_LN2 * total_error / data_len) / M_LN2)
//
// truncated to float32 on store. Go's math.Log is the platform libm
// log on the parity targets, and the float64->float32 conversion
// matches C's (float) cast, so the result is bit-identical. There is no
// FMA in this chain, so no strict-mode decomposition is required.

import "math"

// fixedMaxFixedOrder mirrors FLAC__MAX_FIXED_ORDER (format.h). The
// estimate array residual_bits_per_sample has FLAC__MAX_FIXED_ORDER+1
// entries (orders 0..4).
const fixedMaxFixedOrder = 4

// mLn2 is the natural log of 2, matching the C M_LN2 macro value used by
// FLAC__fixed_compute_best_predictor (math.h on the parity targets
// defines M_LN2 to this exact double).
const mLn2 = 0.69314718055994530942

// fixedLocalAbs32 ports the local_abs macro (fixed.c:48):
// ((uint32_t)((x)<0? -(x) : (x))). The negation and cast happen in
// uint32 arithmetic so INT32_MIN maps to 0x80000000 exactly as the C
// macro does (unsigned negation, no UB).
func fixedLocalAbs32(x int32) uint32 {
	if x < 0 {
		return uint32(-x)
	}
	return uint32(x)
}

// FixedComputeBestPredictor ports FLAC__fixed_compute_best_predictor
// (fixed.c:222, non-integer-only build). It sums |residual| at each
// fixed order 0..4 over data[4 : 4+dataLen] (see caller layout above)
// using 32-bit unsigned accumulators, selects the lowest order whose
// error is <= the min of the higher orders, and fills bits[0..4] with
// the per-order residual bits-per-sample estimate. Returns the chosen
// order.
func FixedComputeBestPredictor(data []int32, dataLen uint32, bits *[fixedMaxFixedOrder + 1]float32) uint32 {
	var totalError0, totalError1, totalError2, totalError3, totalError4 uint32

	// Mirror the C reads data[i], data[i-1]..data[i-4] where i runs
	// 0..dataLen-1 relative to a pointer advanced past the 4-sample
	// history. In Go we keep one slice; base indexes the signal start.
	base := fixedMaxFixedOrder
	n := int(dataLen)

	// On arm64 dispatch the bulk to the quarter-split NEON kernel
	// (fixed_abs_arm64.s), exactly mirroring the SSE2 reference. It is
	// integer-exact so it runs in both the default and flac_strict
	// builds. The kernel needs at least one full sample per quarter, so
	// gate on n >= 4; it processes data_len/4 samples per quarter and the
	// data_len%4 tail is handled by the scalar loop below.
	i := 0
	if fixedAbsSIMDAvailable && n >= 4 {
		l4 := n / 4 // data_len/4, the per-quarter iteration count
		// Seed the four per-quarter lag vectors, SoA, exactly as
		// fixed_intrin_sse2.c lines 83-87: prev_errK[q] for quarter q
		// whose signal pointer is data[base + q*l4].
		var prevErr [16]int32
		for q := 0; q < 4; q++ {
			b := base + q*l4
			pe0 := data[b-1]
			pe1 := data[b-1] - data[b-2]
			pe2 := pe1 - (data[b-2] - data[b-3])
			pe3 := pe2 - (data[b-2] - 2*data[b-3] + data[b-4])
			prevErr[q] = pe0
			prevErr[4+q] = pe1
			prevErr[8+q] = pe2
			prevErr[12+q] = pe3
		}
		var totals [5]uint32
		fixedAbsErrors4NEON(
			&data[base+0*l4], &data[base+1*l4], &data[base+2*l4], &data[base+3*l4],
			&prevErr, l4, &totals,
		)
		totalError0 = totals[0]
		totalError1 = totals[1]
		totalError2 = totals[2]
		totalError3 = totals[3]
		totalError4 = totals[4]
		// The kernel consumed l4 samples in each of the four quarters,
		// i.e. the first 4*l4 = n - (n%4) samples. The scalar loop below
		// finishes the n%4 remainder, matching the C scalar tail (which
		// rebuilds its last_error_* from data[i-1..i-4]).
		i = 4 * l4
	}

	for ; i < n; i++ {
		d0 := data[base+i]
		d1 := data[base+i-1]
		d2 := data[base+i-2]
		d3 := data[base+i-3]
		d4 := data[base+i-4]
		totalError0 += fixedLocalAbs32(d0)
		totalError1 += fixedLocalAbs32(d0 - d1)
		totalError2 += fixedLocalAbs32(d0 - 2*d1 + d2)
		totalError3 += fixedLocalAbs32(d0 - 3*d1 + 3*d2 - d3)
		totalError4 += fixedLocalAbs32(d0 - 4*d1 + 6*d2 - 4*d3 + d4)
	}

	var order uint32
	// prefer lower order (fixed.c:263)
	switch {
	case totalError0 <= fixedMin32(fixedMin32(fixedMin32(totalError1, totalError2), totalError3), totalError4):
		order = 0
	case totalError1 <= fixedMin32(fixedMin32(totalError2, totalError3), totalError4):
		order = 1
	case totalError2 <= fixedMin32(totalError3, totalError4):
		order = 2
	case totalError3 <= totalError4:
		order = 3
	default:
		order = 4
	}

	bits[0] = fixedResidualBPS(uint64(totalError0), dataLen)
	bits[1] = fixedResidualBPS(uint64(totalError1), dataLen)
	bits[2] = fixedResidualBPS(uint64(totalError2), dataLen)
	bits[3] = fixedResidualBPS(uint64(totalError3), dataLen)
	bits[4] = fixedResidualBPS(uint64(totalError4), dataLen)

	return order
}

// FixedComputeBestPredictorWide ports
// FLAC__fixed_compute_best_predictor_wide (fixed.c:301,
// non-integer-only build). Identical to FixedComputeBestPredictor but
// the error accumulators are 64-bit, used when the bits-per-sample and
// blocksize are large enough that the 32-bit sums of
// FixedComputeBestPredictor would overflow.
func FixedComputeBestPredictorWide(data []int32, dataLen uint32, bits *[fixedMaxFixedOrder + 1]float32) uint32 {
	var totalError0, totalError1, totalError2, totalError3, totalError4 uint64

	base := fixedMaxFixedOrder
	n := int(dataLen)
	for i := 0; i < n; i++ {
		d0 := data[base+i]
		d1 := data[base+i-1]
		d2 := data[base+i-2]
		d3 := data[base+i-3]
		d4 := data[base+i-4]
		// local_abs widens to uint32, then accumulates into uint64,
		// matching the C: total_error_* += local_abs(...). The
		// difference expressions stay in int32 (wrapping) exactly as
		// the C does before local_abs is applied.
		totalError0 += uint64(fixedLocalAbs32(d0))
		totalError1 += uint64(fixedLocalAbs32(d0 - d1))
		totalError2 += uint64(fixedLocalAbs32(d0 - 2*d1 + d2))
		totalError3 += uint64(fixedLocalAbs32(d0 - 3*d1 + 3*d2 - d3))
		totalError4 += uint64(fixedLocalAbs32(d0 - 4*d1 + 6*d2 - 4*d3 + d4))
	}

	var order uint32
	switch {
	case totalError0 <= fixedMin64(fixedMin64(fixedMin64(totalError1, totalError2), totalError3), totalError4):
		order = 0
	case totalError1 <= fixedMin64(fixedMin64(totalError2, totalError3), totalError4):
		order = 1
	case totalError2 <= fixedMin64(totalError3, totalError4):
		order = 2
	case totalError3 <= totalError4:
		order = 3
	default:
		order = 4
	}

	bits[0] = fixedResidualBPS(totalError0, dataLen)
	bits[1] = fixedResidualBPS(totalError1, dataLen)
	bits[2] = fixedResidualBPS(totalError2, dataLen)
	bits[3] = fixedResidualBPS(totalError3, dataLen)
	bits[4] = fixedResidualBPS(totalError4, dataLen)

	return order
}

// fixedResidualBPS computes one residual_bits_per_sample entry the way
// the non-integer-only build does (fixed.c:282 etc.):
//
//	(float)((total_error > 0) ? log(M_LN2 * total_error / data_len) / M_LN2 : 0.0)
//
// The whole chain is double precision; the float32 conversion mirrors
// C's (float) cast on store.
func fixedResidualBPS(totalError uint64, dataLen uint32) float32 {
	if totalError > 0 {
		return float32(math.Log(mLn2*float64(totalError)/float64(dataLen)) / mLn2)
	}
	return 0.0
}

// fixedMin32 ports flac_min for uint32 operands (macros.h).
func fixedMin32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

// fixedMin64 ports flac_min for uint64 operands (macros.h).
func fixedMin64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// FixedComputeResidual — port of FLAC__fixed_compute_residual (fixed.c:470).
// Computes the order-th finite difference of the int32 signal into residual
// using int32 (wrapping) arithmetic. data holds `order` warm-up samples before
// index 0; the port indexes with a base offset of `order`.
func FixedComputeResidual(data []int32, dataLen uint32, order uint32, residual []int32) {
	o := int(order)
	n := int(dataLen)
	switch order {
	case 0:
		copy(residual[:n], data[o:o+n])
	case 1:
		for i := 0; i < n; i++ {
			residual[i] = data[o+i] - data[o+i-1]
		}
	case 2:
		for i := 0; i < n; i++ {
			residual[i] = data[o+i] - 2*data[o+i-1] + data[o+i-2]
		}
	case 3:
		for i := 0; i < n; i++ {
			residual[i] = data[o+i] - 3*data[o+i-1] + 3*data[o+i-2] - data[o+i-3]
		}
	case 4:
		for i := 0; i < n; i++ {
			residual[i] = data[o+i] - 4*data[o+i-1] + 6*data[o+i-2] - 4*data[o+i-3] + data[o+i-4]
		}
	}
}

// FixedComputeResidualWide — port of FLAC__fixed_compute_residual_wide
// (fixed.c:501). int64 intermediate arithmetic on int32 input, truncated to
// int32 on store (matching the implicit C narrowing to FLAC__int32 residual).
func FixedComputeResidualWide(data []int32, dataLen uint32, order uint32, residual []int32) {
	o := int(order)
	n := int(dataLen)
	switch order {
	case 0:
		copy(residual[:n], data[o:o+n])
	case 1:
		for i := 0; i < n; i++ {
			residual[i] = int32(int64(data[o+i]) - int64(data[o+i-1]))
		}
	case 2:
		for i := 0; i < n; i++ {
			residual[i] = int32(int64(data[o+i]) - 2*int64(data[o+i-1]) + int64(data[o+i-2]))
		}
	case 3:
		for i := 0; i < n; i++ {
			residual[i] = int32(int64(data[o+i]) - 3*int64(data[o+i-1]) + 3*int64(data[o+i-2]) - int64(data[o+i-3]))
		}
	case 4:
		for i := 0; i < n; i++ {
			residual[i] = int32(int64(data[o+i]) - 4*int64(data[o+i-1]) + 6*int64(data[o+i-2]) - 4*int64(data[o+i-3]) + int64(data[o+i-4]))
		}
	}
}

// FixedComputeResidualWide33Bit — port of
// FLAC__fixed_compute_residual_wide_33bit (fixed.c:532). int64 input (33-bit
// side channel), int32 residual output.
func FixedComputeResidualWide33Bit(data []int64, dataLen uint32, order uint32, residual []int32) {
	o := int(order)
	n := int(dataLen)
	switch order {
	case 0:
		for i := 0; i < n; i++ {
			residual[i] = int32(data[o+i])
		}
	case 1:
		for i := 0; i < n; i++ {
			residual[i] = int32(data[o+i] - data[o+i-1])
		}
	case 2:
		for i := 0; i < n; i++ {
			residual[i] = int32(data[o+i] - 2*data[o+i-1] + data[o+i-2])
		}
	case 3:
		for i := 0; i < n; i++ {
			residual[i] = int32(data[o+i] - 3*data[o+i-1] + 3*data[o+i-2] - data[o+i-3])
		}
	case 4:
		for i := 0; i < n; i++ {
			residual[i] = int32(data[o+i] - 4*data[o+i-1] + 6*data[o+i-2] - 4*data[o+i-3] + data[o+i-4])
		}
	}
}

// fixedLocalAbs64 ports the local_abs64 macro (fixed.c:53):
// ((uint64_t)((x)<0? -(x) : (x))). Unsigned negation, no UB on INT64_MIN.
func fixedLocalAbs64(x int64) uint64 {
	if x < 0 {
		return uint64(-x)
	}
	return uint64(x)
}

// FixedComputeBestPredictorLimitResidual — port of
// FLAC__fixed_compute_best_predictor_limit_residual (fixed.c:377,
// non-integer-only build). Like FixedComputeBestPredictor but accumulates the
// per-order error in 64-bit and tracks per-order validity: an order is invalid
// if any single-sample error exceeds INT32_MAX (so abs() would be UB in the
// real residual). The chosen order is the lowest-error *valid* order; the
// per-order residual_bits_per_sample is the usual log estimate for the winner
// and 34.0f for any losing/invalid order (the CHECK_ORDER_IS_VALID macro).
//
// data holds FLAC__MAX_FIXED_ORDER(=4) history samples followed by dataLen
// signal samples; the C iterates i from -4, so the port indexes data with a
// base of 4 and the windowed-difference guards (i > -4, etc.) become
// (base+i > 0) checks. Pass dataLen = len(data) - 4.
func FixedComputeBestPredictorLimitResidual(data []int32, dataLen uint32, bits *[fixedMaxFixedOrder + 1]float32) uint32 {
	var totalError0, totalError1, totalError2, totalError3, totalError4 uint64
	order0Valid, order1Valid, order2Valid, order3Valid, order4Valid := true, true, true, true, true

	base := fixedMaxFixedOrder
	// C loop: for(i = -4; i < (int)data_len; i++). Errors for orders >0 are 0
	// for the first few i where the difference would read uninitialised
	// history (i > -4 etc.).
	for i := -4; i < int(dataLen); i++ {
		d0 := int64(data[base+i])
		error0 := fixedLocalAbs64(d0)
		var error1, error2, error3, error4 uint64
		if i > -4 {
			error1 = fixedLocalAbs64(d0 - int64(data[base+i-1]))
		}
		if i > -3 {
			error2 = fixedLocalAbs64(d0 - 2*int64(data[base+i-1]) + int64(data[base+i-2]))
		}
		if i > -2 {
			error3 = fixedLocalAbs64(d0 - 3*int64(data[base+i-1]) + 3*int64(data[base+i-2]) - int64(data[base+i-3]))
		}
		if i > -1 {
			error4 = fixedLocalAbs64(d0 - 4*int64(data[base+i-1]) + 6*int64(data[base+i-2]) - 4*int64(data[base+i-3]) + int64(data[base+i-4]))
		}

		totalError0 += error0
		totalError1 += error1
		totalError2 += error2
		totalError3 += error3
		totalError4 += error4

		if error0 > math.MaxInt32 {
			order0Valid = false
		}
		if error1 > math.MaxInt32 {
			order1Valid = false
		}
		if error2 > math.MaxInt32 {
			order2Valid = false
		}
		if error3 > math.MaxInt32 {
			order3Valid = false
		}
		if error4 > math.MaxInt32 {
			order4Valid = false
		}
	}

	order := uint32(0)
	smallestError := uint64(math.MaxUint64)
	checkOrderIsValid(0, order0Valid, totalError0, dataLen, &order, &smallestError, bits)
	checkOrderIsValid(1, order1Valid, totalError1, dataLen, &order, &smallestError, bits)
	checkOrderIsValid(2, order2Valid, totalError2, dataLen, &order, &smallestError, bits)
	checkOrderIsValid(3, order3Valid, totalError3, dataLen, &order, &smallestError, bits)
	checkOrderIsValid(4, order4Valid, totalError4, dataLen, &order, &smallestError, bits)

	return order
}

// FixedComputeBestPredictorLimitResidual33Bit — port of
// FLAC__fixed_compute_best_predictor_limit_residual_33bit (fixed.c:424). int64
// input (33-bit side channel) variant. data holds 4 int64 history samples
// before the signal; pass dataLen = len(data) - 4.
func FixedComputeBestPredictorLimitResidual33Bit(data []int64, dataLen uint32, bits *[fixedMaxFixedOrder + 1]float32) uint32 {
	var totalError0, totalError1, totalError2, totalError3, totalError4 uint64
	order0Valid, order1Valid, order2Valid, order3Valid, order4Valid := true, true, true, true, true

	base := fixedMaxFixedOrder
	for i := -4; i < int(dataLen); i++ {
		d0 := data[base+i]
		error0 := fixedLocalAbs64(d0)
		var error1, error2, error3, error4 uint64
		if i > -4 {
			error1 = fixedLocalAbs64(d0 - data[base+i-1])
		}
		if i > -3 {
			error2 = fixedLocalAbs64(d0 - 2*data[base+i-1] + data[base+i-2])
		}
		if i > -2 {
			error3 = fixedLocalAbs64(d0 - 3*data[base+i-1] + 3*data[base+i-2] - data[base+i-3])
		}
		if i > -1 {
			error4 = fixedLocalAbs64(d0 - 4*data[base+i-1] + 6*data[base+i-2] - 4*data[base+i-3] + data[base+i-4])
		}

		totalError0 += error0
		totalError1 += error1
		totalError2 += error2
		totalError3 += error3
		totalError4 += error4

		if error0 > math.MaxInt32 {
			order0Valid = false
		}
		if error1 > math.MaxInt32 {
			order1Valid = false
		}
		if error2 > math.MaxInt32 {
			order2Valid = false
		}
		if error3 > math.MaxInt32 {
			order3Valid = false
		}
		if error4 > math.MaxInt32 {
			order4Valid = false
		}
	}

	order := uint32(0)
	smallestError := uint64(math.MaxUint64)
	checkOrderIsValid(0, order0Valid, totalError0, dataLen, &order, &smallestError, bits)
	checkOrderIsValid(1, order1Valid, totalError1, dataLen, &order, &smallestError, bits)
	checkOrderIsValid(2, order2Valid, totalError2, dataLen, &order, &smallestError, bits)
	checkOrderIsValid(3, order3Valid, totalError3, dataLen, &order, &smallestError, bits)
	checkOrderIsValid(4, order4Valid, totalError4, dataLen, &order, &smallestError, bits)

	return order
}

// checkOrderIsValid ports the CHECK_ORDER_IS_VALID macro (fixed.c:356,
// non-integer-only build). If this order is valid and beats the running
// smallest error, it becomes the chosen order and its bits estimate is the log
// estimate; otherwise the estimate is set to the sentinel 34.0f.
func checkOrderIsValid(macroOrder uint32, valid bool, totalError uint64, dataLen uint32, order *uint32, smallestError *uint64, bits *[fixedMaxFixedOrder + 1]float32) {
	if valid && totalError < *smallestError {
		*order = macroOrder
		*smallestError = totalError
		if totalError > 0 {
			bits[macroOrder] = float32(math.Log(mLn2*float64(totalError)/float64(dataLen)) / mLn2)
		} else {
			bits[macroOrder] = 0.0
		}
	} else {
		bits[macroOrder] = 34.0
	}
}
