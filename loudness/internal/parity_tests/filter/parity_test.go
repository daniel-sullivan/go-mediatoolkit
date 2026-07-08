//go:build cgo

package filter

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/parity_tests/strictparity"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/r128"
)

// allRates spans every sample-rate class libebur128 handles: the
// coefficient derivation (tan/pow through the bilinear transform) must be
// bit-exact against the C oracle at each.
var allRates = []int{8000, 11025, 16000, 22050, 32000, 44100, 48000, 88200, 96000, 176400, 192000}

// bitsEqual reports whether two float64s are identical down to the bit
// pattern (so -0.0 != 0.0, NaN payloads matter). Bit-exactness is the
// default bar for these parity tests.
func bitsEqual(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b)
}

// ulpDiff returns the distance between a and b in units in the last
// place, for diagnostics when a bit-exact assertion fails.
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

// coeffULPTolerance is the ONE relaxed bound in this slice, and it
// applies only to the coefficient derivation — never to filtered output.
//
// The K-weighting coefficients are computed by the bilinear transform,
// whose only non-arithmetic inputs are math.Tan (the two stage prewarp
// frequencies) and math.Pow (the shelf gains Vh, Vb). All the arithmetic
// around them is reproduced bit-for-bit — every multiply-accumulate in
// the port carries an explicit float64() conversion barrier so the arm64
// compiler cannot fuse it into an FMA and diverge from the
// -ffp-contract=off C oracle (verified: op-order and barriers are
// exhausted; the remaining gap is not arithmetic). What remains is that
// Go's pure-software math.Tan / math.Pow and the platform libm disagree
// by at most one unit in the last place at a handful of inputs.
//
// Empirically on the reference toolchains the derivation is bit-exact at
// 8 of the 11 rate classes (including the canonical 48 kHz self-check,
// which reproduces BS.1770-4 Table 1 exactly) and off by exactly 1 ULP
// at 16000, 88200, and 192000 Hz — a tan/pow last-bit rounding
// difference, not a port defect. Per the plan's transcendental-ULP
// policy this assertion is bounded at 1 ULP with this rationale rather
// than blanket-epsilon'd, and the bound is checked to be tight (no
// coefficient may exceed it).
//
// This relaxation is deliberately confined to the coefficients.
// TestKFilterOutputParity stays bit-exact because its rate set
// (44100/48000/96000) is entirely within the bit-exact-coefficient
// subset, so the feedback filter never amplifies a coefficient ULP.
const coeffULPTolerance = 1

// coeffNonStrictRelTolerance is the non-strict fallback bound for
// coefficient parity, used when strictparity.Enabled is false — i.e. the C oracle
// was NOT necessarily built with -ffp-contract=off (e.g. plain `go test
// ./...` in CI, without the //loudness:parity / //loudness:test mise
// task's CGO_CFLAGS). Without that flag the compiler may fuse the bilinear
// transform's multiply-adds into FMAs, drifting the derived coefficients by
// more than the 1-ULP libm-only bound above. Empirically the FMA-fused
// drift observed here is on the order of 1e-13 relative (see
// TestKFilterCoefficientParity/8000Hz, b[1] off by 2 ULP without the tag);
// 1e-9 leaves several orders of margin while still catching a real port
// defect.
const coeffNonStrictRelTolerance = 1e-9

// coeffNonStrictAbsFloor anchors the non-strict tolerance for near-zero
// coefficients, where a purely relative tolerance would divide by ~0. Same
// order of magnitude as coeffNonStrictRelTolerance.
const coeffNonStrictAbsFloor = 1e-9

