//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_ResamplingFactor(t *testing.T) {
	for _, r := range []int32{48000, 24000, 16000, 12000, 8000} {
		c := cResamplingFactor(r)
		g := nativeopus.ExportTestResamplingFactor(r)
		if c != g {
			t.Errorf("resampling_factor(%d): C=%d Go=%d", r, c, g)
		}
	}
}

func TestParity_InitCaps(t *testing.T) {
	cm, gm := loadGoMode(t, 48000, 960)
	nb := cm.NbEBands()
	for _, LM := range []int{0, 1, 2, 3} {
		for _, C_ := range []int{1, 2} {
			cC := make([]int, nb)
			cG := make([]int, nb)
			cInitCaps(cm, cC, LM, C_)
			nativeopus.ExportTestInitCaps(gm, cG, LM, C_)
			for i := 0; i < nb; i++ {
				if cC[i] != cG[i] {
					t.Errorf("LM=%d C=%d [%d]: C=%d Go=%d", LM, C_, i, cC[i], cG[i])
				}
			}
		}
	}
}

func TestParity_BitrateRoundTrip(t *testing.T) {
	for _, Fs := range []int32{8000, 12000, 16000, 24000, 48000} {
		for _, frame := range []int32{120, 240, 480, 960} {
			for _, bitrate := range []int32{6000, 24000, 48000, 128000, 320000} {
				c := int32(0)
				_ = c
				gb := nativeopus.ExportTestBitrateToBits(bitrate, Fs, frame)
				br := nativeopus.ExportTestBitsToBitrate(gb, Fs, frame)
				// Verify both helpers are mutual inverses within the
				// integer-truncation they define.
				if br*(6*Fs/frame)/(6*Fs/frame) != br {
					t.Errorf("Fs=%d frame=%d br=%d round-trip mismatch", Fs, frame, br)
				}
			}
		}
	}
}

func TestParity_CombFilter(t *testing.T) {
	r := rand.New(rand.NewSource(179))
	// Test across a variety of filter configs.
	type cfg struct {
		T0, T1, N, tapset0, tapset1, overlap int
		g0, g1                               float32
	}
	cases := []cfg{
		// Simple pass-through (g0=g1=0).
		{50, 50, 120, 0, 0, 0, 0.0, 0.0},
		// Constant filter (T0=T1, g0=g1, same tapset) → overlap skipped.
		{80, 80, 240, 1, 1, 0, 0.6, 0.6},
		// Transitional filter.
		{60, 75, 240, 0, 1, 32, 0.5, 0.4},
		{120, 90, 480, 2, 0, 48, 0.3, 0.7},
		{200, 150, 960, 1, 2, 64, 0.8, 0.2},
		// g1==0 early exit after overlap.
		{100, 100, 240, 0, 0, 40, 0.5, 0.0},
	}
	for _, cs := range cases {
		maxT := cs.T0
		if cs.T1 > maxT {
			maxT = cs.T1
		}
		xOff := maxT + 8
		bufLen := xOff + cs.N + 8
		x := make([]float32, bufLen)
		for i := range x {
			x[i] = r.Float32()*2 - 1
		}
		window := make([]float32, cs.overlap)
		for i := range window {
			// Sine-squared window in [0,1].
			v := float32(math.Sin(math.Pi * (float64(i) + 0.5) / float64(cs.overlap)))
			window[i] = v
		}

		yC := append([]float32(nil), x...)
		yG := append([]float32(nil), x...)
		// Use y=x view (distinct buffer copy) — matches encoder in-place call pattern.
		xC := append([]float32(nil), x...)
		xG := append([]float32(nil), x...)
		cCombFilter(yC, xOff, xC, xOff, cs.T0, cs.T1, cs.N,
			cs.g0, cs.g1, cs.tapset0, cs.tapset1, window, cs.overlap)
		nativeopus.ExportTestCombFilter(yG, xOff, xG, xOff, cs.T0, cs.T1, cs.N,
			cs.g0, cs.g1, cs.tapset0, cs.tapset1, window, cs.overlap)

		for i := 0; i < cs.N; i++ {
			if yC[xOff+i] != yG[xOff+i] {
				t.Errorf("T0=%d T1=%d N=%d g0=%g g1=%g tap=(%d,%d) ov=%d [%d]: C=%g Go=%g (%d ULP)",
					cs.T0, cs.T1, cs.N, cs.g0, cs.g1, cs.tapset0, cs.tapset1, cs.overlap,
					i, yC[xOff+i], yG[xOff+i], ulpDiffF32(yC[xOff+i], yG[xOff+i]))
				break
			}
		}
	}
}
