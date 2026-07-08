//go:build cgo

package e2e

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/parity_tests/strictparity"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/r128"
)

// Raw ebur128 mode / channel values (ebur128.h). Not imported from package
// loudness: a parity slice must not link that package's copy of the oracle.
const (
	modeAll     = 63  // ModeIntegrated|ModeLRA|ModeTruePeak
	modeAllHist = 127 // modeAll | EBUR128_MODE_HISTOGRAM

	chUnused   = 0
	chLeft     = 1
	chRight    = 2
	chCenter   = 3
	chLs       = 4
	chRs       = 5
	chDualMono = 6
)

// loudnessTol returns the absolute LU tolerance for the log-domain loudness
// readings at a given sample rate, for the currently active parity mode.
//
// Under strictparity.Enabled: at 8000/44100/48000/96000 Hz the K-weighting
// coefficients are bit-exact against the oracle (Go math.Tan/math.Pow agree
// with libm there — see the filter slice), so the energies are bit-exact
// and the ONLY loudness error is ebur128_energy_to_loudness's log: 1e-9 LU
// covers it (peaks are exact). At 192000 Hz the coefficient derivation
// carries a 1-ULP tan/pow transcendental difference (the filter slice
// documents this exact rate as off-by-1-ULP). A 1-ULP coefficient
// perturbation in the stable K-weighting IIR compounds across the 400 ms/3 s
// energy sums into a slightly larger — but still ~1e-9..1e-8 LU — drift on
// the loudness readings only. Bound it at 1e-5 LU: four orders below the EBU
// ±0.1 LU compliance requirement.
//
// Without strictparity.Enabled (e.g. plain `go test ./...` in CI, no guaranteed
// -ffp-contract=off oracle), every rate's gated energy accumulation may
// additionally carry a little FMA-fused drift, so the sub-192kHz bound
// widens to 1e-6 LU (still five orders below the EBU requirement); the
// 192kHz bound is already wide enough to absorb it unchanged. The peaks
// (sample-domain, no filter) stay bit-exact regardless of strictparity.Enabled —
// see comparePeak.
func loudnessTol(rate int) float64 {
	base := 1e-9
	if rate >= 192000 {
		base = 1e-5
	}
	if strictparity.Enabled || base > 1e-6 {
		return base
	}
	return 1e-6
}

func bitsEqual(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }

func compareLoudness(t *testing.T, label string, want, got, tol float64) {
	t.Helper()
	if math.IsInf(want, 0) || math.IsInf(got, 0) {
		require.Truef(t, bitsEqual(want, got), "%s: non-finite mismatch C=%v Go=%v", label, want, got)
		return
	}
	require.InDeltaf(t, want, got, tol,
		"%s off by %.3g LU (>%.0e): C=%.17g Go=%.17g", label, math.Abs(want-got), tol, want, got)
}

// comparePeak asserts the per-channel sample/true peak readings bit-exact,
// unconditionally on strictparity.Enabled: peaks are a plain float64 max over the
// input samples (sample peak) or a float32-domain polyphase interpolation
// promoted to float64 (true peak, proven bit-exact independent of FMA
// settings by the truepeak slice's TestInterpolatorOutputBitExact /
// TestFullTruePeakBitExact) — neither involves the multiply-accumulate
// chains that FMA contraction can affect.
func comparePeak(t *testing.T, label string, want, got []float64) {
	t.Helper()
	require.Len(t, got, len(want), "%s length", label)
	for c := range want {
		require.Truef(t, bitsEqual(want[c], got[c]),
			"%s[ch=%d] mismatch: C=%.17g (%016x) Go=%.17g (%016x)",
			label, c, want[c], math.Float64bits(want[c]), got[c], math.Float64bits(got[c]))
	}
}

