package nativeflac

// 1:1 port of libflac/src/libFLAC/fixed.c, decoder side.
//
// FLAC's "fixed" subframes use one of five hard-coded predictors (orders
// 0..4) corresponding to repeated finite differences:
//
//   order 0:  d[i] = r[i]
//   order 1:  d[i] = r[i] +   d[-1]
//   order 2:  d[i] = r[i] + 2 d[-1] -   d[-2]
//   order 3:  d[i] = r[i] + 3 d[-1] - 3 d[-2] +   d[-3]
//   order 4:  d[i] = r[i] + 4 d[-1] - 6 d[-2] + 4 d[-3] - d[-4]
//
// Caller layout (mirroring libFLAC's `data[-order..-1]` warm-up
// convention): pass a destination slice `data` of length `order +
// dataLen`. data[0..order] must hold the per-subframe warm-up samples
// in stream order; the predictor writes data[order..order+dataLen]
// from `residual` (length dataLen).
//
// Overflow note: the int32 path matches FLAC__fixed_restore_signal
// (fixed.c:571); int32 wraparound is intentional and bit-identical to
// libFLAC under the same overflow conditions (FLAC's reference code
// silences UBSan via __attribute__((no_sanitize("signed-integer-
// overflow"))) — see fixed.c:631).

// FixedRestoreSignal — port of FLAC__fixed_restore_signal (fixed.c:571).
// Per-order specialized; a function-entry reslice (data shifted to the
// predictor window, residual sliced to the exact write count) drops the
// per-iteration bounds checks the compiler can otherwise not prove away.
// Integer-exact — int32 wraparound matches libFLAC.
func FixedRestoreSignal(residual []int32, order uint32, data []int32) {
	dataLen := len(data) - int(order)
	if dataLen <= 0 {
		return
	}
	o := int(order)
	res := residual[:dataLen]
	switch order {
	case 0:
		copy(data[o:], res)
	case 1:
		d := data[o-1:]
		for i := 0; i < dataLen; i++ {
			d[i+1] = res[i] + d[i]
		}
	case 2:
		d := data[o-2:]
		for i := 0; i < dataLen; i++ {
			d[i+2] = res[i] + 2*d[i+1] - d[i]
		}
	case 3:
		d := data[o-3:]
		for i := 0; i < dataLen; i++ {
			d[i+3] = res[i] + 3*d[i+2] - 3*d[i+1] + d[i]
		}
	case 4:
		d := data[o-4:]
		for i := 0; i < dataLen; i++ {
			d[i+4] = res[i] + 4*d[i+3] - 6*d[i+2] + 4*d[i+1] - d[i]
		}
	}
}

// FixedRestoreSignalWide — port of FLAC__fixed_restore_signal_wide
// (fixed.c:601). Same predictor coefficients but every multiplication
// and addition uses int64 to absorb growth from larger bit depths.
// The output is still int32; truncation happens at the final write,
// matching libFLAC's (FLAC__int32) cast at fixed.c:612 etc.
func FixedRestoreSignalWide(residual []int32, order uint32, data []int32) {
	dataLen := len(data) - int(order)
	o := int(order)
	switch order {
	case 0:
		copy(data[o:], residual[:dataLen])
	case 1:
		for i := 0; i < dataLen; i++ {
			data[o+i] = int32(int64(residual[i]) + int64(data[o+i-1]))
		}
	case 2:
		for i := 0; i < dataLen; i++ {
			data[o+i] = int32(int64(residual[i]) + 2*int64(data[o+i-1]) - int64(data[o+i-2]))
		}
	case 3:
		for i := 0; i < dataLen; i++ {
			data[o+i] = int32(int64(residual[i]) + 3*int64(data[o+i-1]) - 3*int64(data[o+i-2]) + int64(data[o+i-3]))
		}
	case 4:
		for i := 0; i < dataLen; i++ {
			data[o+i] = int32(int64(residual[i]) + 4*int64(data[o+i-1]) - 6*int64(data[o+i-2]) + 4*int64(data[o+i-3]) - int64(data[o+i-4]))
		}
	}
}

// FixedRestoreSignalWide33Bit — port of
// FLAC__fixed_restore_signal_wide_33bit (fixed.c:639). Output buffer
// is int64; used by the 33-bit (rear-channel) decoder path.
func FixedRestoreSignalWide33Bit(residual []int32, order uint32, data []int64) {
	dataLen := len(data) - int(order)
	o := int(order)
	switch order {
	case 0:
		for i := 0; i < dataLen; i++ {
			data[o+i] = int64(residual[i])
		}
	case 1:
		for i := 0; i < dataLen; i++ {
			data[o+i] = int64(residual[i]) + data[o+i-1]
		}
	case 2:
		for i := 0; i < dataLen; i++ {
			data[o+i] = int64(residual[i]) + 2*data[o+i-1] - data[o+i-2]
		}
	case 3:
		for i := 0; i < dataLen; i++ {
			data[o+i] = int64(residual[i]) + 3*data[o+i-1] - 3*data[o+i-2] + data[o+i-3]
		}
	case 4:
		for i := 0; i < dataLen; i++ {
			data[o+i] = int64(residual[i]) + 4*data[o+i-1] - 6*data[o+i-2] + 4*data[o+i-3] - data[o+i-4]
		}
	}
}
