// This file ports modules/audio_processing/aec3/suppression_gain.{h,cc}:
// SuppressionGain, which computes the suppressor gain to apply per
// band.
//
// Dropped per this port's conventions:
//   - ApmDataDumper (data_dumper_) and the instance_count_ used only
//     to number its instances, plus the DumpRaw call at the end of
//     GetGain: pure debug instrumentation, no numerical effect (see
//     e.g. aec_state.go's top comment for the established
//     convention).
//   - The Aec3Optimization parameter and the aec3::VectorMath(...).Sqrt
//     dispatch in LowerBandGain: this whole package is scalar-only
//     (see common.go's top comment), so VectorMath's default branch
//     (elementwise sqrtf) is inlined directly.
//   - The sample_rate_hz constructor parameter: stored/used nowhere in
//     the C++ class (dead constructor parameter upstream).
package aec3

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
)

// suppressionGainLowBandGainLimit == kLowBandGainLimit
// (kFftLengthBy2 / 2), used by UpperBandsGain.
const suppressionGainLowBandGainLimit = FFTLengthBy2 / 2

// limitLowFrequencyGains limits the low frequency gains to avoid the
// impact of the high-pass filter on the lower-frequency gain
// influencing the overall achieved gain. C: (anonymous namespace)'s
// LimitLowFrequencyGains.
func limitLowFrequencyGains(gain *[FFTLengthBy2Plus1]float32) {
	if gain[2] < gain[1] {
		gain[1] = gain[2]
	}
	gain[0] = gain[1]
}

// limitHighFrequencyGains limits the high frequency gains to avoid
// echo leakage due to an imperfect filter. C: (anonymous namespace)'s
// LimitHighFrequencyGains.
func limitHighFrequencyGains(conservativeHfSuppression bool, gain *[FFTLengthBy2Plus1]float32) {
	const kFirstBandToLimit = (64 * 2000) / 8000
	minUpperGain := gain[kFirstBandToLimit]
	for k := kFirstBandToLimit + 1; k < FFTLengthBy2Plus1; k++ {
		if gain[k] > minUpperGain {
			gain[k] = minUpperGain
		}
	}
	gain[FFTLengthBy2] = gain[FFTLengthBy2Minus1]

	if conservativeHfSuppression {
		// Limits the gain in the frequencies for which the adaptive filter
		// has not converged.
		const kUpperAccurateBandPlus1 = 29
		const oneByBandsInSum = 1.0 / float32(kUpperAccurateBandPlus1-20)

		var sum float32
		for k := 20; k < kUpperAccurateBandPlus1; k++ {
			sum = add32(sum, gain[k])
		}
		hfGainBound := mul32(sum, oneByBandsInSum)

		for k := kUpperAccurateBandPlus1; k < FFTLengthBy2Plus1; k++ {
			if gain[k] > hfGainBound {
				gain[k] = hfGainBound
			}
		}
	}
}

// weightEchoForAudibility scales the echo according to assessed
// audibility at the other end. C: (anonymous namespace)'s
// WeightEchoForAudibility.
func weightEchoForAudibility(config config.Config, echo []float32, weightedEcho []float32) {
	weigh := func(threshold, normalizer float32, begin, end int) {
		for k := begin; k < end; k++ {
			if echo[k] < threshold {
				tmp := mul32(sub32(threshold, echo[k]), normalizer)
				v := sub32(1, mul32(tmp, tmp))
				if v < 0 {
					v = 0
				}
				weightedEcho[k] = mul32(echo[k], v)
			} else {
				weightedEcho[k] = echo[k]
			}
		}
	}

	threshold := mul32(config.EchoAudibility.FloorPower, config.EchoAudibility.AudibilityThresholdLF)
	normalizer := 1.0 / sub32(threshold, config.EchoAudibility.FloorPower)
	weigh(threshold, normalizer, 0, 3)

	threshold = mul32(config.EchoAudibility.FloorPower, config.EchoAudibility.AudibilityThresholdMF)
	normalizer = 1.0 / sub32(threshold, config.EchoAudibility.FloorPower)
	weigh(threshold, normalizer, 3, 7)

	threshold = mul32(config.EchoAudibility.FloorPower, config.EchoAudibility.AudibilityThresholdHF)
	normalizer = 1.0 / sub32(threshold, config.EchoAudibility.FloorPower)
	weigh(threshold, normalizer, 7, FFTLengthBy2Plus1)
}

