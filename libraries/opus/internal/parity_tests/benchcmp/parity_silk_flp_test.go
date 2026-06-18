//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// randF32 returns length-N []float32 in [-amp, amp].
func randF32(r *rand.Rand, N int, amp float32) []float32 {
	out := make([]float32, N)
	for i := 0; i < N; i++ {
		out[i] = amp * (r.Float32()*2 - 1)
	}
	return out
}

// ulpDiffF64 — magnitude of ULP gap between two float64s.
func ulpDiffF64(a, b float64) int64 {
	ua := int64(math.Float64bits(a))
	ub := int64(math.Float64bits(b))
	if ua < 0 {
		ua = -0x7FFFFFFFFFFFFFFF - 1 - ua
	}
	if ub < 0 {
		ub = -0x7FFFFFFFFFFFFFFF - 1 - ub
	}
	d := ua - ub
	if d < 0 {
		d = -d
	}
	return d
}

// assertF32Eq — bit-exact float32 comparison with context.
func assertF32Eq(t *testing.T, tag string, c, g float32) {
	t.Helper()
	if c != g {
		t.Errorf("%s: C=%g Go=%g (%d ULP)", tag, c, g, ulpDiffF32(c, g))
	}
}

// ----- Simple vector ops -----

func TestParity_SilkScaleCopyVectorFLP(t *testing.T) {
	r := rand.New(rand.NewSource(101))
	for _, N := range []int{0, 1, 3, 4, 7, 16, 33, 64, 100} {
		for run := 0; run < 3; run++ {
			in := randF32(r, N, 2.5)
			gain := (r.Float32()*2 - 1) * 3.7
			co := cSilkScaleCopyVectorFLP(in, gain)
			go_ := nativeopus.ExportTestSilkScaleCopyVectorFLP(in, gain)
			for i := 0; i < N; i++ {
				if co[i] != go_[i] {
					t.Errorf("N=%d run=%d i=%d: C=%g Go=%g (%d ULP)", N, run, i, co[i], go_[i], ulpDiffF32(co[i], go_[i]))
					break
				}
			}
		}
	}
}

func TestParity_SilkScaleVectorFLP(t *testing.T) {
	r := rand.New(rand.NewSource(103))
	for _, N := range []int{0, 1, 3, 4, 7, 16, 33, 64} {
		for run := 0; run < 3; run++ {
			in := randF32(r, N, 2.0)
			gain := (r.Float32()*2 - 1) * 2.0
			co := cSilkScaleVectorFLP(in, gain)
			go_ := nativeopus.ExportTestSilkScaleVectorFLP(in, gain)
			for i := 0; i < N; i++ {
				if co[i] != go_[i] {
					t.Errorf("N=%d run=%d i=%d: C=%g Go=%g", N, run, i, co[i], go_[i])
					break
				}
			}
		}
	}
}

func TestParity_SilkInsertionSortDecreasingFLP(t *testing.T) {
	r := rand.New(rand.NewSource(107))
	for _, L := range []int{4, 8, 16, 32, 64} {
		for _, K := range []int{1, 2, 4, L / 2, L} {
			if K < 1 || K > L {
				continue
			}
			for run := 0; run < 3; run++ {
				in := randF32(r, L, 1.0)
				ca, ci := cSilkInsertionSortDecreasingFLP(in, K)
				ga, gi := nativeopus.ExportTestSilkInsertionSortDecreasingFLP(in, K)
				for i := 0; i < K; i++ {
					if ca[i] != ga[i] {
						t.Errorf("L=%d K=%d run=%d a[%d]: C=%g Go=%g", L, K, run, i, ca[i], ga[i])
						break
					}
					if ci[i] != gi[i] {
						t.Errorf("L=%d K=%d run=%d idx[%d]: C=%d Go=%d", L, K, run, i, ci[i], gi[i])
						break
					}
				}
			}
		}
	}
}

func TestParity_SilkBwexpanderFLP(t *testing.T) {
	r := rand.New(rand.NewSource(109))
	for _, d := range []int{2, 4, 8, 10, 12, 16, 24} {
		for run := 0; run < 4; run++ {
			in := randF32(r, d, 1.2)
			chirp := 0.3 + r.Float32()*0.6
			co := cSilkBwexpanderFLP(in, chirp)
			go_ := nativeopus.ExportTestSilkBwexpanderFLP(in, chirp)
			for i := 0; i < d; i++ {
				if co[i] != go_[i] {
					t.Errorf("d=%d run=%d i=%d chirp=%g: C=%g Go=%g", d, run, i, chirp, co[i], go_[i])
					break
				}
			}
		}
	}
}

