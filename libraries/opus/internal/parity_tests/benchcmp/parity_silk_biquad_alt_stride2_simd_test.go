//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_SilkBiquadAltStride2_SIMDRef asserts the pure-Go
// 4-lane SIMD-style reference produces bit-identical output samples
// and state updates to the scalar C reference across a broad sweep
// of stereo-interleaved inputs. This is the correctness gate for
// the arm64 NEON port: the asm must match this reference (which
// matches the scalar C) — see libopus'
// silk/arm/biquad_alt_neon_intr.c OPUS_CHECK_ASM block at lines
// 73-82 and 151-154, which asserts the same memcmp equivalence
// upstream.
//
// Coverage: lengths in {2, 16, 40, 80, 160} × 5 seeded random
// trials; B_Q28 and A_Q28 drawn from the same Q28 AR range used by
// the existing TestParity_SilkBiquadAltStride2 (int32(Intn(1<<24)) -
// (1<<23)); S state drawn from int31 >> 10 per lane; inputs are
// full-range int16. Checks both output arrays and the final S state
// for bit-identity.
func TestParity_SilkBiquadAltStride2_SIMDRef(t *testing.T) {
	r := rand.New(rand.NewSource(2029))
	for _, L := range []int{2, 16, 40, 80, 160} {
		for trial := 0; trial < 5; trial++ {
			in_ := make([]int16, 2*L) // L stereo pairs = 2*L int16s.
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			B := randB_Q28(r)
			A := randA_Q28(r)
			S := []int32{
				int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
				int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
			}
			// Scalar Go reference (oracle we must match).
			wOut, wS := nativeopus.ExportTestSilkBiquadAltStride2(
				in_, B, A, append([]int32(nil), S...))
			// Pure-Go 4-lane SIMD reference under test.
			gOut, gS := nativeopus.ExportTestSilkBiquadAltStride2SIMDRef(
				in_, B, A, append([]int32(nil), S...))
			if !eqInt16Slice(wOut, gOut) {
				t.Fatalf("SIMD ref output mismatch: L=%d trial=%d\nwant=%v\ngot=%v",
					L, trial, wOut, gOut)
			}
			if !eqInt32Slice(wS, gS) {
				t.Fatalf("SIMD ref S-state mismatch: L=%d trial=%d\nwant=%v\ngot=%v",
					L, trial, wS, gS)
			}
		}
	}
}

// TestParity_SilkBiquadAltStride2_SIMDRefVsC cross-checks the pure-Go
// 4-lane SIMD reference directly against the upstream C kernel (no
// intermediate Go-scalar hop). Catches any drift between the Go
// scalar and the SIMD reference that would otherwise be masked by
// both passing the Go-vs-Go parity gate in TestParity_SilkBiquadAltStride2.
func TestParity_SilkBiquadAltStride2_SIMDRefVsC(t *testing.T) {
	r := rand.New(rand.NewSource(2030))
	for _, L := range []int{2, 16, 40, 80, 160} {
		for trial := 0; trial < 5; trial++ {
			in_ := make([]int16, 2*L)
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			B := randB_Q28(r)
			A := randA_Q28(r)
			S := []int32{
				int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
				int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
			}
			cOut, cS := cSilkBiquadAltStride2(in_, B, A, S)
			gOut, gS := nativeopus.ExportTestSilkBiquadAltStride2SIMDRef(
				in_, B, A, append([]int32(nil), S...))
			if !eqInt16Slice(cOut, gOut) {
				t.Fatalf("SIMD ref vs C output mismatch: L=%d trial=%d\nwant=%v\ngot=%v",
					L, trial, cOut, gOut)
			}
			if !eqInt32Slice(cS, gS) {
				t.Fatalf("SIMD ref vs C S-state mismatch: L=%d trial=%d\nwant=%v\ngot=%v",
					L, trial, cS, gS)
			}
		}
	}
}

// TestParity_SilkBiquadAltStride2_Arch walks the arch-aware dispatch
// (SIMD when compiled in, scalar C otherwise). The dispatcher is
// still required to be bit-exact with the scalar C (per libopus's
// OPUS_CHECK_ASM assertion in biquad_alt_neon_intr.c lines 151-154),
// so any drift here is a silent SILK-encoder-breaking bug.
func TestParity_SilkBiquadAltStride2_Arch(t *testing.T) {
	if !nativeopus.ExportTestSilkBiquadAltStride2SIMDAvailable() {
		t.Skip("silk_biquad_alt_stride2 SIMD not compiled in (opus_nosimd or opus_strict)")
	}
	r := rand.New(rand.NewSource(2031))
	for _, L := range []int{2, 16, 40, 80, 160} {
		for trial := 0; trial < 5; trial++ {
			in_ := make([]int16, 2*L)
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			B := randB_Q28(r)
			A := randA_Q28(r)
			S := []int32{
				int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
				int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
			}
			cOut, cS := cSilkBiquadAltStride2(in_, B, A, S)
			gOut, gS := nativeopus.ExportTestSilkBiquadAltStride2Arch(
				in_, B, A, append([]int32(nil), S...))
			if !eqInt16Slice(cOut, gOut) {
				t.Fatalf("arch-dispatched SIMD output mismatch: L=%d trial=%d\nwant=%v\ngot=%v",
					L, trial, cOut, gOut)
			}
			if !eqInt32Slice(cS, gS) {
				t.Fatalf("arch-dispatched SIMD S-state mismatch: L=%d trial=%d\nwant=%v\ngot=%v",
					L, trial, cS, gS)
			}
		}
	}
}

// BenchmarkSilkBiquadAltStride2Scalar — scalar Go path, L=80 stereo
// samples, matches the benchmark knob used in the task spec.
func BenchmarkSilkBiquadAltStride2Scalar(b *testing.B) {
	r := rand.New(rand.NewSource(99))
	const L = 80
	in_ := make([]int16, 2*L)
	for i := range in_ {
		in_[i] = int16(r.Intn(65536) - 32768)
	}
	B := randB_Q28(r)
	A := randA_Q28(r)
	S := []int32{
		int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
		int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nativeopus.ExportTestSilkBiquadAltStride2(in_, B, A, S)
	}
}

// BenchmarkSilkBiquadAltStride2SIMDRef — pure-Go 4-lane SIMD ref.
func BenchmarkSilkBiquadAltStride2SIMDRef(b *testing.B) {
	r := rand.New(rand.NewSource(99))
	const L = 80
	in_ := make([]int16, 2*L)
	for i := range in_ {
		in_[i] = int16(r.Intn(65536) - 32768)
	}
	B := randB_Q28(r)
	A := randA_Q28(r)
	S := []int32{
		int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
		int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nativeopus.ExportTestSilkBiquadAltStride2SIMDRef(in_, B, A, S)
	}
}