func goRunMeter(channels, rate, mode int, overrides []int, winMs, histMs int, chunks [][]float64) cReadings {
	st := r128.NewState(channels, rate, mode)
	for c, v := range overrides {
		if v >= 0 {
			st.SetChannel(c, v)
		}
	}
	if winMs > 0 {
		st.SetMaxWindow(uint64(winMs))
	}
	if histMs > 0 {
		st.SetMaxHistory(uint64(histMs))
	}
	for _, chunk := range chunks {
		if len(chunk) == 0 {
			continue
		}
		st.AddFrames(chunk)
	}

	var out cReadings
	out.momentary, _ = st.LoudnessMomentary()
	out.shortTerm, _ = st.LoudnessShortterm()
	out.integrated, _ = st.GatedLoudness()
	out.lra, _ = st.LoudnessRange()
	out.relThreshold, _ = st.RelativeThreshold()
	out.samplePeak = make([]float64, channels)
	out.prevSample = make([]float64, channels)
	out.truePeak = make([]float64, channels)
	out.prevTrue = make([]float64, channels)
	for c := 0; c < channels; c++ {
		out.samplePeak[c], _ = st.SamplePeak(c)
		out.prevSample[c], _ = st.PrevSamplePeak(c)
		out.truePeak[c], _ = st.TruePeak(c)
		out.prevTrue[c], _ = st.PrevTruePeak(c)
	}
	return out
}

