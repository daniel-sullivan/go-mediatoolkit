// This file ports modules/audio_processing/aec3/
// block_delay_buffer.{h,cc}: applies a fixed sample delay to a
// multiband, multichannel signal using a per-channel/per-band
// circular carry buffer. This is a standalone utility not otherwise
// wired into the render/delay pipeline in this repo (the C++ oracle
// only uses it for a separate fixed-delay APM feature) — ported here
// for API completeness per the port's scope.
//
// The C++ signature operates on an AudioBuffer* (APM's own multiband
// audio container, not otherwise part of this repo's AEC3 port); this
// port instead takes the equivalent [channel][band][]float32 slice
// layout directly, matching frame->split_bands(ch) access.
package aec3

// BlockDelayBuffer applies a fixed delay to the samples in a signal
// partitioned using band-splitting. Port of BlockDelayBuffer.
type BlockDelayBuffer struct {
	frameLength int
	delay       int
	buf         [][][]float32 // [channel][band][delay]
	lastInsert  int
}

// NewBlockDelayBuffer mirrors BlockDelayBuffer's C++ constructor.
func NewBlockDelayBuffer(numChannels, numBands, frameLength, delaySamples int) *BlockDelayBuffer {
	b := &BlockDelayBuffer{
		frameLength: frameLength,
		delay:       delaySamples,
		buf:         make([][][]float32, numChannels),
	}
	for ch := range b.buf {
		b.buf[ch] = make([][]float32, numBands)
		for band := range b.buf[ch] {
			b.buf[ch][band] = make([]float32, delaySamples)
		}
	}
	return b
}

// DelaySignal delays the samples in frame ([channel][band][frameLength])
// by the configured delay, in place. C: BlockDelayBuffer::DelaySignal.
func (b *BlockDelayBuffer) DelaySignal(frame [][][]float32) {
	if b.delay == 0 {
		return
	}

	numBands := len(b.buf[0])
	numChannels := len(b.buf)

	iStart := b.lastInsert
	i := 0
	for ch := 0; ch < numChannels; ch++ {
		for band := 0; band < numBands; band++ {
			i = iStart
			bufChBand := b.buf[ch][band]
			frameChBand := frame[ch][band]

			for k := 0; k < b.frameLength; k++ {
				tmp := bufChBand[i]
				bufChBand[i] = frameChBand[k]
				frameChBand[k] = tmp

				if i < b.delay-1 {
					i++
				} else {
					i = 0
				}
			}
		}
	}

	b.lastInsert = i
}