// ----- accumulators -----

func TestParity_SilkInnerProductFLP(t *testing.T) {
	r := rand.New(rand.NewSource(113))
	for _, N := range []int{1, 3, 4, 5, 7, 16, 32, 64, 100, 256} {
		for run := 0; run < 4; run++ {
			a := randF32(r, N, 1.5)
			b := randF32(r, N, 1.5)
			co := cSilkInnerProductFLP(a, b)
			go_ := nativeopus.ExportTestSilkInnerProductFLP(a, b)
			if co != go_ {
				t.Errorf("inner_product N=%d run=%d: C=%g Go=%g (%d ULP) C_bits=%x Go_bits=%x",
					N, run, co, go_, ulpDiffF64(co, go_), math.Float64bits(co), math.Float64bits(go_))
			}
		}
	}
}

func TestParity_SilkEnergyFLP(t *testing.T) {
	r := rand.New(rand.NewSource(119))
	for _, N := range []int{1, 3, 4, 7, 16, 32, 64, 100, 256} {
		for run := 0; run < 4; run++ {
			a := randF32(r, N, 1.5)
			co := cSilkEnergyFLP(a)
			go_ := nativeopus.ExportTestSilkEnergyFLP(a)
			if co != go_ {
				t.Errorf("energy N=%d run=%d: C=%g Go=%g", N, run, co, go_)
			}
		}
	}
}

func TestParity_SilkApplySineWindowFLP(t *testing.T) {
	r := rand.New(rand.NewSource(127))
	for _, L := range []int{16, 32, 64, 128, 192, 256, 384} {
		for _, wt := range []int{1, 2} {
			for run := 0; run < 3; run++ {
				in := randF32(r, L, 1.0)
				co := cSilkApplySineWindowFLP(in, wt)
				go_ := nativeopus.ExportTestSilkApplySineWindowFLP(in, wt)
				for i := 0; i < L; i++ {
					if co[i] != go_[i] {
						t.Errorf("L=%d wt=%d run=%d i=%d: C=%g Go=%g (%d ULP)", L, wt, run, i, co[i], go_[i], ulpDiffF32(co[i], go_[i]))
						break
					}
				}
			}
		}
	}
}

// ----- k2a / schur / LTP_scale_ctrl -----

func TestParity_SilkK2aFLP(t *testing.T) {
	r := rand.New(rand.NewSource(131))
	for _, order := range []int{4, 6, 8, 10, 12, 16, 24} {
		for run := 0; run < 4; run++ {
			rc := randF32(r, order, 0.9)
			co := cSilkK2aFLP(rc)
			go_ := nativeopus.ExportTestSilkK2aFLP(rc)
			for i := 0; i < order; i++ {
				if co[i] != go_[i] {
					t.Errorf("order=%d run=%d i=%d: C=%g Go=%g", order, run, i, co[i], go_[i])
					break
				}
			}
		}
	}
}

func TestParity_SilkSchurFLP(t *testing.T) {
	r := rand.New(rand.NewSource(137))
	for _, order := range []int{4, 8, 10, 12, 16, 24} {
		for run := 0; run < 4; run++ {
			// Build a positive-definite autocorrelation: random signal
			// autocorrelation.
			N := 256
			sig := randF32(r, N, 1.0)
			ac := make([]float32, order+1)
			for lag := 0; lag <= order; lag++ {
				var s float64
				for i := 0; i < N-lag; i++ {
					s += float64(sig[i]) * float64(sig[i+lag])
				}
				ac[lag] = float32(s)
			}
			cR, cRes := cSilkSchurFLP(ac)
			gR, gRes := nativeopus.ExportTestSilkSchurFLP(ac)
			if cRes != gRes {
				t.Errorf("schur order=%d run=%d residual: C=%g Go=%g", order, run, cRes, gRes)
			}
			for i := 0; i < order; i++ {
				if cR[i] != gR[i] {
					t.Errorf("schur order=%d run=%d refl[%d]: C=%g Go=%g", order, run, i, cR[i], gR[i])
					break
				}
			}
		}
	}
}

