// This file ports the scalar path of modules/audio_processing/aec3/
// matched_filter.{h,cc}: MatchedFilter, a bank of NLMS filters used to
// estimate the echo path delay by correlating a downsampled render
// signal against a downsampled capture signal.
//
// Only the scalar kernels (aec3::MatchedFilterCore,
// aec3::MaxSquarePeakIndex) are ported. The SSE2/AVX2/NEON kernels in
// matched_filter.cc are optimized variants of the exact same
// algorithm selected once at construction time via a CPU-detected
// Aec3Optimization enum stored on EchoPathDelayEstimator and passed
// into MatchedFilter's constructor — MatchedFilter itself never
// re-detects the CPU family per call, it just switches on the stored
// enum inside Update(). Since this whole package is scalar-only (see
// common.go's doc comment and fft.go's FFTData.Spectrum), there is no
// SIMD path to select from, so the enum/dispatch is dropped entirely
// rather than "forced" to a scalar setting.
package aec3

// kAccumulatedErrorSubSampleRate == matched_filter.cc's anonymous
// namespace's kAccumulatedErrorSubSampleRate.
const accumulatedErrorSubSampleRate = 4

// LagEstimate stores the delay and pre-echo delay estimated by a
// single MatchedFilter section. C: MatchedFilter::LagEstimate.
type LagEstimate struct {
	Lag        int
	PreEchoLag int
}

// matchedFilterCore is the scalar reference NLMS-correlation kernel.
// C: aec3::MatchedFilterCore (scalar overload, matched_filter.cc).
//
// x is the downsampled render circular buffer, y is the downsampled
// capture sub-block, h is one filter's coefficients (updated
// in-place). accumulatedError is filled in-place when
// computeAccumulatedError is set (its length must be
// len(h)/accumulatedErrorSubSampleRate).
func matchedFilterCore(xStartIndex int, x2SumThreshold, smoothing float32, x, y, h []float32, filtersUpdated *bool, errorSum *float32, computeAccumulatedError bool, accumulatedError []float32) {
	if computeAccumulatedError {
		for k := range accumulatedError {
			accumulatedError[k] = 0
		}
	}

	for i := range y {
		var x2Sum float32
		var s float32
		xIndex := xStartIndex

		if computeAccumulatedError {
			for k := range h {
				x2Sum = mla(x2Sum, x[xIndex], x[xIndex])
				s = mla(s, h[k], x[xIndex])
				if xIndex < len(x)-1 {
					xIndex++
				} else {
					xIndex = 0
				}
				if ((k + 1) & 0b11) == 0 {
					idx := k >> 2
					d := sub32(y[i], s)
					accumulatedError[idx] = mla(accumulatedError[idx], d, d)
				}
			}
		} else {
			for k := range h {
				x2Sum = mla(x2Sum, x[xIndex], x[xIndex])
				s = mla(s, h[k], x[xIndex])
				if xIndex < len(x)-1 {
					xIndex++
				} else {
					xIndex = 0
				}
			}
		}

		e := sub32(y[i], s)
		saturation := y[i] >= 32000 || y[i] <= -32000
		*errorSum = mla(*errorSum, e, e)

		if x2Sum > x2SumThreshold && !saturation {
			alpha := smoothing * e / x2Sum
			xIndex = xStartIndex
			for k := range h {
				h[k] = mla(h[k], alpha, x[xIndex])
				if xIndex < len(x)-1 {
					xIndex++
				} else {
					xIndex = 0
				}
			}
			*filtersUpdated = true
		}

		if xStartIndex > 0 {
			xStartIndex--
		} else {
			xStartIndex = len(x) - 1
		}
	}
}

// maxSquarePeakIndex returns the index of the maximum squared peak in
// h. C: aec3::MaxSquarePeakIndex.
func maxSquarePeakIndex(h []float32) int {
	if len(h) < 2 {
		return 0
	}

	maxElement1 := mul32(h[0], h[0])
	lagEstimate1 := 0
	maxElement2 := mul32(h[1], h[1])
	lagEstimate2 := 1

	lastIndex := len(h) - 1
	k := 2
	for ; k < lastIndex; k += 2 {
		v1 := mul32(h[k], h[k])
		if v1 > maxElement1 {
			maxElement1 = v1
			lagEstimate1 = k
		}
		v2 := mul32(h[k+1], h[k+1])
		if v2 > maxElement2 {
			maxElement2 = v2
			lagEstimate2 = k + 1
		}
	}

	if maxElement2 > maxElement1 {
		maxElement1 = maxElement2
		lagEstimate1 = lagEstimate2
	}

	lastElement := mul32(h[lastIndex], h[lastIndex])
	if lastElement > maxElement1 {
		return lastIndex
	}
	return lagEstimate1
}

