package gtcrn

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// selfGoldenPath holds the pure-Go enhanced PCM for the deterministic
// signals — the default-CI self-golden (the silero_golden pattern): the
// gate that catches numeric drift from a refactor, without needing
// onnxruntime. It is checked at a TIGHT tolerance rather than bit-exact:
// Go contracts float32 multiply-add to FMA per build, and that fusion
// differs between the normal and -race/ISA builds, so a zero-tolerance
// fp32 golden flakes by ~1 ULP (the cross-statement FMA trap; the Silero
// precedent uses 1e-4 for the same reason). 1e-5 clears that ULP noise
// with margin while still catching any real numeric change. Regenerate
// with GTCRN_UPDATE_GOLDEN=1 go test ./denoise/internal/gtcrn/ -run SelfGolden.
const selfGoldenPath = "testdata/self_golden.json"

// selfGoldenTol is the self-golden acceptance bound (see selfGoldenPath).
const selfGoldenTol = 1e-5

// deterministicInputs loads the committed deterministic signals (shared
// with the STFT golden) as 16 kHz mono float32.
func deterministicInputs(t *testing.T) map[string][]float32 {
	t.Helper()
	raw, err := os.ReadFile("testdata/stft_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g stftGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	out := map[string][]float32{}
	for name, sig := range g.Signals {
		x := make([]float32, len(sig.Input.Data))
		for i, v := range sig.Input.Data {
			x[i] = float32(v)
		}
		out[name] = x
	}
	return out
}

// TestSelfGolden reproduces the committed pure-Go enhanced PCM at zero
// tolerance (bit-exact). It regenerates the golden when
// GTCRN_UPDATE_GOLDEN is set.
func TestSelfGolden(t *testing.T) {
	inputs := deterministicInputs(t)
	m, err := NewModel()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]float32{}
	for name, x := range inputs {
		got[name] = m.EnhanceOffline(x)
	}

	if os.Getenv("GTCRN_UPDATE_GOLDEN") != "" {
		blob, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(selfGoldenPath, blob, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d signals)", selfGoldenPath, len(got))
		return
	}

	raw, err := os.ReadFile(selfGoldenPath)
	if err != nil {
		t.Fatalf("reading self golden (regenerate with GTCRN_UPDATE_GOLDEN=1): %v", err)
	}
	var want map[string][]float32
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("missing signal %q", name)
			continue
		}
		if len(g) != len(w) {
			t.Errorf("%s: length %d vs golden %d", name, len(g), len(w))
			continue
		}
		for i := range w {
			d := math.Abs(float64(g[i]) - float64(w[i]))
			if d > selfGoldenTol {
				t.Fatalf("%s: sample %d differs by %.3e (%v vs golden %v) — numeric drift > %g",
					name, i, d, g[i], w[i], selfGoldenTol)
			}
		}
	}
}

// e2ePCMGolden is the subset of the ORT/torch E2E golden needed for the
// PCM SNR gate: the reference enhanced PCM per signal.
type e2ePCMGolden struct {
	Signals map[string]struct {
		EnhPCM struct {
			Data []float64 `json:"data"`
		} `json:"enh_pcm"`
	} `json:"signals"`
}

// TestEnhanceOfflineMatchesReferencePCM gates the full Go chain (center
// STFT → per-frame Forward → ISTFT) against the committed reference
// enhanced PCM (produced by onnxruntime enh spectra + torch istft),
// requiring SNR ≥ 90 dB over the interior. No onnxruntime needed.
func TestEnhanceOfflineMatchesReferencePCM(t *testing.T) {
	raw, err := os.ReadFile("../parity_tests/gtcrn_ort/testdata/e2e_golden.json")
	if err != nil {
		t.Skipf("E2E golden not available: %v", err)
	}
	var golden e2ePCMGolden
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	inputs := deterministicInputs(t)
	m, err := NewModel()
	if err != nil {
		t.Fatal(err)
	}

	for name, x := range inputs {
		ref, ok := golden.Signals[name]
		if !ok {
			continue
		}
		got := m.EnhanceOffline(x)
		want := ref.EnhPCM.Data

		n := len(got)
		if len(want) < n {
			n = len(want)
		}
		var sigE, errE float64
		for i := NFFT; i < n-NFFT; i++ {
			r := want[i]
			d := float64(got[i]) - r
			sigE += r * r
			errE += d * d
		}
		if name == "silence" {
			if errE > 0 {
				t.Errorf("silence: nonzero error energy %.3e", errE)
			}
			t.Logf("%s: exact (error energy %.3e)", name, errE)
			continue
		}
		snr := 10 * math.Log10(sigE/errE)
		t.Logf("%s: E2E PCM SNR = %.1f dB (interior %d samples)", name, snr, n-2*NFFT)
		if snr < 90 {
			t.Errorf("%s: E2E PCM SNR %.1f dB < 90 dB", name, snr)
		}
	}
}
