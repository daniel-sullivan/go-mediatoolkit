//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func randB_Q28(r *rand.Rand) []int32 {
	b := make([]int32, 3)
	for i := range b {
		b[i] = int32(r.Intn(1<<24)) - (1 << 23)
	}
	return b
}
func randA_Q28(r *rand.Rand) []int32 {
	a := make([]int32, 2)
	for i := range a {
		a[i] = int32(r.Intn(1<<24)) - (1 << 23)
	}
	return a
}

func TestParity_SilkBiquadAltStride1(t *testing.T) {
	r := rand.New(rand.NewSource(20))
	for _, n := range []int{2, 8, 40, 80, 200} {
		for trial := 0; trial < 30; trial++ {
			in_ := make([]int16, n)
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			B := randB_Q28(r)
			A := randA_Q28(r)
			S := []int32{int32(r.Int31()) >> 10, int32(r.Int31()) >> 10}
			wo, wS := cSilkBiquadAltStride1(in_, B, A, S)
			go_ := append([]int32(nil), S...)
			gOut, gS := nativeopus.ExportTestSilkBiquadAltStride1(in_, B, A, go_)
			if !eqInt16Slice(wo, gOut) || !eqInt32Slice(wS, gS) {
				t.Fatalf("biquad_alt_stride1 n=%d trial=%d", n, trial)
			}
		}
	}
}

func TestParity_SilkBiquadAltStride2(t *testing.T) {
	r := rand.New(rand.NewSource(21))
	for _, n := range []int{4, 16, 40, 80, 200} { // must be even (n interleaved samples)
		for trial := 0; trial < 30; trial++ {
			in_ := make([]int16, n*2)
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			B := randB_Q28(r)
			A := randA_Q28(r)
			S := []int32{int32(r.Int31()) >> 10, int32(r.Int31()) >> 10,
				int32(r.Int31()) >> 10, int32(r.Int31()) >> 10}
			wo, wS := cSilkBiquadAltStride2(in_, B, A, S)
			gOut, gS := nativeopus.ExportTestSilkBiquadAltStride2(in_, B, A, append([]int32(nil), S...))
			if !eqInt16Slice(wo, gOut) || !eqInt32Slice(wS, gS) {
				t.Fatalf("biquad_alt_stride2 n=%d trial=%d", n, trial)
			}
		}
	}
}

func TestParity_SilkAnaFiltBank1(t *testing.T) {
	r := rand.New(rand.NewSource(22))
	for _, N := range []int{2, 8, 40, 80, 200} {
		for trial := 0; trial < 30; trial++ {
			in_ := make([]int16, N)
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			S := []int32{int32(r.Int31()) >> 10, int32(r.Int31()) >> 10}
			wL, wH, wS := cSilkAnaFiltBank1(in_, S)
			gL, gH, gS := nativeopus.ExportTestSilkAnaFiltBank1(in_, append([]int32(nil), S...))
			if !eqInt16Slice(wL, gL) || !eqInt16Slice(wH, gH) || !eqInt32Slice(wS, gS) {
				t.Fatalf("silk_ana_filt_bank_1 N=%d", N)
			}
		}
	}
}

func TestParity_SilkLPVariableCutoff(t *testing.T) {
	r := rand.New(rand.NewSource(23))
	// Exercise mode=0 (pass-through), mode>0, mode<0, and full trans range.
	for _, mode := range []int{0, 1, -1, 2, -2} {
		for _, trans := range []int32{0, 1, 10, 50, 100, 200, 256} {
			for trial := 0; trial < 10; trial++ {
				n := 100
				frame := make([]int16, n)
				for i := range frame {
					frame[i] = int16(r.Intn(65536) - 32768)
				}
				S := []int32{int32(r.Int31()) >> 10, int32(r.Int31()) >> 10}
				// If trans out of range for Cs assert, skip those.
				if trans < 0 || trans > 256 {
					continue
				}
				// C silk_assert: transition_frame_no in [0, TRANSITION_FRAMES].
				// TRANSITION_FRAMES = TRANSITION_TIME_MS / MAX_FRAME_LENGTH_MS.
				// Compute via ExportTest.
				if trans > int32(nativeopus.ExportTestTransitionFrames()) {
					continue
				}
				wo, wS, wT := cSilkLPVariableCutoff(frame, mode, trans, S)
				go_ := append([]int32(nil), S...)
				gOut, gS, gT := nativeopus.ExportTestSilkLPVariableCutoff(frame, mode, trans, go_)
				if !eqInt16Slice(wo, gOut) || !eqInt32Slice(wS, gS) || wT != gT {
					t.Fatalf("silk_LP_variable_cutoff mode=%d trans=%d trial=%d\nwo=%v\ngOut=%v",
						mode, trans, trial, wo, gOut)
				}
			}
		}
	}
}
