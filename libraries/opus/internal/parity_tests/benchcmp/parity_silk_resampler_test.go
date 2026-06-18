//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_SilkResamplerAR2(t *testing.T) {
	r := rand.New(rand.NewSource(40))
	for _, n := range []int{1, 8, 40, 80, 200} {
		for trial := 0; trial < 30; trial++ {
			in_ := make([]int16, n)
			A := []int16{int16(r.Intn(1 << 14)), int16(r.Intn(1 << 14))}
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			S := []int32{int32(r.Int31()) >> 10, int32(r.Int31()) >> 10}
			wo, wS := cSilkResamplerAR2(S, in_, A)
			go_, gS := nativeopus.ExportTestSilkResamplerAR2(S, in_, A)
			if !eqInt32Slice(wo, go_) || !eqInt32Slice(wS, gS) {
				t.Fatalf("AR2 n=%d trial=%d", n, trial)
			}
		}
	}
}

func TestParity_SilkResamplerDown2(t *testing.T) {
	r := rand.New(rand.NewSource(41))
	for _, n := range []int{2, 80, 200, 480} {
		for trial := 0; trial < 30; trial++ {
			in_ := make([]int16, n)
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			S := []int32{int32(r.Int31()) >> 10, int32(r.Int31()) >> 10}
			wo, wS := cSilkResamplerDown2(S, in_)
			go_, gS := nativeopus.ExportTestSilkResamplerDown2(S, in_)
			if !eqInt16Slice(wo, go_) || !eqInt32Slice(wS, gS) {
				t.Fatalf("down2 n=%d trial=%d", n, trial)
			}
		}
	}
}

func TestParity_SilkResamplerDown23(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for _, n := range []int{12, 30, 120, 480} { // multiples of 3
		for trial := 0; trial < 20; trial++ {
			in_ := make([]int16, n)
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			S := make([]int32, 6)
			for i := range S {
				S[i] = int32(r.Int31()) >> 10
			}
			wo, wS := cSilkResamplerDown23(S, in_)
			go_, gS := nativeopus.ExportTestSilkResamplerDown23(S, in_)
			if !eqInt16Slice(wo, go_) || !eqInt32Slice(wS, gS) {
				t.Fatalf("down2_3 n=%d trial=%d", n, trial)
			}
		}
	}
}

func TestParity_SilkResamplerUp2HQ(t *testing.T) {
	r := rand.New(rand.NewSource(43))
	for _, n := range []int{1, 8, 80, 200, 480} {
		for trial := 0; trial < 20; trial++ {
			in_ := make([]int16, n)
			for i := range in_ {
				in_[i] = int16(r.Intn(65536) - 32768)
			}
			S := make([]int32, 6)
			for i := range S {
				S[i] = int32(r.Int31()) >> 10
			}
			wo, wS := cSilkResamplerUp2HQ(S, in_)
			go_, gS := nativeopus.ExportTestSilkResamplerUp2HQ(S, in_)
			if !eqInt16Slice(wo, go_) || !eqInt32Slice(wS, gS) {
				t.Fatalf("up2_HQ n=%d trial=%d", n, trial)
			}
		}
	}
}

func TestParity_SilkResamplerFull(t *testing.T) {
	r := rand.New(rand.NewSource(44))
	rates := []int{8000, 12000, 16000, 24000, 48000}
	for _, fin := range rates {
		for _, fout := range rates {
			// Based on encoder/decoder direction restrictions.
			for _, forEnc := range []int{0, 1} {
				if forEnc == 1 && (fout != 8000 && fout != 12000 && fout != 16000) {
					continue
				}
				if forEnc == 0 && (fin != 8000 && fin != 12000 && fin != 16000) {
					continue
				}
				// Multiple-of-Fs_in_kHz length.
				n := fin / 1000 * 20 // 20 ms frame
				in_ := make([]int16, n)
				for i := range in_ {
					in_[i] = int16(r.Intn(65536) - 32768)
				}
				wo, wR := cSilkResampler(fin, fout, in_, forEnc)
				go_, gR := nativeopus.ExportTestSilkResampler(fin, fout, in_, forEnc)
				if wR != gR || !eqInt16Slice(wo, go_) {
					t.Fatalf("silk_resampler %d->%d forEnc=%d: wR=%d gR=%d",
						fin, fout, forEnc, wR, gR)
				}
			}
		}
	}
}
