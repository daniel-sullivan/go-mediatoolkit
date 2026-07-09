// This file ports modules/audio_processing/aec3/
// coarse_filter_update_gain.{h,cc}: computes the fixed-rate adaptive
// gain used to update the coarse (fast-adapting) filter in
// Subtractor.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// CoarseFilterUpdateGain computes the fixed gain for the coarse
// filter. Port of coarse_filter_update_gain.{h,cc}'s
// CoarseFilterUpdateGain.
type CoarseFilterUpdateGain struct {
	currentConfig                   config.CoarseFilterConfig
	targetConfig                    config.CoarseFilterConfig
	oldTargetConfig                 config.CoarseFilterConfig
	configChangeDurationBlocks      int
	oneByConfigChangeDurationBlocks float32
	poorSignalExcitationCounter     int
	callCounter                     int
	configChangeCounter             int
}

// NewCoarseFilterUpdateGain mirrors CoarseFilterUpdateGain's C++
// constructor.
func NewCoarseFilterUpdateGain(config config.CoarseFilterConfig, configChangeDurationBlocks int) *CoarseFilterUpdateGain {
	g := &CoarseFilterUpdateGain{
		configChangeDurationBlocks: configChangeDurationBlocks,
	}
	g.SetConfig(config, true)
	g.oneByConfigChangeDurationBlocks = 1.0 / float32(configChangeDurationBlocks)
	return g
}

// SetConfig sets a new config. C: CoarseFilterUpdateGain::SetConfig.
func (g *CoarseFilterUpdateGain) SetConfig(config config.CoarseFilterConfig, immediateEffect bool) {
	if immediateEffect {
		g.oldTargetConfig = config
		g.currentConfig = config
		g.targetConfig = config
		g.configChangeCounter = 0
	} else {
		g.oldTargetConfig = g.currentConfig
		g.targetConfig = config
		g.configChangeCounter = g.configChangeDurationBlocks
	}
}

// HandleEchoPathChange takes action in the case of a known echo path
// change. C: CoarseFilterUpdateGain::HandleEchoPathChange.
func (g *CoarseFilterUpdateGain) HandleEchoPathChange() {
	g.poorSignalExcitationCounter = 0
	g.callCounter = 0
}

// Compute computes the gain. C: CoarseFilterUpdateGain::Compute.
func (g *CoarseFilterUpdateGain) Compute(renderPower *[FFTLengthBy2Plus1]float32, renderSignalAnalyzer *RenderSignalAnalyzer, eCoarse *FFTData, sizePartitions int, saturatedCaptureSignal bool, gainFFT *FFTData) {
	g.callCounter++

	g.updateCurrentConfig()

	if renderSignalAnalyzer.PoorSignalExcitation() {
		g.poorSignalExcitationCounter = 0
	}

	g.poorSignalExcitationCounter++
	if g.poorSignalExcitationCounter < sizePartitions || saturatedCaptureSignal || g.callCounter <= sizePartitions {
		gainFFT.Re = [FFTLengthBy2Plus1]float32{}
		gainFFT.Im = [FFTLengthBy2Plus1]float32{}
		return
	}

	var mu [FFTLengthBy2Plus1]float32
	x2 := renderPower
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		if x2[k] > g.currentConfig.NoiseGate {
			mu[k] = g.currentConfig.Rate / x2[k]
		} else {
			mu[k] = 0
		}
	}

	renderSignalAnalyzer.MaskRegionsAroundNarrowBands(&mu)

	for k := 0; k < FFTLengthBy2Plus1; k++ {
		gainFFT.Re[k] = mu[k] * eCoarse.Re[k]
		gainFFT.Im[k] = mu[k] * eCoarse.Im[k]
	}
}

// updateCurrentConfig updates the current config towards the target
// config. C: CoarseFilterUpdateGain::UpdateCurrentConfig.
func (g *CoarseFilterUpdateGain) updateCurrentConfig() {
	if g.configChangeCounter > 0 {
		g.configChangeCounter--
		if g.configChangeCounter > 0 {
			// mul32 forces the product to round before the 1-changeFactor
			// subtractions below; a plain multiply is fusable into the
			// subtraction (FNMSUB on arm64) across statements, which
			// diverges from the -ffp-contract=off oracle.
			changeFactor := mul32(float32(g.configChangeCounter), g.oneByConfigChangeDurationBlocks)

			g.currentConfig.Rate = muladd(g.oldTargetConfig.Rate, changeFactor, g.targetConfig.Rate, 1-changeFactor)
			g.currentConfig.NoiseGate = muladd(g.oldTargetConfig.NoiseGate, changeFactor, g.targetConfig.NoiseGate, 1-changeFactor)
		} else {
			g.currentConfig = g.targetConfig
			g.oldTargetConfig = g.targetConfig
		}
	}
}
