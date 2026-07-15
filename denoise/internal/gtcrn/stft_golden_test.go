package gtcrn

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// stftGolden is the on-disk schema of testdata/stft_golden.json produced
// by tools/gtcrndump/gen_stft_golden.py from torch.stft/torch.istft.
type stftGolden struct {
	Metadata struct {
		NFFT      int    `json:"n_fft"`
		Hop       int    `json:"hop"`
		WinLength int    `json:"win_length"`
		Window    string `json:"window"`
	} `json:"metadata"`
	Signals map[string]struct {
		Input    tensor1 `json:"input"`
		StftReal tensor2 `json:"stft_real"`
		StftImag tensor2 `json:"stft_imag"`
		Istft    tensor1 `json:"istft"`
		NFrames  int     `json:"n_frames"`
	} `json:"signals"`
}

type tensor1 struct {
	Shape []int     `json:"shape"`
	Data  []float64 `json:"data"`
}
type tensor2 struct {
	Shape []int       `json:"shape"`
	Data  [][]float64 `json:"data"`
}

func loadSTFTGolden(t *testing.T) *stftGolden {
	t.Helper()
	raw, err := os.ReadFile("testdata/stft_golden.json")
	if err != nil {
		t.Fatalf("reading stft golden: %v", err)
	}
	var g stftGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("decoding stft golden: %v", err)
	}
	if g.Metadata.NFFT != NFFT || g.Metadata.Hop != Hop || g.Metadata.WinLength != NFFT {
		t.Fatalf("golden geometry n_fft=%d hop=%d win=%d != port %d/%d/%d",
			g.Metadata.NFFT, g.Metadata.Hop, g.Metadata.WinLength, NFFT, Hop, NFFT)
	}
	return &g
}

// TestSTFTMatchesTorch gates the analysis front end: Go STFT vs
// torch.stft, per bin, over the deterministic signal set. This is the
// #1 silent-divergence guard — a wrong sqrt-Hann normalisation would
// pass ORT parity on both sides while degrading audio.
func TestSTFTMatchesTorch(t *testing.T) {
	g := loadSTFTGolden(t)
	const tol = 1e-4
	for name, sig := range g.Signals {
		x := make([]float32, len(sig.Input.Data))
		for i, v := range sig.Input.Data {
			x[i] = float32(v)
		}
		re, im, frames := STFT(x)
		if frames != sig.NFrames {
			t.Errorf("%s: frames=%d want %d", name, frames, sig.NFrames)
			continue
		}
		maxErr := 0.0
		for k := 0; k < Bins; k++ {
			for f := 0; f < frames; f++ {
				dr := math.Abs(float64(re[k*frames+f]) - sig.StftReal.Data[k][f])
				di := math.Abs(float64(im[k*frames+f]) - sig.StftImag.Data[k][f])
				if dr > maxErr {
					maxErr = dr
				}
				if di > maxErr {
					maxErr = di
				}
			}
		}
		if maxErr > tol {
			t.Errorf("%s: STFT max|Δ|=%.3e > %g", name, maxErr, tol)
		}
		t.Logf("%s: %d frames, STFT max|Δ|=%.3e", name, frames, maxErr)
	}
}

// TestISTFTMatchesTorch gates the synthesis front end over the interior
// region (edges differ by the length/center-trim convention and are
// excluded, matching the golden's own interior round-trip measurement).
func TestISTFTMatchesTorch(t *testing.T) {
	g := loadSTFTGolden(t)
	const tol = 1e-4
	for name, sig := range g.Signals {
		frames := sig.NFrames
		re := make([]float32, Bins*frames)
		im := make([]float32, Bins*frames)
		for k := 0; k < Bins; k++ {
			for f := 0; f < frames; f++ {
				re[k*frames+f] = float32(sig.StftReal.Data[k][f])
				im[k*frames+f] = float32(sig.StftImag.Data[k][f])
			}
		}
		got := ISTFT(re, im, frames)
		want := sig.Istft.Data

		n := len(got)
		if len(want) < n {
			n = len(want)
		}
		maxErr := 0.0
		for i := NFFT; i < n-NFFT; i++ {
			d := math.Abs(float64(got[i]) - want[i])
			if d > maxErr {
				maxErr = d
			}
		}
		if maxErr > tol {
			t.Errorf("%s: ISTFT interior max|Δ|=%.3e > %g", name, maxErr, tol)
		}
		t.Logf("%s: ISTFT interior max|Δ|=%.3e (got %d, want %d)", name, maxErr, len(got), len(want))
	}
}
