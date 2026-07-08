//go:build cgo

package integrated

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/parity_tests/strictparity"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/r128"
)

// Raw ebur128 mode bits (ebur128.h). Not imported from package loudness: a
// parity slice must not link that package's copy of the C oracle.
const (
	modeI          = 5  // EBUR128_MODE_I
	modeHistogram  = 64 // EBUR128_MODE_HISTOGRAM
	modeIHistogram = modeI | modeHistogram
)

func bitsEqual(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }

func ulpDiff(a, b float64) int64 {
	ua := int64(math.Float64bits(a))
	ub := int64(math.Float64bits(b))
	if ua < 0 {
		ua = math.MinInt64 - ua
	}
	if ub < 0 {
		ub = math.MinInt64 - ub
	}
	d := ua - ub
	if d < 0 {
		d = -d
	}
	return d
}

// loudnessAbsTolerance bounds the final log-domain LUFS readings under
// strictparity.Enabled. The gated ENERGY is accumulated bit-for-bit: the
// gatingblock slice proves the block energies bit-exact, TestGateConstantsParity
// proves relative_gate_factor / minus_twenty bit-exact (so every gating
// comparison is identical), and the gating decisions are
// integer-deterministic. Only ebur128_energy_to_loudness = 10*log(e)/log(10)
// - 0.691 introduces a difference: a Go math.Log vs libm log gap of a couple
// ULP, magnified by catastrophic cancellation for near-0-LUFS material (the
// -0.691 cancels most of the ~0.69 log term). An absolute LU bound is the
// meaningful check — 1e-9 LU is eight orders tighter than the EBU ±0.1 LU
// requirement. Silence (-Inf) and the no-block relative threshold (-70.0)
// are asserted bit-exact regardless of strictparity.Enabled (structural, not
// FMA-sensitive).
const loudnessAbsTolerance = 1e-9

// loudnessNonStrictAbsTolerance widens the LUFS bound under strictparity.Enabled ==
// false: without a guaranteed -ffp-contract=off oracle, the gated block
// energies feeding the log may themselves carry a little FMA-fused oracle
// drift (see the gatingblock slice's non-strict tolerance), so the
// resulting LUFS value can move a bit more than the strict 1e-9 bound
// allows. 1e-6 LU is still six orders tighter than the EBU ±0.1 LU
// compliance requirement.
const loudnessNonStrictAbsTolerance = 1e-6

// activeLoudnessTol returns the LU tolerance for the currently active
// parity mode.
func activeLoudnessTol() float64 {
	if strictparity.Enabled {
		return loudnessAbsTolerance
	}
	return loudnessNonStrictAbsTolerance
}

// goIntegrated drives the pure-Go r128.State exactly as cgoIntegrated
// drives libebur128.
func goIntegrated(channels, rate, mode int, chunks [][]float64) (integrated, relThreshold float64) {
	st := r128.NewState(channels, rate, mode)
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		st.AddFrames(chunk)
	}
	integ, _ := st.GatedLoudness()
	rel, _ := st.RelativeThreshold()
	return integ, rel
}

// TestGateConstantsParity pins the port's static gating constants to the C
// oracle's. relative_gate_factor (which multiplies into the RELATIVE gate
// threshold, the one that gates real blocks) and minus_twenty_decibels are
// bit-exact — they are pow(10,-1) and pow(10,-2), where Go's math.Pow and
// libm agree exactly. The absolute-gate energy boundary0 = pow(10,-6.9309)
// differs by up to a handful of ULP: a pure math.Pow-vs-libm transcendental
// gap at that exponent (empirically ~18 ULP on the reference toolchain).
// That boundary only decides whether a block sits above -70 LUFS, so it can
// only change a result for a block whose energy lands within ~18 ULP of
// 1.17e-7 — which no real signal does. The end-to-end integrated tests
// below confirm it does not perturb any actual reading.
//
// This assertion (and TestHistogramTablesParity below) stays unconditional
// on strictparity.Enabled: each value is a single pow() call with no surrounding
// multiply-add chain, so -ffp-contract=off has nothing to fuse — the
// -ffp-contract flag governs contraction of separate multiply/add
// expressions into an FMA instruction, not the internal implementation of a
// library call. Empirically these pass identically with and without
// -tags=loudness_strict.
const boundary0ULPTolerance = 32

func TestGateConstantsParity(t *testing.T) {
	cRel, cMinus, cB0 := cgoGateConstants()
	gRel, gMinus, gB0 := r128.GateConstants()
	require.Truef(t, bitsEqual(cRel, gRel), "relative_gate_factor must be bit-exact: C=%016x Go=%016x", math.Float64bits(cRel), math.Float64bits(gRel))
	require.Truef(t, bitsEqual(cMinus, gMinus), "minus_twenty_decibels must be bit-exact: C=%016x Go=%016x", math.Float64bits(cMinus), math.Float64bits(gMinus))
	require.LessOrEqualf(t, ulpDiff(cB0, gB0), int64(boundary0ULPTolerance),
		"absolute-gate boundary0 (pow transcendental) off by %d ULP (>%d): C=%016x Go=%016x",
		ulpDiff(cB0, gB0), boundary0ULPTolerance, math.Float64bits(cB0), math.Float64bits(gB0))
}

