// This file ports the render-side circular buffer types built on top
// of BlockBuffer (block.go):
//   - modules/audio_processing/aec3/fft_buffer.{h,cc} — FftBuffer
//   - modules/audio_processing/aec3/spectrum_buffer.{h,cc} — SpectrumBuffer
//   - modules/audio_processing/aec3/render_buffer.{h,cc} — RenderBuffer
package aec3

// FftBuffer bundles a circular buffer of per-channel FFTData together
// with the read and write indices. Port of fft_buffer.{h,cc}'s
// FftBuffer.
type FftBuffer struct {
	Size   int
	Buffer [][]*FFTData // [position][channel]
	Write  int
	Read   int
}

// NewFftBuffer mirrors FftBuffer's C++ constructor.
func NewFftBuffer(size, numChannels int) *FftBuffer {
	b := &FftBuffer{Size: size, Buffer: make([][]*FFTData, size)}
	for i := range b.Buffer {
		b.Buffer[i] = make([]*FFTData, numChannels)
		for ch := range b.Buffer[i] {
			b.Buffer[i][ch] = new(FFTData)
			b.Buffer[i][ch].Clear()
		}
	}
	return b
}

// IncIndex returns index advanced by one, wrapping. C: FftBuffer::IncIndex.
func (b *FftBuffer) IncIndex(index int) int {
	if index < b.Size-1 {
		return index + 1
	}
	return 0
}

// DecIndex returns index retarded by one, wrapping. C: FftBuffer::DecIndex.
func (b *FftBuffer) DecIndex(index int) int {
	if index > 0 {
		return index - 1
	}
	return b.Size - 1
}

// OffsetIndex returns index offset by offset, wrapping. C: FftBuffer::OffsetIndex.
func (b *FftBuffer) OffsetIndex(index, offset int) int {
	return (b.Size + index + offset) % b.Size
}

// UpdateWriteIndex offsets the write index. C: FftBuffer::UpdateWriteIndex.
func (b *FftBuffer) UpdateWriteIndex(offset int) { b.Write = b.OffsetIndex(b.Write, offset) }

// IncWriteIndex advances the write index. C: FftBuffer::IncWriteIndex.
func (b *FftBuffer) IncWriteIndex() { b.Write = b.IncIndex(b.Write) }

// DecWriteIndex retards the write index. C: FftBuffer::DecWriteIndex.
func (b *FftBuffer) DecWriteIndex() { b.Write = b.DecIndex(b.Write) }

// UpdateReadIndex offsets the read index. C: FftBuffer::UpdateReadIndex.
func (b *FftBuffer) UpdateReadIndex(offset int) { b.Read = b.OffsetIndex(b.Read, offset) }

// IncReadIndex advances the read index. C: FftBuffer::IncReadIndex.
func (b *FftBuffer) IncReadIndex() { b.Read = b.IncIndex(b.Read) }

// DecReadIndex retards the read index. C: FftBuffer::DecReadIndex.
func (b *FftBuffer) DecReadIndex() { b.Read = b.DecIndex(b.Read) }

// SpectrumBuffer bundles a circular buffer of per-channel power
// spectra together with the read and write indices. Port of
// spectrum_buffer.{h,cc}'s SpectrumBuffer.
type SpectrumBuffer struct {
	Size   int
	Buffer [][][]float32 // [position][channel][FFTLengthBy2Plus1]
	Write  int
	Read   int
}

// NewSpectrumBuffer mirrors SpectrumBuffer's C++ constructor.
func NewSpectrumBuffer(size, numChannels int) *SpectrumBuffer {
	b := &SpectrumBuffer{Size: size, Buffer: make([][][]float32, size)}
	for i := range b.Buffer {
		b.Buffer[i] = make([][]float32, numChannels)
		for ch := range b.Buffer[i] {
			b.Buffer[i][ch] = make([]float32, FFTLengthBy2Plus1)
		}
	}
	return b
}

// IncIndex returns index advanced by one, wrapping. C: SpectrumBuffer::IncIndex.
func (b *SpectrumBuffer) IncIndex(index int) int {
	if index < b.Size-1 {
		return index + 1
	}
	return 0
}

// DecIndex returns index retarded by one, wrapping. C: SpectrumBuffer::DecIndex.
func (b *SpectrumBuffer) DecIndex(index int) int {
	if index > 0 {
		return index - 1
	}
	return b.Size - 1
}

// OffsetIndex returns index offset by offset, wrapping. C: SpectrumBuffer::OffsetIndex.
func (b *SpectrumBuffer) OffsetIndex(index, offset int) int {
	return (b.Size + index + offset) % b.Size
}

// UpdateWriteIndex offsets the write index. C: SpectrumBuffer::UpdateWriteIndex.
func (b *SpectrumBuffer) UpdateWriteIndex(offset int) { b.Write = b.OffsetIndex(b.Write, offset) }

// IncWriteIndex advances the write index. C: SpectrumBuffer::IncWriteIndex.
func (b *SpectrumBuffer) IncWriteIndex() { b.Write = b.IncIndex(b.Write) }

// DecWriteIndex retards the write index. C: SpectrumBuffer::DecWriteIndex.
func (b *SpectrumBuffer) DecWriteIndex() { b.Write = b.DecIndex(b.Write) }

// UpdateReadIndex offsets the read index. C: SpectrumBuffer::UpdateReadIndex.
func (b *SpectrumBuffer) UpdateReadIndex(offset int) { b.Read = b.OffsetIndex(b.Read, offset) }

