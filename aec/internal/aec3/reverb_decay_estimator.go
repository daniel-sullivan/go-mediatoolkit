// This file ports modules/audio_processing/aec3/
// reverb_decay_estimator.{h,cc}: ReverbDecayEstimator, which estimates
// the exponential decay rate of the late reverberant echo tail from
// the adaptive linear filter's impulse response.
package aec3

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
)

const (
	// earlyReverbMinSizeBlocks == kEarlyReverbMinSizeBlocks.
	earlyReverbMinSizeBlocks = 3
	// blocksPerSection == kBlocksPerSection.
	blocksPerSection = 6
	// earlyReverbFirstPointAtLinearRegressors ==
	// kEarlyReverbFirstPointAtLinearRegressors. The linear regression
	// approach assumes symmetric index around 0.
	earlyReverbFirstPointAtLinearRegressors = -0.5*blocksPerSection*FFTLengthBy2 + 0.5
)

// blockAverage averages the values in a block of size FFTLengthBy2. C:
// (anonymous namespace) BlockAverage.
func blockAverage(v []float32, blockIndex int) float32 {
	const oneByFFTLengthBy2 = 1.0 / FFTLengthBy2
	i := blockIndex * FFTLengthBy2
	var sum float32
	for _, x := range v[i : i+FFTLengthBy2] {
		sum = add32(sum, x)
	}
	return mul32(sum, oneByFFTLengthBy2)
}

// analyzeBlockGain analyzes the gain in a block. C: (anonymous
// namespace) AnalyzeBlockGain.
func analyzeBlockGain(h2 [FFTLengthBy2]float32, floorGain float32, previousGain *float32, blockAdapting, decayingGain *bool) {
	gain := blockAverage(h2[:], 0)
	if gain < 1e-32 {
		gain = 1e-32
	}
	*blockAdapting = *previousGain > mul32(1.1, gain) || *previousGain < mul32(0.9, gain)
	*decayingGain = gain > floorGain
	*previousGain = gain
}

// symmetricArithmeticSum computes the arithmetic sum
// 2*sum_{i=0.5}^{(N-1)/2} i^2 directly. C: (anonymous namespace)
// SymmetricArithmetricSum.
func symmetricArithmeticSum(n float32) float32 {
	return mul32(mul32(n, sub32(mul32(n, n), 1.0)), 1.0/12.0)
}

// blockEnergyPeak returns the peak energy of an impulse response
// block. C: (anonymous namespace) BlockEnergyPeak.
func blockEnergyPeak(h []float32, peakBlock int) float32 {
	block := h[peakBlock*FFTLengthBy2 : (peakBlock+1)*FFTLengthBy2]
	peakValue := block[0]
	for _, v := range block[1:] {
		if mul32(v, v) > mul32(peakValue, peakValue) {
			peakValue = v
		}
	}
	return mul32(peakValue, peakValue)
}

// blockEnergyAverage returns the average energy of an impulse response
// block. C: (anonymous namespace) BlockEnergyAverage.
func blockEnergyAverage(h []float32, blockIndex int) float32 {
	const oneByFFTLengthBy2 = 1.0 / FFTLengthBy2
	block := h[blockIndex*FFTLengthBy2 : (blockIndex+1)*FFTLengthBy2]
	var sum float32
	for _, v := range block {
		sum = mla(sum, v, v)
	}
	return mul32(sum, oneByFFTLengthBy2)
}

// lateReverbLinearRegressor estimates the decay of the late reverb
// from the linear filter via a linear regression on log2 power. C:
// ReverbDecayEstimator::LateReverbLinearRegressor.
type lateReverbLinearRegressor struct {
	nz      float32
	nn      float32
	count   float32
	targetN int
	n       int
}

// Reset resets the estimator to receive numDataPoints data points. C:
// LateReverbLinearRegressor::Reset.
func (r *lateReverbLinearRegressor) Reset(numDataPoints int) {
	n := numDataPoints
	r.nz = 0
	r.nn = symmetricArithmeticSum(float32(n))
	if n > 0 {
		r.count = mla(0.5, -0.5, float32(n))
	} else {
		r.count = 0
	}
	r.targetN = n
	r.n = 0
}

// Accumulate accumulates estimation data. C:
// LateReverbLinearRegressor::Accumulate.
func (r *lateReverbLinearRegressor) Accumulate(z float32) {
	r.nz = mla(r.nz, r.count, z)
	r.count++
	r.n++
}

// Estimate estimates the decay. C: LateReverbLinearRegressor::Estimate.
func (r *lateReverbLinearRegressor) Estimate() float32 {
	if r.nn == 0 {
		return 0
	}
	return r.nz / r.nn
}

// EstimateAvailable reports whether an estimate is available. C:
// LateReverbLinearRegressor::EstimateAvailable.
func (r *lateReverbLinearRegressor) EstimateAvailable() bool {
	return r.n == r.targetN && r.targetN != 0
}

