// This file ports the remaining scalar constants and helper functions
// from modules/audio_processing/aec3/aec3_common.{h,cc} that fft.go
// didn't already define (fft.go covers the FFT/block-size family):
// the matched-filter window/shift sizes, the buffer-sizing formulas
// used by the render delay buffer, and ValidFullBandRate.
//
// Aec3Optimization (the CPU-family SIMD dispatch enum) and
// DetectOptimization() are intentionally NOT ported: this whole
// package is scalar-only (see fft.go's FFTData.Spectrum, which already
// dropped the SSE2/AVX2 cases), so there is nothing to dispatch on.
package aec3

import "math"

// MatchedFilterWindowSizeSubBlocks == kMatchedFilterWindowSizeSubBlocks.
const MatchedFilterWindowSizeSubBlocks = 32

// MatchedFilterAlignmentShiftSizeSubBlocks ==
// kMatchedFilterAlignmentShiftSizeSubBlocks.
const MatchedFilterAlignmentShiftSizeSubBlocks = MatchedFilterWindowSizeSubBlocks * 3 / 4

// RenderTransferQueueSizeFrames == kRenderTransferQueueSizeFrames.
const RenderTransferQueueSizeFrames = 100

// ValidFullBandRate reports whether sampleRateHz is one of AEC3's
// supported full-band rates. C: aec3_common.h's ValidFullBandRate.
func ValidFullBandRate(sampleRateHz int) bool {
	return sampleRateHz == 16000 || sampleRateHz == 32000 || sampleRateHz == 48000
}

// GetTimeDomainLength returns the number of samples spanned by
// filterLengthBlocks blocks. C: aec3_common.h's GetTimeDomainLength.
func GetTimeDomainLength(filterLengthBlocks int) int {
	return filterLengthBlocks * FFTLengthBy2
}

// GetDownSampledBufferSize returns the size (in downsampled samples)
// of the matched filter's downsampled-render circular buffer. C:
// aec3_common.h's GetDownSampledBufferSize.
func GetDownSampledBufferSize(downSamplingFactor, numMatchedFilters int) int {
	return BlockSize / downSamplingFactor *
		(MatchedFilterAlignmentShiftSizeSubBlocks*numMatchedFilters + MatchedFilterWindowSizeSubBlocks + 1)
}

// GetRenderDelayBufferSize returns the size (in blocks) of the render
// delay buffer's block/spectrum/fft circular buffers. C:
// aec3_common.h's GetRenderDelayBufferSize.
func GetRenderDelayBufferSize(downSamplingFactor, numMatchedFilters, filterLengthBlocks int) int {
	return GetDownSampledBufferSize(downSamplingFactor, numMatchedFilters)/(BlockSize/downSamplingFactor) +
		filterLengthBlocks + 1
}

// FastApproxLog2f is a fast approximation of log2(in), reinterpreting
// the float32's bit pattern as an integer. C: aec3_common.cc's
// FastApproxLog2f.
func FastApproxLog2f(in float32) float32 {
	a := math.Float32bits(in)
	out := float32(a)
	out = mul32(out, 1.1920929e-7) // 1/2^23
	out = sub32(out, 126.942695)   // Remove bias.
	return out
}

// Log2TodB converts a log2 value to decibels. C: aec3_common.cc's
// Log2TodB. The multiplication is performed in float64 to match the
// C++ side's un-suffixed (double) literal 3.0102999566398121 being
// promoted against the float argument, with the result narrowed back
// to float32 only on return.
func Log2TodB(inLog2 float32) float32 {
	return float32(3.0102999566398121 * float64(inLog2))
}