// lowNoiseRenderDetector detects when the render signal can be
// considered to have low power and consist of stationary noise. C:
// SuppressionGain::LowNoiseRenderDetector.
type lowNoiseRenderDetector struct {
	averagePower float32
}

func newLowNoiseRenderDetector() lowNoiseRenderDetector {
	return lowNoiseRenderDetector{averagePower: 32768 * 32768}
}

// detect mirrors SuppressionGain::LowNoiseRenderDetector::Detect.
func (l *lowNoiseRenderDetector) detect(render *Block) bool {
	var x2Sum float32
	var x2Max float32
	for ch := 0; ch < render.NumChannels(); ch++ {
		for _, xk := range render.View(0, ch) {
			x2 := mul32(xk, xk)
			x2Sum = add32(x2Sum, x2)
			if x2 > x2Max {
				x2Max = x2
			}
		}
	}
	x2Sum /= float32(render.NumChannels())

	const kThreshold = 50.0 * 50.0 * 64.0
	lowNoiseRender := l.averagePower < kThreshold && x2Max < mul32(3, l.averagePower)
	l.averagePower = muladd(l.averagePower, 0.9, x2Sum, 0.1)
	return lowNoiseRender
}

// gainParameters holds the per-band masking thresholds and
// increase/decrease factors for one Suppressor::Tuning. C:
// SuppressionGain::GainParameters.
type gainParameters struct {
	maxIncFactor   float32
	maxDecFactorLF float32
	enrTransparent [FFTLengthBy2Plus1]float32
	enrSuppress    [FFTLengthBy2Plus1]float32
	emrTransparent [FFTLengthBy2Plus1]float32
}

// newGainParameters mirrors SuppressionGain::GainParameters's C++
// constructor.
func newGainParameters(lastLFBand, firstHFBand int, tuning config.SuppressorTuning) gainParameters {
	g := gainParameters{
		maxIncFactor:   tuning.MaxIncFactor,
		maxDecFactorLF: tuning.MaxDecFactorLF,
	}
	lf := tuning.MaskLF
	hf := tuning.MaskHF
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		var a float32
		switch {
		case k <= lastLFBand:
			a = 0
		case k < firstHFBand:
			a = float32(k-lastLFBand) / float32(firstHFBand-lastLFBand)
		default:
			a = 1
		}
		g.enrTransparent[k] = muladd(1-a, lf.EnrTransparent, a, hf.EnrTransparent)
		g.enrSuppress[k] = muladd(1-a, lf.EnrSuppress, a, hf.EnrSuppress)
		g.emrTransparent[k] = muladd(1-a, lf.EmrTransparent, a, hf.EmrTransparent)
	}
	return g
}

// SuppressionGain computes the suppressor gain to apply, both for the
// lower (first) band and, as a single scalar, for the higher bands.
// C: suppression_gain.{h,cc}'s SuppressionGain.
type SuppressionGain struct {
	config                    config.Config
	numCaptureChannels        int
	stateChangeDurationBlocks int
	lastGain                  [FFTLengthBy2Plus1]float32
	lastNearend               [][FFTLengthBy2Plus1]float32
	lastEcho                  [][FFTLengthBy2Plus1]float32
	lowRenderDetector         lowNoiseRenderDetector
	initialState              bool
	initialStateChangeCounter int
	nearendSmoothers          []*MovingAverage
	nearendParams             gainParameters
	normalParams              gainParameters
	useUnboundedEchoSpectrum  bool
	dominantNearendDetector   NearendDetector
}

