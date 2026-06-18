package nativeflac

// 1:1 port of libflac/src/libFLAC/lpc.c, decoder side.
//
// FLAC's LPC subframes carry a vector of `order` quantised linear-
// predictor coefficients (`qlpCoeff`) and a `lpQuantization` shift.
// The inverse is:
//
//   sum = Σ qlpCoeff[j] * data[i-j-1]   (j = 0..order-1)
//   data[i] = residual[i] + (sum >> lpQuantization)
//
// libFLAC's production path (FLAC__LPC_UNROLLED_FILTER_LOOPS) hand-
// unrolls this for orders 1..12, with a switch-fallthrough for orders
// 13..32 (lpc.c:1018+). The Go translation uses the equivalent
// "slower but clearer" loop libFLAC documents at lpc.c:1009 — the two
// produce identical bit patterns under the language's wraparound
// arithmetic, which is what the parity oracle confirms.
//
// Caller layout matches FixedRestoreSignal: data[0..order] holds the
// warm-up samples; data[order..order+dataLen] is overwritten from
// residual (length dataLen). qlpCoeff has length order.

// LPCRestoreSignal — port of FLAC__lpc_restore_signal (lpc.c:978).
// 32-bit accumulator path; used when the bit-depth + order combination
// fits. Mirrors libFLAC's FLAC__LPC_UNROLLED_FILTER_LOOPS production
// build (lpc.c:1018+): per-order specialized loops for the common orders
// 1..12, with a generic loop for 13..32. Integer-exact — int32
// wraparound matches libFLAC, which the strict parity gate verifies.
//
// The result is bit-identical to the prior straight double loop; the
// unrolling just removes the inner-loop overhead and lets the compiler
// keep the coefficient products in registers.
func LPCRestoreSignal(residual []int32, qlpCoeff []int32, order uint32, lpQuantization int, data []int32) {
	dataLen := len(data) - int(order)
	if dataLen <= 0 {
		return
	}
	lpq := uint(lpQuantization)
	o := int(order)
	// Reslice residual to the exact write count so the compiler can prove
	// residual[i] is in range across the loop body.
	res := residual[:dataLen]
	// Hoist a single bounds check covering qlpCoeff[0..order-1].
	q := qlpCoeff[:order:order]

	switch order {
	case 1:
		d := data[o-1:]
		for i := 0; i < dataLen; i++ {
			d[i+1] = res[i] + (q[0]*d[i])>>lpq
		}
	case 2:
		d := data[o-2:]
		for i := 0; i < dataLen; i++ {
			d[i+2] = res[i] + (q[1]*d[i]+q[0]*d[i+1])>>lpq
		}
	case 3:
		d := data[o-3:]
		for i := 0; i < dataLen; i++ {
			d[i+3] = res[i] + (q[2]*d[i]+q[1]*d[i+1]+q[0]*d[i+2])>>lpq
		}
	case 4:
		d := data[o-4:]
		for i := 0; i < dataLen; i++ {
			d[i+4] = res[i] + (q[3]*d[i]+q[2]*d[i+1]+q[1]*d[i+2]+q[0]*d[i+3])>>lpq
		}
	case 5:
		d := data[o-5:]
		for i := 0; i < dataLen; i++ {
			d[i+5] = res[i] + (q[4]*d[i]+q[3]*d[i+1]+q[2]*d[i+2]+q[1]*d[i+3]+q[0]*d[i+4])>>lpq
		}
	case 6:
		d := data[o-6:]
		for i := 0; i < dataLen; i++ {
			d[i+6] = res[i] + (q[5]*d[i]+q[4]*d[i+1]+q[3]*d[i+2]+q[2]*d[i+3]+q[1]*d[i+4]+q[0]*d[i+5])>>lpq
		}
	case 7:
		d := data[o-7:]
		for i := 0; i < dataLen; i++ {
			d[i+7] = res[i] + (q[6]*d[i]+q[5]*d[i+1]+q[4]*d[i+2]+q[3]*d[i+3]+q[2]*d[i+4]+q[1]*d[i+5]+q[0]*d[i+6])>>lpq
		}
	case 8:
		d := data[o-8:]
		for i := 0; i < dataLen; i++ {
			d[i+8] = res[i] + (q[7]*d[i]+q[6]*d[i+1]+q[5]*d[i+2]+q[4]*d[i+3]+q[3]*d[i+4]+q[2]*d[i+5]+q[1]*d[i+6]+q[0]*d[i+7])>>lpq
		}
	case 9:
		d := data[o-9:]
		for i := 0; i < dataLen; i++ {
			d[i+9] = res[i] + (q[8]*d[i]+q[7]*d[i+1]+q[6]*d[i+2]+q[5]*d[i+3]+q[4]*d[i+4]+q[3]*d[i+5]+q[2]*d[i+6]+q[1]*d[i+7]+q[0]*d[i+8])>>lpq
		}
	case 10:
		d := data[o-10:]
		for i := 0; i < dataLen; i++ {
			d[i+10] = res[i] + (q[9]*d[i]+q[8]*d[i+1]+q[7]*d[i+2]+q[6]*d[i+3]+q[5]*d[i+4]+q[4]*d[i+5]+q[3]*d[i+6]+q[2]*d[i+7]+q[1]*d[i+8]+q[0]*d[i+9])>>lpq
		}
	case 11:
		d := data[o-11:]
		for i := 0; i < dataLen; i++ {
			d[i+11] = res[i] + (q[10]*d[i]+q[9]*d[i+1]+q[8]*d[i+2]+q[7]*d[i+3]+q[6]*d[i+4]+q[5]*d[i+5]+q[4]*d[i+6]+q[3]*d[i+7]+q[2]*d[i+8]+q[1]*d[i+9]+q[0]*d[i+10])>>lpq
		}
	case 12:
		d := data[o-12:]
		for i := 0; i < dataLen; i++ {
			d[i+12] = res[i] + (q[11]*d[i]+q[10]*d[i+1]+q[9]*d[i+2]+q[8]*d[i+3]+q[7]*d[i+4]+q[6]*d[i+5]+q[5]*d[i+6]+q[4]*d[i+7]+q[3]*d[i+8]+q[2]*d[i+9]+q[1]*d[i+10]+q[0]*d[i+11])>>lpq
		}
	default: // orders 13..32 — generic, still integer-exact.
		for i := 0; i < dataLen; i++ {
			var sum int32
			base := o + i
			for j := 0; j < o; j++ {
				sum += q[j] * data[base-j-1]
			}
			data[base] = res[i] + (sum >> lpq)
		}
	}
}

