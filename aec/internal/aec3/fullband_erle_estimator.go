// This file ports modules/audio_processing/aec3/
// fullband_erle_estimator.{h,cc}: FullBandErleEstimator, which
// estimates the echo return loss enhancement using the energy of all
// the frequency bands.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

const (
	fullbandErleEpsilon            = 1e-3
	fullbandErleX2BandEnergyThresh = 44015068.0
	fullbandErleBlocksToHold       = 100
	fullbandErlePointsToAccumulate = 6
)

// erleInstantaneous computes an instantaneous ERLE estimate over
// fullbandErlePointsToAccumulate accumulated points, along with a
// 0-1 quality indication of the linear filter's performance. C:
// FullBandErleEstimator::ErleInstantaneous.
type erleInstantaneous struct {
	clampInstQualityToZero bool
	clampInstQualityToOne  bool
	erleLog2Valid          bool
	erleLog2               float32
	instQualityEstimate    float32
	maxErleLog2            float32
	minErleLog2            float32
	y2Acum                 float32
	e2Acum                 float32
	numPoints              int
}

// newErleInstantaneous constructs an erleInstantaneous. C:
// ErleInstantaneous::ErleInstantaneous.
func newErleInstantaneous(config config.ErleConfig) *erleInstantaneous {
	e := &erleInstantaneous{
		clampInstQualityToZero: config.ClampQualityEstimateToZero,
		clampInstQualityToOne:  config.ClampQualityEstimateToOne,
	}
	e.Reset()
	return e
}

// Update updates the estimator with a new point, returning true if
// the instantaneous ERLE was updated due to having enough points to
// perform the estimate. C: ErleInstantaneous::Update.
func (e *erleInstantaneous) Update(y2Sum, e2Sum float32) bool {
	updateEstimates := false
	e.e2Acum = add32(e.e2Acum, e2Sum)
	e.y2Acum = add32(e.y2Acum, y2Sum)
	e.numPoints++
	if e.numPoints == fullbandErlePointsToAccumulate {
		if e.e2Acum > 0 {
			updateEstimates = true
			e.erleLog2Valid = true
			e.erleLog2 = FastApproxLog2f(add32(e.y2Acum/e.e2Acum, fullbandErleEpsilon))
		}
		e.numPoints = 0
		e.e2Acum = 0
		e.y2Acum = 0
	}

	if updateEstimates {
		e.updateMaxMin()
		e.updateQualityEstimate()
	}
	return updateEstimates
}

// Reset resets the instantaneous ERLE estimator to its initial state.
// C: ErleInstantaneous::Reset.
func (e *erleInstantaneous) Reset() {
	e.ResetAccumulators()
	e.maxErleLog2 = -10.0 // -30 dB.
	e.minErleLog2 = 33.0  // 100 dB.
	e.instQualityEstimate = 0.0
}

// ResetAccumulators resets the members related to an instantaneous
// estimate. C: ErleInstantaneous::ResetAccumulators.
func (e *erleInstantaneous) ResetAccumulators() {
	e.erleLog2Valid = false
	e.erleLog2 = 0
	e.instQualityEstimate = 0.0
	e.numPoints = 0
	e.e2Acum = 0.0
	e.y2Acum = 0.0
}

// GetInstErleLog2 returns the instantaneous ERLE in log2 units, and
// whether it is available (mirrors std::optional). C:
// ErleInstantaneous::GetInstErleLog2.
func (e *erleInstantaneous) GetInstErleLog2() (float32, bool) {
	return e.erleLog2, e.erleLog2Valid
}

// GetQualityEstimate gets an indication between 0 and 1 of the
// performance of the linear filter for the current time instant, and
// whether an estimate is available (mirrors std::optional). C:
// ErleInstantaneous::GetQualityEstimate.
func (e *erleInstantaneous) GetQualityEstimate() (float32, bool) {
	if !e.erleLog2Valid {
		return 0, false
	}
	value := e.instQualityEstimate
	if e.clampInstQualityToZero {
		value = max32(0.0, value)
	}
	if e.clampInstQualityToOne {
		value = minFloat32(1.0, value)
	}
	return value, true
}

// updateMaxMin adds the forgetting factors for the maximum and
// minimum and caps the result to the incoming value. C:
// ErleInstantaneous::UpdateMaxMin.
func (e *erleInstantaneous) updateMaxMin() {
	e.maxErleLog2 = sub32(e.maxErleLog2, 0.0004) // Forget factor, approx 1dB every 3 sec.
	e.maxErleLog2 = max32(e.maxErleLog2, e.erleLog2)
	e.minErleLog2 = add32(e.minErleLog2, 0.0004) // Forget factor, approx 1dB every 3 sec.
	e.minErleLog2 = minFloat32(e.minErleLog2, e.erleLog2)
}

// updateQualityEstimate updates the instantaneous quality estimate.
// TODO(peah): Currently, the estimate can become less than 0; this
// should be corrected. C: ErleInstantaneous::UpdateQualityEstimate.
func (e *erleInstantaneous) updateQualityEstimate() {
	const alpha float32 = 0.07
	var qualityEstimate float32
	if e.maxErleLog2 > e.minErleLog2 {
		qualityEstimate = sub32(e.erleLog2, e.minErleLog2) / sub32(e.maxErleLog2, e.minErleLog2)
	}
	if qualityEstimate > e.instQualityEstimate {
		e.instQualityEstimate = qualityEstimate
	} else {
		e.instQualityEstimate = mla(e.instQualityEstimate, alpha, sub32(qualityEstimate, e.instQualityEstimate))
	}
}

