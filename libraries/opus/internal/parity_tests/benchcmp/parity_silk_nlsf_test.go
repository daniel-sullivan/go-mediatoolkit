//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"sort"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// monotoneNLSF returns a vector of sorted, strictly-increasing int16
// samples in [1, 32766] suitable as an NLSF input.
func monotoneNLSF(r *rand.Rand, d int) []int16 {
	raw := make([]int, d)
	for i := range raw {
		raw[i] = 1 + r.Intn(32766)
	}
	sort.Ints(raw)
	// Ensure strict increase.
	for i := 1; i < d; i++ {
		if raw[i] <= raw[i-1] {
			raw[i] = raw[i-1] + 1
		}
	}
	if raw[d-1] > 32766 {
		// Shift down.
		sh := raw[d-1] - 32766
		for i := range raw {
			raw[i] -= sh
		}
	}
	out := make([]int16, d)
	for i, v := range raw {
		out[i] = int16(v)
	}
	return out
}

func TestParity_SilkNLSFVQWeightsLaroia(t *testing.T) {
	r := rand.New(rand.NewSource(30))
	for _, d := range []int{10, 16} {
		for trial := 0; trial < 200; trial++ {
			nlsf := monotoneNLSF(r, d)
			w := cSilkNLSFVQWeightsLaroia(nlsf)
			g := nativeopus.ExportTestSilkNLSFVQWeightsLaroia(nlsf)
			if !eqInt16Slice(w, g) {
				t.Fatalf("NLSF_VQ_weights_laroia d=%d: C=%v Go=%v", d, w, g)
			}
		}
	}
}

func TestParity_SilkNLSFStabilize(t *testing.T) {
	r := rand.New(rand.NewSource(31))
	for _, d := range []int{10, 16} {
		for trial := 0; trial < 200; trial++ {
			nlsf := make([]int16, d)
			for i := range nlsf {
				nlsf[i] = int16(r.Intn(32768))
			}
			// Use deltaMin arrays with small positive values.
			delta := make([]int16, d+1)
			for i := range delta {
				delta[i] = int16(50 + r.Intn(250))
			}
			w := cSilkNLSFStabilize(nlsf, delta)
			g := nativeopus.ExportTestSilkNLSFStabilize(nlsf, delta)
			if !eqInt16Slice(w, g) {
				t.Fatalf("NLSF_stabilize d=%d trial=%d: C=%v Go=%v", d, trial, w, g)
			}
		}
	}
}

func TestParity_SilkNLSFDecode(t *testing.T) {
	r := rand.New(rand.NewSource(32))
	for _, wb := range []bool{false, true} {
		order := cCBOrder(wb)
		// CB1 table size: 32 for NB_MB, 32 for WB (pick realistic range).
		for trial := 0; trial < 200; trial++ {
			idx := make([]int8, order+1)
			idx[0] = int8(r.Intn(32))
			for i := 1; i <= order; i++ {
				idx[i] = int8(r.Intn(21) - 10) // [-10, 10]
			}
			w := cSilkNLSFDecode(idx, wb)
			g := nativeopus.ExportTestSilkNLSFDecode(idx, wb)
			if !eqInt16Slice(w, g) {
				t.Fatalf("NLSF_decode wb=%v idx=%v: C=%v Go=%v", wb, idx, w, g)
			}
		}
	}
}

func TestParity_SilkNLSFEncode(t *testing.T) {
	r := rand.New(rand.NewSource(33))
	mus := []int{0, 40, 80, 120}
	for _, wb := range []bool{false, true} {
		order := cCBOrder(wb)
		for _, mu := range mus {
			for _, nSurv := range []int{1, 4, 8} {
				for _, sig := range []int{0, 1, 2} {
					for trial := 0; trial < 20; trial++ {
						nlsf := monotoneNLSF(r, order)
						cRd, cIdx, cNlsf := cSilkNLSFEncode(nlsf, wb, mu, nSurv, sig)
						gRd, gIdx, gNlsf := nativeopus.ExportTestSilkNLSFEncode(nlsf, wb, mu, nSurv, sig)
						if cRd != gRd || !eqInt8Slice(cIdx, gIdx) || !eqInt16Slice(cNlsf, gNlsf) {
							t.Fatalf("NLSF_encode wb=%v mu=%d nSurv=%d sig=%d trial=%d\ncIdx=%v gIdx=%v\ncRd=%d gRd=%d",
								wb, mu, nSurv, sig, trial, cIdx, gIdx, cRd, gRd)
						}
					}
				}
			}
		}
	}
}

func TestParity_SilkA2NLSF(t *testing.T) {
	r := rand.New(rand.NewSource(34))
	// Use modest filter coefficients; extreme values trigger bwexpander
	// iterations but the algorithm should still converge.
	for _, d := range []int{10, 16} {
		for trial := 0; trial < 100; trial++ {
			a := make([]int32, d)
			for i := range a {
				a[i] = int32(r.Int31())>>uint(16+r.Intn(6)) + 1
				if r.Intn(2) == 0 {
					a[i] = -a[i]
				}
			}
			cOut, _ := cSilkA2NLSF(a, d)
			gOut, _ := nativeopus.ExportTestSilkA2NLSF(a, d)
			if !eqInt16Slice(cOut, gOut) {
				t.Fatalf("A2NLSF d=%d trial=%d: C=%v Go=%v", d, trial, cOut, gOut)
			}
		}
	}
}

func TestParity_SilkNLSF2A(t *testing.T) {
	r := rand.New(rand.NewSource(35))
	for _, d := range []int{10, 16} {
		for trial := 0; trial < 200; trial++ {
			nlsf := monotoneNLSF(r, d)
			c := cSilkNLSF2A(nlsf, d)
			g := nativeopus.ExportTestSilkNLSF2A(nlsf, d)
			if !eqInt16Slice(c, g) {
				t.Fatalf("NLSF2A d=%d: C=%v Go=%v", d, c, g)
			}
		}
	}
}

func eqInt8Slice(a, b []int8) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
