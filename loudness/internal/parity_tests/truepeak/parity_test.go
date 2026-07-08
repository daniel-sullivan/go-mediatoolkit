//go:build cgo

package truepeak

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/r128"
	"github.com/stretchr/testify/require"
)

// truePeakFactor mirrors ebur128_init_resampler's factor selection for
// the rate classes exercised here (all < 192000, so never bypass).
func truePeakFactor(rate int) int {
	if rate < 96000 {
		return 4
	}
	return 2
}

// chunkSchedule cycles through the given per-chunk frame sizes until it
// has covered exactly totalFrames, returning the sizes in order. It lets
// a single persistent-state stream exercise every chunk size (1, 7, 113,
// 4800) and the boundaries between them.
func chunkSchedule(totalFrames int, sizes []int) []int {
	var out []int
	pos, i := 0, 0
	for pos < totalFrames {
		s := sizes[i%len(sizes)]
		if s > totalFrames-pos {
			s = totalFrames - pos
		}
		out = append(out, s)
		pos += s
		i++
	}
	return out
}

var (
	parityRates       = []int{44100, 48000, 96000, 176400}
	parityChannels    = []int{1, 2, 5}
	parityChunkSizes  = []int{1, 7, 113, 4800}
	parityTotalFrames = 12000 // > 2*4800 so the largest chunk recurs
)

// TestInterpolatorOutputBitExact drives the raw polyphase interpolator:
// identical float32 input is streamed, in cycling prime-and-large chunks
// with persistent delay-line state, through both the pure-Go
// r128.TruePeaker.Oversample and the C interp_process oracle. Every
// float32 output sample must be bit-identical (compared via
// math.Float32bits, so even a 1-ULP or sign-of-zero difference fails).
//
// This is the load-bearing precision check: it confirms the Go port's
// float32 delay line, float64 coefficients/accumulator, per-tap
// float32->float64 promotion, and float32 output truncation all match the
// C exactly, and that the FMA-suppression barrier at the MAC keeps the
// accumulation unfused.
//
// Unlike the other parity slices, this assertion is unconditional on
// strictparity.Enabled (see the strictparity package): the slice consults
// strictparity.Enabled, but the assertions here don't gate on it — the
// polyphase interpolator's taps are
// float32, and float32's ~7 decimal digits of precision leave no room for a
// double-rounding or FMA-fusion difference to survive truncation back to
// float32 — verified empirically to pass identically with and without
// -tags=loudness_strict / CGO_CFLAGS.
func TestInterpolatorOutputBitExact(t *testing.T) {
	seed := uint64(0x1770_B517_0AD_0001)
	for _, rate := range parityRates {
		factor := truePeakFactor(rate)
		for _, ch := range parityChannels {
			t.Run(fmt.Sprintf("rate=%d/ch=%d", rate, ch), func(t *testing.T) {
				rng := rand.New(rand.NewPCG(seed, uint64(rate)*131+uint64(ch)))

				goTP := r128.NewTruePeaker(rate, ch)
				require.NotNil(t, goTP)
				cInt, err := cInterpCreate(49, factor, ch)
				require.NoError(t, err)
				defer cInt.destroy()

				// One shared float32 stream fed to both, chunk by chunk.
				stream := make([]float32, parityTotalFrames*ch)
				for k := range stream {
					stream[k] = float32((rng.Float64()*2 - 1) * 1.2) // PCM in +/-1.2
				}

				pos := 0
				for _, frames := range chunkSchedule(parityTotalFrames, parityChunkSizes) {
					in := stream[pos*ch : (pos+frames)*ch]
					pos += frames

					cOut := cInt.process(in)
					goOut := make([]float32, frames*factor*ch)
					n := goTP.Oversample(in, goOut)
					require.Equal(t, frames*factor, n, "output frame count")
					require.Len(t, cOut, len(goOut))

					for j := range goOut {
						if math.Float32bits(goOut[j]) != math.Float32bits(cOut[j]) {
							t.Fatalf("interp output mismatch at output sample %d (chunk of %d frames): go=%#v (%08x) c=%#v (%08x)",
								j, frames, goOut[j], math.Float32bits(goOut[j]), cOut[j], math.Float32bits(cOut[j]))
						}
					}
				}
			})
		}
	}
}

