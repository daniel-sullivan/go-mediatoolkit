// This file ports the AEC3 block-domain framing types:
//   - modules/audio_processing/aec3/block.h — Block
//   - modules/audio_processing/aec3/block_buffer.{h,cc} — BlockBuffer
//   - modules/audio_processing/aec3/frame_blocker.{h,cc} — FrameBlocker
//   - modules/audio_processing/aec3/block_framer.{h,cc} — BlockFramer
//
// All of this is pure data movement (copies, no float arithmetic), so
// parity with the C++ oracle is trivially bit-exact.
package aec3

// Block contains one or more channels of 4 milliseconds (BlockSize =
// 64 samples) of audio data, split in one or more frequency bands,
// each with a sampling rate of 16 kHz. Port of block.h's Block: the
// flat data layout ((band*numChannels+channel)*BlockSize) is preserved
// so that View aliases the same memory as the C iterators.
type Block struct {
	numBands    int
	numChannels int
	data        []float32
}

// NewBlock mirrors Block's C++ constructor with default_value = 0.
func NewBlock(numBands, numChannels int) *Block {
	return &Block{
		numBands:    numBands,
		numChannels: numChannels,
		data:        make([]float32, numBands*numChannels*BlockSize),
	}
}

// NumBands returns the number of bands.
func (b *Block) NumBands() int { return b.numBands }

// NumChannels returns the number of channels.
func (b *Block) NumChannels() int { return b.numChannels }

// SetNumChannels modifies the number of channels and sets all samples
// to zero. C: Block::SetNumChannels (resize + fill(0)).
func (b *Block) SetNumChannels(numChannels int) {
	b.numChannels = numChannels
	n := b.numBands * b.numChannels * BlockSize
	if cap(b.data) >= n {
		b.data = b.data[:n]
	} else {
		b.data = make([]float32, n)
	}
	for i := range b.data {
		b.data[i] = 0
	}
}

// View returns the BlockSize-sample slice for the requested band and
// channel, aliasing the Block's storage. C: Block::View / begin/end.
func (b *Block) View(band, channel int) []float32 {
	i := (band*b.numChannels + channel) * BlockSize
	return b.data[i : i+BlockSize : i+BlockSize]
}

// Swap lets two Blocks swap audio data. C: Block::Swap.
func (b *Block) Swap(other *Block) {
	b.numBands, other.numBands = other.numBands, b.numBands
	b.numChannels, other.numChannels = other.numChannels, b.numChannels
	b.data, other.data = other.data, b.data
}

// BlockBuffer bundles a circular buffer of Blocks together with the
// read and write indices. Port of block_buffer.{h,cc}'s BlockBuffer.
type BlockBuffer struct {
	Size   int
	Buffer []*Block
	Write  int
	Read   int
}

// NewBlockBuffer mirrors BlockBuffer's C++ constructor.
func NewBlockBuffer(size, numBands, numChannels int) *BlockBuffer {
	b := &BlockBuffer{
		Size:   size,
		Buffer: make([]*Block, size),
	}
	for i := range b.Buffer {
		b.Buffer[i] = NewBlock(numBands, numChannels)
	}
	return b
}

// IncIndex returns index advanced by one, wrapping. C: BlockBuffer::IncIndex.
func (b *BlockBuffer) IncIndex(index int) int {
	if index < b.Size-1 {
		return index + 1
	}
	return 0
}

// DecIndex returns index retarded by one, wrapping. C: BlockBuffer::DecIndex.
func (b *BlockBuffer) DecIndex(index int) int {
	if index > 0 {
		return index - 1
	}
	return b.Size - 1
}

// OffsetIndex returns index offset by offset, wrapping. offset must be
// in [-Size, Size] (C: RTC_DCHECK_GE(size, offset)). C: BlockBuffer::OffsetIndex.
func (b *BlockBuffer) OffsetIndex(index, offset int) int {
	return (b.Size + index + offset) % b.Size
}

// UpdateWriteIndex offsets the write index. C: BlockBuffer::UpdateWriteIndex.
func (b *BlockBuffer) UpdateWriteIndex(offset int) { b.Write = b.OffsetIndex(b.Write, offset) }

// IncWriteIndex advances the write index. C: BlockBuffer::IncWriteIndex.
func (b *BlockBuffer) IncWriteIndex() { b.Write = b.IncIndex(b.Write) }

// DecWriteIndex retards the write index. C: BlockBuffer::DecWriteIndex.
func (b *BlockBuffer) DecWriteIndex() { b.Write = b.DecIndex(b.Write) }

// UpdateReadIndex offsets the read index. C: BlockBuffer::UpdateReadIndex.
func (b *BlockBuffer) UpdateReadIndex(offset int) { b.Read = b.OffsetIndex(b.Read, offset) }

// IncReadIndex advances the read index. C: BlockBuffer::IncReadIndex.
func (b *BlockBuffer) IncReadIndex() { b.Read = b.IncIndex(b.Read) }

