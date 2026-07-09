//go:build cgo && aec_oracle

package blocking

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// lcg is the same PRNG the other slices use; both sides consume
// identical Go-generated inputs, so quality is irrelevant.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

func requireBitExact(t *testing.T, what string, iter int, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: iter %d: length mismatch %d vs %d", what, iter, len(got), len(want))
	}
	for i := range want {
		if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
			t.Fatalf("%s: iter %d: index %d: got %v, want %v", what, iter, i, got[i], want[i])
		}
	}
}

// subFrameViews builds the [band][channel][]float32 view structure the
// Go API takes, pointing into a flat sub-frame.
func subFrameViews(flat []float32, bands, channels int) [][][]float32 {
	views := make([][][]float32, bands)
	for band := 0; band < bands; band++ {
		views[band] = make([][]float32, channels)
		for ch := 0; ch < channels; ch++ {
			off := (band*channels + ch) * aec3.SubFrameLength
			views[band][ch] = flat[off : off+aec3.SubFrameLength]
		}
	}
	return views
}

func blockToFlat(b *aec3.Block) []float32 {
	flat := make([]float32, b.NumBands()*b.NumChannels()*aec3.BlockSize)
	for band := 0; band < b.NumBands(); band++ {
		for ch := 0; ch < b.NumChannels(); ch++ {
			copy(flat[(band*b.NumChannels()+ch)*aec3.BlockSize:], b.View(band, ch))
		}
	}
	return flat
}

// TestBlockingParity drives the Go and C++ FrameBlocker+BlockFramer
// pairs through the exact per-subframe sequence EchoCanceller3 uses
// (insert+extract, feed the framer, and drain the every-5th carry
// block), across band/channel combinations, verifying every extracted
// block and produced sub-frame bit-exactly, including the state
// carried across calls.
func TestBlockingParity(t *testing.T) {
	for _, bands := range []int{1, 2, 3} {
		for _, channels := range []int{1, 2, 4} {
			rng := lcg(uint32(bands*100 + channels))

			goBlocker := aec3.NewFrameBlocker(bands, channels)
			goFramer := aec3.NewBlockFramer(bands, channels)
			goBlock := aec3.NewBlock(bands, channels)

			cBlocker := newFrameBlockerC(bands, channels)
			cFramer := newBlockFramerC(bands, channels)
			defer cBlocker.close()
			defer cFramer.close()

			// 41 sub-frames: covers 8+ full 5-subframe carry cycles
			// plus a partial one, so every internal buffer fill level
			// (0, 16, 32, 48, 64) is hit repeatedly.
			for iter := 0; iter < 41; iter++ {
				subFrame := make([]float32, bands*channels*aec3.SubFrameLength)
				for i := range subFrame {
					subFrame[i] = rng.next() * 100
				}

				goBlocker.InsertSubFrameAndExtractBlock(subFrameViews(subFrame, bands, channels), goBlock)
				cBlock := cBlocker.insertSubFrameAndExtractBlock(subFrame)
				requireBitExact(t, "extracted block", iter, blockToFlat(goBlock), cBlock)

				goOut := make([]float32, bands*channels*aec3.SubFrameLength)
				goFramer.InsertBlockAndExtractSubFrame(goBlock, subFrameViews(goOut, bands, channels))
				cOut := cFramer.insertBlockAndExtractSubFrame(cBlock)
				requireBitExact(t, "produced sub-frame", iter, goOut, cOut)

				if avail := goBlocker.IsBlockAvailable(); avail != cBlocker.isBlockAvailable() {
					t.Fatalf("iter %d: IsBlockAvailable mismatch: go=%v", iter, avail)
				}
				if goBlocker.IsBlockAvailable() {
					goBlocker.ExtractBlock(goBlock)
					cBlock := cBlocker.extractBlock()
					requireBitExact(t, "carry block", iter, blockToFlat(goBlock), cBlock)
					goFramer.InsertBlock(goBlock)
					cFramer.insertBlock(cBlock)
				}
			}
		}
	}
}
