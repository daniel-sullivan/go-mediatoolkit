//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_SilkCheckControlInput(t *testing.T) {
	// A set of known-valid inputs plus targeted mutations.
	bases := []struct {
		nAPI, nInt              int32
		fs, maxFs, minFs, desFs int32
		plMs                    int
	}{
		{1, 1, 48000, 16000, 8000, 16000, 20},
		{2, 2, 24000, 16000, 8000, 12000, 40},
		{1, 1, 8000, 8000, 8000, 8000, 10},
		{2, 1, 16000, 16000, 16000, 16000, 60},
	}
	for i, b := range bases {
		g := nativeopus.ExportTestCheckControlInput(
			b.nAPI, b.nInt, b.fs, b.maxFs, b.minFs, b.desFs,
			b.plMs, 16000, 0, 5, 0, 0, 0, 1024, 0, 0, 0)
		c := cCheckControlInput(b.nAPI, b.nInt, b.fs, b.maxFs, b.minFs, b.desFs,
			b.plMs, 16000, 0, 5, 0, 0, 0, 1024, 0, 0, 0)
		if g != c {
			t.Fatalf("case %d: valid C=%d Go=%d", i, c, g)
		}
	}
	// Invalid inputs cannot be parity-tested via cgo because the C build
	// has ENABLE_HARDENING on: celt_assert(0) fatally exits the process.
	// The Go port's error codes are verified by read-through of the
	// 1:1 translation.
}

func TestParity_SilkControlSNR(t *testing.T) {
	r := rand.New(rand.NewSource(700))
	for _, fs_kHz := range []int{8, 12, 16} {
		for _, nb_subfr := range []int{2, 4} {
			for trial := 0; trial < 200; trial++ {
				tr := int32(500 + r.Intn(100000))
				gSnr, gStored := nativeopus.ExportTestControlSNR(fs_kHz, nb_subfr, tr)
				cSnr, cStored := cControlSNR(fs_kHz, nb_subfr, tr)
				if gSnr != cSnr || gStored != cStored {
					t.Fatalf("fs=%d nb=%d tr=%d C=(%d,%d) Go=(%d,%d)",
						fs_kHz, nb_subfr, tr, cSnr, cStored, gSnr, gStored)
				}
			}
		}
	}
}

func TestParity_SilkControlAudioBandwidth(t *testing.T) {
	r := rand.New(rand.NewSource(701))
	for trial := 0; trial < 400; trial++ {
		fsOpts := []int32{8000, 12000, 16000}
		stIn := nativeopus.TestABState{
			Fs_kHz:               []int{0, 8, 12, 16}[r.Intn(4)],
			SavedFsKHz:           []int32{0, 8, 12, 16}[r.Intn(4)],
			TransitionFrameNo:    int32(r.Intn(60)),
			Mode:                 []int{-2, -1, 0, 1}[r.Intn(4)],
			AllowBandwidthSwitch: r.Intn(2),
			APIfsHz:              []int32{8000, 12000, 16000, 24000, 48000}[r.Intn(5)],
			MaxInternalfsHz:      int(fsOpts[r.Intn(3)]),
			MinInternalfsHz:      int(fsOpts[r.Intn(3)]),
			DesiredInternalfsHz:  int(fsOpts[r.Intn(3)]),
			InLPState0:           int32(r.Intn(1000) - 500),
			InLPState1:           int32(r.Intn(1000) - 500),
		}
		ecIn := nativeopus.TestABCtrl{
			OpusCanSwitch:  r.Intn(2),
			SwitchReady:    0,
			MaxBits:        1024 + r.Intn(20000),
			PayloadSize_ms: []int{10, 20, 40, 60}[r.Intn(4)],
		}
		cEcIn := CABCtrl{
			OpusCanSwitch:  ecIn.OpusCanSwitch,
			SwitchReady:    ecIn.SwitchReady,
			MaxBits:        ecIn.MaxBits,
			PayloadSize_ms: ecIn.PayloadSize_ms,
		}
		cStIn := CABState{
			Fs_kHz:               stIn.Fs_kHz,
			SavedFsKHz:           stIn.SavedFsKHz,
			TransitionFrameNo:    stIn.TransitionFrameNo,
			Mode:                 stIn.Mode,
			AllowBandwidthSwitch: stIn.AllowBandwidthSwitch,
			APIfsHz:              stIn.APIfsHz,
			MaxInternalfsHz:      stIn.MaxInternalfsHz,
			MinInternalfsHz:      stIn.MinInternalfsHz,
			DesiredInternalfsHz:  stIn.DesiredInternalfsHz,
			InLPState0:           stIn.InLPState0,
			InLPState1:           stIn.InLPState1,
		}
		gFs, gStOut, gEcOut := nativeopus.ExportTestControlAudioBandwidth(stIn, ecIn)
		cFs, cStOut, cEcOut := cControlAudioBandwidth(cStIn, cEcIn)
		if gFs != cFs ||
			gStOut.Fs_kHz != cStOut.Fs_kHz ||
			gStOut.Mode != cStOut.Mode ||
			gStOut.TransitionFrameNo != cStOut.TransitionFrameNo ||
			gStOut.InLPState0 != cStOut.InLPState0 ||
			gStOut.InLPState1 != cStOut.InLPState1 ||
			gEcOut.SwitchReady != cEcOut.SwitchReady ||
			gEcOut.MaxBits != cEcOut.MaxBits {
			t.Fatalf("trial %d\n  stIn=%+v ecIn=%+v\n  Go fs=%d st=%+v ec=%+v\n  C  fs=%d st=%+v ec=%+v",
				trial, stIn, ecIn, gFs, gStOut, gEcOut, cFs, cStOut, cEcOut)
		}
	}
}