// DecReadIndex retards the read index. C: BlockBuffer::DecReadIndex.
func (b *BlockBuffer) DecReadIndex() { b.Read = b.DecIndex(b.Read) }

// FrameBlocker produces 64-sample multiband blocks from frames
// consisting of 2 subframes of 80 samples. Port of
// frame_blocker.{h,cc}'s FrameBlocker. The per-band/per-channel carry
// buffer starts empty (contrast BlockFramer, whose buffer starts
// BlockSize-full of zeros).
type FrameBlocker struct {
	numBands    int
	numChannels int
	buffer      [][][]float32 // [band][channel], each len in [0, BlockSize]
}

// NewFrameBlocker mirrors FrameBlocker's C++ constructor.
func NewFrameBlocker(numBands, numChannels int) *FrameBlocker {
	b := &FrameBlocker{
		numBands:    numBands,
		numChannels: numChannels,
		buffer:      make([][][]float32, numBands),
	}
	for band := range b.buffer {
		b.buffer[band] = make([][]float32, numChannels)
		for ch := range b.buffer[band] {
			b.buffer[band][ch] = make([]float32, 0, BlockSize)
		}
	}
	return b
}

// InsertSubFrameAndExtractBlock inserts one 80-sample multiband
// subframe (subFrame[band][channel], each of length SubFrameLength)
// and extracts one 64-sample multiband block. C:
// FrameBlocker::InsertSubFrameAndExtractBlock.
func (b *FrameBlocker) InsertSubFrameAndExtractBlock(subFrame [][][]float32, block *Block) {
	for band := 0; band < b.numBands; band++ {
		for channel := 0; channel < b.numChannels; channel++ {
			buf := b.buffer[band][channel]
			samplesToBlock := BlockSize - len(buf)
			out := block.View(band, channel)
			copy(out, buf)
			copy(out[BlockSize-samplesToBlock:], subFrame[band][channel][:samplesToBlock])
			b.buffer[band][channel] = append(buf[:0], subFrame[band][channel][samplesToBlock:]...)
		}
	}
}

// IsBlockAvailable reports whether a multiband block of 64 samples is
// available for extraction. C: FrameBlocker::IsBlockAvailable.
func (b *FrameBlocker) IsBlockAvailable() bool {
	return len(b.buffer[0][0]) == BlockSize
}

// ExtractBlock extracts a multiband block of 64 samples. Requires
// IsBlockAvailable(). C: FrameBlocker::ExtractBlock.
func (b *FrameBlocker) ExtractBlock(block *Block) {
	for band := 0; band < b.numBands; band++ {
		for channel := 0; channel < b.numChannels; channel++ {
			copy(block.View(band, channel), b.buffer[band][channel])
			b.buffer[band][channel] = b.buffer[band][channel][:0]
		}
	}
}

// BlockFramer produces frames consisting of 2 subframes of 80 samples
// each from 64-sample blocks. Designed to work together with
// FrameBlocker; any other insertion rate overruns the internal
// buffers. Port of block_framer.{h,cc}'s BlockFramer.
type BlockFramer struct {
	numBands    int
	numChannels int
	buffer      [][][]float32 // [band][channel], each len in [0, BlockSize]
}

// NewBlockFramer mirrors BlockFramer's C++ constructor: the carry
// buffer starts as BlockSize zero samples per band/channel.
func NewBlockFramer(numBands, numChannels int) *BlockFramer {
	b := &BlockFramer{
		numBands:    numBands,
		numChannels: numChannels,
		buffer:      make([][][]float32, numBands),
	}
	for band := range b.buffer {
		b.buffer[band] = make([][]float32, numChannels)
		for ch := range b.buffer[band] {
			b.buffer[band][ch] = make([]float32, BlockSize)
		}
	}
	return b
}

// InsertBlock adds a 64-sample block into the data that will form the
// next output frame. Requires the buffer to be empty (C:
// RTC_DCHECK_EQ(0, buffer.size())). C: BlockFramer::InsertBlock.
func (b *BlockFramer) InsertBlock(block *Block) {
	for band := 0; band < b.numBands; band++ {
		for channel := 0; channel < b.numChannels; channel++ {
			b.buffer[band][channel] = append(b.buffer[band][channel][:0], block.View(band, channel)...)
		}
	}
}

// InsertBlockAndExtractSubFrame adds a 64-sample block and extracts an
// 80-sample subframe into subFrame[band][channel] (each of length
// SubFrameLength). C: BlockFramer::InsertBlockAndExtractSubFrame.
func (b *BlockFramer) InsertBlockAndExtractSubFrame(block *Block, subFrame [][][]float32) {
	for band := 0; band < b.numBands; band++ {
		for channel := 0; channel < b.numChannels; channel++ {
			buf := b.buffer[band][channel]
			samplesToFrame := SubFrameLength - len(buf)
			in := block.View(band, channel)
			out := subFrame[band][channel]
			copy(out, buf)
			copy(out[len(buf):], in[:samplesToFrame])
			b.buffer[band][channel] = append(buf[:0], in[samplesToFrame:]...)
		}
	}
}
