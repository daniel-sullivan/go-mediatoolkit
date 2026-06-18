package nativeflac

// Subframe parsers — the simple two (CONSTANT, VERBATIM) plus the
// shared partitioned-rice residual reader, ported from
// stream_decoder.c slices 3051–3358.
//
// libFLAC threads its decoder state struct through every parser; the
// Go port exposes each as a free function taking the bit reader and
// the per-frame parameters (blocksize, bits-per-sample, predictor
// order). Each function fills out a Subframe variant struct supplied
// by the caller.
//
// Status codes mirror the FrameHeader* set: BadFrame means LOST_SYNC
// (libFLAC's "search again" path), Unparseable matches
// UNPARSEABLE_STREAM. ReadError surfaces a starved BitReader.

// SubframeStatus is the result of a subframe parse attempt.
type SubframeStatus uint8

const (
	SubframeOK SubframeStatus = iota
	SubframeReadError
	SubframeBadFrame
	SubframeUnparseable
	SubframeAllocFail
)

// ReadSubframeConstant — port of read_subframe_constant_
// (stream_decoder.c:3051).
//
// The constant subframe carries a single bps-bit signed value; every
// sample in the block equals that value. When bps <= 32 the caller
// receives the value via Subframe.Constant.Value and can fill its
// int32 output buffer; bps == 33 (used for the side channel of a
// 33-bit decorrelated subframe) routes through the same field but
// the caller must materialise into an int64 side buffer.
func ReadSubframeConstant(br *BitReader, sub *Subframe, bps uint32) SubframeStatus {
	v, ok := br.ReadRawInt64(bps)
	if !ok {
		return SubframeReadError
	}
	sub.Type = SubframeConstant
	sub.Constant.Value = v
	return SubframeOK
}

// ReadSubframeVerbatim — port of read_subframe_verbatim_
// (stream_decoder.c:3260).
//
// The verbatim subframe stores `blocksize` raw bps-bit signed
// samples back-to-back. bps < 33 fills sub.Verbatim.Data32; bps == 33
// fills sub.Verbatim.Data64 (33-bit side channel after stereo
// decorrelation). Caller-provided slices must already be sized to
// `blocksize`.
func ReadSubframeVerbatim(br *BitReader, sub *Subframe, blocksize, bps uint32) SubframeStatus {
	sub.Type = SubframeVerbatim
	if bps < 33 {
		sub.Verbatim.Type = VerbatimDataInt32
		out := sub.Verbatim.Data32
		for i := uint32(0); i < blocksize; i++ {
			x, ok := br.ReadRawInt32(bps)
			if !ok {
				return SubframeReadError
			}
			out[i] = x
		}
	} else {
		sub.Verbatim.Type = VerbatimDataInt64
		out := sub.Verbatim.Data64
		for i := uint32(0); i < blocksize; i++ {
			x, ok := br.ReadRawInt64(bps)
			if !ok {
				return SubframeReadError
			}
			out[i] = x
		}
	}
	return SubframeOK
}

// ReadResidualPartitionedRice — port of read_residual_partitioned_rice_
// (stream_decoder.c:3300).
//
// Reads `blocksize - predictorOrder` residual samples into `residual`,
// arranged in 2^partitionOrder equal-size partitions. The caller has
// already validated that partitionOrder * partition_samples >=
// predictorOrder and that partition_samples evenly divides blocksize
// (libFLAC's read_subframe_fixed_/_lpc_ checks at lines 3109–3114).
//
// `isExtended` chooses the PARTITIONED_RICE2 parameter widths (5+5
// instead of 4+5). `contents` is filled with the per-partition
// parameters and raw-bit counts so the higher-level Subframe
// struct's EntropyCoding field reflects what was decoded.
func ReadResidualPartitionedRice(
	br *BitReader,
	predictorOrder, partitionOrder, blocksize uint32,
	contents *PartitionedRiceContents,
	residual []int32,
	isExtended bool,
) SubframeStatus {
	partitions := uint32(1) << partitionOrder
	partitionSamples := blocksize >> partitionOrder

	plen := uint32(EntropyCodingMethodPartitionedRiceParam)
	pesc := uint32(EntropyCodingPartitionedRiceEscape)
	if isExtended {
		plen = uint32(EntropyCodingMethodPartitionedRice2Param)
		pesc = uint32(EntropyCodingPartitionedRice2Escape)
	}

	// libFLAC pre-allocates parameters / raw_bits arrays sized to
	// max(6, partition_order). Match that so callers passing a
	// fresh PartitionedRiceContents never see a nil-slice index.
	cap := uint32(1) << maxU32(6, partitionOrder)
	if uint32(len(contents.Parameters)) < cap {
		contents.Parameters = make([]uint32, cap)
	}
	if uint32(len(contents.RawBits)) < cap {
		contents.RawBits = make([]uint32, cap)
	}

	sample := uint32(0)
	for p := uint32(0); p < partitions; p++ {
		riceParam, ok := br.ReadRawUint32(plen)
		if !ok {
			return SubframeReadError
		}
		contents.Parameters[p] = riceParam

		if riceParam < pesc {
			contents.RawBits[p] = 0
			var u uint32
			if p == 0 {
				u = partitionSamples - predictorOrder
			} else {
				u = partitionSamples
			}
			if !br.ReadRiceSignedBlock(residual[sample:sample+u], riceParam) {
				// libFLAC distinguishes "read callback failed"
				// (state already set) from "invalid Rice symbol"
				// (LOST_SYNC). Our BitReader doesn't carry that
				// state, so collapse both into BadFrame; callers
				// resync either way.
				return SubframeBadFrame
			}
			sample += u
		} else {
			rawBits, ok := br.ReadRawUint32(uint32(EntropyCodingMethodPartitionedRiceRawLen))
			if !ok {
				return SubframeReadError
			}
			contents.RawBits[p] = rawBits
			start := uint32(0)
			if p == 0 {
				start = predictorOrder
			}
			if rawBits == 0 {
				for u := start; u < partitionSamples; u++ {
					residual[sample] = 0
					sample++
				}
			} else {
				for u := start; u < partitionSamples; u++ {
					x, ok := br.ReadRawInt32(rawBits)
					if !ok {
						return SubframeReadError
					}
					residual[sample] = x
					sample++
				}
			}
		}
	}
	return SubframeOK
}

