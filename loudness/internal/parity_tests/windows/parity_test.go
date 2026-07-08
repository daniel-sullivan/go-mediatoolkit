//go:build cgo

package windows

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
	modeM = 1 << 0           // EBUR128_MODE_M
	modeS = (1 << 1) | modeM // EBUR128_MODE_S
	modeI = (1 << 2) | modeM // EBUR128_MODE_I
)

// loudnessAbsTolerance bounds the log-domain LUFS reading only under
// strictparity.Enabled. The trailing-window ENERGY is asserted bit-exact (below);
// the LUFS is energyToLoudness(energy) = 10*log(e)/log(10) - 0.691, which
// (a) carries a Go math.Log vs libm log difference of a couple ULP and (b)
// loses relative precision to catastrophic cancellation when the loudness
// is near 0 LUFS (the -0.691 subtracts almost all of the ~0.69 log term for
// a full-scale-loud block). An absolute LU bound is the meaningful check
// there — 1e-9 LU is nine orders tighter than the EBU ±0.1 LU compliance
// requirement. -Inf is asserted bit-exact regardless of strictparity.Enabled (it is
// structural — log(0) — not an FMA-sensitive arithmetic result).
const loudnessAbsTolerance = 1e-9

// loudnessNonStrictAbsTolerance widens the LUFS bound under strictparity.Enabled ==
// false: the energy feeding the log may itself carry FMA-fused oracle
// drift (see energyNonStrictRelTolerance below), so the resulting LUFS
// value can move a bit more than the strict 1e-9 bound allows. 1e-6 LU is
// still six orders tighter than the EBU ±0.1 LU compliance requirement.
const loudnessNonStrictAbsTolerance = 1e-6

// energyNonStrictRelTolerance / energyNonStrictAbsFloor bound the
// trailing-window energy comparison under strictparity.Enabled == false: without a
// guaranteed -ffp-contract=off oracle, the per-sample weighted-sum-of-
// squares accumulation may be FMA-fused on the C side, drifting from the
// FMA-free Go port by a handful of ULP (empirically ~1e-14 relative here).
// 1e-9 leaves several orders of margin while still catching a real port
// defect. The absolute floor anchors near-zero energies (e.g. silence),
// where a purely relative tolerance would divide by ~0.
const energyNonStrictRelTolerance = 1e-9
const energyNonStrictAbsFloor = 1e-9

func bitsEqual(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }

// compareEnergy asserts the pre-log trailing-window energy: bit-exact
// under strictparity.Enabled, else within a combined relative+absolute tolerance
// loose enough to absorb FMA-fused oracle drift but tight enough to still
// catch a real divergence.
func compareEnergy(t *testing.T, label string, want, got float64) {
	t.Helper()
	if strictparity.Enabled {
		require.Truef(t, bitsEqual(want, got),
			"%s energy mismatch: C=%.17g (%016x) Go=%.17g (%016x)",
			label, want, math.Float64bits(want), got, math.Float64bits(got))
		return
	}
	diff := math.Abs(want - got)
	bound := energyNonStrictAbsFloor
	if rel := energyNonStrictRelTolerance * math.Max(math.Abs(want), math.Abs(got)); rel > bound {
		bound = rel
	}
	require.LessOrEqualf(t, diff, bound,
		"%s energy off by %.3g (non-strict bound %.3g): C=%.17g Go=%.17g", label, diff, bound, want, got)
}

// compareLoudness asserts the LUFS reading: -Inf bit-exact (unconditionally
// — structural, not FMA-sensitive), else within the active absolute LU
// bound (tight under strictparity.Enabled, widened otherwise).
func compareLoudness(t *testing.T, label string, want, got float64) {
	t.Helper()
	if math.IsInf(want, -1) || math.IsInf(got, -1) {
		require.Truef(t, bitsEqual(want, got), "%s: -Inf mismatch C=%v Go=%v", label, want, got)
		return
	}
	tol := loudnessNonStrictAbsTolerance
	if strictparity.Enabled {
		tol = loudnessAbsTolerance
	}
	require.InDeltaf(t, want, got, tol,
		"%s off by %.3g LU (>%.0e): C=%.17g Go=%.17g", label, math.Abs(want-got), tol, want, got)
}

type goReadings struct {
	momentary       float64
	momentaryEnergy float64
	shortTerm       float64
	shortTermEnergy float64
	windows         []float64
	windowEnergies  []float64
}