func TestParity_SilkLTPScaleCtrlFLP(t *testing.T) {
	r := rand.New(rand.NewSource(139))
	cc := nativeopus.ExportTestCodeIndependently()
	for _, condCoding := range []int{cc, cc + 1} {
		for _, LBRR := range []int{0, 1} {
			for run := 0; run < 20; run++ {
				pl := r.Intn(100)
				nf := 1 + r.Intn(3)
				snr := 500 + r.Intn(5000)
				gain := (r.Float32() * 200000.0)
				ci, cs := cSilkLTPScaleCtrlFLP(condCoding, pl, nf, snr, LBRR, gain)
				gi, gs := nativeopus.ExportTestSilkLTPScaleCtrlFLP(condCoding, pl, nf, snr, LBRR, gain)
				if ci != gi {
					t.Errorf("LTP_scale_ctrl cond=%d LBRR=%d run=%d pl=%d nf=%d snr=%d gain=%g idx: C=%d Go=%d",
						condCoding, LBRR, run, pl, nf, snr, gain, ci, gi)
				}
				if cs != gs {
					t.Errorf("LTP_scale_ctrl cond=%d LBRR=%d run=%d pl=%d nf=%d snr=%d gain=%g scale: C=%g Go=%g",
						condCoding, LBRR, run, pl, nf, snr, gain, cs, gs)
				}
			}
		}
	}
}

// ----- autocorrelation / warped_autocorrelation -----

func TestParity_SilkAutocorrelationFLP(t *testing.T) {
	r := rand.New(rand.NewSource(149))
	for _, N := range []int{16, 32, 64, 128, 256} {
		for _, cc := range []int{4, 8, 16, 24} {
			if cc > N {
				continue
			}
			for run := 0; run < 3; run++ {
				in := randF32(r, N, 1.0)
				co := cSilkAutocorrelationFLP(in, cc)
				go_ := nativeopus.ExportTestSilkAutocorrelationFLP(in, cc)
				for i := 0; i < cc; i++ {
					if co[i] != go_[i] {
						t.Errorf("auto N=%d cc=%d run=%d i=%d: C=%g Go=%g", N, cc, run, i, co[i], go_[i])
						break
					}
				}
			}
		}
	}
}

func TestParity_SilkWarpedAutocorrelationFLP(t *testing.T) {
	r := rand.New(rand.NewSource(151))
	for _, N := range []int{32, 64, 128, 256} {
		for _, order := range []int{8, 12, 16, 20, 24} {
			for run := 0; run < 3; run++ {
				in := randF32(r, N, 1.0)
				warping := (r.Float32()*2 - 1) * 0.3
				co := cSilkWarpedAutocorrelationFLP(in, warping, order)
				go_ := nativeopus.ExportTestSilkWarpedAutocorrelationFLP(in, warping, order)
				for i := 0; i <= order; i++ {
					if co[i] != go_[i] {
						t.Errorf("warped N=%d order=%d run=%d i=%d warping=%g: C=%g Go=%g",
							N, order, run, i, warping, co[i], go_[i])
						break
					}
				}
			}
		}
	}
}

// ----- LPC_inv_pred_gain / LPC_analysis_filter / LTP_analysis_filter -----

// Generate stable AR coefficients by starting with random reflection
// coefficients in (-0.9, 0.9) and using k2a-style step-up. We use the
// C k2a implementation here as a building block to ensure stability.
func stableAR(r *rand.Rand, order int) []float32 {
	rc := make([]float32, order)
	for i := range rc {
		rc[i] = (r.Float32()*2 - 1) * 0.85
	}
	return cSilkK2aFLP(rc)
}

func TestParity_SilkLPCInvPredGainFLP(t *testing.T) {
	r := rand.New(rand.NewSource(157))
	for _, order := range []int{4, 8, 10, 12, 16} {
		for run := 0; run < 10; run++ {
			A := stableAR(r, order)
			co := cSilkLPCInvPredGainFLP(A)
			go_ := nativeopus.ExportTestSilkLPCInvPredGainFLP(A)
			assertF32Eq(t, "LPC_inv_pred_gain", co, go_)
			_ = co
			_ = go_
		}
	}
}

func TestParity_SilkLPCAnalysisFilterFLP(t *testing.T) {
	r := rand.New(rand.NewSource(163))
	for _, order := range []int{6, 8, 10, 12, 16} {
		for _, L := range []int{48, 96, 192, 384} {
			if L < order {
				continue
			}
			for run := 0; run < 3; run++ {
				A := stableAR(r, order)
				s := randF32(r, L, 1.0)
				co := cSilkLPCAnalysisFilterFLP(A, s, order)
				go_ := nativeopus.ExportTestSilkLPCAnalysisFilterFLP(A, s, order)
				for i := 0; i < L; i++ {
					if co[i] != go_[i] {
						t.Errorf("LPC_analysis_filter order=%d L=%d run=%d i=%d: C=%g Go=%g (%d ULP)",
							order, L, run, i, co[i], go_[i], ulpDiffF32(co[i], go_[i]))
						break
					}
				}
			}
		}
	}
}