// LPCRestoreSignalWide — port of FLAC__lpc_restore_signal_wide
// (lpc.c:1241). int64 accumulator absorbs growth from large bit
// depths; output is still int32 (truncation matches libFLAC's cast at
// lpc.c:1264).
func LPCRestoreSignalWide(residual []int32, qlpCoeff []int32, order uint32, lpQuantization int, data []int32) {
	dataLen := len(data) - int(order)
	o := int(order)
	for i := 0; i < dataLen; i++ {
		var sum int64
		for j := uint32(0); j < order; j++ {
			sum += int64(qlpCoeff[j]) * int64(data[o+i-int(j)-1])
		}
		data[o+i] = int32(int64(residual[i]) + (sum >> lpQuantization))
	}
}

// LPCRestoreSignalWide33Bit — port of
// FLAC__lpc_restore_signal_wide_33bit (lpc.c:1501). Output is int64
// for streams that exceed 32-bit dynamic range after channel
// decorrelation.
func LPCRestoreSignalWide33Bit(residual []int32, qlpCoeff []int32, order uint32, lpQuantization int, data []int64) {
	dataLen := len(data) - int(order)
	o := int(order)
	for i := 0; i < dataLen; i++ {
		var sum int64
		for j := uint32(0); j < order; j++ {
			sum += int64(qlpCoeff[j]) * data[o+i-int(j)-1]
		}
		data[o+i] = int64(residual[i]) + (sum >> lpQuantization)
	}
}

// LPCMaxPredictionValueBeforeShift — port of
// FLAC__lpc_max_prediction_value_before_shift (lpc.c:942). Returns
// max_abs_sample_value × Σ|qlpCoeff[i]| — the upper bound on the
// pre-shift prediction sum used to size the accumulator.
func LPCMaxPredictionValueBeforeShift(subframeBPS uint32, qlpCoeff []int32, order uint32) uint64 {
	maxAbs := uint64(1) << (subframeBPS - 1)
	var absSum uint32
	for i := uint32(0); i < order; i++ {
		v := qlpCoeff[i]
		if v < 0 {
			v = -v
		}
		absSum += uint32(v)
	}
	return maxAbs * uint64(absSum)
}

// LPCMaxPredictionBeforeShiftBPS — port of
// FLAC__lpc_max_prediction_before_shift_bps (lpc.c:952).
func LPCMaxPredictionBeforeShiftBPS(subframeBPS uint32, qlpCoeff []int32, order uint32) uint32 {
	return SILog2(int64(LPCMaxPredictionValueBeforeShift(subframeBPS, qlpCoeff, order)))
}

// LPCMaxResidualBPS — port of FLAC__lpc_max_residual_bps (lpc.c:962).
// Used by the decoder to choose between LPCRestoreSignal and the
// wider int64 variant.
func LPCMaxResidualBPS(subframeBPS uint32, qlpCoeff []int32, order uint32, lpQuantization int) uint32 {
	maxAbs := uint64(1) << (subframeBPS - 1)
	maxBefore := LPCMaxPredictionValueBeforeShift(subframeBPS, qlpCoeff, order)
	// libFLAC computes  -1 * ((-1 * (int64)max_before) >> shift)
	// to get the signed-arithmetic-shift-rounding of max_before
	// before adding to max_abs.
	maxAfter := uint64(-((-int64(maxBefore)) >> lpQuantization))
	return SILog2(int64(maxAbs + maxAfter))
}
