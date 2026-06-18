//go:build cgo

package benchcmp

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// Representative benchmark input: a fixed pseudo-random byte run standing in
// for one MP3 frame's main-data payload. The bit reader is content-agnostic
// (it does pure MSB-first bitstream arithmetic), so a deterministic random
// buffer exercises every shift/mask path. 2304 bytes is the worst-case
// MPEG-1 Layer III main-data size minimp3 sizes its reservoir for.
const (
	benchPayloadBytes = 2304
	benchReps         = 64 // outer passes per cgo crossing / Go iteration
)

// benchWidths are the get_bits read widths minimp3 actually uses, threaded
// through bs_init/side-info/Huffman (1,2,3,4,5,7,8,9,12,15,24 plus a few
// in-between), cycled so the reader desyncs byte alignment the way a real
// granule decode does. int32 so the slice can be passed to C as a *C.int.
var benchWidths = []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 15, 16, 18, 20, 24}

// benchPayload builds the deterministic payload once.
func benchPayload() []byte {
	r := rand.New(rand.NewPCG(0x6d703362, 0x69747265)) // "mp3b","itre"
	b := make([]byte, benchPayloadBytes)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// goGetBitsSweep mirrors the C bench_getbits_sweep loop: re-init the native
// reader, read each width in widths until the next 24-bit read would overrun,
// for reps passes. Returns an XOR accumulator so the optimizer cannot elide
// the work. This is the exact native counterpart of cgoGetBitsSweep.
func goGetBitsSweep(data []byte, widths []int32, reps int) uint32 {
	var acc uint32
	var bs nativemp3.BitStream
	for r := 0; r < reps; r++ {
		nativemp3.BsInit(&bs, data, len(data))
		wi := 0
		for bs.Pos+24 <= bs.Limit {
			acc ^= nativemp3.GetBits(&bs, int(widths[wi]))
			wi++
			if wi == len(widths) {
				wi = 0
			}
		}
	}
	return acc
}

// sink defeats dead-code elimination of the benchmark loop bodies.
var sink uint32

// BenchmarkGetBits compares the pure-Go nativemp3 bit reader against minimp3's
// get_bits over the same payload. The bit reader is integer-only, so the
// mp3_strict tag does not change either column — bench and bench:strict report
// the same figures (the strict run exists for harness uniformity with the
// FP-bearing slices once they land).
func BenchmarkGetBits(b *testing.B) {
	payload := benchPayload()
	// Bytes-per-op = the payload swept benchReps times.
	b.SetBytes(int64(len(payload) * benchReps))

	b.Run("native", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sink ^= goGetBitsSweep(payload, benchWidths, benchReps)
		}
	})
	b.Run("cgo", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sink ^= cgoGetBitsSweep(payload, benchWidths, benchReps)
		}
	})
}
