//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_SilkGainsQuant(t *testing.T) {
	r := rand.New(rand.NewSource(60))
	for _, cond := range []int{0, 1} {
		for trial := 0; trial < 200; trial++ {
			n := 2 + r.Intn(3) // 2 or 4 subframes
			if n == 3 {
				n = 4
			}
			gains := make([]int32, n)
			for i := range gains {
				gains[i] = int32(100 + r.Intn(1000000))
			}
			prev := int8(r.Intn(64))
			cInd, cG, cP := cSilkGainsQuant(gains, prev, cond)
			gInd, gG, gP := nativeopus.ExportTestSilkGainsQuant(gains, prev, cond)
			if !eqInt8Slice(cInd, gInd) || !eqInt32Slice(cG, gG) || cP != gP {
				t.Fatalf("gains_quant n=%d cond=%d trial=%d gains=%v", n, cond, trial, gains)
			}
		}
	}
}

func TestParity_SilkGainsDequant(t *testing.T) {
	r := rand.New(rand.NewSource(61))
	for _, cond := range []int{0, 1} {
		for trial := 0; trial < 200; trial++ {
			n := 2 + (r.Intn(2) * 2)
			ind := make([]int8, n)
			for i := range ind {
				if cond == 0 && i == 0 {
					ind[i] = int8(r.Intn(64))
				} else {
					ind[i] = int8(r.Intn(42) - 4)
				}
			}
			prev := int8(r.Intn(64))
			cG, cP := cSilkGainsDequant(ind, prev, cond)
			gG, gP := nativeopus.ExportTestSilkGainsDequant(ind, prev, cond)
			if !eqInt32Slice(cG, gG) || cP != gP {
				t.Fatalf("gains_dequant n=%d cond=%d trial=%d ind=%v", n, cond, trial, ind)
			}
		}
	}
}

func TestParity_SilkGainsID(t *testing.T) {
	r := rand.New(rand.NewSource(62))
	for trial := 0; trial < 200; trial++ {
		n := 2 + r.Intn(3)
		ind := make([]int8, n)
		for i := range ind {
			ind[i] = int8(r.Intn(256) - 128)
		}
		if cSilkGainsID(ind) != nativeopus.ExportTestSilkGainsID(ind) {
			t.Fatalf("gains_ID ind=%v", ind)
		}
	}
}

func TestParity_SilkDecodePitch(t *testing.T) {
	r := rand.New(rand.NewSource(63))
	fsKHzs := []int{8, 12, 16}
	for _, fs := range fsKHzs {
		for _, nb := range []int{2, 4} {
			// Contour range depends on Fs and nb_subfr:
			// Fs=8,nb=4:0..10; Fs=8,nb=2:0..2; Fs>8,nb=4:0..33; Fs>8,nb=2:0..11.
			maxContour := 3
			if fs == 8 {
				if nb == 4 {
					maxContour = 11
				}
			} else {
				if nb == 4 {
					maxContour = 34
				} else {
					maxContour = 12
				}
			}
			for trial := 0; trial < 100; trial++ {
				lag := int16(r.Intn(256))
				contour := int8(r.Intn(maxContour))
				c := cSilkDecodePitch(lag, contour, fs, nb)
				g := nativeopus.ExportTestSilkDecodePitch(lag, contour, fs, nb)
				if !eqIntSlice(c, g) {
					t.Fatalf("decode_pitch fs=%d nb=%d lag=%d c=%d", fs, nb, lag, contour)
				}
			}
		}
	}
}

func TestParity_SilkStereoPredRoundtrip(t *testing.T) {
	r := rand.New(rand.NewSource(64))
	for trial := 0; trial < 200; trial++ {
		var ix [2][3]int8
		// Constraints from encode assertions: ix[n][0]<3, ix[n][1]<5, n<25.
		for i := 0; i < 2; i++ {
			ix[i][0] = int8(r.Intn(3))
			ix[i][1] = int8(r.Intn(5))
			ix[i][2] = int8(r.Intn(5))
		}
		// n = 5*ix[0][2] + ix[1][2] < 25 always (since <5 each).
		cPkt, cPred := cSilkStereoPredRoundtrip(ix, 64)
		gPkt, gPred := nativeopus.ExportTestSilkStereoPredRoundtrip(ix, 64)
		if !eqByteSlice(cPkt, gPkt) || !eqInt32Slice(cPred, gPred) {
			t.Fatalf("stereo_pred_roundtrip ix=%v", ix)
		}
	}
}