func maxU32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

// ReadSubframeFixed — port of read_subframe_fixed_
// (stream_decoder.c:3081). Reads `order` warm-up samples (signed,
// `bps` bits each), the entropy coding header (2-bit method type +
// 4-bit partition order), the partitioned-rice residual, and — when
// fullDecode is true — feeds the result through the fixed predictor
// inverse to materialise the decoded samples.
//
// Caller layout:
//   - sub.Fixed.Residual must be a pre-allocated slice of length
//     blocksize - order; ReadResidualPartitionedRice fills it.
//   - When bps < 33, output32 receives the decoded samples (length
//     blocksize). The function pre-loads the warmup samples at
//     output32[0..order] and runs FixedRestoreSignal[Wide].
//   - When bps == 33, output64 receives the decoded samples (length
//     blocksize) via FixedRestoreSignalWide33Bit.
//
// Pass nil for the unused output slice; the corresponding bps branch
// skips the restore step.
func ReadSubframeFixed(
	br *BitReader,
	sub *Subframe,
	blocksize, bps, order uint32,
	output32 []int32,
	output64 []int64,
	fullDecode bool,
) SubframeStatus {
	sub.Type = SubframeFixed
	sub.Fixed.Order = order

	// Warm-up samples.
	for u := uint32(0); u < order; u++ {
		v, ok := br.ReadRawInt64(bps)
		if !ok {
			return SubframeReadError
		}
		sub.Fixed.Warmup[u] = v
	}

	// Entropy coding method.
	st, isExtended, partitionOrder := readEntropyCodingHeader(br, &sub.Fixed.EntropyCoding, blocksize, order)
	if st != SubframeOK {
		return st
	}

	if !br.IsConsumedByteAligned() && false {
		// Placeholder branch for symmetry with libFLAC's switch. The
		// actual residual read happens unconditionally below.
	}

	// Residual.
	if st := ReadResidualPartitionedRice(br,
		order, partitionOrder, blocksize,
		&sub.Fixed.EntropyCoding.Contents,
		sub.Fixed.Residual,
		isExtended,
	); st != SubframeOK {
		return st
	}

	if !fullDecode {
		return SubframeOK
	}

	// Decode through the fixed-predictor inverse.
	if bps < 33 {
		// Pre-load the order warmup samples at output[0..order].
		for i := uint32(0); i < order; i++ {
			output32[i] = int32(sub.Fixed.Warmup[i])
		}
		if bps+order <= 32 {
			FixedRestoreSignal(sub.Fixed.Residual, order, output32[:blocksize])
		} else {
			FixedRestoreSignalWide(sub.Fixed.Residual, order, output32[:blocksize])
		}
	} else {
		for i := uint32(0); i < order; i++ {
			output64[i] = sub.Fixed.Warmup[i]
		}
		FixedRestoreSignalWide33Bit(sub.Fixed.Residual, order, output64[:blocksize])
	}
	return SubframeOK
}

