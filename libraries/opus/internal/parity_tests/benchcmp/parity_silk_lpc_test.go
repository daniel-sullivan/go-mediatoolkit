//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_SilkBWExpander(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	for _, d := range []int{1, 2, 3, 6, 8, 10, 12, 16, 24} {
		for trial := 0; trial < 50; trial++ {
			ar := make([]int16, d)
			for i := range ar {
				ar[i] = int16(r.Intn(65536) - 32768)
			}
			chirp := int32(r.Intn(65537))
			w := cSilkBWExpander(ar, chirp)
			g := nativeopus.ExportTestSilkBWExpander(append([]int16(nil), ar...), chirp)
			if !eqInt16Slice(w, g) {
				t.Fatalf("silk_bwexpander d=%d chirp=%d: C=%v Go=%v", d, chirp, w, g)
			}
		}
	}
}

func TestParity_SilkBWExpander32(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for _, d := range []int{1, 2, 3, 6, 8, 10, 12, 16, 24} {
		for trial := 0; trial < 50; trial++ {
			ar := make([]int32, d)
			for i := range ar {
				ar[i] = int32(r.Uint32())
			}
			chirp := int32(r.Intn(65537))
			w := cSilkBWExpander32(ar, chirp)
			g := nativeopus.ExportTestSilkBWExpander32(append([]int32(nil), ar...), chirp)
			if !eqInt32Slice(w, g) {
				t.Fatalf("silk_bwexpander_32 d=%d chirp=%d", d, chirp)
			}
		}
	}
}

func TestParity_SilkLPCFit(t *testing.T) {
	r := rand.New(rand.NewSource(12))
	cases := []struct{ QIN, QOUT int }{{24, 12}, {16, 12}, {20, 12}, {18, 14}}
	for _, qc := range cases {
		for _, d := range []int{6, 10, 16, 24} {
			for trial := 0; trial < 40; trial++ {
				a := make([]int32, d)
				for i := range a {
					// Keep within a reasonable range so LPC_fit has meaningful work.
					a[i] = int32(r.Int31()) >> uint(r.Intn(8))
					if r.Intn(2) == 0 {
						a[i] = -a[i]
					}
				}
				wOut, wIn := cSilkLPCFit(a, qc.QOUT, qc.QIN)
				gOut, gIn := nativeopus.ExportTestSilkLPCFit(a, qc.QOUT, qc.QIN)
				if !eqInt16Slice(wOut, gOut) || !eqInt32Slice(wIn, gIn) {
					t.Fatalf("silk_LPC_fit QOUT=%d QIN=%d d=%d trial=%d",
						qc.QOUT, qc.QIN, d, trial)
				}
			}
		}
	}
}

func TestParity_SilkInterpolate(t *testing.T) {
	r := rand.New(rand.NewSource(13))
	for _, d := range []int{1, 4, 8, 16} {
		for _, f := range []int{0, 1, 2, 3, 4} {
			for trial := 0; trial < 30; trial++ {
				x0 := make([]int16, d)
				x1 := make([]int16, d)
				for i := range x0 {
					x0[i] = int16(r.Intn(65536) - 32768)
					x1[i] = int16(r.Intn(65536) - 32768)
				}
				w := cSilkInterpolate(x0, x1, f)
				g := nativeopus.ExportTestSilkInterpolate(x0, x1, f)
				if !eqInt16Slice(w, g) {
					t.Fatalf("silk_interpolate d=%d f=%d", d, f)
				}
			}
		}
	}
}

func TestParity_SilkLPCInversePredGain(t *testing.T) {
	r := rand.New(rand.NewSource(14))
	for _, order := range []int{6, 10, 16} {
		for trial := 0; trial < 200; trial++ {
			A := make([]int16, order)
			for i := range A {
				// Q12 values: realistic LPC coefficient range.
				A[i] = int16(r.Intn(8192) - 4096)
			}
			w := cSilkLPCInversePredGain(A)
			g := nativeopus.ExportTestSilkLPCInversePredGain(A)
			if w != g {
				t.Fatalf("silk_LPC_inverse_pred_gain order=%d trial=%d: C=%d Go=%d A=%v",
					order, trial, w, g, A)
			}
		}
	}
}

func TestParity_SilkLPCAnalysisFilter(t *testing.T) {
	r := rand.New(rand.NewSource(15))
	for _, d := range []int{6, 8, 10, 12, 16} {
		for _, n := range []int{d + 10, d + 50, d + 200} {
			for trial := 0; trial < 20; trial++ {
				in_ := make([]int16, n)
				B := make([]int16, d)
				for i := range in_ {
					in_[i] = int16(r.Intn(65536) - 32768)
				}
				for i := range B {
					B[i] = int16(r.Intn(8192) - 4096)
				}
				w := cSilkLPCAnalysisFilter(in_, B, d)
				g := nativeopus.ExportTestSilkLPCAnalysisFilter(in_, B, d)
				if !eqInt16Slice(w, g) {
					t.Fatalf("silk_LPC_analysis_filter d=%d n=%d trial=%d", d, n, trial)
				}
			}
		}
	}
}