// chunkFrames returns a frame-aligned chunk schedule covering totalFrames.
func chunkFrames(totalFrames, si int, kind string) []int {
	switch kind {
	case "whole":
		return []int{totalFrames}
	case "100ms":
		var out []int
		for pos := 0; pos < totalFrames; pos += si {
			s := si
			if s > totalFrames-pos {
				s = totalFrames - pos
			}
			out = append(out, s)
		}
		return out
	default: // "prime"
		sizes := []int{1, 7, 113, 4801}
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
}

func splitChunks(src []float64, channels int, sizes []int) [][]float64 {
	var chunks [][]float64
	pos := 0
	for _, s := range sizes {
		chunks = append(chunks, src[pos*channels:(pos+s)*channels])
		pos += s
	}
	return chunks
}

// e2eSignal builds a per-channel-varied, per-half-second-amplitude-stepped
// signal so short-term blocks span a loudness range (non-trivial LRA), the
// channels have distinct peaks, and a near-Nyquist component provokes true
// inter-sample peaks. Deterministic per seed.
func e2eSignal(seed uint64, channels, frames, rate int) []float64 {
	rng := rand.New(rand.NewPCG(seed, 0xE2E))
	half := rate / 2
	out := make([]float64, frames*channels)
	for c := 0; c < channels; c++ {
		freq := 300.0 + 137.0*float64(c) // distinct per channel
		phase := 0.19 * float64(c)
		amp := 0.4
		for n := 0; n < frames; n++ {
			if n%half == 0 {
				amp = 0.05 + rng.Float64()*0.75 // step amplitude each half second
			}
			// main tone plus a small near-Nyquist term for inter-sample peaks
			v := amp * (0.85*math.Sin(2*math.Pi*freq*float64(n)/float64(rate)+phase) +
				0.15*math.Sin(2*math.Pi*(float64(rate)/2-500)*float64(n)/float64(rate)))
			out[n*channels+c] = v
		}
	}
	return out
}

type chanCfg struct {
	name      string
	n         int
	overrides []int // per channel; -1 => no override
}

type e2eCase struct {
	rate     int
	cfg      chanCfg
	modeName string
	mode     int
	chunk    string
	setName  string
	winMs    int
	histMs   int
	seconds  int
}

// buildCases assembles a ~50-case sampled matrix over rates × channel
// layouts, rotating mode / chunking / history settings, plus a couple of
// low-rate SetMaxWindow-reallocation cases (kept at 8 kHz mono because of the
// v1.2.6 samplerate*window buffer sizing).
func buildCases() []e2eCase {
	rates := []int{8000, 44100, 48000, 96000, 192000}
	cfgs := []chanCfg{
		{"mono", 1, nil},
		{"dualmono", 1, []int{chDualMono}},
		{"stereo", 2, nil},
		{"5ch", 5, nil},
		{"6ch-ovr", 6, []int{chLeft, chRight, chCenter, chLs, chRs, chUnused}},
	}
	modes := []struct {
		name string
		m    int
	}{{"all", modeAll}, {"all+hist", modeAllHist}}
	chunkings := []string{"whole", "100ms", "prime"}
	settings := []struct {
		name string
		hist int
	}{{"default", 0}, {"hist10s", 10000}}

	var cases []e2eCase
	// Two passes with different rotation offsets so each mode/chunk/setting
	// value recurs across the rate × layout grid.
	for pass := 0; pass < 2; pass++ {
		for ri, rate := range rates {
			for ci, cfg := range cfgs {
				k := ri + ci + 2*pass
				m := modes[(k)%len(modes)]
				chunk := chunkings[(k+pass)%len(chunkings)]
				set := settings[(k)%len(settings)]
				cases = append(cases, e2eCase{
					rate: rate, cfg: cfg,
					modeName: m.name, mode: m.m,
					chunk:   chunk,
					setName: set.name, histMs: set.hist,
					seconds: 5,
				})
			}
		}
	}
	// SetMaxWindow reallocation cases (8 kHz mono keeps the buffer bounded:
	// 8000*3100 frames ~ 198 MB; see windows slice for the sizing quirk).
	cases = append(cases,
		e2eCase{rate: 8000, cfg: cfgs[0], modeName: "all", mode: modeAll, chunk: "prime", setName: "maxwin3100", winMs: 3100, seconds: 5},
		e2eCase{rate: 8000, cfg: cfgs[0], modeName: "all+hist", mode: modeAllHist, chunk: "100ms", setName: "maxwin3100", winMs: 3100, seconds: 5},
	)
	return cases
}

func TestEndToEndParity(t *testing.T) {
	cases := buildCases()
	for idx, tc := range cases {
		idx, tc := idx, tc
		name := fmt.Sprintf("%d/rate=%d/%s/%s/%s/%s", idx, tc.rate, tc.cfg.name, tc.modeName, tc.chunk, tc.setName)
		t.Run(name, func(t *testing.T) {
			frames := tc.rate * tc.seconds
			si := (tc.rate + 5) / 10
			src := e2eSignal(uint64(idx)*1009+1, tc.cfg.n, frames, tc.rate)
			sizes := chunkFrames(frames, si, tc.chunk)
			chunks := splitChunks(src, tc.cfg.n, sizes)

			want, err := cRunMeter(tc.cfg.n, tc.rate, tc.mode, tc.cfg.overrides, tc.winMs, tc.histMs, chunks)
			require.NoError(t, err)
			got := goRunMeter(tc.cfg.n, tc.rate, tc.mode, tc.cfg.overrides, tc.winMs, tc.histMs, chunks)

			compareLoudness(t, "momentary", want.momentary, got.momentary, loudnessTol(tc.rate))
			compareLoudness(t, "shortterm", want.shortTerm, got.shortTerm, loudnessTol(tc.rate))
			compareLoudness(t, "integrated", want.integrated, got.integrated, loudnessTol(tc.rate))
			compareLoudness(t, "lra", want.lra, got.lra, loudnessTol(tc.rate))
			compareLoudness(t, "relThreshold", want.relThreshold, got.relThreshold, loudnessTol(tc.rate))
			comparePeak(t, "samplePeak", want.samplePeak, got.samplePeak)
			comparePeak(t, "prevSample", want.prevSample, got.prevSample)
			comparePeak(t, "truePeak", want.truePeak, got.truePeak)
			comparePeak(t, "prevTrue", want.prevTrue, got.prevTrue)
		})
	}
}
