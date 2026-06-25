//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// BenchmarkNSQAllpass_Scalar4x measures four serial scalar copies of
// the noise-shape allpass loop body — the cost the current del_dec
// inner loop pays at nStates=4, shapingLPCOrder=16.
func BenchmarkNSQAllpass_Scalar4x(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	const order = 16
	const maxLanes = 4
	aos := make([]nativeopus.NSQ_del_dec_struct, maxLanes)
	for k := 0; k < maxLanes; k++ {
		aos[k] = nativeopus.ExportTestNSQDelDecFillRandom(r)
	}
	ARshp := make([]int16, order)
	for i := range ARshp {
		ARshp[i] = int16(r.Int31())
	}
	warping := int32(0x1234)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for k := 0; k < maxLanes; k++ {
			copyK := aos[k]
			_, _ = nativeopus.ExportTestNSQAllpassScalar(&copyK, warping, ARshp, order)
		}
	}
}

// BenchmarkNSQAllpass_SoAPureGo — pure-Go SoA 4-lane kernel.
func BenchmarkNSQAllpass_SoAPureGo(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	const order = 16
	const maxLanes = 4
	aos := make([]nativeopus.NSQ_del_dec_struct, maxLanes)
	for k := 0; k < maxLanes; k++ {
		aos[k] = nativeopus.ExportTestNSQDelDecFillRandom(r)
	}
	ARshp := make([]int16, order)
	for i := range ARshp {
		ARshp[i] = int16(r.Int31())
	}
	warping := int32(0x1234)
	soa := nativeopus.ExportTestNSQDelDecAoStoSoA(aos, maxLanes)
	pristine := *soa

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset state each iter so the mutation doesn't make the
		// state converge to a degenerate pattern across iterations.
		*soa = pristine
		_ = nativeopus.ExportTestNSQAllpassSoA(soa, warping, ARshp, order)
	}
}

// BenchmarkNSQAllpass_SoASIMD — arm64 NEON asm kernel.
func BenchmarkNSQAllpass_SoASIMD(b *testing.B) {
	if !nativeopus.ExportTestNSQAllpassSIMDAvailable() {
		b.Skip("NSQ allpass SIMD not compiled in")
	}
	r := rand.New(rand.NewSource(1))
	const order = 16
	const maxLanes = 4
	aos := make([]nativeopus.NSQ_del_dec_struct, maxLanes)
	for k := 0; k < maxLanes; k++ {
		aos[k] = nativeopus.ExportTestNSQDelDecFillRandom(r)
	}
	ARshp := make([]int16, order)
	for i := range ARshp {
		ARshp[i] = int16(r.Int31())
	}
	warping := int32(0x1234)
	soa := nativeopus.ExportTestNSQDelDecAoStoSoA(aos, maxLanes)
	pristine := *soa

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		*soa = pristine
		_ = nativeopus.ExportTestNSQAllpassSIMD(soa, warping, ARshp, order)
	}
}
