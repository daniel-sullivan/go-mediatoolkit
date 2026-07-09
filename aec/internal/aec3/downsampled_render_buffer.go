// This file ports modules/audio_processing/aec3/
// downsampled_render_buffer.{h,cc}: the circular buffer of
// downsampled render data used by the matched filter / delay
// estimator, following the same circular-index method set as
// BlockBuffer (block.go).
package aec3

// DownsampledRenderBuffer holds the circular buffer of the
// downsampled render data. Port of DownsampledRenderBuffer. Fields are
// exported (matching the C++ struct's public fields, and the sibling
// BlockBuffer/FftBuffer/SpectrumBuffer convention in this package) so
// parity tests in other packages can drive identical scenarios on the
// Go and oracle sides.
type DownsampledRenderBuffer struct {
	Size   int
	Buffer []float32
	Write  int
	Read   int
}

// NewDownsampledRenderBuffer mirrors DownsampledRenderBuffer's C++
// constructor.
func NewDownsampledRenderBuffer(downsampledBufferSize int) *DownsampledRenderBuffer {
	return &DownsampledRenderBuffer{
		Size:   downsampledBufferSize,
		Buffer: make([]float32, downsampledBufferSize),
	}
}

// IncIndex returns index advanced by one, wrapping. C:
// DownsampledRenderBuffer::IncIndex.
func (b *DownsampledRenderBuffer) IncIndex(index int) int {
	if index < b.Size-1 {
		return index + 1
	}
	return 0
}

// DecIndex returns index retarded by one, wrapping. C:
// DownsampledRenderBuffer::DecIndex.
func (b *DownsampledRenderBuffer) DecIndex(index int) int {
	if index > 0 {
		return index - 1
	}
	return b.Size - 1
}

// OffsetIndex returns index offset by offset, wrapping. C:
// DownsampledRenderBuffer::OffsetIndex.
func (b *DownsampledRenderBuffer) OffsetIndex(index, offset int) int {
	return (b.Size + index + offset) % b.Size
}

// UpdateWriteIndex offsets the write index. C:
// DownsampledRenderBuffer::UpdateWriteIndex.
func (b *DownsampledRenderBuffer) UpdateWriteIndex(offset int) {
	b.Write = b.OffsetIndex(b.Write, offset)
}

// IncWriteIndex advances the write index. C:
// DownsampledRenderBuffer::IncWriteIndex.
func (b *DownsampledRenderBuffer) IncWriteIndex() { b.Write = b.IncIndex(b.Write) }

// DecWriteIndex retards the write index. C:
// DownsampledRenderBuffer::DecWriteIndex.
func (b *DownsampledRenderBuffer) DecWriteIndex() { b.Write = b.DecIndex(b.Write) }

// UpdateReadIndex offsets the read index. C:
// DownsampledRenderBuffer::UpdateReadIndex.
func (b *DownsampledRenderBuffer) UpdateReadIndex(offset int) { b.Read = b.OffsetIndex(b.Read, offset) }

// IncReadIndex advances the read index. C:
// DownsampledRenderBuffer::IncReadIndex.
func (b *DownsampledRenderBuffer) IncReadIndex() { b.Read = b.IncIndex(b.Read) }

// DecReadIndex retards the read index. C:
// DownsampledRenderBuffer::DecReadIndex.
func (b *DownsampledRenderBuffer) DecReadIndex() { b.Read = b.DecIndex(b.Read) }