// FullBandErleEstimator estimates the echo return loss enhancement
// using the energy of all the frequency bands. C:
// fullband_erle_estimator.{h,cc}'s FullBandErleEstimator.
type FullBandErleEstimator struct {
	minErleLog2                   float32
	maxErleLfLog2                 float32
	holdCountersInstantaneousErle []int
	erleTimeDomainLog2            []float32
	instantaneousErle             []*erleInstantaneous
	linearFiltersQualities        []*float32
	linearFiltersQualitiesStorage []float32
}

// NewFullBandErleEstimator constructs a FullBandErleEstimator. C:
// FullBandErleEstimator::FullBandErleEstimator.
func NewFullBandErleEstimator(config config.ErleConfig, numCaptureChannels int) *FullBandErleEstimator {
	minErleLog2 := FastApproxLog2f(add32(config.Min, fullbandErleEpsilon))
	e := &FullBandErleEstimator{
		minErleLog2:                   minErleLog2,
		maxErleLfLog2:                 FastApproxLog2f(add32(config.MaxL, fullbandErleEpsilon)),
		holdCountersInstantaneousErle: make([]int, numCaptureChannels),
		erleTimeDomainLog2:            make([]float32, numCaptureChannels),
		instantaneousErle:             make([]*erleInstantaneous, numCaptureChannels),
		linearFiltersQualities:        make([]*float32, numCaptureChannels),
		linearFiltersQualitiesStorage: make([]float32, numCaptureChannels),
	}
	for ch := 0; ch < numCaptureChannels; ch++ {
		e.instantaneousErle[ch] = newErleInstantaneous(config)
	}
	e.Reset()
	return e
}

// Reset resets the ERLE estimator. C: FullBandErleEstimator::Reset.
func (e *FullBandErleEstimator) Reset() {
	for _, ch := range e.instantaneousErle {
		ch.Reset()
	}

	e.updateQualityEstimates()
	for ch := range e.erleTimeDomainLog2 {
		e.erleTimeDomainLog2[ch] = e.minErleLog2
	}
	for ch := range e.holdCountersInstantaneousErle {
		e.holdCountersInstantaneousErle[ch] = 0
	}
}

// Update updates the ERLE estimator. X2 is the render power spectrum
// (length FFTLengthBy2Plus1); Y2/E2 are the per-capture-channel
// microphone/residual power spectra. C: FullBandErleEstimator::Update.
func (e *FullBandErleEstimator) Update(x2 []float32, y2, e2 [][FFTLengthBy2Plus1]float32, convergedFilters []bool) {
	for ch := 0; ch < len(y2); ch++ {
		if convergedFilters[ch] {
			// Computes the fullband ERLE.
			var x2Sum float32
			for _, v := range x2 {
				x2Sum += v
			}
			if x2Sum > mul32(fullbandErleX2BandEnergyThresh, float32(len(x2))) {
				var y2Sum float32
				for _, v := range y2[ch] {
					y2Sum += v
				}
				var e2Sum float32
				for _, v := range e2[ch] {
					e2Sum += v
				}
				if e.instantaneousErle[ch].Update(y2Sum, e2Sum) {
					e.holdCountersInstantaneousErle[ch] = fullbandErleBlocksToHold
					instErleLog2, _ := e.instantaneousErle[ch].GetInstErleLog2()
					e.erleTimeDomainLog2[ch] = mla(e.erleTimeDomainLog2[ch], 0.05, sub32(instErleLog2, e.erleTimeDomainLog2[ch]))
					e.erleTimeDomainLog2[ch] = max32(e.erleTimeDomainLog2[ch], e.minErleLog2)
				}
			}
		}
		e.holdCountersInstantaneousErle[ch]--
		if e.holdCountersInstantaneousErle[ch] == 0 {
			e.instantaneousErle[ch].ResetAccumulators()
		}
	}

	e.updateQualityEstimates()
}

// FullbandErleLog2 returns the fullband ERLE estimates in log2 units.
// C: FullBandErleEstimator::FullbandErleLog2.
func (e *FullBandErleEstimator) FullbandErleLog2() float32 {
	minErle := e.erleTimeDomainLog2[0]
	for ch := 1; ch < len(e.erleTimeDomainLog2); ch++ {
		minErle = minFloat32(minErle, e.erleTimeDomainLog2[ch])
	}
	return minErle
}

// GetInstLinearQualityEstimates returns an estimation of the current
// linear filter quality per capture channel: a float between 0 and 1
// mapping 1 to the highest possible quality, nil mirroring
// std::optional's empty state. C:
// FullBandErleEstimator::GetInstLinearQualityEstimates.
func (e *FullBandErleEstimator) GetInstLinearQualityEstimates() []*float32 {
	return e.linearFiltersQualities
}

// updateQualityEstimates refreshes linearFiltersQualities from each
// channel's instantaneous ERLE estimator. C:
// FullBandErleEstimator::UpdateQualityEstimates.
func (e *FullBandErleEstimator) updateQualityEstimates() {
	for ch := range e.instantaneousErle {
		if v, ok := e.instantaneousErle[ch].GetQualityEstimate(); ok {
			e.linearFiltersQualitiesStorage[ch] = v
			e.linearFiltersQualities[ch] = &e.linearFiltersQualitiesStorage[ch]
		} else {
			e.linearFiltersQualities[ch] = nil
		}
	}
}