// IncReadIndex advances the read index. C: SpectrumBuffer::IncReadIndex.
func (b *SpectrumBuffer) IncReadIndex() { b.Read = b.IncIndex(b.Read) }

// DecReadIndex retards the read index. C: SpectrumBuffer::DecReadIndex.
func (b *SpectrumBuffer) DecReadIndex() { b.Read = b.DecIndex(b.Read) }

// RenderBuffer provides a buffer of the render data for the echo
// remover, aliasing (not owning) a BlockBuffer, SpectrumBuffer and
// FftBuffer. Port of render_buffer.{h,cc}'s RenderBuffer.
type RenderBuffer struct {
	blockBuffer    *BlockBuffer
	spectrumBuffer *SpectrumBuffer
	fftBuffer      *FftBuffer
	renderActivity bool
}

// NewRenderBuffer mirrors RenderBuffer's C++ constructor.
func NewRenderBuffer(blockBuffer *BlockBuffer, spectrumBuffer *SpectrumBuffer, fftBuffer *FftBuffer) *RenderBuffer {
	return &RenderBuffer{blockBuffer: blockBuffer, spectrumBuffer: spectrumBuffer, fftBuffer: fftBuffer}
}

// GetBlock returns the block at bufferOffsetBlocks relative to the
// block buffer's read index. C: RenderBuffer::GetBlock.
func (r *RenderBuffer) GetBlock(bufferOffsetBlocks int) *Block {
	position := r.blockBuffer.OffsetIndex(r.blockBuffer.Read, bufferOffsetBlocks)
	return r.blockBuffer.Buffer[position]
}

// Spectrum returns the per-channel spectra at bufferOffsetFfts
// relative to the spectrum buffer's read index. C: RenderBuffer::Spectrum.
func (r *RenderBuffer) Spectrum(bufferOffsetFfts int) [][]float32 {
	position := r.spectrumBuffer.OffsetIndex(r.spectrumBuffer.Read, bufferOffsetFfts)
	return r.spectrumBuffer.Buffer[position]
}

// GetFftBuffer returns the circular fft buffer. C: RenderBuffer::GetFftBuffer.
func (r *RenderBuffer) GetFftBuffer() [][]*FFTData { return r.fftBuffer.Buffer }

// Position returns the current position in the circular buffer. C: RenderBuffer::Position.
func (r *RenderBuffer) Position() int { return r.fftBuffer.Read }

// SpectralSum returns the sum of the spectra for numSpectra FFTs,
// into x2 (length FFTLengthBy2Plus1). C: RenderBuffer::SpectralSum.
func (r *RenderBuffer) SpectralSum(numSpectra int, x2 []float32) {
	for k := range x2 {
		x2[k] = 0
	}
	position := r.spectrumBuffer.Read
	for j := 0; j < numSpectra; j++ {
		for _, channelSpectrum := range r.spectrumBuffer.Buffer[position] {
			for k := range x2 {
				x2[k] += channelSpectrum[k]
			}
		}
		position = r.spectrumBuffer.IncIndex(position)
	}
}

// SpectralSums returns the sums of the spectra for two numbers of
// FFTs (numSpectraShorter <= numSpectraLonger), into x2Shorter and
// x2Longer (each length FFTLengthBy2Plus1). C: RenderBuffer::SpectralSums.
func (r *RenderBuffer) SpectralSums(numSpectraShorter, numSpectraLonger int, x2Shorter, x2Longer []float32) {
	for k := range x2Shorter {
		x2Shorter[k] = 0
	}
	position := r.spectrumBuffer.Read
	j := 0
	for ; j < numSpectraShorter; j++ {
		for _, channelSpectrum := range r.spectrumBuffer.Buffer[position] {
			for k := range x2Shorter {
				x2Shorter[k] += channelSpectrum[k]
			}
		}
		position = r.spectrumBuffer.IncIndex(position)
	}
	copy(x2Longer, x2Shorter)
	for ; j < numSpectraLonger; j++ {
		for _, channelSpectrum := range r.spectrumBuffer.Buffer[position] {
			for k := range x2Longer {
				x2Longer[k] += channelSpectrum[k]
			}
		}
		position = r.spectrumBuffer.IncIndex(position)
	}
}

// GetRenderActivity returns the recent activity seen in the render
// signal. C: RenderBuffer::GetRenderActivity.
func (r *RenderBuffer) GetRenderActivity() bool { return r.renderActivity }

// SetRenderActivity sets the recent activity seen in the render
// signal. C: RenderBuffer::SetRenderActivity.
func (r *RenderBuffer) SetRenderActivity(activity bool) { r.renderActivity = activity }

// Headroom returns the headroom between the write and read positions
// in the buffer. C: RenderBuffer::Headroom.
func (r *RenderBuffer) Headroom() int {
	f := r.fftBuffer
	if f.Write < f.Read {
		return f.Read - f.Write
	}
	return f.Size - f.Write + f.Read
}

// GetSpectrumBuffer returns the spectrum buffer. C: RenderBuffer::GetSpectrumBuffer.
func (r *RenderBuffer) GetSpectrumBuffer() *SpectrumBuffer { return r.spectrumBuffer }

// GetBlockBuffer returns the block buffer. C: RenderBuffer::GetBlockBuffer.
func (r *RenderBuffer) GetBlockBuffer() *BlockBuffer { return r.blockBuffer }
