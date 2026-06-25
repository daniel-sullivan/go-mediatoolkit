//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// buildGoMdct mirrors the C mdct_test_wrap into a Go mdct_lookup by
// copying every field byte-for-byte via Cgo accessors. Isolates MDCT
// butterfly+rotation parity from cos/sin computation differences.
//
// Twiddle sharing note: sub-states (shift > 0) reference the base
// state's larger twiddle table in C (opus_fft_alloc_twiddles
// passes `base` and sets `st->twiddles = base->twiddles`). Butterfly
// kernels at sub-state sizes index past sub-state.nfft into the
// base's table, so the Go sub-states must point at the same backing
// slice or kf_bfly{3,5} will panic with an out-of-range access.
func buildGoMdct(t *testing.T, cm cMdct, N, maxshift int) nativeopus.MdctLookupHandle {
	t.Helper()
	ffts := make([]nativeopus.FftStateHandle, maxshift+1)
	// First build the base state (shift=0) with its full twiddle
	// table.
	baseNfft := cm.Nfft(0)
	{
		factors := make([]int16, 16)
		for i := range factors {
			factors[i] = cm.Factor(0, i)
		}
		bitrev := make([]int16, baseNfft)
		for i := range bitrev {
			bitrev[i] = cm.Bitrev(0, i)
		}
		twR := make([]float32, baseNfft)
		twI := make([]float32, baseNfft)
		for i := 0; i < baseNfft; i++ {
			twR[i] = cm.Twr(0, i)
			twI[i] = cm.Twi(0, i)
		}
		ffts[0] = nativeopus.NewFftStateFromData(
			baseNfft, cm.Scale(0), cm.Shift(0), factors, bitrev, twR, twI)
	}
	baseTw := ffts[0].FftTwiddles()
	for s := 1; s <= maxshift; s++ {
		nfft := cm.Nfft(s)
		factors := make([]int16, 16)
		for i := range factors {
			factors[i] = cm.Factor(s, i)
		}
		bitrev := make([]int16, nfft)
		for i := range bitrev {
			bitrev[i] = cm.Bitrev(s, i)
		}
		// Dummy twiddles for NewFftStateFromData (gets overwritten).
		dummyR := make([]float32, 1)
		dummyI := make([]float32, 1)
		ffts[s] = nativeopus.NewFftStateFromData(
			nfft, cm.Scale(s), cm.Shift(s), factors, bitrev, dummyR, dummyI)
		ffts[s].SetFftTwiddles(baseTw)
	}
	N2 := N >> 1
	total := N - (N2 >> maxshift)
	trig := make([]float32, total)
	for i := range trig {
		trig[i] = cm.Trig(i)
	}
	return nativeopus.NewMdctLookupFromData(N, maxshift, ffts, trig)
}

// mdctCases — (N, maxshift) used by CELT in practice plus a minimal
// small case.
// N values chosen so kf_factor's runtime decomposition never produces
// a radix-2 stage with m==1 — which the non-CUSTOM_MODES C kf_bfly2
// asserts against (celt_assert(m==4)). Production libopus uses static
// pre-computed factor tables that hand-pick a decomposition avoiding
// this case; we can't reach those tables at runtime without pulling
// in static_modes_float.h. Our Go kf_bfly2 ports both paths already,
// so these restrictions are test-side only.
var mdctCases = []struct{ N, maxshift int }{
	{240, 0}, // nfft=60 = 4*3*5, clean radix-4
	{480, 0}, // nfft=120 = 4*4*2*3*5 → rewrite fires, clean
	{960, 0}, // nfft=240 = 4*4*3*5, clean
}

func hannWindow(n int) []float32 {
	w := make([]float32, n)
	for i := range w {
		w[i] = float32(math.Sin(math.Pi / 2 * math.Pow(math.Sin(math.Pi*(float64(i)+0.5)/float64(n)), 2)))
	}
	return w
}

func TestParity_MDCTForward(t *testing.T) {
	for _, tc := range mdctCases {
		t.Run(sprintfDec32(int32(tc.N))+"_ms"+sprintfDec32(int32(tc.maxshift)), func(t *testing.T) {
			cm := cMdctAlloc(tc.N, tc.maxshift)
			if cm.w == nil {
				t.Fatalf("cMdctAlloc failed")
			}
			defer cm.Free()
			gm := buildGoMdct(t, cm, tc.N, tc.maxshift)

			for shift := 0; shift <= tc.maxshift; shift++ {
				Nshifted := tc.N >> shift
				// Overlap must be a multiple of 4 and <= 2*(N/4) for
				// the pre-rotation's xp1-N2 to stay in bounds. Use
				// N/2 floored to multiple of 4 — matches Opus ratio
				// (mode->overlap is the window length, not N).
				overlap := (Nshifted / 2) &^ 3
				if overlap < 4 {
					overlap = 4
				}
				stride := 1
				win := hannWindow(overlap)
				inLen := Nshifted + overlap

				r := rand.New(rand.NewSource(int64(tc.N*13 + shift)))
				in := make([]float32, inLen)
				inC := make([]float32, inLen)
				for i := range in {
					in[i] = r.Float32()*2 - 1
					inC[i] = in[i]
				}
				outLen := (Nshifted >> 1) * stride
				outC := make([]float32, outLen)
				outG := make([]float32, outLen)

				cm.Forward(inC, outC, win, overlap, shift, stride)
				nativeopus.ExportTestCltMdctForward(gm, in, outG, win, overlap, shift, stride)
				for i := range outC {
					if math.Float32bits(outC[i]) != math.Float32bits(outG[i]) {
						t.Errorf("N=%d shift=%d [%d]: C=%g (0x%08x) Go=%g (0x%08x)",
							tc.N, shift, i, outC[i], math.Float32bits(outC[i]),
							outG[i], math.Float32bits(outG[i]))
						return
					}
				}
			}
		})
	}
}

func TestParity_MDCTBackward(t *testing.T) {
	for _, tc := range mdctCases {
		t.Run(sprintfDec32(int32(tc.N))+"_ms"+sprintfDec32(int32(tc.maxshift)), func(t *testing.T) {
			cm := cMdctAlloc(tc.N, tc.maxshift)
			if cm.w == nil {
				t.Fatalf("cMdctAlloc failed")
			}
			defer cm.Free()
			gm := buildGoMdct(t, cm, tc.N, tc.maxshift)

			for shift := 0; shift <= tc.maxshift; shift++ {
				Nshifted := tc.N >> shift
				overlap := (Nshifted / 2) &^ 3
				if overlap < 4 {
					overlap = 4
				}
				stride := 1
				win := hannWindow(overlap)

				r := rand.New(rand.NewSource(int64(tc.N*17 + shift)))
				inLen := Nshifted >> 1
				in := make([]float32, inLen)
				inC := make([]float32, inLen)
				for i := range in {
					in[i] = r.Float32()*2 - 1
					inC[i] = in[i]
				}
				outLen := Nshifted + overlap
				outC := make([]float32, outLen)
				outG := make([]float32, outLen)

				cm.Backward(inC, outC, win, overlap, shift, stride)
				nativeopus.ExportTestCltMdctBackward(gm, in, outG, win, overlap, shift, stride)
				for i := range outC {
					if math.Float32bits(outC[i]) != math.Float32bits(outG[i]) {
						t.Errorf("N=%d shift=%d [%d]: C=%g (0x%08x) Go=%g (0x%08x)",
							tc.N, shift, i, outC[i], math.Float32bits(outC[i]),
							outG[i], math.Float32bits(outG[i]))
						return
					}
				}
			}
		})
	}
}
