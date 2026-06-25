//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// ulpDiffF32 — magnitude of ULP gap between two float32s (for error
// reporting only; the tests require 0 ULP drift).
func ulpDiffF32(a, b float32) int32 {
	ua := int32(math.Float32bits(a))
	ub := int32(math.Float32bits(b))
	if ua < 0 {
		ua = -2147483648 - ua
	}
	if ub < 0 {
		ub = -2147483648 - ub
	}
	d := ua - ub
	if d < 0 {
		d = -d
	}
	return d
}

func randNorm(r *rand.Rand, N int, amp float32) []float32 {
	X := make([]float32, N)
	var ss float64
	for i := 0; i < N; i++ {
		X[i] = amp * (r.Float32()*2 - 1)
		ss += float64(X[i]) * float64(X[i])
	}
	// Normalise so |X|=1 — alg_quant expects unit-norm inputs.
	if ss > 0 {
		inv := float32(1.0 / math.Sqrt(ss))
		for i := range X {
			X[i] *= inv
		}
	}
	return X
}

func TestParity_ExpRotation(t *testing.T) {
	r := rand.New(rand.NewSource(31))
	type cfg struct{ N, K, B, spread int }
	cases := []cfg{}
	for _, N := range []int{16, 32, 48, 64, 96, 128} {
		for _, B := range []int{1, 2, 4} {
			if N%B != 0 {
				continue
			}
			for _, K := range []int{1, 3, 6, 12} {
				if 2*K >= N {
					continue
				}
				for sp := 1; sp <= 3; sp++ {
					cases = append(cases, cfg{N, K, B, sp})
				}
			}
		}
	}
	for _, c := range cases {
		X := randNorm(r, c.N, 1.0)
		cX := append([]float32(nil), X...)
		gX := append([]float32(nil), X...)
		cExpRotation(cX, c.N, 1, c.B, c.K, c.spread)
		nativeopus.ExportTestExpRotation(gX, c.N, 1, c.B, c.K, c.spread)
		for i := 0; i < c.N; i++ {
			if cX[i] != gX[i] {
				t.Errorf("exp_rotation N=%d K=%d B=%d sp=%d [%d]: C=%g Go=%g (%d ULP)",
					c.N, c.K, c.B, c.spread, i, cX[i], gX[i], ulpDiffF32(cX[i], gX[i]))
				break
			}
		}
		// Inverse.
		cExpRotation(cX, c.N, -1, c.B, c.K, c.spread)
		nativeopus.ExportTestExpRotation(gX, c.N, -1, c.B, c.K, c.spread)
		for i := 0; i < c.N; i++ {
			if cX[i] != gX[i] {
				t.Errorf("exp_rotation(-1) N=%d K=%d B=%d sp=%d [%d]: C=%g Go=%g",
					c.N, c.K, c.B, c.spread, i, cX[i], gX[i])
				break
			}
		}
	}
}

func TestParity_OpPvqSearch(t *testing.T) {
	r := rand.New(rand.NewSource(37))
	for _, N := range []int{4, 8, 16, 32, 48, 64} {
		for _, K := range []int{1, 2, 5, 10, 20} {
			if K < 1 {
				continue
			}
			for run := 0; run < 3; run++ {
				X := randNorm(r, N, 1.0)
				cX := append([]float32(nil), X...)
				gX := append([]float32(nil), X...)
				cIy := make([]int32, N)
				gIy := make([]int, N)
				cyy := cOpPvqSearch(cX, cIy, K, N)
				gyy := nativeopus.ExportTestOpPvqSearchC(gX, gIy, K, N)
				if cyy != gyy {
					t.Errorf("op_pvq_search N=%d K=%d run=%d: yy C=%g Go=%g (%d ULP)",
						N, K, run, cyy, gyy, ulpDiffF32(cyy, gyy))
					continue
				}
				for i := 0; i < N; i++ {
					if int(cIy[i]) != gIy[i] {
						t.Errorf("op_pvq_search N=%d K=%d run=%d iy[%d]: C=%d Go=%d",
							N, K, run, i, cIy[i], gIy[i])
						break
					}
					if cX[i] != gX[i] {
						t.Errorf("op_pvq_search N=%d K=%d run=%d X[%d]: C=%g Go=%g",
							N, K, run, i, cX[i], gX[i])
						break
					}
				}
			}
		}
	}
}

func TestParity_RenormaliseVector(t *testing.T) {
	r := rand.New(rand.NewSource(41))
	for _, N := range []int{4, 8, 16, 32, 64} {
		for run := 0; run < 5; run++ {
			X := randNorm(r, N, 0.5+r.Float32())
			gain := 0.5 + r.Float32()
			cX := append([]float32(nil), X...)
			gX := append([]float32(nil), X...)
			cRenormaliseVector(cX, N, gain)
			nativeopus.ExportTestRenormaliseVector(gX, N, gain)
			for i := 0; i < N; i++ {
				if cX[i] != gX[i] {
					t.Errorf("renormalise N=%d run=%d [%d]: C=%g Go=%g (%d ULP)",
						N, run, i, cX[i], gX[i], ulpDiffF32(cX[i], gX[i]))
					break
				}
			}
		}
	}
}