func TestParity_SilkStereoMidOnlyRoundtrip(t *testing.T) {
	for _, flag := range []int8{0, 1} {
		cPkt, cV := cSilkStereoMidOnlyRoundtrip(flag, 16)
		gPkt, gV := nativeopus.ExportTestSilkStereoMidOnlyRoundtrip(flag, 16)
		if !eqByteSlice(cPkt, gPkt) || cV != gV {
			t.Fatalf("stereo_mid_only flag=%d", flag)
		}
	}
}

func TestParity_SilkStereoQuantPred(t *testing.T) {
	r := rand.New(rand.NewSource(65))
	for trial := 0; trial < 200; trial++ {
		pred := []int32{int32(r.Intn(16384) - 8192), int32(r.Intn(16384) - 8192)}
		cOut, cIdx := cSilkStereoQuantPred(pred)
		gOut, gIdx := nativeopus.ExportTestSilkStereoQuantPred(pred)
		if !eqInt32Slice(cOut, gOut) || cIdx != gIdx {
			t.Fatalf("stereo_quant_pred pred=%v", pred)
		}
	}
}

func TestParity_SilkStereoFindPredictor(t *testing.T) {
	r := rand.New(rand.NewSource(66))
	for _, n := range []int{40, 80, 160, 320} {
		for trial := 0; trial < 20; trial++ {
			x := make([]int16, n)
			y := make([]int16, n)
			for i := range x {
				x[i] = int16(r.Intn(65536) - 32768)
				y[i] = int16(r.Intn(65536) - 32768)
			}
			amp := []int32{int32(r.Intn(1 << 16)), int32(r.Intn(1 << 16))}
			smooth := r.Intn(32768)
			cP, cR, cA := cSilkStereoFindPredictor(x, y, amp, smooth)
			gP, gR, gA := nativeopus.ExportTestSilkStereoFindPredictor(x, y, amp, smooth)
			if cP != gP || cR != gR || !eqInt32Slice(cA, gA) {
				t.Fatalf("stereo_find_predictor n=%d trial=%d", n, trial)
			}
		}
	}
}

func TestParity_SilkStereoMSToLR(t *testing.T) {
	r := rand.New(rand.NewSource(67))
	for _, fs := range []int{8, 16} {
		for _, frameLen := range []int{fs * 10, fs * 20} {
			for trial := 0; trial < 20; trial++ {
				// Input needs frame_length + 2 samples.
				x1 := make([]int16, frameLen+2)
				x2 := make([]int16, frameLen+2)
				for i := range x1 {
					x1[i] = int16(r.Intn(32768) - 16384)
					x2[i] = int16(r.Intn(32768) - 16384)
				}
				pred := []int32{int32(r.Intn(16384) - 8192), int32(r.Intn(16384) - 8192)}
				var predPrev, sMid, sSide [2]int16
				for i := 0; i < 2; i++ {
					predPrev[i] = int16(r.Intn(16384) - 8192)
					sMid[i] = int16(r.Intn(4096) - 2048)
					sSide[i] = int16(r.Intn(4096) - 2048)
				}
				c1, c2, cPP, cSM, cSS := cSilkStereoMSToLR(pred, x1, x2, predPrev, sMid, sSide, fs, frameLen)
				g1, g2, gPP, gSM, gSS := nativeopus.ExportTestSilkStereoMSToLR(pred, x1, x2, predPrev, sMid, sSide, fs, frameLen)
				if !eqInt16Slice(c1, g1) || !eqInt16Slice(c2, g2) ||
					cPP != gPP || cSM != gSM || cSS != gSS {
					t.Fatalf("stereo_MS_to_LR fs=%d frame=%d trial=%d", fs, frameLen, trial)
				}
			}
		}
	}
}