// earlyReverbLengthEstimator identifies the length of the early reverb
// from the linear filter, by dividing the impulse response into
// overlapping sections and computing the tilt of each section via a
// linear regressor. C:
// ReverbDecayEstimator::EarlyReverbLengthEstimator.
type earlyReverbLengthEstimator struct {
	numeratorsSmooth    []float32
	numerators          []float32
	coefficientsCounter int
	blockCounter        int
	nSections           int
}

// newEarlyReverbLengthEstimator constructs an
// earlyReverbLengthEstimator. C:
// EarlyReverbLengthEstimator::EarlyReverbLengthEstimator.
func newEarlyReverbLengthEstimator(maxBlocks int) *earlyReverbLengthEstimator {
	return &earlyReverbLengthEstimator{
		numeratorsSmooth: make([]float32, maxBlocks-blocksPerSection),
		numerators:       make([]float32, maxBlocks-blocksPerSection),
	}
}

// Reset resets the estimator. C: EarlyReverbLengthEstimator::Reset.
func (e *earlyReverbLengthEstimator) Reset() {
	e.coefficientsCounter = 0
	for i := range e.numerators {
		e.numerators[i] = 0
	}
	e.blockCounter = 0
}

// Accumulate accumulates estimation data. Each section is composed of
// blocksPerSection blocks and overlaps with the next one in
// (blocksPerSection-1) blocks: the first section covers blocks [0:5],
// the second [1:6], and so on -- so for each value, blocksPerSection
// sections need updating. C:
// EarlyReverbLengthEstimator::Accumulate.
func (e *earlyReverbLengthEstimator) Accumulate(value, smoothing float32) {
	firstSectionIndex := e.blockCounter - blocksPerSection + 1
	if firstSectionIndex < 0 {
		firstSectionIndex = 0
	}
	lastSectionIndex := e.blockCounter
	if lastSectionIndex > len(e.numerators)-1 {
		lastSectionIndex = len(e.numerators) - 1
	}
	xValue := add32(float32(e.coefficientsCounter), earlyReverbFirstPointAtLinearRegressors)
	valueToInc := mul32(FFTLengthBy2, value)
	valueToAdd := add32(mul32(xValue, value), mul32(float32(e.blockCounter-lastSectionIndex), valueToInc))
	for section := lastSectionIndex; section >= firstSectionIndex; section-- {
		e.numerators[section] = add32(e.numerators[section], valueToAdd)
		valueToAdd = add32(valueToAdd, valueToInc)
	}

	// Check if this update was the last coefficient of the current
	// block. If so, check if we are at the end of one of the sections
	// and update the numerator of the linear regressor computed in
	// that section.
	e.coefficientsCounter++
	if e.coefficientsCounter == FFTLengthBy2 {
		if e.blockCounter >= blocksPerSection-1 {
			section := e.blockCounter - (blocksPerSection - 1)
			e.numeratorsSmooth[section] = mla(e.numeratorsSmooth[section], smoothing, sub32(e.numerators[section], e.numeratorsSmooth[section]))
			e.nSections = section + 1
		}
		e.blockCounter++
		e.coefficientsCounter = 0
	}
}

// Estimate estimates the size in blocks of the early reverb, by
// comparing the tilt estimated in each section -- since all the
// linear regressors share the same denominator, the comparison of
// tilts is done via a comparison of the numerators. C:
// EarlyReverbLengthEstimator::Estimate.
func (e *earlyReverbLengthEstimator) Estimate() int {
	const nBlocks = blocksPerSection * FFTLengthBy2
	nn := symmetricArithmeticSum(float32(nBlocks))
	// numerator11 is the quantity the linear regressor needs in the
	// numerator to get a decay equal to 1.1 (which is not a decay):
	// log2(1.1) * nn / FFTLengthBy2.
	numerator11 := mul32(0.13750352374993502, nn) / FFTLengthBy2
	// log2(0.8) * nn / FFTLengthBy2.
	numerator08 := mul32(-0.32192809488736229, nn) / FFTLengthBy2
	const numSectionsToAnalyze = 9

	if e.nSections < numSectionsToAnalyze {
		return 0
	}

	minNumeratorTail := e.numeratorsSmooth[numSectionsToAnalyze]
	for _, v := range e.numeratorsSmooth[numSectionsToAnalyze:e.nSections] {
		if v < minNumeratorTail {
			minNumeratorTail = v
		}
	}
	earlyReverbSizeMinus1 := 0
	for k := 0; k < numSectionsToAnalyze; k++ {
		if e.numeratorsSmooth[k] > numerator11 ||
			(e.numeratorsSmooth[k] < numerator08 && e.numeratorsSmooth[k] < mul32(0.9, minNumeratorTail)) {
			earlyReverbSizeMinus1 = k
		}
	}

	if earlyReverbSizeMinus1 == 0 {
		return 0
	}
	return earlyReverbSizeMinus1 + 1
}

