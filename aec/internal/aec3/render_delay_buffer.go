// This file ports modules/audio_processing/aec3/
// render_delay_buffer.{h,cc}: the render-side circular buffer that
// feeds the echo remover, decimated down to a low-rate buffer for the
// delay estimator, with delay bookkeeping (AlignFromDelay/
// AlignFromExternalDelay/ApplyTotalDelay) tying the two together.
//
// Only one C++ implementation (RenderDelayBufferImpl) exists behind
// the RenderDelayBuffer interface, so — matching this port's
// established convention elsewhere (Decimator, AlignmentMixer,
// MatchedFilter) — it is ported directly as a single concrete type
// rather than an interface plus impl. ApmDataDumper logging and the
// (upstream-vestigial, never-referenced) field_trial.h include are
// dropped, matching the rest of this package.
package aec3

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
)

// RenderDelayBufferEvent is RenderDelayBuffer::BufferingEvent.
type RenderDelayBufferEvent int

const (
	// RenderDelayBufferEventNone == BufferingEvent::kNone.
	RenderDelayBufferEventNone RenderDelayBufferEvent = iota
	// RenderDelayBufferEventRenderUnderrun == BufferingEvent::kRenderUnderrun.
	RenderDelayBufferEventRenderUnderrun
	// RenderDelayBufferEventRenderOverrun == BufferingEvent::kRenderOverrun.
	RenderDelayBufferEventRenderOverrun
	// RenderDelayBufferEventAPICallSkew == BufferingEvent::kApiCallSkew
	// (never produced upstream either — kept only for enum parity).
	RenderDelayBufferEventAPICallSkew
)

// RenderDelayBuffer holds the render-side data used by the echo
// remover and delay estimator/controller, aligned according to the
// currently applied delay. Port of RenderDelayBufferImpl.
type RenderDelayBuffer struct {
	config                    config.Config
	renderLinearAmplitudeGain float32
	downSamplingFactor        int
	subBlockSize              int

	blocks  *BlockBuffer
	spectra *SpectrumBuffer
	ffts    *FftBuffer

	delay *int

	echoRemoverBuffer *RenderBuffer
	lowRate           *DownsampledRenderBuffer
	renderMixer       *AlignmentMixer
	renderDecimator   *Decimator
	fft               *Aec3FFT

	renderDS       []float32
	bufferHeadroom int

	lastCallWasRender     bool
	numAPICallsInARow     int
	maxObservedJitter     int
	captureCallCounter    int64
	renderCallCounter     int64
	renderActivity        bool
	renderActivityCounter int

	externalAudioBufferDelay                   *int
	externalAudioBufferDelayVerifiedAfterReset bool
	minLatencyBlocks                           int
	excessRenderDetectionCounter               int
}

// NewRenderDelayBuffer mirrors RenderDelayBufferImpl's C++
// constructor (minus the ApmDataDumper/DCHECK-only bookkeeping).
func NewRenderDelayBuffer(config config.Config, sampleRateHz, numRenderChannels int) *RenderDelayBuffer {
	downSamplingFactor := config.Delay.DownSamplingFactor
	subBlockSize := BlockSize
	if downSamplingFactor > 0 {
		subBlockSize = BlockSize / downSamplingFactor
	}

	bufferSize := GetRenderDelayBufferSize(downSamplingFactor, config.Delay.NumFilters, config.Filter.Refined.LengthBlocks)
	blocks := NewBlockBuffer(bufferSize, NumBandsForRate(sampleRateHz), numRenderChannels)
	spectra := NewSpectrumBuffer(blocks.Size, numRenderChannels)
	ffts := NewFftBuffer(blocks.Size, numRenderChannels)

	delay := config.Delay.DefaultDelay

	b := &RenderDelayBuffer{
		config:                    config,
		renderLinearAmplitudeGain: float32(math.Pow(10.0, float64(config.RenderLevels.RenderPowerGainDB)/20.0)),
		downSamplingFactor:        downSamplingFactor,
		subBlockSize:              subBlockSize,
		blocks:                    blocks,
		spectra:                   spectra,
		ffts:                      ffts,
		delay:                     &delay,
		lowRate:                   NewDownsampledRenderBuffer(GetDownSampledBufferSize(downSamplingFactor, config.Delay.NumFilters)),
		renderMixer:               NewAlignmentMixerFromConfig(numRenderChannels, config.Delay.RenderAlignmentMixing),
		renderDecimator:           NewDecimator(downSamplingFactor),
		fft:                       NewAec3FFT(),
		renderDS:                  make([]float32, subBlockSize),
		bufferHeadroom:            config.Filter.Refined.LengthBlocks,
	}
	b.echoRemoverBuffer = NewRenderBuffer(b.blocks, b.spectra, b.ffts)

	b.Reset()
	return b
}

