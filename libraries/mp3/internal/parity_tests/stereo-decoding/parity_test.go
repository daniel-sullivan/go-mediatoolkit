//go:build cgo

package stereodecoding

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// requireStrict skips a parity test unless the mp3_strict build tag is set.
// The stereo slice's reconstruction (mid/side a+b / a-b sums and the kl / kr
// intensity weights) is floating point, so its output only matches the cgo
// oracle bit-for-bit when the strict, FMA-free Go build is paired with the
// scalar (-ffp-contract=off, -fno-vectorize, …) cgo oracle the mise `parity`
// task configures. A bare `go test` builds the default (FMA-fusing) helpers and
// would diverge in the last ULP, so the assertions are gated to the canonical
// `mise run //libraries/mp3:parity` gate. See the FP-parity convention in the
// add-audio-format skill.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("stereo-decoding parity asserts bit-exactness; run under -tags=mp3_strict (mise run //libraries/mp3:parity)")
	}
}

// makeGranule fills a 1152-float granule buffer (left at [0..575], right at
// [576..1151]) with deterministically seeded values in [-1, 1). Distinct,
// non-zero content in both channels exercises every mid/side and intensity
// arithmetic path; a misaligned channel split would surface as a wrong
// magnitude on either side.
func makeGranule(seed uint64) []float32 {
	r := rand.New(rand.NewPCG(seed, seed+1))
	g := make([]float32, 1152)
	for i := range g {
		g[i] = r.Float32()*2 - 1
	}
	return g
}

// header4 builds a synthetic 4-byte MPEG frame header carrying just the two
// bits the stereo functions consult: HDR_TEST_MPEG1 (h[1] & 0x8) and
// HDR_TEST_MS_STEREO (h[3] & 0x20). The other bits are irrelevant to the
// stereo slice, so a minimal header is sufficient for both the C oracle and
// the Go port (both read only these masks).
func header4(mpeg1, msStereo bool) []byte {
	h := []byte{0xff, 0xe0, 0x00, 0x00}
	if mpeg1 {
		h[1] |= 0x08
	}
	if msStereo {
		h[3] |= 0x20
	}
	return h
}

// ── L3_midside_stereo ───────────────────────────────────────────────

// TestL3MidsideStereoParity sweeps the mid/side reconstruction across band
// widths (including non-multiple-of-4 tails the scalar reference falls through
// to) and asserts the full 1152-sample granule buffer matches bit-for-bit.
func TestL3MidsideStereoParity(t *testing.T) {
	requireStrict(t)
	for _, n := range []int{0, 1, 2, 3, 4, 5, 7, 8, 18, 36, 192, 576} {
		for _, seed := range []uint64{1, 2, 3} {
			g := makeGranule(seed*100 + uint64(n))

			cOut := cgoL3MidsideStereo(g, n)

			goOut := append([]float32(nil), g...)
			nativemp3.L3MidsideStereo(goOut, 0, n)

			assert.Equalf(t, cOut, goOut, "n=%d seed=%d", n, seed)
		}
	}
}

// ── L3_intensity_stereo_band ────────────────────────────────────────

// TestL3IntensityStereoBandParity sweeps the intensity-band weighting across
// band widths and a corpus of (kl, kr) weights drawn from the MPEG-1 g_pan
// table and the MPEG-2 L3_ldexp_q2 ladder, asserting the granule buffer
// matches bit-for-bit. The weights are exactly the values the real stereo
// process feeds this function, so the float32 multiplies round identically.
func TestL3IntensityStereoBandParity(t *testing.T) {
	requireStrict(t)
	type kw struct{ kl, kr float32 }
	weights := []kw{
		{0, 1}, {1, 0}, {0.5, 0.5},
		{0.21132487, 0.78867513}, {0.36602540, 0.63397460},
		// MPEG-2 ladder values via L3_ldexp_q2, including the MS sqrt2 scale.
		{1, nativemp3.L3Ldexp(1, 1)}, {nativemp3.L3Ldexp(1, 2), 1},
		{1.41421356 * 0.5, 1.41421356 * 0.5},
	}
	for _, w := range weights {
		for _, n := range []int{0, 1, 4, 12, 18, 192, 576} {
			g := makeGranule(uint64(n) + 7)

			cOut := cgoL3IntensityStereoBand(g, n, w.kl, w.kr)

			goOut := append([]float32(nil), g...)
			nativemp3.L3IntensityStereoBand(goOut, 0, n, w.kl, w.kr)

			assert.Equalf(t, cOut, goOut, "kl=%v kr=%v n=%d", w.kl, w.kr, n)
		}
	}
}