// ReverbDecayEstimator estimates the decay of the late reverb. C:
// reverb_decay_estimator.{h,cc}'s ReverbDecayEstimator.
type ReverbDecayEstimator struct {
	filterLengthBlocks       int
	filterLengthCoefficients int
	useAdaptiveEchoDecay     bool

	lateReverbDecayEstimator lateReverbLinearRegressor
	earlyReverbEstimator     *earlyReverbLengthEstimator

	lateReverbStart               int
	lateReverbEnd                 int
	blockToAnalyze                int
	estimationRegionCandidateSize int
	estimationRegionIdentified    bool
	previousGains                 []float32
	decay                         float32
	mildDecay                     float32
	tailGain                      float32
	smoothingConstant             float32
}

// NewReverbDecayEstimator constructs a ReverbDecayEstimator from
// config. C: ReverbDecayEstimator::ReverbDecayEstimator.
func NewReverbDecayEstimator(config config.Config) *ReverbDecayEstimator {
	filterLengthBlocks := config.Filter.Refined.LengthBlocks
	return &ReverbDecayEstimator{
		filterLengthBlocks:       filterLengthBlocks,
		filterLengthCoefficients: GetTimeDomainLength(filterLengthBlocks),
		useAdaptiveEchoDecay:     config.EpStrength.DefaultLen < 0,
		earlyReverbEstimator:     newEarlyReverbLengthEstimator(filterLengthBlocks - earlyReverbMinSizeBlocks),
		lateReverbStart:          earlyReverbMinSizeBlocks,
		lateReverbEnd:            earlyReverbMinSizeBlocks,
		previousGains:            make([]float32, filterLengthBlocks),
		decay:                    abs32(config.EpStrength.DefaultLen),
		mildDecay:                abs32(config.EpStrength.NearendLen),
	}
}

// Decay returns the decay for the exponential model. mild indicates
// which exponential decay to return, the default one or a milder one.
// C: ReverbDecayEstimator::Decay.
func (e *ReverbDecayEstimator) Decay(mild bool) float32 {
	if e.useAdaptiveEchoDecay {
		return e.decay
	}
	if mild {
		return e.mildDecay
	}
	return e.decay
}

// Update updates the decay estimate. filterQuality is nil to mirror
// std::optional's empty state. C: ReverbDecayEstimator::Update.
func (e *ReverbDecayEstimator) Update(filter []float32, filterQuality *float32, filterDelayBlocks int, usableLinearFilter, stationarySignal bool) {
	filterSize := len(filter)

	if stationarySignal {
		return
	}

	estimationFeasible := filterDelayBlocks <= e.filterLengthBlocks-earlyReverbMinSizeBlocks-1
	estimationFeasible = estimationFeasible && filterSize == e.filterLengthCoefficients
	estimationFeasible = estimationFeasible && filterDelayBlocks > 0
	estimationFeasible = estimationFeasible && usableLinearFilter

	if !estimationFeasible {
		e.resetDecayEstimation()
		return
	}

	if !e.useAdaptiveEchoDecay {
		return
	}

	var newSmoothing float32
	if filterQuality != nil {
		newSmoothing = mul32(*filterQuality, 0.2)
	}
	e.smoothingConstant = max32(newSmoothing, e.smoothingConstant)
	if e.smoothingConstant == 0 {
		return
	}

	if e.blockToAnalyze < e.filterLengthBlocks {
		// Analyze the filter and accumulate data for reverb estimation.
		e.analyzeFilter(filter)
		e.blockToAnalyze++
	} else {
		// When the filter is fully analyzed, estimate the reverb decay
		// and reset the blockToAnalyze counter.
		e.estimateDecay(filter, filterDelayBlocks)
	}
}

// resetDecayEstimation resets the decay estimation. C:
// ReverbDecayEstimator::ResetDecayEstimation.
func (e *ReverbDecayEstimator) resetDecayEstimation() {
	e.earlyReverbEstimator.Reset()
	e.lateReverbDecayEstimator.Reset(0)
	e.blockToAnalyze = 0
	e.estimationRegionCandidateSize = 0
	e.estimationRegionIdentified = false
	e.smoothingConstant = 0
	e.lateReverbStart = 0
	e.lateReverbEnd = 0
}

