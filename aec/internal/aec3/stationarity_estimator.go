// This file ports modules/audio_processing/aec3/
// stationarity_estimator.{h,cc}: StationarityEstimator, which
// estimates the stationarity of the render signal per frequency band.
package aec3

const (
	stationarityMinNoisePower       = 10.0
	stationarityHangoverBlocks      = NumBlocksPerSecond / 20 // 12
	stationarityNBlocksAvgInitPhase = 20
	stationarityNBlocksInitialPhase = float32(NumBlocksPerSecond) * 2.0 // 500
	stationarityWindowLength        = 13
)

// noiseSpectrum tracks a smoothed estimate of the noise power
// spectrum. C: StationarityEstimator::NoiseSpectrum.
type noiseSpectrum struct {
	noise        [FFTLengthBy2Plus1]float32
	blockCounter int
}

func (n *noiseSpectrum) Reset() {
	n.blockCounter = 0
	for k := range n.noise {
		n.noise[k] = stationarityMinNoisePower
	}
}

// Update updates the noise spectrum estimate from spectrum, one
// []float32 (length FFTLengthBy2Plus1) power spectrum per render
// channel. C: NoiseSpectrum::Update.
//
// NOTE (matches upstream exactly): for the multi-channel path, both
// the accumulation loop and the scaling loop start at k=1, so band 0
// of avgSpectrum ends up as plain spectrum[0][0] -- the other render
// channels' band-0 power is never folded in and no 1/numChannels
// scaling is applied to band 0. This looks like an oracle quirk, but
// is replicated bit-exact since correctness parity, not idiomatic
// cleanliness, is the goal of this port.
func (n *noiseSpectrum) Update(spectrum [][]float32) {
	var avgSpectrum [FFTLengthBy2Plus1]float32
	numRenderChannels := len(spectrum)
	if numRenderChannels == 1 {
		copy(avgSpectrum[:], spectrum[0])
	} else {
		copy(avgSpectrum[:], spectrum[0])
		for ch := 1; ch < numRenderChannels; ch++ {
			for k := 1; k < FFTLengthBy2Plus1; k++ {
				avgSpectrum[k] += spectrum[ch][k]
			}
		}
		oneByNumChannels := 1.0 / float32(numRenderChannels)
		for k := 1; k < FFTLengthBy2Plus1; k++ {
			avgSpectrum[k] = mul32(avgSpectrum[k], oneByNumChannels)
		}
	}

	n.blockCounter++
	alpha := n.getAlpha()
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		if n.blockCounter <= stationarityNBlocksAvgInitPhase {
			n.noise[k] = mla(n.noise[k], 1.0/stationarityNBlocksAvgInitPhase, avgSpectrum[k])
		} else {
			n.noise[k] = n.updateBandBySmoothing(avgSpectrum[k], n.noise[k], alpha)
		}
	}
}

func (n *noiseSpectrum) Spectrum() [FFTLengthBy2Plus1]float32 { return n.noise }

func (n *noiseSpectrum) Power(band int) float32 { return n.noise[band] }

func (n *noiseSpectrum) getAlpha() float32 {
	const kAlpha float32 = 0.004
	const kAlphaInit float32 = 0.04
	kTiltAlpha := (kAlphaInit - kAlpha) / stationarityNBlocksInitialPhase
	if float32(n.blockCounter) > stationarityNBlocksInitialPhase+stationarityNBlocksAvgInitPhase {
		return kAlpha
	}
	return sub32(kAlphaInit, mul32(kTiltAlpha, float32(n.blockCounter-stationarityNBlocksAvgInitPhase)))
}

func (n *noiseSpectrum) updateBandBySmoothing(powerBand, powerBandNoise, alpha float32) float32 {
	updated := powerBandNoise
	if powerBandNoise < powerBand {
		alphaInc := mul32(alpha, powerBandNoise/powerBand)
		if n.blockCounter > int(stationarityNBlocksInitialPhase) {
			if mul32(10.0, powerBandNoise) < powerBand {
				alphaInc = mul32(alphaInc, 0.1)
			}
		}
		updated = mla(updated, alphaInc, sub32(powerBand, powerBandNoise))
	} else {
		updated = mla(updated, alpha, sub32(powerBand, powerBandNoise))
		updated = max32(updated, stationarityMinNoisePower)
	}
	return updated
}

// StationarityEstimator estimates the stationarity of the render
// signal, per frequency band and for the block as a whole. C:
// stationarity_estimator.{h,cc}'s StationarityEstimator.
type StationarityEstimator struct {
	noise           noiseSpectrum
	hangovers       [FFTLengthBy2Plus1]int
	stationaryFlags [FFTLengthBy2Plus1]bool
}

// NewStationarityEstimator constructs a StationarityEstimator. C:
// StationarityEstimator::StationarityEstimator.
func NewStationarityEstimator() *StationarityEstimator {
	s := &StationarityEstimator{}
	s.Reset()
	return s
}