// Reset resets the buffer delays and clears the reported delays. C:
// RenderDelayBufferImpl::Reset.
func (b *RenderDelayBuffer) Reset() {
	b.lastCallWasRender = false
	b.numAPICallsInARow = 1
	b.minLatencyBlocks = 0
	b.excessRenderDetectionCounter = 0

	// Initialize the read index to one sub-block before the write index.
	b.lowRate.Read = b.lowRate.OffsetIndex(b.lowRate.Write, b.subBlockSize)

	// Check for any external audio buffer delay and whether it is feasible.
	if b.externalAudioBufferDelay != nil {
		const headroom = 2
		var audioBufferDelayToSet int
		// Minimum delay is 1 (like the low-rate render buffer).
		if *b.externalAudioBufferDelay <= headroom {
			audioBufferDelayToSet = 1
		} else {
			audioBufferDelayToSet = *b.externalAudioBufferDelay - headroom
		}

		if maxDelay := b.MaxDelay(); audioBufferDelayToSet > maxDelay {
			audioBufferDelayToSet = maxDelay
		}

		// When an external delay estimate is available, use that delay as
		// the initial render buffer delay.
		b.applyTotalDelay(audioBufferDelayToSet)
		d := b.computeDelay()
		b.delay = &d

		b.externalAudioBufferDelayVerifiedAfterReset = false
	} else {
		// If an external delay estimate is not available, use that delay as
		// the initial delay. Set the render buffer delays to the default
		// delay.
		b.applyTotalDelay(b.config.Delay.DefaultDelay)

		// Unset the delays which are set by AlignFromDelay.
		b.delay = nil
	}
}

// Insert inserts a new block into the render buffers. C:
// RenderDelayBufferImpl::Insert.
func (b *RenderDelayBuffer) Insert(block *Block) RenderDelayBufferEvent {
	b.renderCallCounter++
	if b.delay != nil {
		if !b.lastCallWasRender {
			b.lastCallWasRender = true
			b.numAPICallsInARow = 1
		} else {
			b.numAPICallsInARow++
			if b.numAPICallsInARow > b.maxObservedJitter {
				b.maxObservedJitter = b.numAPICallsInARow
			}
		}
	}

	// Increase the write indices to where the new blocks should be written.
	previousWrite := b.blocks.Write
	b.incrementWriteIndices()

	// Allow overrun and do a reset when render overrun occurs due to more
	// render data being inserted than capture data is received.
	event := RenderDelayBufferEventNone
	if b.renderOverrun() {
		event = RenderDelayBufferEventRenderOverrun
	}

	// Detect and update render activity.
	if !b.renderActivity {
		if b.detectActiveRender(block.View(0, 0)) {
			b.renderActivityCounter++
		}
		b.renderActivity = b.renderActivityCounter >= 20
	}

	// Insert the new render block into the specified position.
	b.insertBlock(block, previousWrite)

	if event != RenderDelayBufferEventNone {
		b.Reset()
	}

	return event
}

// HandleSkippedCaptureProcessing accounts for a capture call for
// which the corresponding render processing was skipped. C:
// RenderDelayBufferImpl::HandleSkippedCaptureProcessing.
func (b *RenderDelayBuffer) HandleSkippedCaptureProcessing() {
	b.captureCallCounter++
}

