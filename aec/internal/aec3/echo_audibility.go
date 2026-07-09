// This file ports modules/audio_processing/aec3/echo_audibility.{h,cc}:
// EchoAudibility, which tracks the stationarity of the render signal
// to determine how audible the residual echo is likely to be.
package aec3

// EchoAudibility tracks the render signal's stationarity to scale
// down residual echo suppression where the echo would be masked. C:
// echo_audibility.{h,cc}'s EchoAudibility.
type EchoAudibility struct {
	renderSpectrumWritePrevValid bool
	renderSpectrumWritePrev      int
	renderBlockWritePrev         int
	nonZeroRenderSeen            bool
	useRenderStationarityAtInit  bool
	renderStationarity           *StationarityEstimator
}

// NewEchoAudibility constructs an EchoAudibility. C:
// EchoAudibility::EchoAudibility.
func NewEchoAudibility(useRenderStationarityAtInit bool) *EchoAudibility {
	e := &EchoAudibility{
		useRenderStationarityAtInit: useRenderStationarityAtInit,
		renderStationarity:          NewStationarityEstimator(),
	}
	e.Reset()
	return e
}

// Reset resets the EchoAudibility class. C: EchoAudibility::Reset.
func (e *EchoAudibility) Reset() {
	e.renderStationarity.Reset()
	e.nonZeroRenderSeen = false
	e.renderSpectrumWritePrevValid = false
}

// Update feeds new render data to the echo audibility estimator. C:
// EchoAudibility::Update.
func (e *EchoAudibility) Update(renderBuffer *RenderBuffer, averageReverb []float32, minChannelDelayBlocks int, externalDelaySeen bool) {
	e.updateRenderNoiseEstimator(renderBuffer.GetSpectrumBuffer(), renderBuffer.GetBlockBuffer(), externalDelaySeen)

	if externalDelaySeen || e.useRenderStationarityAtInit {
		e.updateRenderStationarityFlags(renderBuffer, averageReverb, minChannelDelayBlocks)
	}
}

// GetResidualEchoScaling gets the residual echo scaling. C:
// EchoAudibility::GetResidualEchoScaling.
func (e *EchoAudibility) GetResidualEchoScaling(filterHasHadTimeToConverge bool, residualScaling []float32) {
	for band := range residualScaling {
		if e.renderStationarity.IsBandStationary(band) && (filterHasHadTimeToConverge || e.useRenderStationarityAtInit) {
			residualScaling[band] = 0.0
		} else {
			residualScaling[band] = 1.0
		}
	}
}

// IsBlockStationary reports whether the current render block is
// estimated as stationary. C: EchoAudibility::IsBlockStationary.
func (e *EchoAudibility) IsBlockStationary() bool {
	return e.renderStationarity.IsBlockStationary()
}

// updateRenderStationarityFlags updates the render stationarity flags
// for the current frame. C: EchoAudibility::UpdateRenderStationarityFlags.
func (e *EchoAudibility) updateRenderStationarityFlags(renderBuffer *RenderBuffer, averageReverb []float32, minChannelDelayBlocks int) {
	spectrumBuffer := renderBuffer.GetSpectrumBuffer()
	idxAtDelay := spectrumBuffer.OffsetIndex(spectrumBuffer.Read, minChannelDelayBlocks)

	numLookahead := renderBuffer.Headroom() - minChannelDelayBlocks + 1
	if numLookahead < 0 {
		numLookahead = 0
	}

	e.renderStationarity.UpdateStationarityFlags(spectrumBuffer, averageReverb, idxAtDelay, numLookahead)
}

// updateRenderNoiseEstimator updates the noise estimator with the new
// render data since the previous call to this method. C:
// EchoAudibility::UpdateRenderNoiseEstimator.
func (e *EchoAudibility) updateRenderNoiseEstimator(spectrumBuffer *SpectrumBuffer, blockBuffer *BlockBuffer, externalDelaySeen bool) {
	if !e.renderSpectrumWritePrevValid {
		e.renderSpectrumWritePrev = spectrumBuffer.Write
		e.renderSpectrumWritePrevValid = true
		e.renderBlockWritePrev = blockBuffer.Write
		return
	}
	renderSpectrumWriteCurrent := spectrumBuffer.Write
	if !e.nonZeroRenderSeen && !externalDelaySeen {
		e.nonZeroRenderSeen = !e.isRenderTooLow(blockBuffer)
	}
	if e.nonZeroRenderSeen {
		for idx := e.renderSpectrumWritePrev; idx != renderSpectrumWriteCurrent; idx = spectrumBuffer.DecIndex(idx) {
			e.renderStationarity.UpdateNoiseEstimator(spectrumBuffer.Buffer[idx])
		}
	}
	e.renderSpectrumWritePrev = renderSpectrumWriteCurrent
}

// isRenderTooLow reports whether the render signal contains just
// close to zero values. C: EchoAudibility::IsRenderTooLow.
func (e *EchoAudibility) isRenderTooLow(blockBuffer *BlockBuffer) bool {
	numRenderChannels := blockBuffer.Buffer[0].NumChannels()
	tooLow := false
	renderBlockWriteCurrent := blockBuffer.Write
	if renderBlockWriteCurrent == e.renderBlockWritePrev {
		tooLow = true
	} else {
		for idx := e.renderBlockWritePrev; idx != renderBlockWriteCurrent; idx = blockBuffer.IncIndex(idx) {
			var maxAbsOverChannels float32
			for ch := 0; ch < numRenderChannels; ch++ {
				block := blockBuffer.Buffer[idx].View(0, ch)
				var maxAbsChannel float32
				for _, v := range block {
					av := v
					if av < 0 {
						av = -av
					}
					if av > maxAbsChannel {
						maxAbsChannel = av
					}
				}
				if maxAbsChannel > maxAbsOverChannels {
					maxAbsOverChannels = maxAbsChannel
				}
			}
			if maxAbsOverChannels < 10.0 {
				tooLow = true // Discards all blocks if one of them is too low.
				break
			}
		}
	}
	e.renderBlockWritePrev = renderBlockWriteCurrent
	return tooLow
}