// ReadSubframeLPC — port of read_subframe_lpc_ (stream_decoder.c:3156).
// Reads warm-up samples, qlp_coeff_precision (4 bits + 1, with the
// reserved escape value 15 → BadFrame), qlp shift (5-bit signed; if
// negative → BadFrame), `order` qlp_coeff values of qlp_coeff_precision
// bits each, then the entropy coding header + residual.
//
// Output layout matches ReadSubframeFixed.
func ReadSubframeLPC(
	br *BitReader,
	sub *Subframe,
	blocksize, bps, order uint32,
	output32 []int32,
	output64 []int64,
	fullDecode bool,
) SubframeStatus {
	sub.Type = SubframeLPC
	sub.LPC.Order = order

	for u := uint32(0); u < order; u++ {
		v, ok := br.ReadRawInt64(bps)
		if !ok {
			return SubframeReadError
		}
		sub.LPC.Warmup[u] = v
	}

	prec, ok := br.ReadRawUint32(uint32(SubframeLPCQLPCoeffPrecisionLen))
	if !ok {
		return SubframeReadError
	}
	if prec == (1<<SubframeLPCQLPCoeffPrecisionLen)-1 {
		return SubframeBadFrame
	}
	sub.LPC.QLPCoeffPrecision = prec + 1

	shift, ok := br.ReadRawInt32(uint32(SubframeLPCQLPShiftLen))
	if !ok {
		return SubframeReadError
	}
	if shift < 0 {
		return SubframeBadFrame
	}
	sub.LPC.QuantizationLevel = int(shift)

	for u := uint32(0); u < order; u++ {
		c, ok := br.ReadRawInt32(sub.LPC.QLPCoeffPrecision)
		if !ok {
			return SubframeReadError
		}
		sub.LPC.QLPCoeff[u] = c
	}

	st, isExtended, partitionOrder := readEntropyCodingHeader(br, &sub.LPC.EntropyCoding, blocksize, order)
	if st != SubframeOK {
		return st
	}

	if st := ReadResidualPartitionedRice(br,
		order, partitionOrder, blocksize,
		&sub.LPC.EntropyCoding.Contents,
		sub.LPC.Residual,
		isExtended,
	); st != SubframeOK {
		return st
	}

	if !fullDecode {
		return SubframeOK
	}

	if bps <= 32 {
		for i := uint32(0); i < order; i++ {
			output32[i] = int32(sub.LPC.Warmup[i])
		}
		// libFLAC chooses between narrow and wide LPC restore based
		// on whether the worst-case residual + accumulator fits in
		// int32 (lpc.c:962, 952). Ports of those bounds live next to
		// the restore predictors.
		coeff := sub.LPC.QLPCoeff[:order]
		if LPCMaxResidualBPS(bps, coeff, order, sub.LPC.QuantizationLevel) <= 32 &&
			LPCMaxPredictionBeforeShiftBPS(bps, coeff, order) <= 32 {
			LPCRestoreSignal(sub.LPC.Residual, coeff, order,
				sub.LPC.QuantizationLevel, output32[:blocksize])
		} else {
			LPCRestoreSignalWide(sub.LPC.Residual, coeff, order,
				sub.LPC.QuantizationLevel, output32[:blocksize])
		}
	} else {
		for i := uint32(0); i < order; i++ {
			output64[i] = sub.LPC.Warmup[i]
		}
		LPCRestoreSignalWide33Bit(sub.LPC.Residual, sub.LPC.QLPCoeff[:order],
			order, sub.LPC.QuantizationLevel, output64[:blocksize])
	}
	return SubframeOK
}

// readEntropyCodingHeader is the shared 2+4-bit method header read
// from both fixed and LPC subframes. Returns isExtended (true for
// PARTITIONED_RICE2) and the partition order.
func readEntropyCodingHeader(br *BitReader, ec *EntropyCodingMethod, blocksize, predictorOrder uint32) (status SubframeStatus, isExtended bool, partitionOrder uint32) {
	t, ok := br.ReadRawUint32(uint32(EntropyCodingMethodTypeLen))
	if !ok {
		return SubframeReadError, false, 0
	}
	switch EntropyCodingMethodType(t) {
	case EntropyCodingMethodPartitionedRice, EntropyCodingMethodPartitionedRice2:
		ec.Type = EntropyCodingMethodType(t)
	default:
		return SubframeUnparseable, false, 0
	}
	po, ok := br.ReadRawUint32(uint32(EntropyCodingMethodPartitionedRiceOrder))
	if !ok {
		return SubframeReadError, false, 0
	}
	// libFLAC's partition_samples = blocksize >> po check.
	if (blocksize>>po) < predictorOrder ||
		(blocksize%(uint32(1)<<po)) > 0 {
		return SubframeBadFrame, false, 0
	}
	ec.PartitionOrder = po
	return SubframeOK, ec.Type == EntropyCodingMethodPartitionedRice2, po
}

// ReadZeroPadding — port of read_zero_padding_ (stream_decoder.c:3360).
// After the final subframe the decoder consumes the remaining bits of
// the current byte; if any are non-zero the stream has lost sync.
// Returns SubframeBadFrame in that case (mirroring libFLAC's
// LOST_SYNC behaviour).
func ReadZeroPadding(br *BitReader) SubframeStatus {
	if br.IsConsumedByteAligned() {
		return SubframeOK
	}
	zero, ok := br.ReadRawUint32(br.BitsLeftForByteAlignment())
	if !ok {
		return SubframeReadError
	}
	if zero != 0 {
		return SubframeBadFrame
	}
	return SubframeOK
}