// PrepareCaptureProcessing prepares the render buffers for processing
// another capture block. C: RenderDelayBufferImpl::PrepareCaptureProcessing.
func (b *RenderDelayBuffer) PrepareCaptureProcessing() RenderDelayBufferEvent {
	event := RenderDelayBufferEventNone
	b.captureCallCounter++

	if b.delay != nil {
		if b.lastCallWasRender {
			b.lastCallWasRender = false
			b.numAPICallsInARow = 1
		} else {
			b.numAPICallsInARow++
			if b.numAPICallsInARow > b.maxObservedJitter {
				b.maxObservedJitter = b.numAPICallsInARow
			}
		}
	}

	if b.detectExcessRenderBlocks() {
		// Too many render blocks compared to capture blocks. Risk of delay
		// ending up before the filter used by the delay estimator.
		b.Reset()
		event = RenderDelayBufferEventRenderOverrun
	} else if b.renderUnderrun() {
		// Don't increment the read indices of the low rate buffer if there
		// is a render underrun.
		b.incrementReadIndices()
		// Incrementing the buffer index without increasing the low rate
		// buffer index means that the delay is reduced by one.
		if b.delay != nil && *b.delay > 0 {
			d := *b.delay - 1
			b.delay = &d
		}
		event = RenderDelayBufferEventRenderUnderrun
	} else {
		// Increment the read indices in the render buffers to point to the
		// most recent block to use in the capture processing.
		b.incrementLowRateReadIndices()
		b.incrementReadIndices()
	}

	b.echoRemoverBuffer.SetRenderActivity(b.renderActivity)
	if b.renderActivity {
		b.renderActivityCounter = 0
		b.renderActivity = false
	}

	return event
}

// AlignFromDelay sets the delay and returns whether the delay was
// changed. C: RenderDelayBufferImpl::AlignFromDelay.
func (b *RenderDelayBuffer) AlignFromDelay(delay int) bool {
	// Mismatch-after-reset logging (upstream RTC_LOG_V) dropped; it has
	// no numeric effect, only setting
	// externalAudioBufferDelayVerifiedAfterReset.
	if !b.externalAudioBufferDelayVerifiedAfterReset && b.externalAudioBufferDelay != nil && b.delay != nil {
		b.externalAudioBufferDelayVerifiedAfterReset = true
	}
	if b.delay != nil && *b.delay == delay {
		return false
	}
	b.delay = &delay

	// Compute the total delay and limit the delay to the allowed range.
	totalDelay := b.mapDelayToTotalDelay(*b.delay)
	if totalDelay < 0 {
		totalDelay = 0
	}
	if maxDelay := b.MaxDelay(); totalDelay > maxDelay {
		totalDelay = maxDelay
	}

	// Apply the delay to the buffers.
	b.applyTotalDelay(totalDelay)
	return true
}

// SetAudioBufferDelay converts delayMs (milliseconds) to blocks
// (rounded down) and records it as the externally reported audio
// buffer delay. C: RenderDelayBufferImpl::SetAudioBufferDelay.
func (b *RenderDelayBuffer) SetAudioBufferDelay(delayMs int) {
	d := delayMs / 4
	b.externalAudioBufferDelay = &d
}

// HasReceivedBufferDelay reports whether an external audio buffer
// delay has been received. C: RenderDelayBufferImpl::HasReceivedBufferDelay.
func (b *RenderDelayBuffer) HasReceivedBufferDelay() bool {
	return b.externalAudioBufferDelay != nil
}

// Delay always recomputes the current delay (not including call
// jitter) from the buffer indices, irrespective of whether AlignFromDelay
// has ever been called. C: RenderDelayBufferImpl::Delay (inline).
func (b *RenderDelayBuffer) Delay() int {
	return b.computeDelay()
}

// MaxDelay returns the maximum delay (in blocks) that can be applied.
// C: RenderDelayBufferImpl::MaxDelay (inline).
func (b *RenderDelayBuffer) MaxDelay() int {
	return b.blocks.Size - 1 - b.bufferHeadroom
}

// GetRenderBuffer returns the render buffer aliasing this buffer's
// blocks/spectra/ffts. C: RenderDelayBufferImpl::GetRenderBuffer (inline).
func (b *RenderDelayBuffer) GetRenderBuffer() *RenderBuffer {
	return b.echoRemoverBuffer
}