// assertCoeffParity asserts filter-coefficient parity for one coefficient.
// Under strictparity.Enabled (the C oracle built with -ffp-contract=off, via the
// //loudness:parity / //loudness:test mise tasks) it keeps the exact
// documented 1-ULP tan/pow libm exception. Otherwise it falls back to a
// combined relative+absolute tolerance loose enough to absorb FMA-fused
// oracle drift but tight enough to still catch a real divergence.
func assertCoeffParity(t *testing.T, label string, want, got float64) {
	t.Helper()
	if strictparity.Enabled {
		d := ulpDiff(want, got)
		require.LessOrEqualf(t, d, int64(coeffULPTolerance),
			"%s off by %d ULP (>%d): C=%x Go=%x (%v vs %v)",
			label, d, coeffULPTolerance, math.Float64bits(want), math.Float64bits(got), want, got)
		return
	}
	diff := math.Abs(want - got)
	bound := coeffNonStrictAbsFloor
	if rel := coeffNonStrictRelTolerance * math.Max(math.Abs(want), math.Abs(got)); rel > bound {
		bound = rel
	}
	require.LessOrEqualf(t, diff, bound,
		"%s off by %.3g (non-strict bound %.3g): C=%v Go=%v", label, diff, bound, want, got)
}

func TestKFilterCoefficientParity(t *testing.T) {
	for _, rate := range allRates {
		t.Run(rateName(rate), func(t *testing.T) {
			// channels do not affect the coefficients; use 2.
			wantB, wantA, err := cgoKFilterCoeffs(2, rate)
			require.NoError(t, err)

			f := r128.NewKFilter(rate, 2)
			for i := 0; i < 5; i++ {
				assertCoeffParity(t, fmt.Sprintf("b[%d] at %d Hz", i, rate), wantB[i], f.B[i])
				assertCoeffParity(t, fmt.Sprintf("a[%d] at %d Hz", i, rate), wantA[i], f.A[i])
			}
		})
	}
}

// outputNonStrictRelTolerance / outputNonStrictAbsFloor bound the filtered
// PCM output under strictparity.Enabled == false, for the same reason as the
// coefficient tolerances above: without a guaranteed -ffp-contract=off
// oracle, the IIR filter's multiply-accumulate chain may be FMA-fused on
// the C side, drifting from the FMA-free Go port by a handful of ULP per
// sample. Because the filter is recursive (each output feeds back into the
// next), that tiny per-sample drift compounds over the run: an exhaustive
// scan of this test's exact rate/channel/chunk/seed matrix (the full
// ~10,000-frame makeSignal, not just the first divergence) found the
// absolute drift growing to a worst case of ~2.1e-9 during the near-
// subnormal decay tail — where both sides are ringing down through values
// near zero, so a purely relative comparison blows up (up to ~9% relative)
// despite the absolute difference staying tiny. outputNonStrictAbsFloor is
// set well above that observed ceiling so the near-zero regime doesn't spike
// the bound-check, while outputNonStrictRelTolerance still catches a real
// divergence at O(1) signal magnitudes (observed relative drift there is
// ~1e-14, four orders below this bound).
const outputNonStrictRelTolerance = 1e-9
const outputNonStrictAbsFloor = 5e-8

// samplesMatch reports whether a filtered-output sample pair satisfies the
// active parity bound: bit-exact under strictparity.Enabled, else the combined
// relative+absolute non-strict tolerance.
func samplesMatch(want, got float64) bool {
	if strictparity.Enabled {
		return bitsEqual(want, got)
	}
	diff := math.Abs(want - got)
	bound := outputNonStrictAbsFloor
	if rel := outputNonStrictRelTolerance * math.Max(math.Abs(want), math.Abs(got)); rel > bound {
		bound = rel
	}
	return diff <= bound
}