func TestParity_SilkLTPAnalysisFilterFLP(t *testing.T) {
	r := rand.New(rand.NewSource(167))
	maxPitch := 200
	for _, nb := range []int{2, 4} {
		for _, sl := range []int{40, 60, 80} {
			for _, pl := range []int{2, 8} {
				for run := 0; run < 3; run++ {
					xOff := maxPitch + 8 // ensure xOff - pitchL[k] >= 0.
					total := xOff + nb*sl + sl + pl + 16
					x := randF32(r, total, 1.0)
					pitchL := make([]int, nb)
					for i := range pitchL {
						pitchL[i] = 20 + r.Intn(maxPitch-20)
					}
					B := randF32(r, 5*nb, 0.3)
					invG := randF32(r, nb, 1.0)
					co := cSilkLTPAnalysisFilterFLP(x, xOff, B, pitchL, invG, sl, nb, pl)
					go_ := nativeopus.ExportTestSilkLTPAnalysisFilterFLP(x, xOff, B, pitchL, invG, sl, nb, pl)
					for i := 0; i < len(co); i++ {
						if co[i] != go_[i] {
							t.Errorf("LTP_analysis nb=%d sl=%d pl=%d run=%d i=%d: C=%g Go=%g (%d ULP)",
								nb, sl, pl, run, i, co[i], go_[i], ulpDiffF32(co[i], go_[i]))
							break
						}
					}
				}
			}
		}
	}
}

// ----- corrMatrix / corrVector / regularize / residual_energy -----

func TestParity_SilkCorrVectorFLP(t *testing.T) {
	r := rand.New(rand.NewSource(173))
	for _, Order := range []int{4, 8, 12, 16} {
		for _, L := range []int{32, 64, 128, 256} {
			for run := 0; run < 3; run++ {
				x := randF32(r, L+Order-1, 1.0)
				target := randF32(r, L, 1.0)
				co := cSilkCorrVectorFLP(x, target, L, Order)
				go_ := nativeopus.ExportTestSilkCorrVectorFLP(x, target, L, Order)
				for i := 0; i < Order; i++ {
					if co[i] != go_[i] {
						t.Errorf("corrVector Order=%d L=%d run=%d i=%d: C=%g Go=%g", Order, L, run, i, co[i], go_[i])
						break
					}
				}
			}
		}
	}
}

func TestParity_SilkCorrMatrixFLP(t *testing.T) {
	r := rand.New(rand.NewSource(179))
	for _, Order := range []int{4, 8, 12, 16} {
		for _, L := range []int{32, 64, 128, 256} {
			for run := 0; run < 3; run++ {
				x := randF32(r, L+Order-1, 1.0)
				co := cSilkCorrMatrixFLP(x, L, Order)
				go_ := nativeopus.ExportTestSilkCorrMatrixFLP(x, L, Order)
				for i := 0; i < Order*Order; i++ {
					if co[i] != go_[i] {
						t.Errorf("corrMatrix Order=%d L=%d run=%d [%d]: C=%g Go=%g", Order, L, run, i, co[i], go_[i])
						break
					}
				}
			}
		}
	}
}

func TestParity_SilkRegularizeCorrelationsFLP(t *testing.T) {
	r := rand.New(rand.NewSource(181))
	for _, D := range []int{4, 8, 12, 16} {
		for run := 0; run < 3; run++ {
			XX := randF32(r, D*D, 1.0)
			xx := randF32(r, D, 1.0)
			noise := r.Float32() * 0.01
			cXX, cxx := cSilkRegularizeCorrelationsFLP(XX, xx, noise, D)
			gXX, gxx := nativeopus.ExportTestSilkRegularizeCorrelationsFLP(XX, xx, noise, D)
			for i := 0; i < D*D; i++ {
				if cXX[i] != gXX[i] {
					t.Errorf("regularize D=%d run=%d XX[%d]: C=%g Go=%g", D, run, i, cXX[i], gXX[i])
					break
				}
			}
			for i := 0; i < D; i++ {
				if cxx[i] != gxx[i] {
					t.Errorf("regularize D=%d run=%d xx[%d]: C=%g Go=%g", D, run, i, cxx[i], gxx[i])
					break
				}
			}
		}
	}
}