// GetDownsampledRenderBuffer returns the low-rate render buffer. C:
// RenderDelayBufferImpl::GetDownsampledRenderBuffer (inline).
func (b *RenderDelayBuffer) GetDownsampledRenderBuffer() *DownsampledRenderBuffer {
	return b.lowRate
}

// BufferLatency computes the latency in the buffer (the number of
// unread sub-blocks). C: RenderDelayBufferImpl::BufferLatency.
func (b *RenderDelayBuffer) BufferLatency() int {
	l := b.lowRate
	latencySamples := (l.Size + l.Read - l.Write) % l.Size
	return latencySamples / b.subBlockSize
}

// mapDelayToTotalDelay maps the externally computed delay to the
// delay used internally. C: RenderDelayBufferImpl::MapDelayToTotalDelay.
func (b *RenderDelayBuffer) mapDelayToTotalDelay(externalDelayBlocks int) int {
	return b.BufferLatency() + externalDelayBlocks
}

// computeDelay returns the delay (not including call jitter). C:
// RenderDelayBufferImpl::ComputeDelay.
func (b *RenderDelayBuffer) computeDelay() int {
	latencyBlocks := b.BufferLatency()
	var internalDelay int
	if b.spectra.Read >= b.spectra.Write {
		internalDelay = b.spectra.Read - b.spectra.Write
	} else {
		internalDelay = b.spectra.Size + b.spectra.Read - b.spectra.Write
	}
	return internalDelay - latencyBlocks
}

// applyTotalDelay sets the read indices according to the delay. C:
// RenderDelayBufferImpl::ApplyTotalDelay.
func (b *RenderDelayBuffer) applyTotalDelay(delay int) {
	b.blocks.Read = b.blocks.OffsetIndex(b.blocks.Write, -delay)
	b.spectra.Read = b.spectra.OffsetIndex(b.spectra.Write, delay)
	b.ffts.Read = b.ffts.OffsetIndex(b.ffts.Write, delay)
}

// AlignFromExternalDelay applies the delay derived from the render
// and capture call counters plus the externally reported audio buffer
// delay. C: RenderDelayBufferImpl::AlignFromExternalDelay.
func (b *RenderDelayBuffer) AlignFromExternalDelay() {
	if b.externalAudioBufferDelay != nil {
		delay := b.renderCallCounter - b.captureCallCounter + int64(*b.externalAudioBufferDelay)
		delayWithHeadroom := delay - int64(b.config.Delay.DelayHeadroomSamples/BlockSize)
		b.applyTotalDelay(int(delayWithHeadroom))
	}
}

// insertBlock inserts block into the render buffers at the position
// that was the write position before incrementWriteIndices was called
// (previousWrite). C: RenderDelayBufferImpl::InsertBlock.
func (b *RenderDelayBuffer) insertBlock(block *Block, previousWrite int) {
	write := b.blocks.Write
	current := b.blocks.Buffer[write]
	numBands := current.NumBands()
	numRenderChannels := current.NumChannels()

	for band := 0; band < numBands; band++ {
		for ch := 0; ch < numRenderChannels; ch++ {
			copy(current.View(band, ch), block.View(band, ch))
		}
	}

	if b.renderLinearAmplitudeGain != 1 {
		for band := 0; band < numBands; band++ {
			for ch := 0; ch < numRenderChannels; ch++ {
				view := current.View(band, ch)
				for i := range view {
					view[i] *= b.renderLinearAmplitudeGain
				}
			}
		}
	}

	var downmixedRender [BlockSize]float32
	b.renderMixer.ProduceOutput(current, downmixedRender[:])
	b.renderDecimator.Decimate(downmixedRender[:], b.renderDS)

	// std::copy(ds.rbegin(), ds.rend(), lr.buffer.begin() + lr.write): the
	// decimated sub-block is written into the low-rate circular buffer in
	// REVERSE sample order starting at the write index. This never wraps:
	// low_rate_'s size is always an exact multiple of subBlockSize (see
	// GetDownSampledBufferSize) and write only ever moves by
	// -subBlockSize (mod size), so write is always itself a multiple of
	// subBlockSize and write+subBlockSize<=size.
	ds := b.renderDS
	lr := b.lowRate
	n := len(ds)
	for i := 0; i < n; i++ {
		lr.Buffer[lr.Write+i] = ds[n-1-i]
	}

	previous := b.blocks.Buffer[previousWrite]
	for channel := 0; channel < current.NumChannels(); channel++ {
		b.fft.PaddedFftRectangular(current.View(0, channel), previous.View(0, channel), b.ffts.Buffer[b.ffts.Write][channel])
		b.ffts.Buffer[b.ffts.Write][channel].Spectrum(b.spectra.Buffer[b.spectra.Write][channel])
	}
}