func TestParity_StereoItheta(t *testing.T) {
	r := rand.New(rand.NewSource(43))
	for _, N := range []int{4, 8, 16, 32, 64} {
		for _, stereo := range []int{0, 1} {
			for run := 0; run < 5; run++ {
				X := randNorm(r, N, 1.0)
				Y := randNorm(r, N, 1.0)
				c := cStereoItheta(X, Y, stereo, N)
				g := nativeopus.ExportTestStereoItheta(X, Y, stereo, N)
				if c != g {
					t.Errorf("stereo_itheta N=%d stereo=%d run=%d: C=%d Go=%d",
						N, stereo, run, c, g)
				}
			}
		}
	}
}

func TestParity_AlgQuant(t *testing.T) {
	r := rand.New(rand.NewSource(47))
	// CELT_PVQ_U_ROW[k][n] is only valid for n within the table-row
	// length (see cwrs_table.go). The test keeps (N, K) pairs that fit
	// the standard-mode cwrs tables used by real Opus.
	type cfg struct{ N, K, B, spread int }
	cases := []cfg{
		{4, 2, 1, 2}, {6, 3, 1, 2}, {8, 3, 1, 2},
		{8, 4, 2, 1}, {12, 4, 1, 2}, {12, 5, 2, 3},
	}
	for _, cs := range cases {
		for run := 0; run < 4; run++ {
			X := randNorm(r, cs.N, 1.0)
			cX := append([]float32(nil), X...)
			gX := append([]float32(nil), X...)

			cBuf := make([]byte, 4096)
			goBuf := make([]byte, 4096)
			cE := cEcEncNew(cBuf)
			gE := nativeopus.ExportTestEcEncNew(goBuf)

			cMask := cAlgQuant(cX, cs.N, cs.K, cs.spread, cs.B, cE, 1.0, 1)
			gMask := nativeopus.ExportTestAlgQuant(gX, cs.N, cs.K, cs.spread, cs.B, gE, 1.0, 1)

			if cMask != gMask {
				t.Errorf("alg_quant N=%d K=%d B=%d sp=%d run=%d: mask C=%x Go=%x",
					cs.N, cs.K, cs.B, cs.spread, run, cMask, gMask)
			}
			for i := 0; i < cs.N; i++ {
				if cX[i] != gX[i] {
					t.Errorf("alg_quant N=%d K=%d B=%d sp=%d run=%d X[%d]: C=%g Go=%g (%d ULP)",
						cs.N, cs.K, cs.B, cs.spread, run, i, cX[i], gX[i], ulpDiffF32(cX[i], gX[i]))
					break
				}
			}
			if cE.Rng() != nativeopus.ExportTestEcRng(gE) ||
				cE.Val() != nativeopus.ExportTestEcVal(gE) {
				t.Errorf("alg_quant N=%d K=%d run=%d: ec state diverged", cs.N, cs.K, run)
			}
			cE.EncDone()
			nativeopus.ExportTestEcEncDone(gE)
			if !bytes.Equal(cBuf, goBuf) {
				t.Errorf("alg_quant N=%d K=%d B=%d sp=%d run=%d: ec bytes differ",
					cs.N, cs.K, cs.B, cs.spread, run)
			}
			cE.Free()
		}
	}
}

func TestParity_AlgUnquant(t *testing.T) {
	r := rand.New(rand.NewSource(53))
	type cfg struct{ N, K, B, spread int }
	cases := []cfg{
		{4, 2, 1, 2}, {6, 3, 1, 2}, {8, 3, 1, 2},
		{8, 4, 2, 1}, {12, 4, 1, 2}, {12, 5, 2, 3},
	}
	for _, cs := range cases {
		for run := 0; run < 3; run++ {
			X := randNorm(r, cs.N, 1.0)
			// Encode with C to get a valid bitstream + reference X output.
			cBuf := make([]byte, 4096)
			cE := cEcEncNew(cBuf)
			cXref := append([]float32(nil), X...)
			cAlgQuant(cXref, cs.N, cs.K, cs.spread, cs.B, cE, 1.0, 1)
			cE.EncDone()
			cE.Free()

			// Decode with both sides.
			cD := cEcDecNew(cBuf)
			gD := nativeopus.ExportTestEcDecNew(cBuf)
			cX := make([]float32, cs.N)
			gX := make([]float32, cs.N)
			cMask := cAlgUnquant(cX, cs.N, cs.K, cs.spread, cs.B, cD, 1.0)
			gMask := nativeopus.ExportTestAlgUnquant(gX, cs.N, cs.K, cs.spread, cs.B, gD, 1.0)
			if cMask != gMask {
				t.Errorf("alg_unquant N=%d K=%d B=%d sp=%d run=%d: mask C=%x Go=%x",
					cs.N, cs.K, cs.B, cs.spread, run, cMask, gMask)
			}
			for i := 0; i < cs.N; i++ {
				if cX[i] != gX[i] {
					t.Errorf("alg_unquant N=%d K=%d B=%d sp=%d run=%d X[%d]: C=%g Go=%g (%d ULP)",
						cs.N, cs.K, cs.B, cs.spread, run, i, cX[i], gX[i], ulpDiffF32(cX[i], gX[i]))
					break
				}
			}
			cD.Free()
		}
	}
}
