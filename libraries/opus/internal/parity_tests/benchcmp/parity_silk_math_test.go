//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_SilkLin2Log(t *testing.T) {
	xs := []int32{0, 1, 2, 3, 4, 5, 10, 100, 1000, 10000, 100000,
		1 << 16, 1 << 20, 1 << 24, 1 << 28, 0x7FFFFFFF,
		-1, -10, -1000, int32(-0x80000000)}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		xs = append(xs, int32(r.Uint32()))
	}
	for _, x := range xs {
		want := cSilkLin2Log(x)
		got := nativeopus.ExportTestSilkLin2Log(x)
		if want != got {
			t.Fatalf("silk_lin2log(%d): want %d got %d", x, want, got)
		}
	}
}

func TestParity_SilkLog2Lin(t *testing.T) {
	xs := []int32{-1000, -1, 0, 1, 2, 127, 128, 200, 500, 1000, 2047,
		2048, 2049, 3000, 3966, 3967, 3968, 10000, 0x7FFFFFFF}
	for x := int32(0); x < 4000; x += 13 {
		xs = append(xs, x)
	}
	for _, x := range xs {
		want := cSilkLog2Lin(x)
		got := nativeopus.ExportTestSilkLog2Lin(x)
		if want != got {
			t.Fatalf("silk_log2lin(%d): want %d got %d", x, want, got)
		}
	}
}

func TestParity_SilkSigmQ15(t *testing.T) {
	for x := -300; x <= 300; x++ {
		want := cSilkSigmQ15(x)
		got := nativeopus.ExportTestSilkSigmQ15(x)
		if want != got {
			t.Fatalf("silk_sigm_Q15(%d): want %d got %d", x, want, got)
		}
	}
}

func TestParity_SilkSumSqrShift(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	lens := []int{1, 2, 3, 4, 5, 7, 8, 15, 16, 31, 32, 63, 64, 127, 128, 256, 500}
	for _, n := range lens {
		for trial := 0; trial < 20; trial++ {
			x := make([]int16, n)
			for i := range x {
				x[i] = int16(r.Intn(65536) - 32768)
			}
			we, ws := cSilkSumSqrShift(x)
			ge, gs := nativeopus.ExportTestSilkSumSqrShift(x)
			if we != ge || ws != gs {
				t.Fatalf("silk_sum_sqr_shift len=%d trial=%d: want (%d,%d) got (%d,%d)",
					n, trial, we, ws, ge, gs)
			}
		}
	}
}

func TestParity_SilkInnerProd(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	lens := []int{1, 2, 4, 8, 15, 16, 32, 63, 64, 128, 200}
	scales := []int{0, 1, 2, 4, 8, 16, 24, 31}
	for _, n := range lens {
		a := make([]int16, n)
		b := make([]int16, n)
		for i := range a {
			a[i] = int16(r.Intn(65536) - 32768)
			b[i] = int16(r.Intn(65536) - 32768)
		}
		for _, s := range scales {
			if cSilkInnerProdAlignedScale(a, b, s) !=
				nativeopus.ExportTestSilkInnerProdAlignedScale(a, b, s) {
				t.Fatalf("silk_inner_prod_aligned_scale n=%d scale=%d", n, s)
			}
		}
		if cSilkInnerProd16(a, b) != nativeopus.ExportTestSilkInnerProd16(a, b) {
			t.Fatalf("silk_inner_prod16_c n=%d", n)
		}
	}
}

func TestParity_SilkInsertionSortIncreasing(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	for _, L := range []int{1, 2, 4, 8, 16, 32} {
		for _, K := range []int{1, L / 2, L} {
			if K < 1 {
				K = 1
			}
			for trial := 0; trial < 30; trial++ {
				a := make([]int32, L)
				for i := range a {
					a[i] = int32(r.Int31()) - (1 << 30)
				}
				wa, wi := cSilkInsertionSortIncreasing(a, K)
				ga, gi := nativeopus.ExportTestSilkInsertionSortIncreasing(
					append([]int32(nil), a...), K)
				if !eqInt32Slice(wa, ga) || !eqIntSlice(wi, gi) {
					t.Fatalf("silk_insertion_sort_increasing L=%d K=%d: mismatch\nC a=%v\nGo a=%v\nC idx=%v\nGo idx=%v",
						L, K, wa, ga, wi, gi)
				}
			}
		}
	}
}

func TestParity_SilkInsertionSortDecreasingInt16(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for _, L := range []int{1, 2, 4, 8, 16, 32} {
		for _, K := range []int{1, L / 2, L} {
			if K < 1 {
				K = 1
			}
			for trial := 0; trial < 30; trial++ {
				a := make([]int16, L)
				for i := range a {
					a[i] = int16(r.Intn(65536) - 32768)
				}
				wa, wi := cSilkInsertionSortDecreasingInt16(a, K)
				ga, gi := nativeopus.ExportTestSilkInsertionSortDecreasingInt16(
					append([]int16(nil), a...), K)
				if !eqInt16Slice(wa, ga) || !eqIntSlice(wi, gi) {
					t.Fatalf("silk_insertion_sort_decreasing_int16 L=%d K=%d", L, K)
				}
			}
		}
	}
}

func TestParity_SilkInsertionSortIncreasingAllValuesInt16(t *testing.T) {
	r := rand.New(rand.NewSource(6))
	for _, L := range []int{1, 2, 4, 8, 16, 32, 64} {
		for trial := 0; trial < 30; trial++ {
			a := make([]int16, L)
			for i := range a {
				a[i] = int16(r.Intn(65536) - 32768)
			}
			w := cSilkInsertionSortIncreasingAllValuesInt16(a)
			g := nativeopus.ExportTestSilkInsertionSortIncreasingAllValuesInt16(
				append([]int16(nil), a...))
			if !eqInt16Slice(w, g) {
				t.Fatalf("silk_insertion_sort_increasing_all_values_int16 L=%d", L)
			}
		}
	}
}

func TestParity_SilkLSFCosTab(t *testing.T) {
	w := cSilkLSFCosTab()
	g := nativeopus.ExportTestSilkLSFCosTabFIXQ12()
	if !eqInt16Slice(w, g) {
		t.Fatalf("silk_LSFCosTab_FIX_Q12 mismatch\nC=%v\nGo=%v", w, g)
	}
}

func eqInt32Slice(a, b []int32) bool {
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
func eqInt16Slice(a, b []int16) bool {
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
func eqIntSlice(a, b []int) bool {
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