// estimateDecay estimates the reverb decay from filter, with peakBlock
// the filter's delay block. C: ReverbDecayEstimator::EstimateDecay.
func (e *ReverbDecayEstimator) estimateDecay(filter []float32, peakBlock int) {
	h := filter

	// Reset the block analysis counter.
	e.blockToAnalyze = peakBlock + earlyReverbMinSizeBlocks
	if e.blockToAnalyze > e.filterLengthBlocks {
		e.blockToAnalyze = e.filterLengthBlocks
	}

	// To estimate the reverb decay, the energy of the first filter
	// section must be substantially larger than the last. Also, the
	// first filter section energy must not deviate too much from the
	// max peak.
	firstReverbGain := blockEnergyAverage(h, e.blockToAnalyze)
	hSizeBlocks := len(h) >> FFTLengthBy2Log2
	e.tailGain = blockEnergyAverage(h, hSizeBlocks-1)
	peakEnergy := blockEnergyPeak(h, peakBlock)
	sufficientReverbDecay := firstReverbGain > mul32(4.0, e.tailGain)
	validFilter := firstReverbGain > mul32(2.0, e.tailGain) && peakEnergy < 100.0

	// Estimate the size of the regions with early and late reflections.
	sizeEarlyReverb := e.earlyReverbEstimator.Estimate()
	sizeLateReverb := e.estimationRegionCandidateSize - sizeEarlyReverb
	if sizeLateReverb < 0 {
		sizeLateReverb = 0
	}

	// Only update the reverb decay estimate if the size of the
	// identified late reverb is sufficiently large.
	if sizeLateReverb >= 5 {
		if validFilter && e.lateReverbDecayEstimator.EstimateAvailable() {
			decay := pow2(mul32(e.lateReverbDecayEstimator.Estimate(), FFTLengthBy2))
			const maxDecay = 0.95 // ~1 sec min RT60.
			const minDecay = 0.02 // ~15 ms max RT60.
			decay = max32(mul32(0.97, e.decay), decay)
			decay = minFloat32(decay, maxDecay)
			decay = max32(decay, minDecay)
			e.decay = mla(e.decay, e.smoothingConstant, sub32(decay, e.decay))
		}

		// Update length of decay. Must have enough data (number of
		// sections) in order to estimate decay rate.
		e.lateReverbDecayEstimator.Reset(sizeLateReverb * FFTLengthBy2)
		e.lateReverbStart = peakBlock + earlyReverbMinSizeBlocks + sizeEarlyReverb
		e.lateReverbEnd = e.blockToAnalyze + e.estimationRegionCandidateSize - 1
	} else {
		e.lateReverbDecayEstimator.Reset(0)
		e.lateReverbStart = 0
		e.lateReverbEnd = 0
	}

	// Reset variables for the identification of the region for reverb
	// decay estimation.
	e.estimationRegionIdentified = !(validFilter && sufficientReverbDecay)
	e.estimationRegionCandidateSize = 0

	// Stop estimation of the decay until another good filter is
	// received.
	e.smoothingConstant = 0

	// Reset early reflections detector.
	e.earlyReverbEstimator.Reset()
}

// analyzeFilter analyzes the filter block at blockToAnalyze,
// accumulating data for reverb decay estimation and the estimation of
// early reflections. C: ReverbDecayEstimator::AnalyzeFilter.
func (e *ReverbDecayEstimator) analyzeFilter(filter []float32) {
	h := filter[e.blockToAnalyze*FFTLengthBy2 : e.blockToAnalyze*FFTLengthBy2+FFTLengthBy2]

	// Compute squared filter coefficients for the block to analyze.
	var h2 [FFTLengthBy2]float32
	for k, v := range h {
		h2[k] = mul32(v, v)
	}

	// Map out the region for estimating the reverb decay.
	var adapting, aboveNoiseFloor bool
	analyzeBlockGain(h2, e.tailGain, &e.previousGains[e.blockToAnalyze], &adapting, &aboveNoiseFloor)

	// Count consecutive number of "good" filter sections, where "good"
	// means: 1) energy is above noise floor. 2) energy of current
	// section has not changed too much from the last check.
	e.estimationRegionIdentified = e.estimationRegionIdentified || adapting || !aboveNoiseFloor
	if !e.estimationRegionIdentified {
		e.estimationRegionCandidateSize++
	}

	if e.blockToAnalyze <= e.lateReverbEnd {
		if e.blockToAnalyze >= e.lateReverbStart {
			for _, h2K := range h2 {
				h2Log2 := FastApproxLog2f(add32(h2K, 1e-10))
				e.lateReverbDecayEstimator.Accumulate(h2Log2)
				e.earlyReverbEstimator.Accumulate(h2Log2, e.smoothingConstant)
			}
		} else {
			for _, h2K := range h2 {
				h2Log2 := FastApproxLog2f(add32(h2K, 1e-10))
				e.earlyReverbEstimator.Accumulate(h2Log2, e.smoothingConstant)
			}
		}
	}
}

// pow2 computes 2^x. C: std::pow(2.0f, x) as used by
// ReverbDecayEstimator::EstimateDecay.
func pow2(x float32) float32 {
	return float32(math.Pow(2.0, float64(x)))
}