// TestFullTruePeakBitExact drives the full public true-peak path. The same
// float64 stream, chunked identically, goes to the C
// ebur128_init(TRUE_PEAK)/add_frames_double/ebur128_true_peak oracle and
// to a faithful Go replication built on r128.TruePeaker plus the sample
// peak. ebur128_true_peak returns max(true_peak, sample_peak), so the Go
// side reproduces libebur128's per-add_frames bookkeeping:
//
//   - prev_true_peak and prev_sample_peak reset to 0 each add_frames;
//   - the interpolator folds this chunk's inter-sample peaks into
//     prev_true_peak (TruePeaker.Process);
//   - the raw sample max folds into prev_sample_peak;
//   - both fold (max) into the persistent true_peak / sample_peak;
//   - the reading is max(true_peak, sample_peak).
//
// The comparison is bit-exact (math.Float64bits): because true_peak is
// derived from the float32 interpolator output (already proven bit-exact
// above) promoted to float64, and sample_peak is an exact float64 max, no
// tolerance is needed. Like TestInterpolatorOutputBitExact, this stays
// unconditional on strictparity.Enabled — verified empirically to pass identically
// with and without -tags=loudness_strict.
func TestFullTruePeakBitExact(t *testing.T) {
	seed := uint64(0x1770_B517_0AD_0002)
	for _, rate := range parityRates {
		for _, ch := range parityChannels {
			t.Run(fmt.Sprintf("rate=%d/ch=%d", rate, ch), func(t *testing.T) {
				rng := rand.New(rand.NewPCG(seed, uint64(rate)*911+uint64(ch)))

				sig := make([]float64, parityTotalFrames*ch)
				for k := range sig {
					sig[k] = (rng.Float64()*2 - 1) * 1.2
				}

				// Split into the shared chunk schedule.
				var chunks [][]float64
				pos := 0
				for _, frames := range chunkSchedule(parityTotalFrames, parityChunkSizes) {
					chunks = append(chunks, sig[pos*ch:(pos+frames)*ch])
					pos += frames
				}

				cPeaks, err := cTruePeakFull(ch, rate, chunks)
				require.NoError(t, err)

				// Go replication of libebur128's add_frames peak bookkeeping.
				goTP := r128.NewTruePeaker(rate, ch)
				require.NotNil(t, goTP)
				truePeak := make([]float64, ch)
				samplePeak := make([]float64, ch)
				for _, chunk := range chunks {
					prevTP := make([]float64, ch) // prev_true_peak reset per add_frames
					goTP.Process(chunk, prevTP)

					prevSP := make([]float64, ch) // prev_sample_peak reset per add_frames
					frames := len(chunk) / ch
					for f := 0; f < frames; f++ {
						for c := 0; c < ch; c++ {
							cur := chunk[f*ch+c]
							abs := cur // EBUR128_MAX(cur,-cur), scaling_factor==1.0
							if -cur > abs {
								abs = -cur
							}
							if abs > prevSP[c] {
								prevSP[c] = abs
							}
						}
					}
					for c := 0; c < ch; c++ {
						if prevTP[c] > truePeak[c] {
							truePeak[c] = prevTP[c]
						}
						if prevSP[c] > samplePeak[c] {
							samplePeak[c] = prevSP[c]
						}
					}
				}

				for c := 0; c < ch; c++ {
					rep := truePeak[c] // ebur128_true_peak = max(true_peak, sample_peak)
					if samplePeak[c] > rep {
						rep = samplePeak[c]
					}
					if math.Float64bits(rep) != math.Float64bits(cPeaks[c]) {
						t.Fatalf("true peak mismatch ch=%d: go=%.17g (%016x) c=%.17g (%016x) [truePeak=%.17g samplePeak=%.17g]",
							c, rep, math.Float64bits(rep), cPeaks[c], math.Float64bits(cPeaks[c]),
							truePeak[c], samplePeak[c])
					}
				}
			})
		}
	}
}