// detectActiveRender reports whether x shows signs of activity, using
// the render level's active render limit. C:
// RenderDelayBufferImpl::DetectActiveRender.
func (b *RenderDelayBuffer) detectActiveRender(x []float32) bool {
	var xEnergy float32
	for _, v := range x {
		xEnergy = mla(xEnergy, v, v)
	}
	limit := b.config.RenderLevels.ActiveRenderLimit
	return xEnergy > mul32(limit, limit)*FFTLengthBy2
}

// detectExcessRenderBlocks reports (and resets the tracking state for)
// whether there have been more render than capture blocks recently.
// C: RenderDelayBufferImpl::DetectExcessRenderBlocks.
func (b *RenderDelayBuffer) detectExcessRenderBlocks() bool {
	excessRenderDetected := false
	latencyBlocks := b.BufferLatency()
	// The recently seen minimum latency in blocks. Should be close to 0.
	if latencyBlocks < b.minLatencyBlocks {
		b.minLatencyBlocks = latencyBlocks
	}
	// After processing a configurable number of blocks the minimum
	// latency is checked.
	b.excessRenderDetectionCounter++
	if b.excessRenderDetectionCounter >= b.config.Buffering.ExcessRenderDetectionIntervalBlocks {
		// If the minimum latency is not lower than the threshold there
		// have been more render than capture frames.
		excessRenderDetected = b.minLatencyBlocks > b.config.Buffering.MaxAllowedExcessRenderBlocks
		// Reset the counter and let the minimum latency be the current
		// latency.
		b.minLatencyBlocks = latencyBlocks
		b.excessRenderDetectionCounter = 0
	}

	return excessRenderDetected
}

// incrementWriteIndices increments the write indices for the render
// buffers. C: RenderDelayBufferImpl::IncrementWriteIndices.
func (b *RenderDelayBuffer) incrementWriteIndices() {
	b.lowRate.UpdateWriteIndex(-b.subBlockSize)
	b.blocks.IncWriteIndex()
	b.spectra.DecWriteIndex()
	b.ffts.DecWriteIndex()
}

// incrementLowRateReadIndices increments the read indices of the
// low-rate render buffer. C: RenderDelayBufferImpl::IncrementLowRateReadIndices.
func (b *RenderDelayBuffer) incrementLowRateReadIndices() {
	b.lowRate.UpdateReadIndex(-b.subBlockSize)
}

// incrementReadIndices increments the read indices for the render
// buffers. C: RenderDelayBufferImpl::IncrementReadIndices.
func (b *RenderDelayBuffer) incrementReadIndices() {
	if b.blocks.Read != b.blocks.Write {
		b.blocks.IncReadIndex()
		b.spectra.DecReadIndex()
		b.ffts.DecReadIndex()
	}
}

// renderOverrun checks for a render buffer overrun. C:
// RenderDelayBufferImpl::RenderOverrun.
func (b *RenderDelayBuffer) renderOverrun() bool {
	return b.lowRate.Read == b.lowRate.Write || b.blocks.Read == b.blocks.Write
}

// renderUnderrun checks for a render buffer underrun. C:
// RenderDelayBufferImpl::RenderUnderrun.
func (b *RenderDelayBuffer) renderUnderrun() bool {
	return b.lowRate.Read == b.lowRate.Write
}
