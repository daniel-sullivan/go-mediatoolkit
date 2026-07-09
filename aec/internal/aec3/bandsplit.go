// This file ports the band-splitting filters that EchoCanceller3's
// multi-rate path uses (via AudioBuffer/SplittingFilter) to split
// 32kHz/48kHz audio into 16kHz bands and merge them back:
//
//   - modules/audio_processing/three_band_filter_bank.{h,cc} —
//     ThreeBandFilterBank, the 48kHz => 3-band FIR filter bank with DCT
//     modulation (pure float32 arithmetic; kFilterCoeffs/kDctModulation
//     are literal constants, so parity is bit-exact — no libm
//     tolerance exception needed).
//   - common_audio/signal_processing/splitting_filter.c —
//     WebRtcSpl_AllPassQMF/AnalysisQMF/SynthesisQMF, the 32kHz =>
//     2-band QMF (pure int16/int32 fixed-point arithmetic; Go's
//     defined two's-complement wraparound matches the C code's
//     uint32_t-cast overflow handling exactly, so parity is trivially
//     bit-exact).
//   - common_audio/signal_processing/include/spl_inl.h —
//     WebRtcSpl_SatW32ToW16, WebRtcSpl_SubSatW32 (the QMF's saturation
//     helpers).
//   - common_audio/include/audio_util.{h,cc} — FloatS16ToS16 /
//     S16ToFloatS16, the FloatS16-domain conversions SplittingFilter's
//     two-band path wraps around the fixed-point QMF.
//
// Note vs the plan doc: the 48kHz filter bank lives at
// modules/audio_processing/three_band_filter_bank.{h,cc} (top level,
// not common_audio/), and the 2-band QMF is the C file
// common_audio/signal_processing/splitting_filter.c (distinct from the
// float dispatcher modules/audio_processing/splitting_filter.{h,cc}).
package aec3

import "math"

// Three-band (48kHz) filter bank constants. C: three_band_filter_bank.h.
const (
	sparsity       = 4 // kSparsity
	strideLog2     = 2 // kStrideLog2
	stride         = 1 << strideLog2
	numZeroFilters = 2                       // kNumZeroFilters
	filterSize     = 4                       // kFilterSize
	memorySize     = filterSize*stride - 1   // kMemorySize == 15
	subSampling    = ThreeBandFilterNumBands // kSubSampling
	dctSize        = ThreeBandFilterNumBands // kDctSize

	// ThreeBandFilterNumBands == ThreeBandFilterBank::kNumBands.
	ThreeBandFilterNumBands = 3
	// FullBandSize == ThreeBandFilterBank::kFullBandSize (10ms at 48kHz).
	FullBandSize = 480
	// SplitBandSize == ThreeBandFilterBank::kSplitBandSize.
	SplitBandSize = FullBandSize / ThreeBandFilterNumBands
	// numNonZeroFilters == ThreeBandFilterBank::kNumNonZeroFilters.
	numNonZeroFilters = sparsity*ThreeBandFilterNumBands - numZeroFilters

	zeroFilterIndex1 = 3 // kZeroFilterIndex1
	zeroFilterIndex2 = 9 // kZeroFilterIndex2
)

// kFilterCoeffs are the polyphase decompositions of the low-pass
// prototype (Matlab: fir1(N, 1/(2*kNumBands), kaiser(N+1, 3.5))).
// Literal constants from three_band_filter_bank.cc.
var kFilterCoeffs = [numNonZeroFilters][filterSize]float32{
	{-0.00047749, -0.00496888, +0.16547118, +0.00425496},
	{-0.00173287, -0.01585778, +0.14989004, +0.00994113},
	{-0.00304815, -0.02536082, +0.12154542, +0.01157993},
	{-0.00346946, -0.02587886, +0.04760441, +0.00607594},
	{-0.00154717, -0.01136076, +0.01387458, +0.00186353},
	{+0.00186353, +0.01387458, -0.01136076, -0.00154717},
	{+0.00607594, +0.04760441, -0.02587886, -0.00346946},
	{+0.00983212, +0.08543175, -0.02982767, -0.00383509},
	{+0.00994113, +0.14989004, -0.01585778, -0.00173287},
	{+0.00425496, +0.16547118, -0.00496888, -0.00047749},
}

// kDctModulation shifts the half-bandwidth low-pass prototype to the
// center frequencies [1/12, 3/12, 5/12]. Literal constants from
// three_band_filter_bank.cc.
var kDctModulation = [numNonZeroFilters][dctSize]float32{
	{2, 2, 2},
	{1.73205077, 0, -1.73205077},
	{1, -2, 1},
	{-1, 2, -1},
	{-1.73205077, 0, 1.73205077},
	{-2, -2, -2},
	{-1.73205077, 0, 1.73205077},
	{-1, 2, -1},
	{1, -2, 1},
	{1.73205077, 0, -1.73205077},
}