func goWindowMeter(channels, rate, mode, maxWindow int, chunks [][]float64, queryWindows []int) goReadings {
	si := (rate + 5) / 10
	st := r128.NewState(channels, rate, mode)
	if maxWindow > 0 {
		st.SetMaxWindow(uint64(maxWindow))
	}
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		st.AddFrames(chunk)
	}
	var out goReadings
	out.momentary, _ = st.LoudnessMomentary()
	out.momentaryEnergy = st.CalcGatingBlockEnergy(si * 4)
	if mode&modeS == modeS {
		out.shortTerm, _ = st.LoudnessShortterm()
		out.shortTermEnergy = st.CalcGatingBlockEnergy(si * 30)
	}
	for _, w := range queryWindows {
		lw, _ := st.LoudnessWindow(uint64(w))
		out.windows = append(out.windows, lw)
		out.windowEnergies = append(out.windowEnergies, st.CalcGatingBlockEnergy(rate*w/1000))
	}
	return out
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

func randStream(seed uint64, channels, frames int, amp float64) []float64 {
	rng := rand.New(rand.NewPCG(seed, 0x1770))
	out := make([]float64, frames*channels)
	for i := range out {
		out[i] = (rng.Float64()*2 - 1) * amp
	}
	return out
}

// TestWindowReadingsParity covers momentary, short-term, and several
// loudness_window queries against the C oracle, over prime and large
// frame-aligned chunk schedules, with the default (mode-derived) window.
// Energies are bit-exact; LUFS readings are within the absolute LU bound.
func TestWindowReadingsParity(t *testing.T) {
	const rate = 48000
	si := (rate + 5) / 10

	queryWindows := []int{100, 400, 1000, 2000, 3000}
	chunkSizes := []int{1, 7, 113, 1024 + 1, si} // frame-aligned prime-ish

	for _, ch := range []int{1, 2, 5} {
		for _, dur := range []int{si * 4, rate * 2, rate * 5} { // 400 ms, 2 s, 5 s
			ch, dur := ch, dur
			t.Run(fmt.Sprintf("ch=%d/frames=%d", ch, dur), func(t *testing.T) {
				// amp 0.3 keeps loudness comfortably below 0 LUFS while the
				// bit-exact energy check does the heavy lifting regardless.
				src := randStream(uint64(ch)*1e6+uint64(dur), ch, dur, 0.3)
				chunks := chunkStream(src, ch, chunkSizes)

				want, err := cWindowMeter(ch, rate, modeS, 0, chunks, queryWindows)
				require.NoError(t, err)
				got := goWindowMeter(ch, rate, modeS, 0, chunks, queryWindows)

				compareEnergy(t, "momentary", want.momentaryEnergy, got.momentaryEnergy)
				compareLoudness(t, "momentary", want.momentary, got.momentary)
				compareEnergy(t, "shortterm", want.shortTermEnergy, got.shortTermEnergy)
				compareLoudness(t, "shortterm", want.shortTerm, got.shortTerm)
				for i, w := range queryWindows {
					compareEnergy(t, fmt.Sprintf("window(%dms)", w), want.windowEnergies[i], got.windowEnergies[i])
					compareLoudness(t, fmt.Sprintf("window(%dms)", w), want.windows[i], got.windows[i])
				}
			})
		}
	}
}

// TestSetMaxWindowParity exercises the SetMaxWindow reallocation path and
// the loudness_window read that follows. It stays at a low sample rate and
// mono because libebur128 v1.2.6 sizes the reallocated buffer proportional
// to samplerate*window (NOT /1000) — a faithfully-ported quirk — so a large
// rate or window would allocate gigabytes on both sides. At 8 kHz mono a
// 1000 ms max window is ~8M frames (64 MB), bounded but still exercising the
// realloc, the block-accounting reset, and wider-than-default queries.
func TestSetMaxWindowParity(t *testing.T) {
	const (
		rate = 8000
		ch   = 1
	)
	si := (rate + 5) / 10

	maxWindow := 1000 // grow beyond the default 400 ms (mode M|I)
	queryWindows := []int{200, 400, 1000}
	chunkSizes := []int{1, 7, 113, si}

	src := randStream(0x5ED_0AD_0001, ch, rate*2, 0.3) // 2 s
	chunks := chunkStream(src, ch, chunkSizes)

	want, err := cWindowMeter(ch, rate, modeI, maxWindow, chunks, queryWindows)
	require.NoError(t, err)
	got := goWindowMeter(ch, rate, modeI, maxWindow, chunks, queryWindows)

	compareEnergy(t, "momentary", want.momentaryEnergy, got.momentaryEnergy)
	compareLoudness(t, "momentary", want.momentary, got.momentary)
	for i, w := range queryWindows {
		compareEnergy(t, fmt.Sprintf("window(%dms)", w), want.windowEnergies[i], got.windowEnergies[i])
		compareLoudness(t, fmt.Sprintf("window(%dms)", w), want.windows[i], got.windows[i])
	}
}