// updateAccumulatedError smooths instantaneousAccumulatedError into
// accumulatedError. C: matched_filter.cc's anonymous namespace's
// UpdateAccumulatedError.
func updateAccumulatedError(instantaneousAccumulatedError, accumulatedError []float32, oneOverErrorSumAnchor float32) {
	const kSmoothConstantIncreases float32 = 0.015

	for k := range accumulatedError {
		errorNorm := mul32(instantaneousAccumulatedError[k], oneOverErrorSumAnchor)
		if errorNorm < accumulatedError[k] {
			accumulatedError[k] = errorNorm
		} else {
			diff := sub32(errorNorm, accumulatedError[k])
			accumulatedError[k] = mla(accumulatedError[k], kSmoothConstantIncreases, diff)
		}
	}
}

// computePreEchoLag returns the pre-echo lag computed from
// accumulatedError. C: matched_filter.cc's anonymous namespace's
// ComputePreEchoLag.
func computePreEchoLag(accumulatedError []float32, lag, alignmentShiftWinner int) int {
	const kPreEchoThreshold float32 = 0.5

	preEchoLagEstimate := lag - alignmentShiftWinner
	maximumPreEchoLag := preEchoLagEstimate / accumulatedErrorSubSampleRate
	if maximumPreEchoLag > len(accumulatedError) {
		maximumPreEchoLag = len(accumulatedError)
	}

	for k := maximumPreEchoLag - 1; k >= 0; k-- {
		if accumulatedError[k] > kPreEchoThreshold {
			break
		}
		preEchoLagEstimate = (k+1)*accumulatedErrorSubSampleRate - 1
	}

	return preEchoLagEstimate + alignmentShiftWinner
}

// MatchedFilter is a bank of NLMS filters used to estimate the echo
// path delay. Port of MatchedFilter (scalar path only — see the
// package doc comment above for the dropped SIMD dispatch).
type MatchedFilter struct {
	subBlockSize         int
	filterIntraLagShift  int
	filters              [][]float32
	accumulatedError     [][]float32
	instAccumulatedError []float32

	reportedLagEstimate       *LagEstimate
	winnerLag                 *int
	lastDetectedBestLagFilter int

	numberPreEchoUpdates int

	excitationLimit         float32
	smoothingFast           float32
	smoothingSlow           float32
	matchingFilterThreshold float32
	detectPreEcho           bool
}

// NewMatchedFilter mirrors MatchedFilter's C++ constructor (minus the
// data_dumper and optimization parameters, neither of which apply to
// this scalar-only, non-logging port).
func NewMatchedFilter(subBlockSize, windowSizeSubBlocks, numMatchedFilters, alignmentShiftSubBlocks int, excitationLimit, smoothingFast, smoothingSlow, matchingFilterThreshold float32, detectPreEcho bool) *MatchedFilter {
	m := &MatchedFilter{
		subBlockSize:              subBlockSize,
		filterIntraLagShift:       alignmentShiftSubBlocks * subBlockSize,
		filters:                   make([][]float32, numMatchedFilters),
		lastDetectedBestLagFilter: -1,
		excitationLimit:           excitationLimit,
		smoothingFast:             smoothingFast,
		smoothingSlow:             smoothingSlow,
		matchingFilterThreshold:   matchingFilterThreshold,
		detectPreEcho:             detectPreEcho,
	}

	filterSize := windowSizeSubBlocks * subBlockSize
	for i := range m.filters {
		m.filters[i] = make([]float32, filterSize)
	}

	if detectPreEcho {
		errSize := filterSize / accumulatedErrorSubSampleRate
		m.accumulatedError = make([][]float32, numMatchedFilters)
		for i := range m.accumulatedError {
			m.accumulatedError[i] = make([]float32, errSize)
			for k := range m.accumulatedError[i] {
				m.accumulatedError[i][k] = 1
			}
		}
		m.instAccumulatedError = make([]float32, errSize)
	}

	return m
}