func TestParity_SilkVADInit(t *testing.T) {
	cNL, cInv, cBias, cSmth, cCnt := cVADInit()
	gNL, gInv, gBias, gSmth, gCnt := nativeopus.ExportTestVADInit()
	if gCnt != cCnt {
		t.Fatalf("counter C=%d Go=%d", cCnt, gCnt)
	}
	if !eqInt32Slice(gNL, cNL) || !eqInt32Slice(gInv, cInv) ||
		!eqInt32Slice(gBias, cBias) || !eqInt32Slice(gSmth, cSmth) {
		t.Fatalf("VAD_Init mismatch:\nNL C=%v Go=%v\ninvNL C=%v Go=%v\nbias C=%v Go=%v\nsmth C=%v Go=%v",
			cNL, gNL, cInv, gInv, cBias, gBias, cSmth, gSmth)
	}
}

func TestParity_SilkVADGetSAQ8(t *testing.T) {
	r := rand.New(rand.NewSource(702))
	// frame_length must satisfy fs_kHz multiples of 10 or 20 ms, 8-sample alignment.
	cases := []struct{ fs_kHz, frame_length int }{
		{8, 80}, {8, 160},
		{12, 120}, {12, 240},
		{16, 160}, {16, 320},
	}
	for _, cs := range cases {
		for trial := 0; trial < 30; trial++ {
			frame := make([]int16, cs.frame_length)
			amp := 1 + r.Intn(10000)
			for i := range frame {
				frame[i] = int16(r.Intn(2*amp) - amp)
			}
			cSA, cTilt, cIQB, cNL, cInv, cXnrg, cSmth := cVADGetSAQ8(frame, cs.fs_kHz)
			gSA, gTilt, gIQB, gNL, gInv, gXnrg, gSmth := nativeopus.ExportTestVADGetSAQ8(frame, cs.frame_length, cs.fs_kHz)
			if gSA != cSA || gTilt != cTilt || gIQB != cIQB ||
				gNL != cNL || gInv != cInv || gXnrg != cXnrg || gSmth != cSmth {
				t.Fatalf("fs=%d fl=%d trial=%d\nC  SA=%d tilt=%d iqb=%v NL=%v inv=%v Xnrg=%v smth=%v\nGo SA=%d tilt=%d iqb=%v NL=%v inv=%v Xnrg=%v smth=%v",
					cs.fs_kHz, cs.frame_length, trial,
					cSA, cTilt, cIQB, cNL, cInv, cXnrg, cSmth,
					gSA, gTilt, gIQB, gNL, gInv, gXnrg, gSmth)
			}
		}
	}
}

func TestParity_SilkHPVariableCutoff(t *testing.T) {
	r := rand.New(rand.NewSource(703))
	for trial := 0; trial < 500; trial++ {
		prevST := r.Intn(3) // 0=no voice, 1=unvoiced, 2=voiced
		prevLag := 40 + r.Intn(180)
		fs_kHz := []int{8, 12, 16}[r.Intn(3)]
		iq0 := r.Intn(32768)
		saQ8 := r.Intn(257)
		smth1 := int32(1 << 15 * (6 + r.Intn(20)))
		cOut := cHPVariableCutoff(prevST, prevLag, fs_kHz, iq0, saQ8, smth1)
		gOut := nativeopus.ExportTestHPVariableCutoff(prevST, prevLag, fs_kHz, iq0, saQ8, smth1)
		if cOut != gOut {
			t.Fatalf("trial %d (pst=%d lag=%d fs=%d iq0=%d saQ8=%d smth1=%d): C=%d Go=%d",
				trial, prevST, prevLag, fs_kHz, iq0, saQ8, smth1, cOut, gOut)
		}
	}
}