// NewSuppressionGain mirrors SuppressionGain's C++ constructor.
func NewSuppressionGain(config config.Config, numCaptureChannels int) *SuppressionGain {
	s := &SuppressionGain{
		config:                    config,
		numCaptureChannels:        numCaptureChannels,
		stateChangeDurationBlocks: config.Filter.ConfigChangeDurationBlocks,
		lastNearend:               make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		lastEcho:                  make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		lowRenderDetector:         newLowNoiseRenderDetector(),
		initialState:              true,
		nearendSmoothers:          make([]*MovingAverage, numCaptureChannels),
		nearendParams:             newGainParameters(config.Suppressor.LastLFBand, config.Suppressor.FirstHFBand, config.Suppressor.NearendTuning),
		normalParams:              newGainParameters(config.Suppressor.LastLFBand, config.Suppressor.FirstHFBand, config.Suppressor.NormalTuning),
		useUnboundedEchoSpectrum:  config.Suppressor.DominantNearendDetection.UseUnboundedEchoSpectrum,
	}
	for ch := range s.nearendSmoothers {
		s.nearendSmoothers[ch] = NewMovingAverage(FFTLengthBy2Plus1, config.Suppressor.NearendAverageBlocks)
	}
	for k := range s.lastGain {
		s.lastGain[k] = 1
	}
	if config.Suppressor.UseSubbandNearendDetection {
		s.dominantNearendDetector = NewSubbandNearendDetector(config.Suppressor.SubbandNearendDetection, numCaptureChannels)
	} else {
		s.dominantNearendDetector = NewDominantNearendDetector(config.Suppressor.DominantNearendDetection, numCaptureChannels)
	}
	return s
}

// IsDominantNearend reports whether the suppressor is in the nearend
// state. C: SuppressionGain::IsDominantNearend.
func (s *SuppressionGain) IsDominantNearend() bool {
	return s.dominantNearendDetector.IsNearendState()
}

// SetInitialState toggles the usage of the initial state. C:
// SuppressionGain::SetInitialState.
func (s *SuppressionGain) SetInitialState(state bool) {
	s.initialState = state
	if state {
		s.initialStateChangeCounter = s.stateChangeDurationBlocks
	} else {
		s.initialStateChangeCounter = 0
	}
}

// lowFrequencyEnergy16 sums spectrum[1:16], matching the local
// low_frequency_energy lambda used by both DominantNearendDetector
// and UpperBandsGain. C: (anonymous)'s low_frequency_energy lambda.
func lowFrequencyEnergy16(spectrum [FFTLengthBy2Plus1]float32) float32 {
	return lowFrequencyEnergy(spectrum)
}

// upperBandsGain computes the gain to apply for the bands beyond the
// first band. C: SuppressionGain::UpperBandsGain.
func (s *SuppressionGain) upperBandsGain(echoSpectrum [][FFTLengthBy2Plus1]float32, comfortNoiseSpectrum [][FFTLengthBy2Plus1]float32, narrowPeakBand *int, saturatedEcho bool, render *Block, lowBandGain [FFTLengthBy2Plus1]float32) float32 {
	if render.NumBands() == 1 {
		return 1
	}
	numRenderChannels := render.NumChannels()

	if narrowPeakBand != nil && *narrowPeakBand > FFTLengthBy2Plus1-10 {
		return 0.001
	}

	gainBelow8kHz := lowBandGain[suppressionGainLowBandGainLimit]
	for k := suppressionGainLowBandGainLimit + 1; k < FFTLengthBy2Plus1; k++ {
		if lowBandGain[k] < gainBelow8kHz {
			gainBelow8kHz = lowBandGain[k]
		}
	}

	// Always attenuate the upper bands when there is saturated echo.
	if saturatedEcho {
		if gainBelow8kHz < 0.001 {
			return gainBelow8kHz
		}
		return 0.001
	}

	// Compute the upper and lower band energies.
	var lowBandEnergy float32
	for ch := 0; ch < numRenderChannels; ch++ {
		var channelEnergy float32
		for _, x := range render.View(0, ch) {
			channelEnergy = mla(channelEnergy, x, x)
		}
		if channelEnergy > lowBandEnergy {
			lowBandEnergy = channelEnergy
		}
	}
	var highBandEnergy float32
	for k := 1; k < render.NumBands(); k++ {
		for ch := 0; ch < numRenderChannels; ch++ {
			var energy float32
			for _, x := range render.View(k, ch) {
				energy = mla(energy, x, x)
			}
			if energy > highBandEnergy {
				highBandEnergy = energy
			}
		}
	}

	// If there is more power in the lower frequencies than the upper
	// frequencies, or if the power in upper frequencies is low, do not
	// bound the gain in the upper bands.
	var antiHowlingGain float32
	activationThreshold := mul32(BlockSize, s.config.Suppressor.HighBandsSuppression.AntiHowlingActivationThreshold)
	maxLowActivation := lowBandEnergy
	if activationThreshold > maxLowActivation {
		maxLowActivation = activationThreshold
	}
	if highBandEnergy < maxLowActivation {
		antiHowlingGain = 1
	} else {
		// In all other cases, bound the gain for upper frequencies.
		antiHowlingGain = mul32(s.config.Suppressor.HighBandsSuppression.AntiHowlingGain, float32(math.Sqrt(float64(lowBandEnergy/highBandEnergy))))
	}

	gainBound := float32(1)
	if !s.dominantNearendDetector.IsNearendState() {
		// Bound the upper gain during significant echo activity.
		cfg := s.config.Suppressor.HighBandsSuppression
		for ch := 0; ch < s.numCaptureChannels; ch++ {
			echoSum := lowFrequencyEnergy16(echoSpectrum[ch])
			noiseSum := lowFrequencyEnergy16(comfortNoiseSpectrum[ch])
			if echoSum > mul32(cfg.EnrThreshold, noiseSum) {
				gainBound = cfg.MaxGainDuringEcho
				break
			}
		}
	}

	// Choose the gain as the minimum of the lower and upper gains.
	result := gainBelow8kHz
	if antiHowlingGain < result {
		result = antiHowlingGain
	}
	if gainBound < result {
		result = gainBound
	}
	return result
}

