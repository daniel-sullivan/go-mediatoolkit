//go:build cgo && aec_oracle

// Package blocking is the bit-exact parity slice for
// aec/internal/aec3's block.go (FrameBlocker + BlockFramer, driven
// through Block) against the fetched C++ oracle. Env/link setup is
// shared with the other slices via ../run.sh — see the fft slice's
// cgo.go and the smoke slice for the full rationale.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package blocking

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
*/
import "C"

// frameBlockerC wraps the oracle's webrtc::FrameBlocker. Flat slices
// are [band][channel][n] in Block's own index order.
type frameBlockerC struct {
	h           *C.aec3_frame_blocker
	bands, chns int
}

func newFrameBlockerC(numBands, numChannels int) *frameBlockerC {
	return &frameBlockerC{
		h:     C.aec3_frame_blocker_create(C.int(numBands), C.int(numChannels)),
		bands: numBands,
		chns:  numChannels,
	}
}

func (f *frameBlockerC) close() {
	C.aec3_frame_blocker_destroy(f.h)
	f.h = nil
}

func (f *frameBlockerC) insertSubFrameAndExtractBlock(subFrame []float32) []float32 {
	block := make([]float32, f.bands*f.chns*64)
	C.aec3_frame_blocker_insert_and_extract(f.h,
		(*C.float)(&subFrame[0]), (*C.float)(&block[0]))
	return block
}

func (f *frameBlockerC) isBlockAvailable() bool {
	return C.aec3_frame_blocker_block_available(f.h) != 0
}

func (f *frameBlockerC) extractBlock() []float32 {
	block := make([]float32, f.bands*f.chns*64)
	C.aec3_frame_blocker_extract(f.h, (*C.float)(&block[0]))
	return block
}

// blockFramerC wraps the oracle's webrtc::BlockFramer.
type blockFramerC struct {
	h           *C.aec3_block_framer
	bands, chns int
}

func newBlockFramerC(numBands, numChannels int) *blockFramerC {
	return &blockFramerC{
		h:     C.aec3_block_framer_create(C.int(numBands), C.int(numChannels)),
		bands: numBands,
		chns:  numChannels,
	}
}

func (b *blockFramerC) close() {
	C.aec3_block_framer_destroy(b.h)
	b.h = nil
}

func (b *blockFramerC) insertBlock(block []float32) {
	C.aec3_block_framer_insert(b.h, (*C.float)(&block[0]))
}

func (b *blockFramerC) insertBlockAndExtractSubFrame(block []float32) []float32 {
	subFrame := make([]float32, b.bands*b.chns*80)
	C.aec3_block_framer_insert_and_extract(b.h,
		(*C.float)(&block[0]), (*C.float)(&subFrame[0]))
	return subFrame
}
