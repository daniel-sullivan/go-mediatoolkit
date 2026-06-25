//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func sliceF32Equal(t *testing.T, name string, want, got []float32) bool {
	t.Helper()
	return sliceF32EqualTol(t, name, want, got, 0)
}

// sliceF32EqualTol allows up to `tolUlps` ULP drift. A small tolerance
// is required on functions that compose several float accumulation
// loops because clang's `-O2` does additional loop optimizations
// (beyond vectorization / SLP / unrolling) — likely LoopIdiomRecognize
// rewriting reduction patterns — that we can't fully disable via cgo
// CFLAGS without giving up FMA fusion. The drift we see is <= 1 ULP
// per accumulated product. Whether it cascades into actual packet-
// level divergence will be verified by the end-to-end round-trip
// tests in Phase 11.
func sliceF32EqualTol(t *testing.T, name string, want, got []float32, tolUlps int32) bool {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("%s: length want=%d got=%d", name, len(want), len(got))
		return false
	}
	for i := range want {
		w := math.Float32bits(want[i])
		g := math.Float32bits(got[i])
		diff := int32(g) - int32(w)
		if diff < 0 {
			diff = -diff
		}
		if diff > tolUlps {
			t.Errorf("%s[%d]: want %g (0x%08x) got %g (0x%08x) diff %d ULP (tol %d)",
				name, i, want[i], w, got[i], g, diff, tolUlps)
			return false
		}
	}
	return true
}

// f32EqualTol — single-value ULP check.
func f32EqualTol(t *testing.T, name, args string, want, got float32, tolUlps int32) {
	t.Helper()
	w := math.Float32bits(want)
	g := math.Float32bits(got)
	diff := int32(g) - int32(w)
	if diff < 0 {
		diff = -diff
	}
	if diff > tolUlps {
		t.Errorf("%s(%s): want %g (0x%08x) got %g (0x%08x) diff %d ULP (tol %d)",
			name, args, want, w, got, g, diff, tolUlps)
	}
}

// fmtIntPair — tiny helper for test labels without pulling in fmt's
// locale-sensitive printing.
func fmtIntPair(a, b int) string {
	return sprintfDec32(int32(a)) + "," + sprintfDec32(int32(b))
}

// rand signal filling with deterministic seed.
func randSig(r *rand.Rand, n int, amp float32) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = (r.Float32()*2 - 1) * amp
	}
	return out
}

func TestParity_XcorrKernel(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for _, ln := range []int{3, 4, 7, 8, 15, 16, 32, 96, 100, 160} {
		for run := 0; run < 5; run++ {
			x := randSig(r, ln, 0.5)
			y := randSig(r, ln+4, 0.5)
			cSum := []float32{0.1, -0.2, 0.3, -0.4}
			goSum := []float32{0.1, -0.2, 0.3, -0.4}
			cXcorrKernel(x, y, cSum, ln)
			nativeopus.ExportTestXcorrKernelC(x, y, goSum, ln)
			sliceF32Equal(t, "xcorr_kernel", cSum, goSum)
		}
	}
}

func TestParity_CeltInnerProd(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	for _, N := range []int{1, 2, 8, 30, 120, 480} {
		for run := 0; run < 5; run++ {
			x := randSig(r, N, 0.5)
			y := randSig(r, N, 0.5)
			c := cCeltInnerProd(x, y, N)
			g := nativeopus.ExportTestCeltInnerProdC(x, y, N)
			f32EqualTol(t, "celt_inner_prod",
				fmtIntPair(N, run), c, g, 0)
		}
	}
}

func TestParity_DualInnerProd(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for _, N := range []int{1, 4, 20, 160, 480} {
		for run := 0; run < 5; run++ {
			x := randSig(r, N, 0.5)
			y01 := randSig(r, N, 0.5)
			y02 := randSig(r, N, 0.5)
			c1, c2 := cDualInnerProd(x, y01, y02, N)
			g1, g2 := nativeopus.ExportTestDualInnerProdC(x, y01, y02, N)
			f32EqualTol(t, "dual_inner_prod_1", fmtIntPair(N, run), c1, g1, 0)
			f32EqualTol(t, "dual_inner_prod_2", fmtIntPair(N, run), c2, g2, 0)
		}
	}
}

func TestParity_CeltPitchXcorr(t *testing.T) {
	r := rand.New(rand.NewSource(6))
	// (len, max_pitch) pairs used by CELT in practice.
	cases := []struct{ ln, max int }{
		{120, 100}, {240, 16}, {480, 16}, {40, 16}, {120, 20},
	}
	for _, tc := range cases {
		x := randSig(r, tc.ln, 0.5)
		y := randSig(r, tc.ln+tc.max, 0.5)
		cOut := make([]float32, tc.max)
		goOut := make([]float32, tc.max)
		cCeltPitchXcorr(x, y, cOut, tc.ln, tc.max)
		nativeopus.ExportTestCeltPitchXcorrC(x, y, goOut, tc.ln, tc.max)
		if !sliceF32Equal(t, "celt_pitch_xcorr", cOut, goOut) {
			break
		}
	}
}

func TestParity_CeltLPC(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for _, p := range []int{2, 4, 8, 16, 24} {
		for run := 0; run < 5; run++ {
			// Build a plausible autocorrelation: positive, monotonically
			// non-increasing in magnitude.
			ac := make([]float32, p+1)
			ac[0] = 10.0 + r.Float32()*100
			for i := 1; i <= p; i++ {
				ac[i] = ac[i-1] * (0.3 + 0.5*r.Float32())
				if r.Intn(2) == 0 {
					ac[i] = -ac[i]
				}
			}
			cLPC := make([]float32, p)
			goLPC := make([]float32, p)
			cCeltLPC(cLPC, ac, p)
			nativeopus.ExportTestCeltLPC(goLPC, ac, p)
			if !sliceF32Equal(t, "_celt_lpc", cLPC, goLPC) {
				break
			}
		}
	}
}