// ── L3_stereo_top_band ──────────────────────────────────────────────

// TestL3StereoTopBandParity asserts the integer top-band scan matches the C
// reference across a few band layouts, including a fully-silent right channel
// (all max_band stay -1) and a layout with non-zero content seeded at known
// bands so the per-window (i % 3) result is exercised. This is integer/control
// flow only, so it matches in both build modes, but gates with the suite.
func TestL3StereoTopBandParity(t *testing.T) {
	requireStrict(t)

	build := func(sfb []byte, nonzeroBands map[int]bool) []float32 {
		// right channel occupies [0..575] of the slice we pass (the caller of
		// the real function passes grbuf+576; here `right` is that subslice).
		right := make([]float32, 576)
		base := 0
		for i, w := range sfb {
			if w == 0 {
				break
			}
			if nonzeroBands[i] {
				// place a non-zero pair at the start of the band
				right[base] = 0.5
			}
			base += int(w)
		}
		return right
	}

	cases := []struct {
		name    string
		sfb     []byte
		nonzero map[int]bool
		nbands  int
	}{
		{"all-silent", []byte{4, 4, 4, 4, 4, 4, 0}, map[int]bool{}, 6},
		{"single-band-2", []byte{4, 4, 4, 4, 4, 4, 0}, map[int]bool{2: true}, 6},
		{"per-window", []byte{6, 6, 6, 6, 6, 6, 6, 6, 6, 0}, map[int]bool{3: true, 7: true, 8: true}, 9},
		{"top-of-each", []byte{4, 4, 4, 4, 4, 4, 4, 4, 4, 0}, map[int]bool{6: true, 7: true, 8: true}, 9},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			right := build(c.sfb, c.nonzero)

			cMB := cgoL3StereoTopBand(right, c.sfb, c.nbands)

			var goMB [3]int
			nativemp3.L3StereoTopBand(right, 0, c.sfb, c.nbands, &goMB)

			assert.Equalf(t, cMB, [3]int{goMB[0], goMB[1], goMB[2]}, "%s", c.name)
		})
	}
}

// ── L3_stereo_process ───────────────────────────────────────────────

// TestL3StereoProcessParity drives the band-by-band stereo processor across
// the MPEG-1/MPEG-2 and MS/non-MS header combinations, with intensity
// positions chosen to exercise both the intensity branch (ipos < max_pos and
// band above the window top) and the mid/side fallback, asserting the full
// granule buffer matches bit-for-bit.
func TestL3StereoProcessParity(t *testing.T) {
	requireStrict(t)

	// A band layout terminated by the 0 sentinel L3_stereo_process loops on.
	sfb := []byte{4, 4, 4, 4, 4, 4, 4, 4, 0}
	nbands := 8

	specs := []struct {
		name     string
		mpeg1    bool
		msStereo bool
		mpeg2Sh  int
		istPos   []byte
		maxBand  [3]int
	}{
		{"mpeg1-ms-intensity", true, true, 0, []byte{0, 1, 2, 3, 4, 5, 6, 0, 0}, [3]int{-1, -1, -1}},
		{"mpeg1-ms-mixed", true, true, 0, []byte{7, 7, 2, 7, 4, 7, 6, 0, 0}, [3]int{1, 2, 0}},
		{"mpeg1-noms", true, false, 0, []byte{0, 1, 2, 3, 4, 5, 6, 0, 0}, [3]int{-1, -1, -1}},
		{"mpeg2-ms", false, true, 0, []byte{0, 1, 2, 3, 4, 5, 6, 0, 0}, [3]int{-1, -1, -1}},
		{"mpeg2-ms-shift", false, true, 1, []byte{1, 3, 5, 7, 9, 11, 13, 0, 0}, [3]int{-1, -1, -1}},
		{"mpeg2-noms", false, false, 0, []byte{0, 2, 4, 6, 8, 10, 12, 0, 0}, [3]int{0, 0, 0}},
	}

	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			g := makeGranule(uint64(len(s.name)) + 11)
			hdr := header4(s.mpeg1, s.msStereo)
			istPos := append([]byte(nil), s.istPos...)
			_ = nbands

			cOut := cgoL3StereoProcess(g, istPos, sfb, hdr, s.maxBand, s.mpeg2Sh)

			goOut := append([]float32(nil), g...)
			goMB := s.maxBand
			nativemp3.L3StereoProcess(goOut, append([]byte(nil), istPos...), sfb, hdr, &goMB, s.mpeg2Sh)

			assert.Equalf(t, cOut, goOut, "%s", s.name)
		})
	}
}