func TestParity_SilkInitEncoder(t *testing.T) {
	for _, arch := range []int{0, 1, 2} {
		cFF, cS1, cS2, cVC, cNL := cInitEncoder(arch)
		gFF, gS1, gS2, gVC, gNL := nativeopus.ExportTestInitEncoder(arch)
		if cFF != gFF || cS1 != gS1 || cS2 != gS2 || cVC != gVC || cNL != gNL {
			t.Fatalf("arch=%d C=(%d %d %d %d %v) Go=(%d %d %d %d %v)",
				arch, cFF, cS1, cS2, cVC, cNL, gFF, gS1, gS2, gVC, gNL)
		}
	}
}

// makePSDMatrix5 builds a positive-semidefinite 5x5 matrix of Q17 values
// as the sum of a few rank-1 outer products plus a diagonal ridge so the
// matrix's rows represent plausible LTP-correlation values.
func makePSDMatrix5(r *rand.Rand) []int32 {
	XX := make([]int32, 25)
	// Add diagonal ridge first.
	for d := 0; d < 5; d++ {
		XX[d*5+d] = int32(r.Int31n(1 << 18))
	}
	// Two random rank-1 components.
	for rep := 0; rep < 2; rep++ {
		var v [5]int64
		for i := range v {
			v[i] = int64(r.Int31n(1<<10)) - (1 << 9)
		}
		for i := 0; i < 5; i++ {
			for j := 0; j < 5; j++ {
				XX[i*5+j] += int32(v[i] * v[j])
			}
		}
	}
	return XX
}

func TestParity_SilkVQWMatEC(t *testing.T) {
	r := rand.New(rand.NewSource(704))
	for trial := 0; trial < 200; trial++ {
		L := 8 + r.Intn(25)
		XX := makePSDMatrix5(r)
		xX := make([]int32, 5)
		for i := range xX {
			xX[i] = int32(r.Int31n(1<<20) - (1 << 19))
		}
		cb := make([]int8, L*5)
		for i := range cb {
			cb[i] = int8(r.Intn(256) - 128)
		}
		cbg := make([]uint8, L)
		for i := range cbg {
			cbg[i] = uint8(r.Intn(256))
		}
		cl := make([]uint8, L)
		for i := range cl {
			cl[i] = uint8(1 + r.Intn(255))
		}
		subfr := 20 + r.Intn(60)
		maxGain := int32(r.Intn(2048))
		cInd, cRn, cRd, cG := cVQWMatEC(XX, xX, cb, cbg, cl, subfr, maxGain, L)
		gInd, gRn, gRd, gG := nativeopus.ExportTestVQWMatEC(XX, xX, cb, cbg, cl, subfr, maxGain, L)
		if cInd != gInd || cRn != gRn || cRd != gRd || cG != gG {
			t.Fatalf("trial %d: C=(%d %d %d %d) Go=(%d %d %d %d)", trial,
				cInd, cRn, cRd, cG, gInd, gRn, gRd, gG)
		}
	}
}

func TestParity_SilkQuantLTPGains(t *testing.T) {
	r := rand.New(rand.NewSource(705))
	for _, nb_subfr := range []int{2, 4} {
		for trial := 0; trial < 50; trial++ {
			XX := make([]int32, 0, nb_subfr*25)
			for s := 0; s < nb_subfr; s++ {
				XX = append(XX, makePSDMatrix5(r)...)
			}
			xX := make([]int32, nb_subfr*5)
			for i := range xX {
				xX[i] = int32(r.Int31n(1<<20)) - (1 << 19)
			}
			slgIn := int32(r.Intn(2048))
			subfr := 20 + r.Intn(60)
			cB, cCbk, cPer, cSlg, cPg := cQuantLTPGains(XX, xX, slgIn, subfr, nb_subfr)
			gB, gCbk, gPer, gSlg, gPg := nativeopus.ExportTestQuantLTPGains(XX, xX, slgIn, subfr, nb_subfr)
			if !eqInt16Slice(cB, gB) || cCbk != gCbk || cPer != gPer || cSlg != gSlg || cPg != gPg {
				t.Fatalf("trial %d (nb_subfr=%d):\nC  B=%v cbk=%v per=%d slg=%d pg=%d\nGo B=%v cbk=%v per=%d slg=%d pg=%d",
					trial, nb_subfr, cB, cCbk, cPer, cSlg, cPg, gB, gCbk, gPer, gSlg, gPg)
			}
		}
	}
}