// filterCore filters in (length SplitBandSize) with filter using a
// shift by inShift, taking into account the previous state. The
// accumulation order (sequential += in the exact C loop nesting) is
// preserved for bit-exactness. C: three_band_filter_bank.cc's FilterCore.
func filterCore(filter *[filterSize]float32, in *[SplitBandSize]float32, inShift int, out *[SplitBandSize]float32, state *[memorySize]float32) {
	for k := range out {
		out[k] = 0
	}

	for k := 0; k < inShift; k++ {
		for i, j := 0, memorySize+k-inShift; i < filterSize; i, j = i+1, j-stride {
			out[k] = mla(out[k], state[j], filter[i])
		}
	}

	for k, shift := inShift, 0; k < filterSize*stride; k, shift = k+1, shift+1 {
		loopLimit := filterSize
		if l := 1 + (shift >> strideLog2); l < loopLimit {
			loopLimit = l
		}
		for i, j := 0, shift; i < loopLimit; i, j = i+1, j-stride {
			out[k] = mla(out[k], in[j], filter[i])
		}
		for i, j := loopLimit, memorySize+shift-loopLimit*stride; i < filterSize; i, j = i+1, j-stride {
			out[k] = mla(out[k], state[j], filter[i])
		}
	}

	for k, shift := filterSize*stride, filterSize*stride-inShift; k < SplitBandSize; k, shift = k+1, shift+1 {
		for i, j := 0, shift; i < filterSize; i, j = i+1, j-stride {
			out[k] = mla(out[k], in[j], filter[i])
		}
	}

	// Update current state.
	copy(state[:], in[SplitBandSize-memorySize:])
}

// ThreeBandFilterBank is a 3-band FIR filter bank with DCT modulation
// (after Harris, "Multirate Signal Processing for Communication
// Systems") used for the 48kHz <-> 3x16kHz split. It does not satisfy
// perfect reconstruction (~9.5dB SNR through analysis+synthesis).
// Port of three_band_filter_bank.{h,cc}'s ThreeBandFilterBank.
type ThreeBandFilterBank struct {
	stateAnalysis  [numNonZeroFilters][memorySize]float32
	stateSynthesis [numNonZeroFilters][memorySize]float32
}

// NewThreeBandFilterBank returns a zero-state filter bank (the C
// constructor just zero-fills the state arrays).
func NewThreeBandFilterBank() *ThreeBandFilterBank {
	return &ThreeBandFilterBank{}
}

// filterIndexFor maps the sparsity/subsampling index to a non-zero
// filter index, or -1 for the two all-zero filters. C: the inline
// zero-filter skip + filter_index selection in Analysis/Synthesis.
func filterIndexFor(index int) int {
	switch {
	case index == zeroFilterIndex1 || index == zeroFilterIndex2:
		return -1
	case index < zeroFilterIndex1:
		return index
	case index < zeroFilterIndex2:
		return index - 1
	default:
		return index - 2
	}
}

// Analysis splits in (length FullBandSize) into 3 downsampled
// frequency bands in out, each of length SplitBandSize. C:
// ThreeBandFilterBank::Analysis.
func (f *ThreeBandFilterBank) Analysis(in *[FullBandSize]float32, out *[ThreeBandFilterNumBands][]float32) {
	for band := 0; band < ThreeBandFilterNumBands; band++ {
		for n := range out[band] {
			out[band][n] = 0
		}
	}

	for downsamplingIndex := 0; downsamplingIndex < subSampling; downsamplingIndex++ {
		// Downsample to form the filter input.
		var inSubsampled [SplitBandSize]float32
		for k := 0; k < SplitBandSize; k++ {
			inSubsampled[k] = in[(subSampling-1)-downsamplingIndex+subSampling*k]
		}

		for inShift := 0; inShift < stride; inShift++ {
			// Choose filter, skip zero filters.
			filterIndex := filterIndexFor(downsamplingIndex + inShift*subSampling)
			if filterIndex < 0 {
				continue
			}

			// Filter.
			var outSubsampled [SplitBandSize]float32
			filterCore(&kFilterCoeffs[filterIndex], &inSubsampled, inShift, &outSubsampled, &f.stateAnalysis[filterIndex])

			// Band and modulate the output.
			for band := 0; band < ThreeBandFilterNumBands; band++ {
				outBand := out[band]
				for n := 0; n < SplitBandSize; n++ {
					outBand[n] = mla(outBand[n], kDctModulation[filterIndex][band], outSubsampled[n])
				}
			}
		}
	}
}