func TestParity_SilkResidualEnergyCovarFLP(t *testing.T) {
	r := rand.New(rand.NewSource(191))
	for _, D := range []int{4, 8, 12, 16} {
		for run := 0; run < 5; run++ {
			// Build a symmetric positive-definite-ish wXX.
			M := make([]float32, D*D)
			for i := 0; i < D; i++ {
				for j := 0; j < D; j++ {
					M[i*D+j] = (r.Float32()*2 - 1) * 0.1
				}
			}
			// Symmetrise with strong diagonal.
			wXX := make([]float32, D*D)
			for i := 0; i < D; i++ {
				for j := 0; j < D; j++ {
					v := 0.5 * (M[i*D+j] + M[j*D+i])
					if i == j {
						v += 2.0
					}
					// Column-major storage.
					wXX[i+D*j] = v
				}
			}
			c := randF32(r, D, 0.3)
			wXx := randF32(r, D, 0.5)
			wxx := 1.0 + r.Float32()
			cnrg, _ := cSilkResidualEnergyCovarFLP(c, wXX, wXx, wxx, D)
			gnrg, _ := nativeopus.ExportTestSilkResidualEnergyCovarFLP(c, wXX, wXx, wxx, D)
			if cnrg != gnrg {
				t.Errorf("residual_energy_covar D=%d run=%d: C=%g Go=%g (%d ULP)",
					D, run, cnrg, gnrg, ulpDiffF32(cnrg, gnrg))
			}
		}
	}
}

func TestParity_SilkResidualEnergyFLP(t *testing.T) {
	r := rand.New(rand.NewSource(193))
	// bufLen in silk_residual_energy_FLP equals
	//   (MAX_FRAME_LENGTH + MAX_NB_SUBFR*MAX_LPC_ORDER)/2 = (320+64)/2 = 192
	// which caps 2*(ord+sl) at 192, i.e. ord+sl <= 96.
	for _, nb := range []int{2, 4} {
		for _, sl := range []int{40, 60, 80} {
			for _, ord := range []int{10, 12, 16} {
				if ord+sl > 96 {
					continue
				}
				for run := 0; run < 3; run++ {
					shift := ord + sl
					var total int
					if nb == 4 {
						total = 4 * shift
					} else {
						total = 2 * shift
					}
					x := randF32(r, total, 0.8)
					a0 := stableAR(r, ord)
					a1 := stableAR(r, ord)
					// Pad a0/a1 to MAX_LPC_ORDER for passing to the shim.
					a0p := make([]float32, 16)
					a1p := make([]float32, 16)
					copy(a0p, a0)
					copy(a1p, a1)
					gains := randF32(r, nb, 1.0)
					// Avoid negative gains to keep nrg positive.
					for i := range gains {
						if gains[i] < 0 {
							gains[i] = -gains[i]
						}
						gains[i] += 0.1
					}
					co := cSilkResidualEnergyFLP(x, a0p, a1p, gains, sl, nb, ord)
					go_ := nativeopus.ExportTestSilkResidualEnergyFLP(x, a0p, a1p, gains, sl, nb, ord)
					for i := 0; i < nb; i++ {
						if co[i] != go_[i] {
							t.Errorf("residual_energy nb=%d sl=%d ord=%d run=%d [%d]: C=%g Go=%g (%d ULP)",
								nb, sl, ord, run, i, co[i], go_[i], ulpDiffF32(co[i], go_[i]))
							break
						}
					}
				}
			}
		}
	}
}

// ----- burg_modified -----

func TestParity_SilkBurgModifiedFLP(t *testing.T) {
	r := rand.New(rand.NewSource(199))
	for _, D := range []int{10, 12, 16} {
		for _, nb := range []int{1, 2, 4} {
			for _, sl := range []int{40, 60, 80} {
				if sl*nb > silkMaxBurgFrame {
					continue
				}
				for run := 0; run < 3; run++ {
					x := randF32(r, sl*nb, 0.8)
					minInvGain := float32(1.0 / 16384.0)
					cr, cA := cSilkBurgModifiedFLP(x, minInvGain, sl, nb, D)
					gr, gA := nativeopus.ExportTestSilkBurgModifiedFLP(x, minInvGain, sl, nb, D)
					if cr != gr {
						t.Errorf("burg D=%d nb=%d sl=%d run=%d res: C=%g Go=%g (%d ULP)",
							D, nb, sl, run, cr, gr, ulpDiffF32(cr, gr))
					}
					for i := 0; i < D; i++ {
						if cA[i] != gA[i] {
							t.Errorf("burg D=%d nb=%d sl=%d run=%d A[%d]: C=%g Go=%g (%d ULP)",
								D, nb, sl, run, i, cA[i], gA[i], ulpDiffF32(cA[i], gA[i]))
							break
						}
					}
				}
			}
		}
	}
}

const silkMaxBurgFrame = 384
