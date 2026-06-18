package nativeflac

// This file is the family-B slice of the 1:1 port of
// libflac/src/libFLAC/stream_encoder.c — subframe *evaluation*: deciding,
// for each channel, the smallest-bits subframe among CONSTANT / VERBATIM /
// FIXED / LPC candidates, driving the apodization -> autocorrelation ->
// Levinson -> quantize -> residual chain, and choosing the rice partition
// order. It composes the frame bitbuffer by invoking family C's
// FrameAddHeader + addSubframe_.
//
// Functions ported here (with their libFLAC counterparts):
//
//   - processSubframes_          (stream_encoder.c:3735)
//   - processSubframe_           (stream_encoder.c:4033)
//   - setNextSubdivideTukey      (stream_encoder.c:4281)
//   - applyApodization_          (stream_encoder.c:4306)
//   - evaluateConstantSubframe_  (stream_encoder.c:4454)
//   - evaluateFixedSubframe_     (stream_encoder.c:4477)
//   - evaluateLpcSubframe_       (stream_encoder.c:4552)
//   - evaluateVerbatimSubframe_  (stream_encoder.c:4657)
//   - findBestPartitionOrder_    (stream_encoder.c:4689)
//   - precomputePartitionInfoSums_    (stream_encoder.c:4785)
//   - precomputePartitionInfoEscapes_ (stream_encoder.c:4842)
//   - countRiceBitsInPartition_  (stream_encoder.c:4917, non-EXACT path)
//   - setPartitionedRice_        (stream_encoder.c:4942, single-parameter path)
//   - getWastedBits_             (stream_encoder.c:5065)
//   - getWastedBitsWide_         (stream_encoder.c:5089)
//
// Single-thread parity target: this family operates on a single
// StreamEncoderThreadTask (threadtask[0]). The EXACT_RICE_BITS_CALCULATION
// and ENABLE_RICE_PARAMETER_SEARCH ifdef'd paths are NOT defined in the
// vendored build, so only the non-EXACT count_rice_bits_in_partition_ and the
// single-parameter (no-search) set_partitioned_rice_ are ported (the
// alternatives are noted in comments). SPOTCHECK_ESTIMATE is off.
//
// The shared FLAC__StreamEncoder / Protected / Private / ThreadTask state and
// the ApodizationSpecification / ApodizationFunction definitions are owned by
// family A (encoder_state.go); this file codes against those agreed field and
// method names. See the family-B report for the dependency contract.

// applyApodizationState — port of apply_apodization_state_struct
// (stream_encoder.c:105). Local to family B (process_subframe_ /
// apply_apodization_). a, b, c are the (apodizations, depth, part) cursor for
// SUBDIVIDE_TUKEY; currentApodization points at the active spec;
// autocRoot/autoc are the order+1-length autocorrelation scratch arrays.
type applyApodizationState struct {
	a, b, c            uint32
	currentApodization *ApodizationSpecification
	autocRoot          [MaxLPCOrder + 1]float64
	autoc              [MaxLPCOrder + 1]float64
}

// signalView models the C `const void *integer_signal`, which aliases either
// an int32 buffer (subframe_bps <= 32) or an int64 buffer (33-bit side
// channel, subframe_bps > 32). Exactly one of i32/i64 is populated; wide
// reports which. Indexing helpers below mirror the casts the C performs at
// each use site.
type signalView struct {
	i32  []int32
	i64  []int64
	wide bool // true => 33-bit side channel (subframe_bps > 32, use i64)
}

// fixedMaxFixedOrderU is FLAC__MAX_FIXED_ORDER as a uint32 for index math.
const fixedMaxFixedOrderU = uint32(fixedMaxFixedOrder)