// TestKFilterOutputParity checks the filtered output against the C
// ebur128_filter_double path across sample rates, channel counts, and chunk
// sizes, over random PCM that includes long silences and near-subnormal
// tails to exercise the manual flush-to-zero denormal guard. The
// comparison is bit-exact under strictparity.Enabled, or within a tight relative
// tolerance otherwise (see samplesMatch).
func TestKFilterOutputParity(t *testing.T) {
	rates := []int{44100, 48000, 96000}
	channelCounts := []int{1, 2, 5}
	chunkSizes := []int{1, 7, 113, 4800}

	seed := uint64(0)
	for _, rate := range rates {
		for _, channels := range channelCounts {
			for _, chunk := range chunkSizes {
				rate, channels, chunk := rate, channels, chunk
				seed++
				name := rateName(rate) + "/" + chanName(channels) + "/" + chunkName(chunk)
				t.Run(name, func(t *testing.T) {
					// Deterministic per-case PCG stream.
					rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
					src := makeSignal(rng, channels)

					goFilter := r128.NewKFilter(rate, channels)
					cFilter := newCgoKFilter(channels, rate)
					defer cFilter.Free()

					frameSamples := channels
					total := len(src)
					step := chunk * frameSamples
					for off := 0; off < total; off += step {
						end := off + step
						if end > total {
							end = total
						}
						in := src[off:end]

						goOut := make([]float64, len(in))
						goFilter.FilterInto(goOut, in)
						cOut := cFilter.chunk(in)

						require.Equal(t, len(cOut), len(goOut))
						for i := range goOut {
							if !samplesMatch(cOut[i], goOut[i]) {
								frame := (off + i) / frameSamples
								ch := i % frameSamples
								t.Fatalf("output mismatch at abs sample %d (frame %d, ch %d): C=%x Go=%x (%v vs %v, %d ULP)",
									off+i, frame, ch,
									math.Float64bits(cOut[i]), math.Float64bits(goOut[i]),
									cOut[i], goOut[i], ulpDiff(cOut[i], goOut[i]))
							}
						}
					}
				})
			}
		}
	}
}

// makeSignal builds an interleaved test signal with three regimes: a
// loud random burst (amplitudes up to ±1.2, i.e. past full scale), a
// long silence over which the IIR state rings down into the subnormal
// range, and a near-subnormal tail that feeds tiny inputs directly — so
// the manual flush-to-zero guard fires in both the state-decay and
// tiny-input paths. Each channel gets an independent stream, which also
// stresses per-channel state independence.
func makeSignal(rng *rand.Rand, channels int) []float64 {
	const (
		burst   = 2000
		silence = 6000
		tiny    = 2000
	)
	frames := burst + silence + tiny
	out := make([]float64, frames*channels)
	for i := 0; i < frames; i++ {
		for c := 0; c < channels; c++ {
			var v float64
			switch {
			case i < burst:
				v = (rng.Float64()*2 - 1) * 1.2
			case i < burst+silence:
				v = 0.0
			default:
				// Near-subnormal inputs: |v| ~ 1e-300, well below
				// DBL_MIN once the filter attenuates them, provoking the
				// FTZ guard on the freshly computed state.
				v = (rng.Float64()*2 - 1) * 1e-300
			}
			out[i*channels+c] = v
		}
	}
	return out
}

func rateName(rate int) string {
	switch rate {
	case 8000:
		return "8000Hz"
	case 11025:
		return "11025Hz"
	case 16000:
		return "16000Hz"
	case 22050:
		return "22050Hz"
	case 32000:
		return "32000Hz"
	case 44100:
		return "44100Hz"
	case 48000:
		return "48000Hz"
	case 88200:
		return "88200Hz"
	case 96000:
		return "96000Hz"
	case 176400:
		return "176400Hz"
	case 192000:
		return "192000Hz"
	default:
		return "unknownHz"
	}
}

func chanName(ch int) string {
	switch ch {
	case 1:
		return "mono"
	case 2:
		return "stereo"
	case 5:
		return "5ch"
	default:
		return "nch"
	}
}

func chunkName(chunk int) string {
	switch chunk {
	case 1:
		return "chunk1"
	case 7:
		return "chunk7"
	case 113:
		return "chunk113"
	case 4800:
		return "chunk4800"
	default:
		return "chunkN"
	}
}