// Synthesis merges the 3 downsampled frequency bands in in, each of
// length SplitBandSize, into out (length FullBandSize). C:
// ThreeBandFilterBank::Synthesis.
func (f *ThreeBandFilterBank) Synthesis(in *[ThreeBandFilterNumBands][]float32, out *[FullBandSize]float32) {
	for n := range out {
		out[n] = 0
	}
	for upsamplingIndex := 0; upsamplingIndex < subSampling; upsamplingIndex++ {
		for inShift := 0; inShift < stride; inShift++ {
			// Choose filter, skip zero filters.
			filterIndex := filterIndexFor(upsamplingIndex + inShift*subSampling)
			if filterIndex < 0 {
				continue
			}

			// Prepare filter input by modulating the banded input.
			var inSubsampled [SplitBandSize]float32
			for band := 0; band < ThreeBandFilterNumBands; band++ {
				inBand := in[band]
				for n := 0; n < SplitBandSize; n++ {
					inSubsampled[n] = mla(inSubsampled[n], kDctModulation[filterIndex][band], inBand[n])
				}
			}

			// Filter.
			var outSubsampled [SplitBandSize]float32
			filterCore(&kFilterCoeffs[filterIndex], &inSubsampled, inShift, &outSubsampled, &f.stateSynthesis[filterIndex])

			// Upsample.
			const upsamplingScaling = float32(subSampling)
			for k := 0; k < SplitBandSize; k++ {
				out[upsamplingIndex+subSampling*k] = mla(out[upsamplingIndex+subSampling*k], upsamplingScaling, outSubsampled[k])
			}
		}
	}
}

// Two-band (32kHz) fixed-point QMF. C: common_audio/signal_processing/
// splitting_filter.c. All arithmetic below is integer-only, so Go's
// defined two's-complement semantics reproduce the C bit-for-bit.

// QMFStateSize == TwoBandsStates::kStateSize (splitting_filter.h): each
// direction of each QMF branch carries 6 int32 Q10 state words.
const QMFStateSize = 6

// kAllPassFilter1 and kAllPassFilter2 are the QMF all-pass filter
// coefficients in Q16. C: WebRtcSpl_kAllPassFilter1/2.
var (
	kAllPassFilter1 = [3]uint16{6418, 36982, 57261}
	kAllPassFilter2 = [3]uint16{21333, 49062, 63010}
)

// satW32ToW16 saturates an int32 to int16 range. C: WebRtcSpl_SatW32ToW16.
func satW32ToW16(value32 int32) int16 {
	if value32 > 32767 {
		return 32767
	}
	if value32 < -32768 {
		return -32768
	}
	return int16(value32)
}

// subSatW32 is a saturating int32 subtraction. C: WebRtcSpl_SubSatW32.
func subSatW32(a, b int32) int32 {
	diff := int32(uint32(a) - uint32(b))
	// a - b can't overflow if a and b have the same sign. If they have
	// different signs, a - b has the same sign as a iff it didn't overflow.
	if (a < 0) != (b < 0) && (a < 0) != (diff < 0) {
		if diff < 0 {
			return math.MaxInt32
		}
		return math.MinInt32
	}
	return diff
}

// scaleDiff32 computes C + the 32 most significant bits of A * B,
// with the exact mixed signed/unsigned wraparound of the C macro.
// C: WEBRTC_SPL_SCALEDIFF32(A, B, C).
func scaleDiff32(a uint16, b, c int32) int32 {
	return int32(uint32(c) + uint32((b>>16)*int32(a)) + (uint32(b&0xFFFF)*uint32(a))>>16)
}