// processSubframes_ — port of process_subframes_ (stream_encoder.c:3735).
// Decides the channel assignment (independent / mid-side / loose), prepares
// the mid-side signals, computes wasted bits + effective bps per subframe,
// runs processSubframe_ on each candidate channel, then composes the frame
// bitbuffer via FrameAddHeader + addSubframe_ (family C). Returns false (and
// sets encoder state) on framing failure.
func (encoder *StreamEncoder) processSubframes_(threadtask *StreamEncoderThreadTask) bool {
	var frameHeader FrameHeader
	minPartitionOrder := encoder.prot.minResidualPartitionOrder
	allSubframesConstant := true
	blocksize := encoder.prot.blocksize
	bps := encoder.prot.bitsPerSample

	threadtask.disableConstantSubframes = encoder.priv.disableConstantSubframes

	// Calculate the min,max Rice partition orders.
	maxPartitionOrder := MaxRicePartitionOrderFromBlocksize(blocksize)
	maxPartitionOrder = flacMinU32(maxPartitionOrder, encoder.prot.maxResidualPartitionOrder)
	minPartitionOrder = flacMinU32(minPartitionOrder, maxPartitionOrder)

	// Setup the frame.
	frameHeader.Blocksize = blocksize
	frameHeader.SampleRate = encoder.prot.sampleRate
	frameHeader.Channels = encoder.prot.channels
	frameHeader.ChannelAssignment = ChannelAssignmentIndependent // default unless we determine otherwise
	frameHeader.BitsPerSample = bps
	frameHeader.NumberType = FrameNumberTypeFrameNumber
	frameHeader.Number = uint64(threadtask.currentFrameNumber)

	// Figure out what channel assignments to try.
	var doIndependent, doMidSide bool
	if encoder.prot.doMidSideStereo {
		if encoder.prot.looseMidSideStereo {
			var sumAbsLR, sumAbsMS uint64
			if bps < 25 {
				for i := uint32(1); i < blocksize; i++ {
					predictionLeft := threadtask.integerSignal[0][i] - threadtask.integerSignal[0][i-1]
					predictionRight := threadtask.integerSignal[1][i] - threadtask.integerSignal[1][i-1]
					sumAbsLR += uint64(absI32(predictionLeft)) + uint64(absI32(predictionRight))
					sumAbsMS += uint64(absI32((predictionLeft+predictionRight)>>1)) + uint64(absI32(predictionLeft-predictionRight))
				}
			} else { // bps 25 or higher
				for i := uint32(1); i < blocksize; i++ {
					predictionLeft := int64(threadtask.integerSignal[0][i]) - int64(threadtask.integerSignal[0][i-1])
					predictionRight := int64(threadtask.integerSignal[1][i]) - int64(threadtask.integerSignal[1][i-1])
					sumAbsLR += localAbs64(predictionLeft) + localAbs64(predictionRight)
					sumAbsMS += localAbs64((predictionLeft+predictionRight)>>1) + localAbs64(predictionLeft-predictionRight)
				}
			}
			if sumAbsLR < sumAbsMS {
				doIndependent = true
				doMidSide = false
				frameHeader.ChannelAssignment = ChannelAssignmentIndependent
			} else {
				doIndependent = false
				doMidSide = true
				frameHeader.ChannelAssignment = ChannelAssignmentMidSide
			}
		} else {
			doIndependent = true
			doMidSide = true
		}
	} else {
		doIndependent = true
		doMidSide = false
	}

	// FLAC__ASSERT(do_independent || do_mid_side);

	// Prepare mid-side signals if applicable.
	if doMidSide {
		// FLAC__ASSERT(channels == 2)
		if bps < 32 {
			for i := uint32(0); i < blocksize; i++ {
				threadtask.integerSignalMidSide[1][i] = threadtask.integerSignal[0][i] - threadtask.integerSignal[1][i]
				threadtask.integerSignalMidSide[0][i] = (threadtask.integerSignal[0][i] + threadtask.integerSignal[1][i]) >> 1 // NOTE: not the same as 'mid = (l+r)/2'
			}
		} else {
			for i := uint32(0); i <= blocksize; i++ {
				threadtask.integerSignal33bitSide[i] = int64(threadtask.integerSignal[0][i]) - int64(threadtask.integerSignal[1][i])
				threadtask.integerSignalMidSide[0][i] = int32((int64(threadtask.integerSignal[0][i]) + int64(threadtask.integerSignal[1][i])) >> 1)
			}
		}
	}

	// Check for wasted bits; set effective bps for each subframe.
	if doIndependent {
		for channel := uint32(0); channel < encoder.prot.channels; channel++ {
			w := getWastedBits_(threadtask.integerSignal[channel], blocksize)
			if w > bps {
				w = bps
			}
			threadtask.subframeWorkspace[channel][0].WastedBits = w
			threadtask.subframeWorkspace[channel][1].WastedBits = w
			threadtask.subframeBps[channel] = bps - w
		}
	}
	if doMidSide {
		// FLAC__ASSERT(channels == 2)
		for channel := uint32(0); channel < 2; channel++ {
			var w uint32
			if bps < 32 || channel == 0 {
				w = getWastedBits_(threadtask.integerSignalMidSide[channel], blocksize)
			} else {
				w = getWastedBitsWide_(threadtask.integerSignal33bitSide, threadtask.integerSignalMidSide[channel], blocksize)
			}
			if w > bps {
				w = bps
			}
			threadtask.subframeWorkspaceMidSide[channel][0].WastedBits = w
			threadtask.subframeWorkspaceMidSide[channel][1].WastedBits = w
			extra := uint32(0)
			if channel != 0 {
				extra = 1
			}
			threadtask.subframeBpsMidSide[channel] = bps - w + extra
		}
	}

	// First do a normal encoding pass of each independent channel.
	if doIndependent {
		for channel := uint32(0); channel < encoder.prot.channels; channel++ {
			if encoder.prot.limitMinBitrate && allSubframesConstant && (channel+1) == encoder.prot.channels {
				// This frame contains only constant subframes at this point.
				// To prevent the frame from becoming too small, make sure the
				// last subframe isn't constant.
				threadtask.disableConstantSubframes = true
			}
			sig := signalView{i32: threadtask.integerSignal[channel]}
			if !encoder.processSubframe_(
				threadtask,
				minPartitionOrder,
				maxPartitionOrder,
				&frameHeader,
				threadtask.subframeBps[channel],
				sig,
				threadtask.subframeWorkspacePtr[channel][:],
				threadtask.partitionedRiceContentsWorkspacePtr[channel][:],
				threadtask.residualWorkspace[channel][:],
				&threadtask.bestSubframe[channel],
				&threadtask.bestSubframeBits[channel],
			) {
				return false
			}
			if threadtask.subframeWorkspace[channel][threadtask.bestSubframe[channel]].Type != SubframeConstant {
				allSubframesConstant = false
			}
		}
	}

	// Now do mid and side channels if requested.
	if doMidSide {
		// FLAC__ASSERT(channels == 2)
		for channel := uint32(0); channel < 2; channel++ {
			var sig signalView
			if threadtask.subframeBpsMidSide[channel] <= 32 {
				sig = signalView{i32: threadtask.integerSignalMidSide[channel]}
			} else {
				sig = signalView{i64: threadtask.integerSignal33bitSide, wide: true}
			}
			if !encoder.processSubframe_(
				threadtask,
				minPartitionOrder,
				maxPartitionOrder,
				&frameHeader,
				threadtask.subframeBpsMidSide[channel],
				sig,
				threadtask.subframeWorkspacePtrMidSide[channel][:],
				threadtask.partitionedRiceContentsWorkspacePtrMidSide[channel][:],
				threadtask.residualWorkspaceMidSide[channel][:],
				&threadtask.bestSubframeMidSide[channel],
				&threadtask.bestSubframeBitsMidSide[channel],
			) {
				return false
			}
		}
	}

	// Compose the frame bitbuffer.
	if (doIndependent && doMidSide) || encoder.prot.looseMidSideStereo {
		// FLAC__ASSERT(channels == 2)
		var leftBps, rightBps uint32
		var leftSubframe, rightSubframe *Subframe

		if !encoder.prot.looseMidSideStereo {
			// bits indexed by ChannelAssignment.
			var bits [4]uint32
			bits[ChannelAssignmentIndependent] = threadtask.bestSubframeBits[0] + threadtask.bestSubframeBits[1]
			bits[ChannelAssignmentLeftSide] = threadtask.bestSubframeBits[0] + threadtask.bestSubframeBitsMidSide[1]
			bits[ChannelAssignmentRightSide] = threadtask.bestSubframeBits[1] + threadtask.bestSubframeBitsMidSide[1]
			bits[ChannelAssignmentMidSide] = threadtask.bestSubframeBitsMidSide[0] + threadtask.bestSubframeBitsMidSide[1]

			channelAssignment := ChannelAssignmentIndependent
			minBits := bits[channelAssignment]
			for ca := 1; ca <= 3; ca++ {
				if bits[ca] < minBits {
					minBits = bits[ca]
					channelAssignment = ChannelAssignment(ca)
				}
			}
			frameHeader.ChannelAssignment = channelAssignment
		}

		if !FrameAddHeader(&frameHeader, threadtask.frame) {
			encoder.prot.state = StreamEncoderFramingError
			return false
		}

		switch frameHeader.ChannelAssignment {
		case ChannelAssignmentIndependent:
			leftSubframe = &threadtask.subframeWorkspace[0][threadtask.bestSubframe[0]]
			rightSubframe = &threadtask.subframeWorkspace[1][threadtask.bestSubframe[1]]
		case ChannelAssignmentLeftSide:
			leftSubframe = &threadtask.subframeWorkspace[0][threadtask.bestSubframe[0]]
			rightSubframe = &threadtask.subframeWorkspaceMidSide[1][threadtask.bestSubframeMidSide[1]]
		case ChannelAssignmentRightSide:
			leftSubframe = &threadtask.subframeWorkspaceMidSide[1][threadtask.bestSubframeMidSide[1]]
			rightSubframe = &threadtask.subframeWorkspace[1][threadtask.bestSubframe[1]]
		case ChannelAssignmentMidSide:
			leftSubframe = &threadtask.subframeWorkspaceMidSide[0][threadtask.bestSubframeMidSide[0]]
			rightSubframe = &threadtask.subframeWorkspaceMidSide[1][threadtask.bestSubframeMidSide[1]]
		}

		switch frameHeader.ChannelAssignment {
		case ChannelAssignmentIndependent:
			leftBps = threadtask.subframeBps[0]
			rightBps = threadtask.subframeBps[1]
		case ChannelAssignmentLeftSide:
			leftBps = threadtask.subframeBps[0]
			rightBps = threadtask.subframeBpsMidSide[1]
		case ChannelAssignmentRightSide:
			leftBps = threadtask.subframeBpsMidSide[1]
			rightBps = threadtask.subframeBps[1]
		case ChannelAssignmentMidSide:
			leftBps = threadtask.subframeBpsMidSide[0]
			rightBps = threadtask.subframeBpsMidSide[1]
		}

		// add_subframe_ sets the state for us in case of an error.
		if !encoder.addSubframe_(frameHeader.Blocksize, leftBps, leftSubframe, threadtask.frame) {
			return false
		}
		if !encoder.addSubframe_(frameHeader.Blocksize, rightBps, rightSubframe, threadtask.frame) {
			return false
		}
	} else {
		// FLAC__ASSERT(do_independent)
		if !FrameAddHeader(&frameHeader, threadtask.frame) {
			encoder.prot.state = StreamEncoderFramingError
			return false
		}
		for channel := uint32(0); channel < encoder.prot.channels; channel++ {
			if !encoder.addSubframe_(frameHeader.Blocksize, threadtask.subframeBps[channel], &threadtask.subframeWorkspace[channel][threadtask.bestSubframe[channel]], threadtask.frame) {
				// The above function sets the state for us in case of an error.
				return false
			}
		}
	}

	return true
}

