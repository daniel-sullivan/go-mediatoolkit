// This file ports modules/audio_processing/aec3/
// refined_filter_update_gain.{h,cc}: computes the per-bin NLMS-style
// adaptive gain used to update the refined (high-precision, slower
// adapting) filter in Subtractor.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

const (
	refinedHErrorInitial                = 10000.0
	refinedPoorExcitationCounterInitial = 1000
)

// RefinedFilterUpdateGain computes the adaptive gain for the refined
// filter. Port of refined_filter_update_gain.{h,cc}'s
// RefinedFilterUpdateGain (minus ApmDataDumper logging).
type RefinedFilterUpdateGain struct {
	configChangeDurationBlocks      int
	oneByConfigChangeDurationBlocks float32
	currentConfig                   config.RefinedFilterConfig
	targetConfig                    config.RefinedFilterConfig
	oldTargetConfig                 config.RefinedFilterConfig
	hError                          [FFTLengthBy2Plus1]float32
	poorExcitationCounter           int
	callCounter                     int
	configChangeCounter             int
}

// NewRefinedFilterUpdateGain mirrors RefinedFilterUpdateGain's C++
// constructor.
func NewRefinedFilterUpdateGain(config config.RefinedFilterConfig, configChangeDurationBlocks int) *RefinedFilterUpdateGain {
	g := &RefinedFilterUpdateGain{
		configChangeDurationBlocks: configChangeDurationBlocks,
		poorExcitationCounter:      refinedPoorExcitationCounterInitial,
	}
	g.SetConfig(config, true)
	for i := range g.hError {
		g.hError[i] = refinedHErrorInitial
	}
	g.oneByConfigChangeDurationBlocks = 1.0 / float32(configChangeDurationBlocks)
	return g
}

// SetConfig sets a new config. C: RefinedFilterUpdateGain::SetConfig.
func (g *RefinedFilterUpdateGain) SetConfig(config config.RefinedFilterConfig, immediateEffect bool) {
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
// change. C: RefinedFilterUpdateGain::HandleEchoPathChange.
func (g *RefinedFilterUpdateGain) HandleEchoPathChange(echoPathVariability EchoPathVariability) {
	if echoPathVariability.DelayChange != DelayAdjustmentNone {
		for i := range g.hError {
			g.hError[i] = refinedHErrorInitial
		}
	}

	if !echoPathVariability.GainChange {
		g.poorExcitationCounter = refinedPoorExcitationCounterInitial
		g.callCounter = 0
	}
}

// Compute computes the gain. erl and mu (internally) have length
// FFTLengthBy2Plus1. C: RefinedFilterUpdateGain::Compute.
func (g *RefinedFilterUpdateGain) Compute(renderPower *[FFTLengthBy2Plus1]float32, renderSignalAnalyzer *RenderSignalAnalyzer, subtractorOutput *SubtractorOutput, erl []float32, sizePartitions int, saturatedCaptureSignal bool, disallowLeakageDiverged bool, gainFFT *FFTData) {
	eRefined := &subtractorOutput.ERefined
	e2Refined := &subtractorOutput.E2Refined
	e2Coarse := &subtractorOutput.E2Coarse
	x2 := renderPower

	g.callCounter++

	g.updateCurrentConfig()

	if renderSignalAnalyzer.PoorSignalExcitation() {
		g.poorExcitationCounter = 0
	}

	g.poorExcitationCounter++
	if g.poorExcitationCounter < sizePartitions || saturatedCaptureSignal || g.callCounter <= sizePartitions {
		gainFFT.Re = [FFTLengthBy2Plus1]float32{}
		gainFFT.Im = [FFTLengthBy2Plus1]float32{}
	} else {
		var mu [FFTLengthBy2Plus1]float32
		for k := 0; k < FFTLengthBy2Plus1; k++ {
			if x2[k] >= g.currentConfig.NoiseGate {
				denom := muladd(mul32(0.5, g.hError[k]), x2[k], float32(sizePartitions), e2Refined[k])
				mu[k] = g.hError[k] / denom
			} else {
				mu[k] = 0
			}
		}

		renderSignalAnalyzer.MaskRegionsAroundNarrowBands(&mu)

		for k := 0; k < FFTLengthBy2Plus1; k++ {
			p := mul32(mul32(0.5, mu[k]), x2[k])
			p = mul32(p, g.hError[k])
			g.hError[k] = sub32(g.hError[k], p)
		}

		for k := 0; k < FFTLengthBy2Plus1; k++ {
			gainFFT.Re[k] = mu[k] * eRefined.Re[k]
			gainFFT.Im[k] = mu[k] * eRefined.Im[k]
		}
	}

	for k := 0; k < FFTLengthBy2Plus1; k++ {
		if e2Refined[k] <= e2Coarse[k] || disallowLeakageDiverged {
			g.hError[k] = mla(g.hError[k], g.currentConfig.LeakageConverged, erl[k])
		} else {
			g.hError[k] = mla(g.hError[k], g.currentConfig.LeakageDiverged, erl[k])
		}

		if g.hError[k] < g.currentConfig.ErrorFloor {
			g.hError[k] = g.currentConfig.ErrorFloor
		}
		if g.hError[k] > g.currentConfig.ErrorCeil {
			g.hError[k] = g.currentConfig.ErrorCeil
		}
	}
}

// updateCurrentConfig updates the current config towards the target
// config. C: RefinedFilterUpdateGain::UpdateCurrentConfig.
func (g *RefinedFilterUpdateGain) updateCurrentConfig() {
	if g.configChangeCounter > 0 {
		g.configChangeCounter--
		if g.configChangeCounter > 0 {
			// mul32 forces the product to round before the 1-changeFactor
			// subtractions below; a plain multiply is fusable into the
			// subtraction (FNMSUB on arm64) across statements, which
			// diverges from the -ffp-contract=off oracle.
			changeFactor := mul32(float32(g.configChangeCounter), g.oneByConfigChangeDurationBlocks)

			g.currentConfig.LeakageConverged = muladd(g.oldTargetConfig.LeakageConverged, changeFactor, g.targetConfig.LeakageConverged, 1-changeFactor)
			g.currentConfig.LeakageDiverged = muladd(g.oldTargetConfig.LeakageDiverged, changeFactor, g.targetConfig.LeakageDiverged, 1-changeFactor)
			g.currentConfig.ErrorFloor = muladd(g.oldTargetConfig.ErrorFloor, changeFactor, g.targetConfig.ErrorFloor, 1-changeFactor)
			g.currentConfig.ErrorCeil = muladd(g.oldTargetConfig.ErrorCeil, changeFactor, g.targetConfig.ErrorCeil, 1-changeFactor)
			g.currentConfig.NoiseGate = muladd(g.oldTargetConfig.NoiseGate, changeFactor, g.targetConfig.NoiseGate, 1-changeFactor)
		} else {
			g.currentConfig = g.targetConfig
			g.oldTargetConfig = g.targetConfig
		}
	}
}