// allPassQMF filters inData (Q10) through the three-cascade first-order
// all-pass filter into outData, updating filterState (length
// QMFStateSize, Q10). inData is clobbered (used as intermediate
// storage), exactly as in C. C: WebRtcSpl_AllPassQMF.
func allPassQMF(inData []int32, outData []int32, filterCoefficients *[3]uint16, filterState []int32) {
	dataLength := len(inData)

	// First all-pass cascade; filter from inData to outData.
	diff := subSatW32(inData[0], filterState[1])
	outData[0] = scaleDiff32(filterCoefficients[0], diff, filterState[0])
	for k := 1; k < dataLength; k++ {
		diff = subSatW32(inData[k], outData[k-1])
		outData[k] = scaleDiff32(filterCoefficients[0], diff, inData[k-1])
	}
	filterState[0] = inData[dataLength-1]
	filterState[1] = outData[dataLength-1]

	// Second all-pass cascade; filter from outData to inData.
	diff = subSatW32(outData[0], filterState[3])
	inData[0] = scaleDiff32(filterCoefficients[1], diff, filterState[2])
	for k := 1; k < dataLength; k++ {
		diff = subSatW32(outData[k], inData[k-1])
		inData[k] = scaleDiff32(filterCoefficients[1], diff, outData[k-1])
	}
	filterState[2] = outData[dataLength-1]
	filterState[3] = inData[dataLength-1]

	// Third all-pass cascade; filter from inData to outData.
	diff = subSatW32(inData[0], filterState[5])
	outData[0] = scaleDiff32(filterCoefficients[2], diff, filterState[4])
	for k := 1; k < dataLength; k++ {
		diff = subSatW32(inData[k], outData[k-1])
		outData[k] = scaleDiff32(filterCoefficients[2], diff, inData[k-1])
	}
	filterState[4] = inData[dataLength-1]
	filterState[5] = outData[dataLength-1]
}

// AnalysisQMF splits inData (even length) into a low and a high band,
// each of half the length. filterState1/filterState2 (each length
// QMFStateSize) carry the all-pass state across calls. C:
// WebRtcSpl_AnalysisQMF.
func AnalysisQMF(inData []int16, lowBand, highBand []int16, filterState1, filterState2 []int32) {
	bandLength := len(inData) / 2
	halfIn1 := make([]int32, bandLength)
	halfIn2 := make([]int32, bandLength)
	filter1 := make([]int32, bandLength)
	filter2 := make([]int32, bandLength)

	// Split even and odd samples. Also shift them to Q10.
	for i, k := 0, 0; i < bandLength; i, k = i+1, k+2 {
		halfIn2[i] = int32(inData[k]) * (1 << 10)
		halfIn1[i] = int32(inData[k+1]) * (1 << 10)
	}

	// All pass filter even and odd samples, independently.
	allPassQMF(halfIn1, filter1, &kAllPassFilter1, filterState1)
	allPassQMF(halfIn2, filter2, &kAllPassFilter2, filterState2)

	// Take the sum and difference of filtered version of odd and even
	// branches to get upper & lower band.
	for i := 0; i < bandLength; i++ {
		lowBand[i] = satW32ToW16((filter1[i] + filter2[i] + 1024) >> 11)
		highBand[i] = satW32ToW16((filter1[i] - filter2[i] + 1024) >> 11)
	}
}

// SynthesisQMF merges lowBand and highBand (each of length bandLength)
// into outData (twice the length). filterState1/filterState2 (each
// length QMFStateSize) carry the all-pass state across calls. C:
// WebRtcSpl_SynthesisQMF.
func SynthesisQMF(lowBand, highBand []int16, outData []int16, filterState1, filterState2 []int32) {
	bandLength := len(lowBand)
	halfIn1 := make([]int32, bandLength)
	halfIn2 := make([]int32, bandLength)
	filter1 := make([]int32, bandLength)
	filter2 := make([]int32, bandLength)

	// Obtain the sum and difference channels out of upper and lower-band
	// channels. Also shift to Q10 domain.
	for i := 0; i < bandLength; i++ {
		halfIn1[i] = (int32(lowBand[i]) + int32(highBand[i])) * (1 << 10)
		halfIn2[i] = (int32(lowBand[i]) - int32(highBand[i])) * (1 << 10)
	}

	// All-pass filter the sum and difference channels.
	allPassQMF(halfIn1, filter1, &kAllPassFilter2, filterState1)
	allPassQMF(halfIn2, filter2, &kAllPassFilter1, filterState2)

	// The filtered signals are even and odd samples of the output.
	// Combine them, shifting back from Q10 to Q0 with saturation.
	for i, k := 0, 0; i < bandLength; i, k = i+1, k+2 {
		outData[k] = satW32ToW16((filter2[i] + 512) >> 10)
		outData[k+1] = satW32ToW16((filter1[i] + 512) >> 10)
	}
}

// FloatS16ToS16 converts a FloatS16-domain ([-32768.0, 32767.0]) float
// to int16, clamping and rounding half away from zero. C:
// audio_util.h's FloatS16ToS16.
func FloatS16ToS16(v float32) int16 {
	if v > 32767 {
		v = 32767
	}
	if v < -32768 {
		v = -32768
	}
	return int16(v + float32(math.Copysign(0.5, float64(v))))
}

// S16ToFloatS16 converts an int16 sample to the FloatS16 domain (a
// plain widening cast). C: audio_util.cc's S16ToFloatS16.
func S16ToFloatS16(v int16) float32 {
	return float32(v)
}
