//go:build cgo

package lra

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/parity_tests/strictparity"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/r128"
)

// Raw ebur128 mode bits (ebur128.h).
const (
	modeM         = 1 << 0
	modeS         = (1 << 1) | modeM
	modeLRA       = (1 << 3) | modeS // EBUR128_MODE_LRA == 11
	modeHistogram = 64
)

// lraAbsTolerance bounds the loudness-range value under strictparity.Enabled. LRA is
// a DIFFERENCE of two energy-to-loudness conversions, so the -0.691 offsets
// cancel and the result is 10*(log(hEn)-log(lEn))/log(10) — no catastrophic
// cancellation, only the Go math.Log vs libm log gap of a couple ULP on
// each term. The selected energies hEn/lEn are picked from bit-exact
// short-term block energies (proven by the windows slice's bit-exact
// short-term energy) by integer-deterministic sort/percentile/gating logic,
// so a divergence in that selection would move the LRA by LU, not by 1e-9.
// An empty result is exactly 0.0. 1e-9 LU is eight orders tighter than the
// EBU ±1 LU LRA requirement.
const lraAbsTolerance = 1e-9

// lraNonStrictAbsTolerance widens the LRA bound under strictparity.Enabled == false:
// without a guaranteed -ffp-contract=off oracle, the short-term block
// energies feeding the two log terms may themselves carry a little
// FMA-fused oracle drift, so the resulting LRA can move a bit more than the
// strict 1e-9 bound allows. 1e-6 LU is still six orders tighter than the EBU
// ±1 LU LRA requirement.
const lraNonStrictAbsTolerance = 1e-6

// activeLRATol returns the LU tolerance for the currently active parity
// mode.
func activeLRATol() float64 {
	if strictparity.Enabled {
		return lraAbsTolerance
	}
	return lraNonStrictAbsTolerance
}

func goLoudnessRange(channels, rate, mode, historyMs int, chunks [][]float64) (lra float64, stCount int) {
	st := r128.NewState(channels, rate, mode)
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		st.AddFrames(chunk)
	}
	if historyMs > 0 {
		st.SetMaxHistory(uint64(historyMs))
	}
	lra, _ = st.LoudnessRange()
	return lra, st.NumShortTermBlocks()
}

func chunkStream(src []float64, channels int, sizes []int) [][]float64 {
	var chunks [][]float64
	frames := len(src) / channels
	pos, i := 0, 0
	for pos < frames {
		s := sizes[i%len(sizes)]
		if s > frames-pos {
			s = frames - pos
		}
		chunks = append(chunks, src[pos*channels:(pos+s)*channels])
		pos += s
		i++
	}
	if len(chunks) == 0 {
		chunks = [][]float64{src[:0]}
	}
	return chunks
}

// varyingSignal builds interleaved audio whose amplitude steps every second,
// so successive short-term (3 s, 1 s hop) blocks span a range of loudness —
// giving a non-trivial LRA and exercising the percentile selection. The
// per-second amplitudes are drawn from a seeded PCG so the spread is
// deterministic but not monotonic.
func varyingSignal(seed uint64, channels, seconds, rate int) []float64 {
	rng := rand.New(rand.NewPCG(seed, 0x3342))
	out := make([]float64, seconds*rate*channels)
	for s := 0; s < seconds; s++ {
		// Amplitude between ~-40 dBFS and ~-3 dBFS, varied per second.
		amp := 0.02 + rng.Float64()*0.68
		for n := 0; n < rate; n++ {
			idx := s*rate + n
			v := amp * math.Sin(2*math.Pi*500*float64(idx)/float64(rate))
			for c := 0; c < channels; c++ {
				out[idx*channels+c] = v
			}
		}
	}
	return out
}

// TestLoudnessRangeParity compares LRA and the retained short-term block
// count across n = 1, 2, 3, 31 short-term blocks (durations n+2 seconds),
// list vs histogram mode, and stereo.
func TestLoudnessRangeParity(t *testing.T) {
	const (
		rate = 48000
		ch   = 2
	)
	si := (rate + 5) / 10
	chunkSizes := []int{1, 7, 4801, si} // frame-aligned, prime-ish

	blockCounts := []int{1, 2, 3, 31}
	modes := []struct {
		name string
		mode int
	}{
		{"list", modeLRA},
		{"histogram", modeLRA | modeHistogram},
	}

	for _, n := range blockCounts {
		for _, m := range modes {
			n, m := n, m
			t.Run(fmt.Sprintf("n=%d/%s", n, m.name), func(t *testing.T) {
				seconds := n + 2 // first ST block at 3 s, then +1 per second
				src := varyingSignal(uint64(n)*7+uint64(m.mode), ch, seconds, rate)
				chunks := chunkStream(src, ch, chunkSizes)

				wantLRA, wantCount, err := cLoudnessRange(ch, rate, m.mode, 0, chunks)
				require.NoError(t, err)
				gotLRA, gotCount := goLoudnessRange(ch, rate, m.mode, 0, chunks)

				require.Equalf(t, wantCount, gotCount, "short-term block count (n=%d)", n)
				require.Equalf(t, n, gotCount, "expected exactly n short-term blocks")
				tol := activeLRATol()
				require.InDeltaf(t, wantLRA, gotLRA, tol,
					"LRA off by %.3g LU (>%.0e): C=%.17g Go=%.17g", math.Abs(wantLRA-gotLRA), tol, wantLRA, gotLRA)
				_ = si
			})
		}
	}
}

// TestLoudnessRangeHistoryTrim feeds 33 s (31 short-term blocks) then caps
// the history so only the most recent blocks survive, exercising the
// set_max_history trim-from-head loop in both modes. The trimmed LRA and
// surviving block count must match the oracle.
func TestLoudnessRangeHistoryTrim(t *testing.T) {
	const (
		rate = 48000
		ch   = 2
	)
	si := (rate + 5) / 10
	chunkSizes := []int{7, si}

	// 10 s history => st_block_list_max = 10000/3000 = 3 short-term blocks.
	const historyMs = 10000
	const wantSurviving = historyMs / 3000

	modes := []struct {
		name string
		mode int
	}{
		{"list", modeLRA},
		{"histogram", modeLRA | modeHistogram},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			src := varyingSignal(0x7213+uint64(m.mode), ch, 33, rate) // 31 ST blocks
			chunks := chunkStream(src, ch, chunkSizes)

			wantLRA, wantCount, err := cLoudnessRange(ch, rate, m.mode, historyMs, chunks)
			require.NoError(t, err)
			gotLRA, gotCount := goLoudnessRange(ch, rate, m.mode, historyMs, chunks)

			require.Equal(t, wantCount, gotCount, "surviving short-term block count")
			if m.mode&modeHistogram == 0 {
				// The list mode trims to exactly st_block_list_max entries;
				// the histogram mode does not trim per-bin counts (it has no
				// per-block identity), so only the list count is pinned here.
				require.Equalf(t, wantSurviving, gotCount, "list-mode surviving blocks")
			}
			require.InDeltaf(t, wantLRA, gotLRA, activeLRATol(),
				"trimmed LRA off by %.3g LU: C=%.17g Go=%.17g", math.Abs(wantLRA-gotLRA), wantLRA, gotLRA)
		})
	}
}