// ── L3_intensity_stereo (full granule driver) ───────────────────────

// TestL3IntensityStereoParity drives the full per-granule stereo
// reconstruction (top-band scan + trailing intensity-position fixup +
// stereo_process) and asserts both the reconstructed granule buffer and the
// mutated ist_pos array match the C reference bit-for-bit, across the
// MPEG-1/MPEG-2 and long/short (n_short_sfb 0 vs non-zero) combinations.
func TestL3IntensityStereoParity(t *testing.T) {
	requireStrict(t)

	specs := []struct {
		name      string
		mpeg1     bool
		msStereo  bool
		nLongSfb  uint8
		nShortSfb uint8
		gr1Sc     uint16
		seed      uint64
	}{
		{"mpeg1-long", true, true, 8, 0, 0, 1},
		{"mpeg1-long-noms", true, false, 8, 0, 0, 2},
		{"mpeg1-short", true, true, 0, 6, 0, 3},
		{"mpeg1-mixed", true, true, 2, 6, 0, 4},
		{"mpeg2-long", false, true, 8, 0, 1, 5},
		{"mpeg2-short", false, true, 0, 6, 1, 6},
		{"mpeg2-mixed-shift", false, true, 2, 6, 1, 7},
	}

	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			g := makeGranule(s.seed * 13)
			hdr := header4(s.mpeg1, s.msStereo)

			nSfb := int(s.nLongSfb) + int(s.nShortSfb)
			// Band-width table: nSfb bands of width 4 then a 0 sentinel, sized
			// so the right channel ([576..1151]) and the intensity fixup stay
			// in range. ist_pos has one entry per band.
			sfb := make([]byte, 64)
			for i := 0; i < nSfb && i < len(sfb); i++ {
				sfb[i] = 4
			}
			istPos := make([]byte, 64)
			r := rand.New(rand.NewPCG(s.seed, s.seed+99))
			for i := 0; i < nSfb; i++ {
				istPos[i] = byte(r.Uint32() % 8)
			}

			cOut, cIst := cgoL3IntensityStereo(g, istPos, sfb, s.nLongSfb, s.nShortSfb, s.gr1Sc, hdr)

			// Go port: assemble gr[0] from the discrete fields and gr[1] carrying
			// only the scalefac_compress the mpeg2_sh derivation reads.
			goOut := append([]float32(nil), g...)
			goIst := append([]byte(nil), istPos...)
			gr := []nativemp3.L3GrInfo{
				{Sfbtab: sfb, NLongSfb: s.nLongSfb, NShortSfb: s.nShortSfb},
				{ScalefacCompress: s.gr1Sc},
			}
			nativemp3.L3IntensityStereo(goOut, goIst, gr, hdr)

			require.Equal(t, cIst, goIst, "%s ist_pos mismatch", s.name)
			assert.Equalf(t, cOut, goOut, "%s granule mismatch", s.name)
		})
	}
}