// gainToNoAudibleEcho computes the gain to reduce the echo to a non
// audible level. C: SuppressionGain::GainToNoAudibleEcho.
func (s *SuppressionGain) gainToNoAudibleEcho(nearend, echo, masker [FFTLengthBy2Plus1]float32, gain *[FFTLengthBy2Plus1]float32) {
	p := &s.normalParams
	if s.dominantNearendDetector.IsNearendState() {
		p = &s.nearendParams
	}
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		enr := echo[k] / (nearend[k] + 1) // Echo-to-nearend ratio.
		emr := echo[k] / (masker[k] + 1)  // Echo-to-masker (noise) ratio.
		g := float32(1)
		if enr > p.enrTransparent[k] && emr > p.emrTransparent[k] {
			g = (p.enrSuppress[k] - enr) / (p.enrSuppress[k] - p.enrTransparent[k])
			bound := p.emrTransparent[k] / emr
			if bound > g {
				g = bound
			}
		}
		gain[k] = g
	}
}

// getMinGain computes the minimum gain as the attenuating gain to put
// the signal just above the zero sample values. C:
// SuppressionGain::GetMinGain.
func (s *SuppressionGain) getMinGain(weightedResidualEcho, lastNearend, lastEcho [FFTLengthBy2Plus1]float32, lowNoiseRender, saturatedEcho bool, minGain *[FFTLengthBy2Plus1]float32) {
	if !saturatedEcho {
		minEchoPower := s.config.EchoAudibility.NormalRenderLimit
		if lowNoiseRender {
			minEchoPower = s.config.EchoAudibility.LowRenderLimit
		}

		for k := 0; k < FFTLengthBy2Plus1; k++ {
			if weightedResidualEcho[k] > 0 {
				minGain[k] = minEchoPower / weightedResidualEcho[k]
			} else {
				minGain[k] = 1
			}
			if minGain[k] > 1 {
				minGain[k] = 1
			}
		}

		if !s.initialState || s.config.Suppressor.LFSmoothingDuringInitialPhase {
			dec := s.normalParams.maxDecFactorLF
			if s.dominantNearendDetector.IsNearendState() {
				dec = s.nearendParams.maxDecFactorLF
			}

			for k := 0; k <= s.config.Suppressor.LastLFSmoothingBand; k++ {
				// Make sure the gains of the low frequencies do not decrease
				// too quickly after strong nearend.
				if lastNearend[k] > lastEcho[k] || k <= s.config.Suppressor.LastPermanentLFSmoothingBand {
					bound := mul32(s.lastGain[k], dec)
					if bound > minGain[k] {
						minGain[k] = bound
					}
					if minGain[k] > 1 {
						minGain[k] = 1
					}
				}
			}
		}
	} else {
		for k := range minGain {
			minGain[k] = 0
		}
	}
}

// getMaxGain computes the maximum gain by limiting the gain increase
// from the previous gain. C: SuppressionGain::GetMaxGain.
func (s *SuppressionGain) getMaxGain(maxGain *[FFTLengthBy2Plus1]float32) {
	inc := s.normalParams.maxIncFactor
	if s.dominantNearendDetector.IsNearendState() {
		inc = s.nearendParams.maxIncFactor
	}
	floor := s.config.Suppressor.FloorFirstIncrease
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		v := mul32(s.lastGain[k], inc)
		if v < floor {
			v = floor
		}
		if v > 1 {
			v = 1
		}
		maxGain[k] = v
	}
}