// TestHistogramTablesParity checks the 1000-bin histogram tables against
// the C oracle across a spread of bins. Every entry is pow(10, ...) at a
// bin-dependent exponent, so like boundary0 they carry a small math.Pow-vs-
// libm ULP gap (never a structural difference); the bound names that
// transcendental. The bit arithmetic that USES these tables
// (find_histogram_index) is exercised end-to-end by the histogram-mode
// integrated tests below.
const histTableULPTolerance = 32

func TestHistogramTablesParity(t *testing.T) {
	for _, i := range []int{0, 1, 250, 500, 750, 999} {
		d := ulpDiff(cgoHistEnergy(i), r128.HistogramEnergy(i))
		require.LessOrEqualf(t, d, int64(histTableULPTolerance),
			"histogram_energies[%d] off by %d ULP (>%d)", i, d, histTableULPTolerance)
	}
	for _, i := range []int{0, 1, 250, 500, 1000} {
		d := ulpDiff(cgoHistBoundary(i), r128.HistogramBoundary(i))
		require.LessOrEqualf(t, d, int64(histTableULPTolerance),
			"histogram_energy_boundaries[%d] off by %d ULP (>%d)", i, d, histTableULPTolerance)
	}
}

// chunkStream splits an interleaved stream into a few chunks (frame-aligned)
// so the add_frames loop crosses buffer boundaries; both oracles get the
// identical split.
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

func TestIntegratedLoudnessParity(t *testing.T) {
	const rate = 48000
	si := (rate + 5) / 10
	block := si * 4 // 400 ms in frames

	// Stream builders, given channel count. Each returns interleaved PCM.
	tone := func(channels, frames int, amp, freq float64) []float64 {
		out := make([]float64, frames*channels)
		for n := 0; n < frames; n++ {
			v := amp * math.Sin(2*math.Pi*freq*float64(n)/rate)
			for c := 0; c < channels; c++ {
				out[n*channels+c] = v
			}
		}
		return out
	}

	type streamCase struct {
		name  string
		build func(channels int) []float64
	}
	streams := []streamCase{
		{"silence-2s", func(ch int) []float64 { return make([]float64, rate*2*ch) }},
		{"single-block", func(ch int) []float64 { return tone(ch, block, 0.5, 997) }},
		{"block-minus-1", func(ch int) []float64 { return tone(ch, block-1, 0.5, 997) }},
		{"exactly-1s", func(ch int) []float64 { return tone(ch, rate, 0.5, 997) }},
		{"mixed-loud-quiet", func(ch int) []float64 {
			// 10 loud blocks then 10 quiet blocks: the quiet ones fall below
			// the relative gate and must be excluded identically.
			loud := tone(ch, block*10, 0.8, 500)
			quiet := tone(ch, block*10, 0.02, 500)
			return append(loud, quiet...)
		}},
		{"random-3s", func(ch int) []float64 {
			rng := rand.New(rand.NewPCG(0xA11CE, 0x1770))
			out := make([]float64, rate*3*ch)
			for i := range out {
				out[i] = (rng.Float64()*2 - 1) * 0.6
			}
			return out
		}},
	}

	modes := []struct {
		name string
		mode int
	}{
		{"list", modeI},
		{"histogram", modeIHistogram},
	}
	channelCounts := []int{1, 2}
	chunkSizes := []int{block, 7, 1024, si} // varied, frame-aligned

	for _, sc := range streams {
		for _, m := range modes {
			for _, ch := range channelCounts {
				sc, m, ch := sc, m, ch
				t.Run(fmt.Sprintf("%s/%s/ch=%d", sc.name, m.name, ch), func(t *testing.T) {
					src := sc.build(ch)
					chunks := chunkStream(src, ch, chunkSizes)

					wantI, wantR, err := cgoIntegrated(ch, rate, m.mode, chunks)
					require.NoError(t, err)
					gotI, gotR := goIntegrated(ch, rate, m.mode, chunks)

					// Integrated loudness: -Inf bit-exact, else within the
					// active absolute LU bound (tight under strictparity.Enabled,
					// widened otherwise).
					tol := activeLoudnessTol()
					if math.IsInf(wantI, -1) || math.IsInf(gotI, -1) {
						require.Truef(t, bitsEqual(wantI, gotI),
							"integrated -Inf mismatch: C=%v Go=%v", wantI, gotI)
					} else {
						require.InDeltaf(t, wantI, gotI, tol,
							"integrated LUFS off by %.3g LU (>%.0e): C=%.17g Go=%.17g",
							math.Abs(wantI-gotI), tol, wantI, gotI)
					}

					// Relative threshold: -70.0 bit-exact when no block
					// passed the absolute gate, else within the active bound.
					if wantR == -70.0 || gotR == -70.0 {
						require.Truef(t, bitsEqual(wantR, gotR),
							"relative threshold -70 mismatch: C=%v Go=%v", wantR, gotR)
					} else {
						require.InDeltaf(t, wantR, gotR, tol,
							"relative threshold LUFS off by %.3g LU (>%.0e): C=%.17g Go=%.17g",
							math.Abs(wantR-gotR), tol, wantR, gotR)
					}
				})
			}
		}
	}
}