// processSubframe_ — port of process_subframe_ (stream_encoder.c:4033).
// Evaluates VERBATIM (the baseline), then CONSTANT / FIXED / LPC candidates,
// keeping the smallest-bits subframe in subframe[*best_subframe]. subframe,
// partitionedRiceContents and residual are 2-element ping-pong workspaces
// (candidate vs best). The void* integer_signal is modelled by signalView.
func (encoder *StreamEncoder) processSubframe_(
	threadtask *StreamEncoderThreadTask,
	minPartitionOrder uint32,
	maxPartitionOrder uint32,
	frameHeader *FrameHeader,
	subframeBps uint32,
	integerSignal signalView,
	subframe []*Subframe,
	partitionedRiceContents []*PartitionedRiceContents,
	residual [][]int32,
	bestSubframe *uint32,
	bestBits *uint32,
) bool {
	var fixedResidualBitsPerSample [fixedMaxFixedOrder + 1]float32
	var lpcError [MaxLPCOrder]float64

	var minFixedOrder, maxFixedOrder, guessFixedOrder, fixedOrder uint32

	// Only use RICE2 partitions if stream bps > 16.
	riceParameterLimit := uint32(EntropyCodingPartitionedRiceEscape)
	if encoder.prot.bitsPerSample > 16 {
		riceParameterLimit = EntropyCodingPartitionedRice2Escape
	}

	// FLAC__ASSERT(frame_header->blocksize > 0);

	blocksize := frameHeader.Blocksize

	// Verbatim subframe is the baseline against which we measure others.
	bestSubframeIdx := uint32(0)
	var bestBitsV uint32
	if encoder.priv.disableVerbatimSubframes && blocksize >= fixedMaxFixedOrderU {
		bestBitsV = 0xFFFFFFFF
	} else {
		bestBitsV = encoder.evaluateVerbatimSubframe_(integerSignal, blocksize, subframeBps, subframe[bestSubframeIdx])
	}
	*bestBits = bestBitsV

	if blocksize > fixedMaxFixedOrderU {
		signalIsConstant := false
		// Select the fixed-predictor best-order accumulator width. The error
		// of a 4th-order predictor for one sample is the sum of 17 sample
		// values; over (blocksize-order) values the max total is
		// max_sample * (blocksize-order) * 17. ilog2 gives floor(log2); the
		// result must be 31 or lower for the 32-bit accumulator.
		if subframeBps < 28 {
			if subframeBps+ILog2((blocksize-fixedMaxFixedOrderU)*17) < 32 {
				guessFixedOrder = FixedComputeBestPredictor(integerSignal.i32, blocksize-fixedMaxFixedOrderU, &fixedResidualBitsPerSample)
			} else {
				guessFixedOrder = FixedComputeBestPredictorWide(integerSignal.i32, blocksize-fixedMaxFixedOrderU, &fixedResidualBitsPerSample)
			}
		} else if subframeBps <= 32 {
			guessFixedOrder = FixedComputeBestPredictorLimitResidual(integerSignal.i32, blocksize-fixedMaxFixedOrderU, &fixedResidualBitsPerSample)
		} else {
			guessFixedOrder = FixedComputeBestPredictorLimitResidual33Bit(integerSignal.i64, blocksize-fixedMaxFixedOrderU, &fixedResidualBitsPerSample)
		}

		// Check for constant subframe.
		if !threadtask.disableConstantSubframes && fixedResidualBitsPerSample[1] == 0.0 {
			// The above means it's possible all samples are the same value;
			// now double-check it.
			signalIsConstant = true
			if !integerSignal.wide {
				s := integerSignal.i32
				for i := uint32(1); i < blocksize; i++ {
					if s[0] != s[i] {
						signalIsConstant = false
						break
					}
				}
			} else {
				s := integerSignal.i64
				for i := uint32(1); i < blocksize; i++ {
					if s[0] != s[i] {
						signalIsConstant = false
						break
					}
				}
			}
		}

		if signalIsConstant {
			var candidateBits uint32
			if !integerSignal.wide {
				candidateBits = encoder.evaluateConstantSubframe_(int64(integerSignal.i32[0]), blocksize, subframeBps, subframe[bestSubframeIdx^1])
			} else {
				candidateBits = encoder.evaluateConstantSubframe_(integerSignal.i64[0], blocksize, subframeBps, subframe[bestSubframeIdx^1])
			}
			if candidateBits < bestBitsV {
				bestSubframeIdx ^= 1
				bestBitsV = candidateBits
			}
		} else {
			if !encoder.priv.disableFixedSubframes || (encoder.prot.maxLPCOrder == 0 && bestBitsV == 0xFFFFFFFF) {
				// Encode fixed.
				if encoder.prot.doExhaustiveModelSearch {
					minFixedOrder = 0
					maxFixedOrder = fixedMaxFixedOrderU
				} else {
					minFixedOrder = guessFixedOrder
					maxFixedOrder = guessFixedOrder
				}
				if maxFixedOrder >= blocksize {
					maxFixedOrder = blocksize - 1
				}
				for fixedOrder = minFixedOrder; fixedOrder <= maxFixedOrder; fixedOrder++ {
					if fixedResidualBitsPerSample[fixedOrder] >= float32(subframeBps) {
						continue // don't even try
					}
					candidateBits := encoder.evaluateFixedSubframe_(
						threadtask,
						integerSignal,
						residual[bestSubframeIdx^1],
						threadtask.absResidualPartitionSums,
						threadtask.rawBitsPerPartition,
						blocksize,
						subframeBps,
						fixedOrder,
						riceParameterLimit,
						minPartitionOrder,
						maxPartitionOrder,
						encoder.prot.doEscapeCoding,
						encoder.prot.riceParameterSearchDist,
						subframe[bestSubframeIdx^1],
						partitionedRiceContents[bestSubframeIdx^1],
					)
					if candidateBits < bestBitsV {
						bestSubframeIdx ^= 1
						bestBitsV = candidateBits
					}
				}
			}

			// Encode LPC.
			if encoder.prot.maxLPCOrder > 0 {
				var maxLpcOrder uint32
				if encoder.prot.maxLPCOrder >= blocksize {
					maxLpcOrder = blocksize - 1
				} else {
					maxLpcOrder = encoder.prot.maxLPCOrder
				}
				if maxLpcOrder > 0 {
					var st applyApodizationState
					st.a = 0
					st.b = 1
					st.c = 0
					for st.a < encoder.prot.numApodizations {
						maxLpcOrderThisApodization := maxLpcOrder
						var guessLpcOrder uint32

						if !encoder.applyApodization_(threadtask, &st, blocksize, lpcError[:], &maxLpcOrderThisApodization, subframeBps, integerSignal, &guessLpcOrder) {
							// If apply_apodization_ fails, try next apodization.
							continue
						}

						var minLpcOrder uint32
						if encoder.prot.doExhaustiveModelSearch {
							minLpcOrder = 1
						} else {
							minLpcOrder = guessLpcOrder
							maxLpcOrderThisApodization = guessLpcOrder
						}
						for lpcOrder := minLpcOrder; lpcOrder <= maxLpcOrderThisApodization; lpcOrder++ {
							lpcResidualBitsPerSample := LPCComputeExpectedBitsPerResidualSample(lpcError[lpcOrder-1], blocksize-lpcOrder)
							if lpcResidualBitsPerSample >= float64(subframeBps) {
								continue // don't even try
							}
							var minQlpCoeffPrecision, maxQlpCoeffPrecision uint32
							if encoder.prot.doQLPCoeffPrecSearch {
								minQlpCoeffPrecision = MinQLPCoeffPrecision
								// Try to keep qlp coeff precision such that only
								// 32-bit math is required for decode of
								// <=16bps(+1bps for side channel) streams.
								if subframeBps <= 17 {
									maxQlpCoeffPrecision = flacMinU32(32-subframeBps-ILog2(lpcOrder), MaxQLPCoeffPrecision)
									maxQlpCoeffPrecision = flacMaxU32(maxQlpCoeffPrecision, minQlpCoeffPrecision)
								} else {
									maxQlpCoeffPrecision = MaxQLPCoeffPrecision
								}
							} else {
								minQlpCoeffPrecision = encoder.prot.qlpCoeffPrecision
								maxQlpCoeffPrecision = encoder.prot.qlpCoeffPrecision
							}
							for qlpCoeffPrecision := minQlpCoeffPrecision; qlpCoeffPrecision <= maxQlpCoeffPrecision; qlpCoeffPrecision++ {
								candidateBits := encoder.evaluateLpcSubframe_(
									threadtask,
									integerSignal,
									residual[bestSubframeIdx^1],
									threadtask.absResidualPartitionSums,
									threadtask.rawBitsPerPartition,
									threadtask.lpCoeff[lpcOrder-1][:],
									blocksize,
									subframeBps,
									lpcOrder,
									qlpCoeffPrecision,
									riceParameterLimit,
									minPartitionOrder,
									maxPartitionOrder,
									encoder.prot.doEscapeCoding,
									encoder.prot.riceParameterSearchDist,
									subframe[bestSubframeIdx^1],
									partitionedRiceContents[bestSubframeIdx^1],
								)
								if candidateBits > 0 { // if == 0, there was a problem quantizing the lpcoeffs
									if candidateBits < bestBitsV {
										bestSubframeIdx ^= 1
										bestBitsV = candidateBits
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Under rare circumstances this can happen when all but lpc subframe types
	// are disabled.
	if bestBitsV == 0xFFFFFFFF {
		// FLAC__ASSERT(_best_subframe == 0)
		bestBitsV = encoder.evaluateVerbatimSubframe_(integerSignal, blocksize, subframeBps, subframe[bestSubframeIdx])
	}

	*bestSubframe = bestSubframeIdx
	*bestBits = bestBitsV

	return true
}

// setNextSubdivideTukey — port of set_next_subdivide_tukey
// (stream_encoder.c:4281). Advances the (apodizations, depth, part) cursor for
// the SUBDIVIDE_TUKEY apodization across its interleaved partial/punchout
// windows. The argument names here mirror apply_apodization_'s call: a is the
// apodizations index, b is the depth, c is the part.
func setNextSubdivideTukey(parts int32, apodizations, currentDepth, currentPart *uint32) {
	// current_part is interleaved: even are partial, odd are punchout.
	if *currentDepth == 2 {
		// For depth 2, we only do partial, no punchout as that is almost
		// redundant.
		if *currentPart == 0 {
			*currentPart = 2
		} else { // *currentPart == 2
			*currentPart = 0
			*currentDepth++
		}
	} else if *currentPart < (2*(*currentDepth) - 1) {
		*currentPart++
	} else { // *currentPart >= 2*depth-1
		*currentPart = 0
		*currentDepth++
	}

	// Now check if we are done with this SUBDIVIDE_TUKEY apodization.
	if *currentDepth > uint32(parts) {
		*apodizations++
		*currentDepth = 1
		*currentPart = 0
	}
}

// applyApodization_ — port of apply_apodization_ (stream_encoder.c:4306).
// Windows the (possibly partial) signal, computes the autocorrelation, runs
// Levinson-Durbin into threadtask.lpCoeff + lpcError, and computes the guess
// LPC order. Returns false to signal "skip / try next apodization" (the C
// returns false both on the subdivide-tukey part-too-small case and on a
// constant signal). On the SUBDIVIDE_TUKEY path it ping-pongs the
// autoc_root/autoc state across calls exactly as the C does.
func (encoder *StreamEncoder) applyApodization_(
	threadtask *StreamEncoderThreadTask,
	st *applyApodizationState,
	blocksize uint32,
	lpcError []float64,
	maxLpcOrderThisApodization *uint32,
	subframeBps uint32,
	integerSignal signalView,
	guessLpcOrder *uint32,
) bool {
	st.currentApodization = &encoder.prot.apodizations[st.a]

	if st.b == 1 {
		// Window full subblock.
		if !integerSignal.wide {
			LPCWindowData(integerSignal.i32, encoder.priv.window[st.a], threadtask.windowedSignal, blocksize)
		} else {
			LPCWindowDataWide(integerSignal.i64, encoder.priv.window[st.a], threadtask.windowedSignal, blocksize)
		}
		LPCComputeAutocorrelation(threadtask.windowedSignal, blocksize, (*maxLpcOrderThisApodization)+1, st.autoc[:])
		if st.currentApodization.Type == ApodizationSubdivideTukey {
			// NOTE: faithful reproduction of the libFLAC loop, which copies
			// the whole autoc[] into autoc_root[] on every iteration of the
			// (otherwise empty-bodied) for loop — see stream_encoder.c:4327.
			copy(st.autocRoot[:*maxLpcOrderThisApodization], st.autoc[:*maxLpcOrderThisApodization])
			st.b++
		} else {
			st.a++
		}
	} else {
		// Window part of subblock.
		if blocksize/st.b <= MaxLPCOrder {
			// Windowing parts smaller than or equal to FLAC__MAX_LPC_ORDER
			// (32) samples is not supported (see stream_encoder.c:4337).
			setNextSubdivideTukey(st.currentApodization.SubdivideTukey.Parts, &st.a, &st.b, &st.c)
			return false
		}
		if st.c%2 == 0 {
			// On even c, evaluate the (c/2)th partial window of size
			// blocksize/b.
			if !integerSignal.wide {
				LPCWindowDataPartial(integerSignal.i32, encoder.priv.window[st.a], threadtask.windowedSignal, blocksize, blocksize/st.b/2, (st.c/2*blocksize)/st.b)
			} else {
				LPCWindowDataPartialWide(integerSignal.i64, encoder.priv.window[st.a], threadtask.windowedSignal, blocksize, blocksize/st.b/2, (st.c/2*blocksize)/st.b)
			}
			LPCComputeAutocorrelation(threadtask.windowedSignal, blocksize/st.b, (*maxLpcOrderThisApodization)+1, st.autoc[:])
		} else {
			// On uneven c, evaluate the root window (over the whole block)
			// minus the previous partial window.
			for i := uint32(0); i < *maxLpcOrderThisApodization; i++ {
				st.autoc[i] = st.autocRoot[i] - st.autoc[i]
			}
		}
		// Next function sets a, b and c appropriate for next iteration.
		setNextSubdivideTukey(st.currentApodization.SubdivideTukey.Parts, &st.a, &st.b, &st.c)
	}

	if st.autoc[0] == 0.0 {
		// Signal seems to be constant, so we can't do lp.
		return false
	}
	*maxLpcOrderThisApodization = LPCComputeLPCoefficients(st.autoc[:], *maxLpcOrderThisApodization, lpCoeffRows(&threadtask.lpCoeff), lpcError)

	overhead := encoder.prot.qlpCoeffPrecision
	if encoder.prot.doQLPCoeffPrecSearch {
		// Have to guess; use the min possible size to avoid accidentally
		// favoring lower orders.
		overhead = MinQLPCoeffPrecision
	}
	*guessLpcOrder = LPCComputeBestOrder(lpcError, *maxLpcOrderThisApodization, blocksize, subframeBps+overhead)
	return true
}

// evaluateConstantSubframe_ — port of evaluate_constant_subframe_
// (stream_encoder.c:4454). Fills a CONSTANT subframe and returns its bit
// estimate (no residual; just the header + one sample value).
func (encoder *StreamEncoder) evaluateConstantSubframe_(signal int64, blocksize, subframeBps uint32, subframe *Subframe) uint32 {
	_ = blocksize // (void)blocksize when SPOTCHECK_ESTIMATE is off.
	subframe.Type = SubframeConstant
	subframe.Constant.Value = signal
	return SubframeZeroPadLen + SubframeTypeLen + SubframeWastedBitsFlagLen + subframe.WastedBits + subframeBps
}

// evaluateFixedSubframe_ — port of evaluate_fixed_subframe_
// (stream_encoder.c:4477). Computes the fixed-order residual, picks the best
// rice partition order, fills the FIXED subframe, and returns the total bit
// estimate (or UINT32_MAX if the residual bits would overflow the sum).
func (encoder *StreamEncoder) evaluateFixedSubframe_(
	threadtask *StreamEncoderThreadTask,
	signal signalView,
	residual []int32,
	absResidualPartitionSums []uint64,
	rawBitsPerPartition []uint32,
	blocksize uint32,
	subframeBps uint32,
	order uint32,
	riceParameterLimit uint32,
	minPartitionOrder uint32,
	maxPartitionOrder uint32,
	doEscapeCoding bool,
	riceParameterSearchDist uint32,
	subframe *Subframe,
	partitionedRiceContents *PartitionedRiceContents,
) uint32 {
	residualSamples := blocksize - order

	if (subframeBps + order) <= 32 {
		FixedComputeResidual(signal.i32, residualSamples, order, residual)
	} else if subframeBps <= 32 {
		FixedComputeResidualWide(signal.i32, residualSamples, order, residual)
	} else {
		FixedComputeResidualWide33Bit(signal.i64, residualSamples, order, residual)
	}

	subframe.Type = SubframeFixed
	subframe.Fixed.EntropyCoding.Type = EntropyCodingMethodPartitionedRice
	subframe.Fixed.EntropyCoding.Contents = *partitionedRiceContents
	subframe.Fixed.Residual = residual

	residualBits := encoder.findBestPartitionOrder_(
		threadtask,
		residual,
		absResidualPartitionSums,
		rawBitsPerPartition,
		residualSamples,
		order,
		riceParameterLimit,
		minPartitionOrder,
		maxPartitionOrder,
		subframeBps,
		doEscapeCoding,
		riceParameterSearchDist,
		&subframe.Fixed.EntropyCoding,
	)
	// find_best_partition_order_ wrote the winning parameters/raw_bits into the
	// caller's contents object; mirror that back so the framing writer reads
	// them (the C aliases the same pointer).
	*partitionedRiceContents = subframe.Fixed.EntropyCoding.Contents

	subframe.Fixed.Order = order
	if !signal.wide {
		for i := uint32(0); i < order; i++ {
			subframe.Fixed.Warmup[i] = int64(signal.i32[i])
		}
	} else {
		for i := uint32(0); i < order; i++ {
			subframe.Fixed.Warmup[i] = signal.i64[i]
		}
	}

	estimate := SubframeZeroPadLen + SubframeTypeLen + SubframeWastedBitsFlagLen + subframe.WastedBits + (order * subframeBps)
	if residualBits < 0xFFFFFFFF-estimate { // make sure estimate doesn't overflow
		estimate += residualBits
	} else {
		estimate = 0xFFFFFFFF
	}
	return estimate
}

// evaluateLpcSubframe_ — port of evaluate_lpc_subframe_
// (stream_encoder.c:4552). Quantizes the LP coefficients, computes the
// residual (selecting the narrow / wide / 64-bit / limit-residual variant the
// way the C does), picks the best rice partition order, fills the LPC
// subframe, and returns the total bit estimate. Returns 0 if quantization
// failed or a residual overflowed (the C "hack" return value).
func (encoder *StreamEncoder) evaluateLpcSubframe_(
	threadtask *StreamEncoderThreadTask,
	signal signalView,
	residual []int32,
	absResidualPartitionSums []uint64,
	rawBitsPerPartition []uint32,
	lpCoeff []float32,
	blocksize uint32,
	subframeBps uint32,
	order uint32,
	qlpCoeffPrecision uint32,
	riceParameterLimit uint32,
	minPartitionOrder uint32,
	maxPartitionOrder uint32,
	doEscapeCoding bool,
	riceParameterSearchDist uint32,
	subframe *Subframe,
	partitionedRiceContents *PartitionedRiceContents,
) uint32 {
	var qlpCoeff [MaxLPCOrder]int32
	residualSamples := blocksize - order

	// Try to keep qlp coeff precision such that only 32-bit math is required
	// for decode of <=16bps(+1bps for side channel) streams.
	if subframeBps <= 17 {
		// FLAC__ASSERT(order > 0 && order <= FLAC__MAX_LPC_ORDER)
		qlpCoeffPrecision = flacMinU32(qlpCoeffPrecision, 32-subframeBps-ILog2(order))
	}

	var quantization int
	ret := LPCQuantizeCoefficients(lpCoeff, order, qlpCoeffPrecision, qlpCoeff[:], &quantization)
	if ret != 0 {
		return 0 // hack to tell the caller we can't do lp at this order
	}

	if LPCMaxResidualBPS(subframeBps, qlpCoeff[:], order, quantization) > 32 {
		if subframeBps <= 32 {
			if !LPCComputeResidualFromQLPCoefficientsLimitResidual(signal.i32, residualSamples, qlpCoeff[:], order, quantization, residual) {
				return 0
			}
		} else {
			if !LPCComputeResidualFromQLPCoefficientsLimitResidual33Bit(signal.i64, residualSamples, qlpCoeff[:], order, quantization, residual) {
				return 0
			}
		}
	} else if LPCMaxPredictionBeforeShiftBPS(subframeBps, qlpCoeff[:], order) <= 32 {
		// local_lpc_compute_residual_from_qlp_coefficients_16bit defaults (in
		// the scalar build) to FLAC__lpc_compute_residual_from_qlp_coefficients,
		// the same narrow routine as the non-16bit local pointer; both map to
		// LPCComputeResidualFromQLPCoefficients in pure Go.
		LPCComputeResidualFromQLPCoefficients(signal.i32, residualSamples, qlpCoeff[:], order, quantization, residual)
	} else {
		// local_lpc_compute_residual_from_qlp_coefficients_64bit defaults to
		// FLAC__lpc_compute_residual_from_qlp_coefficients_wide.
		LPCComputeResidualFromQLPCoefficientsWide(signal.i32, residualSamples, qlpCoeff[:], order, quantization, residual)
	}

	subframe.Type = SubframeLPC
	subframe.LPC.EntropyCoding.Type = EntropyCodingMethodPartitionedRice
	subframe.LPC.EntropyCoding.Contents = *partitionedRiceContents
	subframe.LPC.Residual = residual

	residualBits := encoder.findBestPartitionOrder_(
		threadtask,
		residual,
		absResidualPartitionSums,
		rawBitsPerPartition,
		residualSamples,
		order,
		riceParameterLimit,
		minPartitionOrder,
		maxPartitionOrder,
		subframeBps,
		doEscapeCoding,
		riceParameterSearchDist,
		&subframe.LPC.EntropyCoding,
	)
	*partitionedRiceContents = subframe.LPC.EntropyCoding.Contents

	subframe.LPC.Order = order
	subframe.LPC.QLPCoeffPrecision = qlpCoeffPrecision
	subframe.LPC.QuantizationLevel = quantization
	subframe.LPC.QLPCoeff = qlpCoeff
	if !signal.wide {
		for i := uint32(0); i < order; i++ {
			subframe.LPC.Warmup[i] = int64(signal.i32[i])
		}
	} else {
		for i := uint32(0); i < order; i++ {
			subframe.LPC.Warmup[i] = signal.i64[i]
		}
	}

	estimate := SubframeZeroPadLen + SubframeTypeLen + SubframeWastedBitsFlagLen + subframe.WastedBits +
		SubframeLPCQLPCoeffPrecisionLen + SubframeLPCQLPShiftLen + (order * (qlpCoeffPrecision + subframeBps))
	if residualBits < 0xFFFFFFFF-estimate {
		estimate += residualBits
	} else {
		estimate = 0xFFFFFFFF
	}
	return estimate
}

// evaluateVerbatimSubframe_ — port of evaluate_verbatim_subframe_
// (stream_encoder.c:4657). Fills a VERBATIM subframe (storing the signal by
// reference, int32 or int64) and returns its bit estimate.
func (encoder *StreamEncoder) evaluateVerbatimSubframe_(signal signalView, blocksize, subframeBps uint32, subframe *Subframe) uint32 {
	subframe.Type = SubframeVerbatim
	if !signal.wide {
		subframe.Verbatim.Type = VerbatimDataInt32
		subframe.Verbatim.Data32 = signal.i32
		subframe.Verbatim.Data64 = nil
	} else {
		subframe.Verbatim.Type = VerbatimDataInt64
		subframe.Verbatim.Data64 = signal.i64
		subframe.Verbatim.Data32 = nil
	}
	return SubframeZeroPadLen + SubframeTypeLen + SubframeWastedBitsFlagLen + subframe.WastedBits + (blocksize * subframeBps)
}

// findBestPartitionOrder_ — port of find_best_partition_order_
// (stream_encoder.c:4689). Precomputes the per-partition abs-residual sums (and
// escape raw bits if requested), then iterates partition orders max..min,
// ping-ponging partitioned_rice_contents_extra[!idx], and keeps the order with
// the fewest residual bits. Writes the winning parameters/raw_bits into
// best_ecm's contents and promotes the ECM type to RICE2 if any parameter
// reaches the escape value. Returns the best residual bit count.
func (encoder *StreamEncoder) findBestPartitionOrder_(
	threadtask *StreamEncoderThreadTask,
	residual []int32,
	absResidualPartitionSums []uint64,
	rawBitsPerPartition []uint32,
	residualSamples uint32,
	predictorOrder uint32,
	riceParameterLimit uint32,
	minPartitionOrder uint32,
	maxPartitionOrder uint32,
	bps uint32,
	doEscapeCoding bool,
	riceParameterSearchDist uint32,
	bestEcm *EntropyCodingMethod,
) uint32 {
	var bestResidualBits uint32
	bestParametersIndex := uint32(0)
	bestPartitionOrder := uint32(0)
	blocksize := residualSamples + predictorOrder

	maxPartitionOrder = MaxRicePartitionOrderFromBlocksizeLimited(maxPartitionOrder, blocksize, predictorOrder)
	minPartitionOrder = flacMinU32(minPartitionOrder, maxPartitionOrder)

	precomputePartitionInfoSums_(residual, absResidualPartitionSums, residualSamples, predictorOrder, minPartitionOrder, maxPartitionOrder, bps)

	if doEscapeCoding {
		precomputePartitionInfoEscapes_(residual, rawBitsPerPartition, residualSamples, predictorOrder, minPartitionOrder, maxPartitionOrder)
	}

	{
		sum := uint32(0)
		for partitionOrder := int(maxPartitionOrder); partitionOrder >= int(minPartitionOrder); partitionOrder-- {
			// FLAC__ASSERT(do_escape_coding != (raw_bits_per_partition == NULL))
			var rawBitsSlice []uint32
			if doEscapeCoding {
				rawBitsSlice = rawBitsPerPartition[sum:]
			}
			var residualBits uint32
			if !setPartitionedRice_(
				absResidualPartitionSums[sum:],
				rawBitsSlice,
				residualSamples,
				predictorOrder,
				riceParameterLimit,
				riceParameterSearchDist,
				uint32(partitionOrder),
				doEscapeCoding,
				&threadtask.partitionedRiceContentsExtra[bestParametersIndex^1],
				&residualBits,
			) {
				// FLAC__ASSERT(best_residual_bits != 0)
				break
			}
			sum += 1 << uint(partitionOrder)
			if bestResidualBits == 0 || residualBits < bestResidualBits {
				bestResidualBits = residualBits
				bestParametersIndex ^= 1
				bestPartitionOrder = uint32(partitionOrder)
			}
		}
	}

	bestEcm.PartitionOrder = bestPartitionOrder

	{
		// Save best parameters and raw_bits. The C de-consts the contents
		// pointer; here bestEcm.Contents is a value, so we copy the winning
		// parameters/raw_bits into it.
		prc := &bestEcm.Contents
		src := &threadtask.partitionedRiceContentsExtra[bestParametersIndex]
		n := 1 << bestPartitionOrder
		PartitionedRiceContentsEnsureSize(prc, bestPartitionOrder)
		copy(prc.Parameters[:n], src.Parameters[:n])
		if doEscapeCoding {
			copy(prc.RawBits[:n], src.RawBits[:n])
		}
		// Check if the type should be changed to RICE2 based on the size of
		// the rice parameters.
		for partition := 0; partition < n; partition++ {
			if prc.Parameters[partition] >= EntropyCodingPartitionedRiceEscape {
				bestEcm.Type = EntropyCodingMethodPartitionedRice2
				break
			}
		}
	}

	return bestResidualBits
}

// precomputePartitionInfoSums_ — port of precompute_partition_info_sums_
// (stream_encoder.c:4785). Computes, for max_partition_order, the sum of
// abs(residual) per partition (using a 32-bit accumulator when bps +
// FLAC__MAX_EXTRA_RESIDUAL_BPS < threshold, else a 64-bit one), then merges
// adjacent partitions down to min_partition_order.
func precomputePartitionInfoSums_(
	residual []int32,
	absResidualPartitionSums []uint64,
	residualSamples uint32,
	predictorOrder uint32,
	minPartitionOrder uint32,
	maxPartitionOrder uint32,
	bps uint32,
) {
	defaultPartitionSamples := (residualSamples + predictorOrder) >> maxPartitionOrder
	partitions := uint32(1) << maxPartitionOrder

	// FLAC__ASSERT(default_partition_samples > predictor_order)

	// First do max_partition_order.
	{
		threshold := 32 - ILog2(defaultPartitionSamples)
		// end starts at -predictor_order (unsigned wraparound, matching the C
		// uint32_t end = (uint32_t)(-(int)predictor_order)).
		end := uint32(-int32(predictorOrder))
		// "bps + FLAC__MAX_EXTRA_RESIDUAL_BPS" is the max assumed avg residual
		// magnitude size.
		if bps+4 < threshold { // FLAC__MAX_EXTRA_RESIDUAL_BPS == 4
			residualSample := uint32(0)
			for partition := uint32(0); partition < partitions; partition++ {
				var absResidualPartitionSum uint32
				end += defaultPartitionSamples
				// On arm64 fold the bulk of this partition's abs-sum four
				// samples at a time with the NEON kernel (integer-exact,
				// uint32-wrapping; built in both default and flac_strict).
				// The kernel handles full groups of 4; the <4 tail and the
				// partition-0 short count fall through to the scalar loop.
				if partitionAbsSumSIMDAvailable {
					if vecs := int(end-residualSample) / 4; vecs > 0 {
						absResidualPartitionSum += partitionAbsSum32NEON(&residual[residualSample], vecs)
						residualSample += uint32(vecs * 4)
					}
				}
				for ; residualSample < end; residualSample++ {
					absResidualPartitionSum += absI32(residual[residualSample])
				}
				absResidualPartitionSums[partition] = uint64(absResidualPartitionSum)
			}
		} else { // have to pessimistically use 64 bits for accumulator
			residualSample := uint32(0)
			for partition := uint32(0); partition < partitions; partition++ {
				var absResidualPartitionSum64 uint64
				end += defaultPartitionSamples
				for ; residualSample < end; residualSample++ {
					absResidualPartitionSum64 += uint64(absI32(residual[residualSample]))
				}
				absResidualPartitionSums[partition] = absResidualPartitionSum64
			}
		}
	}

	// Now merge partitions for lower orders.
	{
		fromPartition := uint32(0)
		toPartition := partitions
		for partitionOrder := int(maxPartitionOrder) - 1; partitionOrder >= int(minPartitionOrder); partitionOrder-- {
			partitions >>= 1
			for i := uint32(0); i < partitions; i++ {
				absResidualPartitionSums[toPartition] = absResidualPartitionSums[fromPartition] + absResidualPartitionSums[fromPartition+1]
				toPartition++
				fromPartition += 2
			}
		}
	}
}

// precomputePartitionInfoEscapes_ — port of precompute_partition_info_escapes_
// (stream_encoder.c:4842). Computes, per partition, the raw bits needed to
// store the residual verbatim (escape coding), for max_partition_order, then
// merges adjacent partitions down to min_partition_order taking the max.
func precomputePartitionInfoEscapes_(
	residual []int32,
	rawBitsPerPartition []uint32,
	residualSamples uint32,
	predictorOrder uint32,
	minPartitionOrder uint32,
	maxPartitionOrder uint32,
) {
	blocksize := residualSamples + predictorOrder

	// First do max_partition_order. The C wraps this in a 'for' loop that
	// always breaks on the first iteration (a yuck noted in the source); we
	// reproduce the single max_partition_order pass directly.
	partitionOrder := int(maxPartitionOrder)
	{
		partitions := uint32(1) << uint(partitionOrder)
		defaultPartitionSamples := blocksize >> uint(partitionOrder)
		// FLAC__ASSERT(default_partition_samples > predictor_order)
		residualSample := uint32(0)
		for partition := uint32(0); partition < partitions; partition++ {
			partitionSamples := defaultPartitionSamples
			if partition == 0 {
				partitionSamples -= predictorOrder
			}
			var rmax uint32
			for partitionSample := uint32(0); partitionSample < partitionSamples; partitionSample++ {
				r := residual[residualSample]
				residualSample++
				if r < 0 {
					rmax |= uint32(^r)
				} else {
					rmax |= uint32(r)
				}
			}
			// Now we know all residual values are in [-rmax-1, rmax].
			if rmax != 0 {
				rawBitsPerPartition[partition] = ILog2(rmax) + 2
			} else {
				rawBitsPerPartition[partition] = 1
			}
		}
	}
	toPartition := uint32(1) << uint(partitionOrder)

	// Now merge partitions for lower orders.
	fromPartition := uint32(0)
	for partitionOrder--; partitionOrder >= int(minPartitionOrder); partitionOrder-- {
		partitions := uint32(1) << uint(partitionOrder)
		for i := uint32(0); i < partitions; i++ {
			m := rawBitsPerPartition[fromPartition]
			fromPartition++
			rawBitsPerPartition[toPartition] = flacMaxU32(m, rawBitsPerPartition[fromPartition])
			fromPartition++
			toPartition++
		}
	}
}

// countRiceBitsInPartition_ — port of count_rice_bits_in_partition_
// (stream_encoder.c:4917, the non-EXACT_RICE_BITS_CALCULATION variant the
// vendored build compiles). Estimates the number of bits a partition occupies
// from the sum of abs(residual) and the rice parameter. The
// EXACT_RICE_BITS_CALCULATION alternative (stream_encoder.c:4901) is not
// compiled in the vendored build and is not ported.
func countRiceBitsInPartition_(riceParameter, partitionSamples uint32, absResidualPartitionSum uint64) uint32 {
	var term uint64
	if riceParameter != 0 {
		// rice_parameter-1 because the real coder sign-folds instead of using
		// a sign bit.
		term = absResidualPartitionSum >> (riceParameter - 1)
	} else {
		// Can't shift by a negative number, so reverse.
		term = absResidualPartitionSum << 1
	}
	bits := uint64(EntropyCodingMethodPartitionedRiceParam) +
		uint64(1+riceParameter)*uint64(partitionSamples) +
		term -
		uint64(partitionSamples>>1)
	return uint32(flacMinU64(bits, 0xFFFFFFFF))
}

// setPartitionedRice_ — port of set_partitioned_rice_ (stream_encoder.c:4942),
// the single-parameter (ENABLE_RICE_PARAMETER_SEARCH undefined) path. For each
// partition, derives the rice parameter from the mean magnitude via the
// fixed-point divisor trick, optionally compares against an escape (raw) code,
// and accumulates the total bit count. Returns false if partition 0 has no
// room for the predictor warm-up. The rice_parameter_search_dist argument is
// unused in this build (it is consumed only by the search path).
func setPartitionedRice_(
	absResidualPartitionSums []uint64,
	rawBitsPerPartition []uint32,
	residualSamples uint32,
	predictorOrder uint32,
	riceParameterLimit uint32,
	riceParameterSearchDist uint32,
	partitionOrder uint32,
	searchForEscapes bool,
	partitionedRiceContents *PartitionedRiceContents,
	bits *uint32,
) bool {
	_ = riceParameterSearchDist // (void)rice_parameter_search_dist;

	bitsAcc := uint32(EntropyCodingMethodTypeLen + EntropyCodingMethodPartitionedRiceOrder)
	partitions := uint32(1) << partitionOrder

	// FLAC__ASSERT(rice_parameter_limit <= RICE2_ESCAPE_PARAMETER)

	PartitionedRiceContentsEnsureSize(partitionedRiceContents, partitionOrder)
	parameters := partitionedRiceContents.Parameters
	rawBits := partitionedRiceContents.RawBits

	partitionSamplesBase := (residualSamples + predictorOrder) >> partitionOrder

	// Integer division is slow; precalculate a fixed point divisor (18 bits)
	// because all partitions except the first are the same size.
	partitionSamplesFixedPointDivisorBase := uint32(0x40000) / partitionSamplesBase

	residualSample := uint32(0)
	for partition := uint32(0); partition < partitions; partition++ {
		partitionSamples := partitionSamplesBase
		var partitionSamplesFixedPointDivisor uint32
		if partition > 0 {
			partitionSamplesFixedPointDivisor = partitionSamplesFixedPointDivisorBase
		} else {
			if partitionSamples <= predictorOrder {
				return false
			}
			partitionSamples -= predictorOrder
			partitionSamplesFixedPointDivisor = uint32(0x40000) / partitionSamples
		}
		// 'mean' is actually the sum of magnitudes of all residual values in
		// the partition.
		mean := absResidualPartitionSums[partition]
		var riceParameter uint32
		if mean < 2 || (((mean-1)*uint64(partitionSamplesFixedPointDivisor))>>18) == 0 {
			riceParameter = 0
		} else {
			riceParameter = ILog2Wide(((mean-1)*uint64(partitionSamplesFixedPointDivisor))>>18) + 1
		}

		if riceParameter >= riceParameterLimit {
			riceParameter = riceParameterLimit - 1
		}

		bestPartitionBits := uint32(0xFFFFFFFF)
		bestRiceParameter := uint32(0)
		// ENABLE_RICE_PARAMETER_SEARCH is undefined: single rice_parameter.
		partitionBits := countRiceBitsInPartition_(riceParameter, partitionSamples, absResidualPartitionSums[partition])
		if partitionBits < bestPartitionBits {
			bestRiceParameter = riceParameter
			bestPartitionBits = partitionBits
		}

		if searchForEscapes {
			partitionBits = EntropyCodingMethodPartitionedRice2Param + EntropyCodingMethodPartitionedRiceRawLen + rawBitsPerPartition[partition]*partitionSamples
			if partitionBits <= bestPartitionBits && rawBitsPerPartition[partition] < 32 {
				rawBits[partition] = rawBitsPerPartition[partition]
				bestRiceParameter = 0 // will be converted to escape param later
				bestPartitionBits = partitionBits
			} else {
				rawBits[partition] = 0
			}
		}
		parameters[partition] = bestRiceParameter
		if bestPartitionBits < 0xFFFFFFFF-bitsAcc { // make sure _bits doesn't overflow
			bitsAcc += bestPartitionBits
		} else {
			bitsAcc = 0xFFFFFFFF
		}
		residualSample += partitionSamples
	}

	*bits = bitsAcc
	return true
}

// getWastedBits_ — port of get_wasted_bits_ (stream_encoder.c:5065). Finds the
// common trailing-zero-bit count of the int32 signal, shifts the signal right
// in place by that count, and returns the shift. Returns 0 (no shift) when the
// signal is all zero.
func getWastedBits_(signal []int32, samples uint32) uint32 {
	var x int32
	for i := uint32(0); i < samples && x&1 == 0; i++ {
		x |= signal[i]
	}

	var shift uint32
	if x == 0 {
		shift = 0
	} else {
		for x&1 == 0 {
			shift++
			x >>= 1
		}
	}

	if shift > 0 {
		for i := uint32(0); i < samples; i++ {
			signal[i] >>= shift
		}
	}
	return shift
}

// getWastedBitsWide_ — port of get_wasted_bits_wide_ (stream_encoder.c:5089).
// Like getWastedBits_ but reads the 33-bit int64 signal and writes the shifted
// result into the int32 signal[]. Faithful quirk: when the wide signal is all
// zero, the returned shift is 1 (not 0).
func getWastedBitsWide_(signalWide []int64, signal []int32, samples uint32) uint32 {
	var x int64
	for i := uint32(0); i < samples && x&1 == 0; i++ {
		x |= signalWide[i]
	}

	var shift uint32
	if x == 0 {
		shift = 1
	} else {
		for x&1 == 0 {
			shift++
			x >>= 1
		}
	}

	if shift > 0 {
		for i := uint32(0); i < samples; i++ {
			signal[i] = int32(signalWide[i] >> shift)
		}
	}
	return shift
}

// --- small helpers (macros.h flac_min/flac_max, abs) ---

// flacMinU32 / flacMaxU32 port flac_min / flac_max for uint32 operands.
func flacMinU32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func flacMaxU32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

// flacMinU64 ports flac_min for uint64 operands.
func flacMinU64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// absI32 ports the C abs() applied to FLAC__int32 in the partition-sum and
// channel-decision loops: ((uint32_t)((x)<0? -(x) : (x))). Unsigned negation,
// so abs(INT32_MIN) yields 0x80000000 without UB (the C comment notes
// abs(INT_MIN) is undefined but residual is never INT32_MIN here).
func absI32(x int32) uint32 {
	if x < 0 {
		return uint32(-x)
	}
	return uint32(x)
}

// localAbs64 ports the local_abs64 macro used in the loose mid-side decision.
func localAbs64(x int64) uint64 {
	if x < 0 {
		return uint64(-x)
	}
	return uint64(x)
}

// lpCoeffRows adapts the threadtask's [MaxLPCOrder][MaxLPCOrder]float32 lp
// coefficient matrix to the [][]float32 LPCComputeLPCoefficients expects,
// without copying (each inner slice aliases a row of the array).
func lpCoeffRows(m *[MaxLPCOrder][MaxLPCOrder]float32) [][]float32 {
	rows := make([][]float32, MaxLPCOrder)
	for i := range m {
		rows[i] = m[i][:]
	}
	return rows
}