// lowerBandGain computes the gain for the lower (first) band. C:
// SuppressionGain::LowerBandGain.
func (s *SuppressionGain) lowerBandGain(lowNoiseRender bool, aecState *AecState, suppressorInput, residualEcho, comfortNoise [][FFTLengthBy2Plus1]float32, clockDrift bool, gain *[FFTLengthBy2Plus1]float32) {
	for k := range gain {
		gain[k] = 1
	}
	saturatedEcho := aecState.SaturatedEcho()
	var maxGain [FFTLengthBy2Plus1]float32
	s.getMaxGain(&maxGain)

	for ch := 0; ch < s.numCaptureChannels; ch++ {
		var g [FFTLengthBy2Plus1]float32
		var nearend [FFTLengthBy2Plus1]float32
		in := suppressorInput[ch]
		s.nearendSmoothers[ch].Average(in[:], nearend[:])

		// Weight echo power in terms of audibility.
		var weightedResidualEcho [FFTLengthBy2Plus1]float32
		re := residualEcho[ch]
		weightEchoForAudibility(s.config, re[:], weightedResidualEcho[:])

		var minGain [FFTLengthBy2Plus1]float32
		s.getMinGain(weightedResidualEcho, s.lastNearend[ch], s.lastEcho[ch], lowNoiseRender, saturatedEcho, &minGain)

		s.gainToNoAudibleEcho(nearend, weightedResidualEcho, comfortNoise[0], &g)

		// Clamp gains.
		for k := range gain {
			if g[k] > maxGain[k] {
				g[k] = maxGain[k]
			}
			if g[k] < minGain[k] {
				g[k] = minGain[k]
			}
			if g[k] < gain[k] {
				gain[k] = g[k]
			}
		}

		// Store data required for the gain computation of the next block.
		s.lastNearend[ch] = nearend
		s.lastEcho[ch] = weightedResidualEcho
	}

	limitLowFrequencyGains(gain)
	// Use conservative high-frequency gains during clock-drift or when
	// not in dominant nearend.
	if !s.dominantNearendDetector.IsNearendState() || clockDrift || s.config.Suppressor.ConservativeHFSuppression {
		limitHighFrequencyGains(s.config.Suppressor.ConservativeHFSuppression, gain)
	}

	// Store computed gains.
	s.lastGain = *gain

	// Transform gains to amplitude domain.
	for k := range gain {
		gain[k] = float32(math.Sqrt(float64(gain[k])))
	}
}

// GetGain computes the suppressor gains to apply to the lower band
// (per-bin) and the upper bands (a single scalar). C:
// SuppressionGain::GetGain.
func (s *SuppressionGain) GetGain(nearendSpectrum, echoSpectrum, residualEchoSpectrum, residualEchoSpectrumUnbounded, comfortNoiseSpectrum [][FFTLengthBy2Plus1]float32, renderSignalAnalyzer *RenderSignalAnalyzer, aecState *AecState, render *Block, clockDrift bool, highBandsGain *float32, lowBandGain *[FFTLengthBy2Plus1]float32) {
	// Choose residual echo spectrum for dominant nearend detection.
	echo := residualEchoSpectrum
	if s.useUnboundedEchoSpectrum {
		echo = residualEchoSpectrumUnbounded
	}

	// Update the nearend state selection.
	s.dominantNearendDetector.Update(nearendSpectrum, echo, comfortNoiseSpectrum, s.initialState)

	// Compute gain for the lower band.
	lowNoiseRender := s.lowRenderDetector.detect(render)
	s.lowerBandGain(lowNoiseRender, aecState, nearendSpectrum, residualEchoSpectrum, comfortNoiseSpectrum, clockDrift, lowBandGain)

	// Compute the gain for the upper bands.
	narrowPeakBand := renderSignalAnalyzer.NarrowPeakBand()

	*highBandsGain = s.upperBandsGain(echoSpectrum, comfortNoiseSpectrum, narrowPeakBand, aecState.SaturatedEcho(), render, *lowBandGain)
}