func TestParity_CeltFir(t *testing.T) {
	r := rand.New(rand.NewSource(8))
	for _, tc := range []struct{ N, ord int }{
		{8, 4}, {16, 4}, {100, 8}, {480, 16}, {120, 24},
	} {
		// Caller-owned buffer has ord-sample history prefix then the
		// N-sample signal. C wants a pointer *after* the prefix; Go's
		// celt_fir_c wants the whole buffer with ord offset implicit.
		xbuf := randSig(r, tc.N+tc.ord, 0.5)
		num := randSig(r, tc.ord, 0.1)
		cY := make([]float32, tc.N)
		goY := make([]float32, tc.N)
		cCeltFir(xbuf[tc.ord:], num, cY, tc.N, tc.ord)
		nativeopus.ExportTestCeltFirC(xbuf, num, goY, tc.N, tc.ord)
		if !sliceF32Equal(t, "celt_fir", cY, goY) {
			break
		}
	}
}

func TestParity_CeltIir(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	for _, tc := range []struct{ N, ord int }{
		{16, 4}, {100, 4}, {120, 8}, {480, 16}, {240, 24},
	} {
		x := randSig(r, tc.N, 0.5)
		den := make([]float32, tc.ord)
		for i := range den {
			den[i] = (r.Float32()*2 - 1) * 0.1 // keep IIR stable
		}
		cMem := make([]float32, tc.ord)
		goMem := make([]float32, tc.ord)
		cY := make([]float32, tc.N)
		goY := make([]float32, tc.N)
		cCeltIir(x, den, cY, tc.N, tc.ord, cMem)
		nativeopus.ExportTestCeltIir(x, den, goY, tc.N, tc.ord, goMem)
		if !sliceF32Equal(t, "celt_iir", cY, goY) {
			break
		}
		if !sliceF32Equal(t, "celt_iir_mem", cMem, goMem) {
			break
		}
	}
}

func TestParity_CeltAutocorr(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	cases := []struct{ n, lag, overlap int }{
		{120, 4, 0}, {240, 8, 0}, {480, 16, 32},
	}
	for _, tc := range cases {
		x := randSig(r, tc.n, 0.5)
		var window []float32
		if tc.overlap > 0 {
			window = make([]float32, tc.overlap)
			for i := range window {
				window[i] = 0.5 - 0.5*float32(math.Cos(float64(i)*math.Pi/float64(tc.overlap)))
			}
		}
		cAc := make([]float32, tc.lag+1)
		goAc := make([]float32, tc.lag+1)
		cCeltAutocorr(x, cAc, window, tc.overlap, tc.lag, tc.n)
		nativeopus.ExportTestCeltAutocorr(x, goAc, window, tc.overlap, tc.lag, tc.n)
		if !sliceF32Equal(t, "_celt_autocorr", cAc, goAc) {
			break
		}
	}
}

func TestParity_PitchDownsample(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	ln := 240
	factor := 2
	for _, C_ := range []int{1, 2} {
		buf := make([][]float32, C_)
		for c := 0; c < C_; c++ {
			buf[c] = randSig(r, ln*factor, 0.5)
		}
		// Make copies so C and Go each operate on matching inputs.
		cBuf := make([][]float32, C_)
		goBuf := make([][]float32, C_)
		for c := 0; c < C_; c++ {
			cBuf[c] = append([]float32(nil), buf[c]...)
			goBuf[c] = append([]float32(nil), buf[c]...)
		}
		cLp := make([]float32, ln)
		goLp := make([]float32, ln)
		cPitchDownsample(cBuf, cLp, ln, C_, factor)
		nativeopus.ExportTestPitchDownsample(goBuf, goLp, ln, C_, factor)
		if !sliceF32Equal(t, "pitch_downsample", cLp, goLp) {
			break
		}
	}
}

func TestParity_PitchSearch(t *testing.T) {
	r := rand.New(rand.NewSource(12))
	ln := 240
	maxp := 48
	for run := 0; run < 3; run++ {
		xlp := randSig(r, ln, 0.5)
		y := randSig(r, ln+maxp, 0.5)
		cY := append([]float32(nil), y...)
		goY := append([]float32(nil), y...)
		cP := cPitchSearch(xlp, cY, ln, maxp)
		goP := nativeopus.ExportTestPitchSearch(xlp, goY, ln, maxp)
		if cP != goP {
			t.Errorf("run=%d: pitch C=%d Go=%d", run, cP, goP)
		}
	}
}

func TestParity_RemoveDoubling(t *testing.T) {
	r := rand.New(rand.NewSource(13))
	for run := 0; run < 3; run++ {
		maxperiod := 1024
		x := randSig(r, maxperiod+500, 0.3)
		cX := append([]float32(nil), x...)
		goX := append([]float32(nil), x...)
		cT0 := 300
		goT0 := 300
		cPg := cRemoveDoubling(cX, maxperiod, 60, 500, &cT0, 0, 0)
		goPg := nativeopus.ExportTestRemoveDoubling(goX, maxperiod, 60, 500, &goT0, 0, 0)
		if cT0 != goT0 {
			t.Errorf("run=%d: T0 C=%d Go=%d", run, cT0, goT0)
		}
		f32EqualTol(t, "remove_doubling_pg", fmtIntPair(run, 0), cPg, goPg, 0)
	}
}