// Reset resets the estimator. C: StationarityEstimator::Reset.
func (s *StationarityEstimator) Reset() {
	s.noise.Reset()
	s.hangovers = [FFTLengthBy2Plus1]int{}
	s.stationaryFlags = [FFTLengthBy2Plus1]bool{}
}

// UpdateNoiseEstimator updates the noise estimator with spectrum, one
// []float32 (length FFTLengthBy2Plus1) power spectrum per render
// channel. C: StationarityEstimator::UpdateNoiseEstimator.
func (s *StationarityEstimator) UpdateNoiseEstimator(spectrum [][]float32) {
	s.noise.Update(spectrum)
}

// UpdateStationarityFlags updates the per-band and block
// stationarity flags. spectrumBuffer is the render spectrum circular
// buffer, renderReverbContributionSpectrum is the average render
// reverb power spectrum (length FFTLengthBy2Plus1), idxCurrent is the
// buffer index of the current (aligned) render spectrum, and
// numLookahead is the number of not-yet-consumed lookahead blocks
// available in the buffer. C:
// StationarityEstimator::UpdateStationarityFlags.
func (s *StationarityEstimator) UpdateStationarityFlags(spectrumBuffer *SpectrumBuffer, renderReverbContributionSpectrum []float32, idxCurrent, numLookahead int) {
	var indexes [stationarityWindowLength]int
	numLookaheadBounded := numLookahead
	if numLookaheadBounded > stationarityWindowLength-1 {
		numLookaheadBounded = stationarityWindowLength - 1
	}
	idx := idxCurrent
	if numLookaheadBounded < stationarityWindowLength-1 {
		numLookback := (stationarityWindowLength - 1) - numLookaheadBounded
		idx = spectrumBuffer.OffsetIndex(idxCurrent, numLookback)
	}
	// indexes[0] is the furthest-forward (least historical) spectrum in
	// the window; each subsequent entry steps one block further back.
	indexes[0] = idx
	for k := 1; k < stationarityWindowLength; k++ {
		indexes[k] = spectrumBuffer.DecIndex(indexes[k-1])
	}

	for band := 0; band < FFTLengthBy2Plus1; band++ {
		s.stationaryFlags[band] = s.estimateBandStationarity(spectrumBuffer, renderReverbContributionSpectrum, indexes, band)
	}
	s.updateHangover()
	s.smoothStationaryPerFreq()
}

// IsBandStationary reports whether band is deemed stationary,
// including hangover. C: StationarityEstimator::IsBandStationary.
func (s *StationarityEstimator) IsBandStationary(band int) bool {
	return s.stationaryFlags[band] && s.hangovers[band] == 0
}

// IsBlockStationary reports whether the block as a whole is deemed
// stationary. C: StationarityEstimator::IsBlockStationary.
func (s *StationarityEstimator) IsBlockStationary() bool {
	var acumStationarity float32
	for band := 0; band < FFTLengthBy2Plus1; band++ {
		if s.IsBandStationary(band) {
			acumStationarity++
		}
	}
	return mul32(acumStationarity, 1.0/FFTLengthBy2Plus1) > 0.75
}

func (s *StationarityEstimator) estimateBandStationarity(spectrumBuffer *SpectrumBuffer, averageReverb []float32, indexes [stationarityWindowLength]int, band int) bool {
	const kThrStationarity float32 = 10.0
	var acumPower float32
	numRenderChannels := len(spectrumBuffer.Buffer[0])
	oneByNumChannels := 1.0 / float32(numRenderChannels)
	for _, idx := range indexes {
		for ch := 0; ch < numRenderChannels; ch++ {
			acumPower = mla(acumPower, spectrumBuffer.Buffer[idx][ch][band], oneByNumChannels)
		}
	}
	acumPower += averageReverb[band]
	noise := mul32(stationarityWindowLength, s.noise.Power(band))
	return acumPower < mul32(kThrStationarity, noise)
}

func (s *StationarityEstimator) areAllBandsStationary() bool {
	for band := range s.stationaryFlags {
		if !s.stationaryFlags[band] {
			return false
		}
	}
	return true
}

func (s *StationarityEstimator) updateHangover() {
	reduceHangover := s.areAllBandsStationary()
	for k := range s.stationaryFlags {
		if !s.stationaryFlags[k] {
			s.hangovers[k] = stationarityHangoverBlocks
		} else if reduceHangover {
			if s.hangovers[k]-1 > 0 {
				s.hangovers[k] = s.hangovers[k] - 1
			} else {
				s.hangovers[k] = 0
			}
		}
	}
}

func (s *StationarityEstimator) smoothStationaryPerFreq() {
	var allAheadStationarySmooth [FFTLengthBy2Plus1]bool
	for k := 1; k < FFTLengthBy2; k++ {
		allAheadStationarySmooth[k] = s.stationaryFlags[k-1] && s.stationaryFlags[k] && s.stationaryFlags[k+1]
	}
	allAheadStationarySmooth[0] = allAheadStationarySmooth[1]
	allAheadStationarySmooth[FFTLengthBy2] = allAheadStationarySmooth[FFTLengthBy2-1]
	s.stationaryFlags = allAheadStationarySmooth
}