// Reset resets the filter banks. C: MatchedFilter::Reset.
func (m *MatchedFilter) Reset(fullReset bool) {
	for _, f := range m.filters {
		for k := range f {
			f[k] = 0
		}
	}

	m.winnerLag = nil
	m.reportedLagEstimate = nil

	if fullReset {
		for _, e := range m.accumulatedError {
			for k := range e {
				e[k] = 1
			}
		}
		m.numberPreEchoUpdates = 0
	}
}

// Update updates the matched filter banks with the provided render
// buffer and capture sub-block. C: MatchedFilter::Update.
func (m *MatchedFilter) Update(renderBuffer *DownsampledRenderBuffer, capture []float32, useSlowSmoothing bool) {
	y := capture

	smoothing := m.smoothingFast
	if useSlowSmoothing {
		smoothing = m.smoothingSlow
	}

	x2SumThreshold := float32(len(m.filters[0])) * m.excitationLimit * m.excitationLimit

	var errorSumAnchor float32
	for _, v := range y {
		errorSumAnchor = mla(errorSumAnchor, v, v)
	}

	winnerErrorSum := errorSumAnchor
	m.winnerLag = nil
	m.reportedLagEstimate = nil
	alignmentShift := 0
	var previousLagEstimate *int
	winnerIndex := -1

	for n := range m.filters {
		var errorSum float32
		filtersUpdated := false

		computePreEcho := m.detectPreEcho && n == m.lastDetectedBestLagFilter

		xStartIndex := renderBuffer.OffsetIndex(renderBuffer.Read, alignmentShift+m.subBlockSize-1)

		var accErr []float32
		if computePreEcho {
			accErr = m.instAccumulatedError
		}
		matchedFilterCore(xStartIndex, x2SumThreshold, smoothing, renderBuffer.Buffer, y, m.filters[n], &filtersUpdated, &errorSum, computePreEcho, accErr)

		lagEstimate := maxSquarePeakIndex(m.filters[n])
		reliable := lagEstimate > 2 && lagEstimate < len(m.filters[n])-10 && errorSum < m.matchingFilterThreshold*errorSumAnchor

		lag := lagEstimate + alignmentShift

		if filtersUpdated && reliable && errorSum < winnerErrorSum {
			winnerErrorSum = errorSum
			winnerIndex = n
			if previousLagEstimate != nil && *previousLagEstimate == lag {
				w := *previousLagEstimate
				m.winnerLag = &w
				winnerIndex = n - 1
			} else {
				w := lag
				m.winnerLag = &w
			}
		}

		lagCopy := lag
		previousLagEstimate = &lagCopy
		alignmentShift += m.filterIntraLagShift
	}

	if winnerIndex != -1 {
		m.reportedLagEstimate = &LagEstimate{Lag: *m.winnerLag, PreEchoLag: *m.winnerLag}

		if m.detectPreEcho && m.lastDetectedBestLagFilter == winnerIndex {
			const kEnergyThreshold float32 = 1.0
			if errorSumAnchor > kEnergyThreshold {
				updateAccumulatedError(m.instAccumulatedError, m.accumulatedError[winnerIndex], 1.0/errorSumAnchor)
				m.numberPreEchoUpdates++
			}

			if m.numberPreEchoUpdates >= 50 {
				m.reportedLagEstimate.PreEchoLag = computePreEchoLag(m.accumulatedError[winnerIndex], *m.winnerLag, winnerIndex*m.filterIntraLagShift)
			} else {
				m.reportedLagEstimate.PreEchoLag = *m.winnerLag
			}
		}

		m.lastDetectedBestLagFilter = winnerIndex
	}
}

// GetBestLagEstimate returns the best lag estimate, if one is
// available. C: MatchedFilter::GetBestLagEstimate.
func (m *MatchedFilter) GetBestLagEstimate() *LagEstimate {
	return m.reportedLagEstimate
}

// GetMaxFilterLag returns the maximum lag reachable by any filter in
// the bank. C: MatchedFilter::GetMaxFilterLag.
func (m *MatchedFilter) GetMaxFilterLag() int {
	return len(m.filters)*m.filterIntraLagShift + len(m.filters[0])
}
