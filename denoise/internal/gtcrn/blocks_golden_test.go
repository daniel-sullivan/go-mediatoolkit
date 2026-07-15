package gtcrn

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// blocksGolden is testdata/blocks_golden.json: per-frame intermediate
// tensors captured from the reference streaming model (VERSION). Torch
// Version-2 intermediates; the committed torch-vs-ORT enh gap is 6.85e-7.
type blocksGolden struct {
	Metadata struct {
		Signal     string   `json:"signal"`
		NFrames    int      `json:"n_frames"`
		BlockOrder []string `json:"block_order"`
	} `json:"metadata"`
	Frames []struct {
		Frame  int                `json:"frame"`
		Blocks map[string]block4D `json:"blocks"`
	} `json:"frames"`
}

type block4D struct {
	Shape []int           `json:"shape"`
	Data  [][][][]float64 `json:"data"`
}

// flat returns the [1,C,1,F] tensor as [C*F] (index c*F+f). For enh
// ([1,257,1,2]) this yields index k*2+c, matching the port's enh layout.
func (b block4D) flat() []float32 {
	C, F := b.Shape[1], b.Shape[3]
	data := make([]float32, C*F)
	for c := 0; c < C; c++ {
		for f := 0; f < F; f++ {
			data[c*F+f] = float32(b.Data[0][c][0][f])
		}
	}
	return data
}

func loadBlocksGolden(t *testing.T) *blocksGolden {
	t.Helper()
	raw, err := os.ReadFile("testdata/blocks_golden.json")
	if err != nil {
		t.Fatalf("reading blocks golden: %v", err)
	}
	var g blocksGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("decoding blocks golden: %v", err)
	}
	return &g
}

// blockErr returns the max absolute and max relative error.
func blockErr(got, want []float32) (maxAbs, maxRel float64) {
	for i := range want {
		d := math.Abs(float64(got[i]) - float64(want[i]))
		if d > maxAbs {
			maxAbs = d
		}
		if aw := math.Abs(float64(want[i])); aw > 1e-6 {
			if r := d / aw; r > maxRel {
				maxRel = r
			}
		}
	}
	return maxAbs, maxRel
}

// whiteNoiseSpec recomputes the white-noise input's per-frame spectrum
// via the (independently gated) Go STFT: specRe/specImag each [257].
func whiteNoiseSpec(t *testing.T, n int) (re, im [][]float32) {
	t.Helper()
	raw, err := os.ReadFile("testdata/stft_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g stftGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	sig := g.Signals["white-noise"]
	x := make([]float32, len(sig.Input.Data))
	for i, v := range sig.Input.Data {
		x[i] = float32(v)
	}
	fr, fi, frames := STFT(x)
	for f := 0; f < n && f < frames; f++ {
		r := make([]float32, Bins)
		ii := make([]float32, Bins)
		for k := 0; k < Bins; k++ {
			r[k] = fr[k*frames+f]
			ii[k] = fi[k*frames+f]
		}
		re = append(re, r)
		im = append(im, ii)
	}
	return re, im
}

// TestForwardAllBlocks gates every ported forward sub-block against the
// per-block golden over the full 8-frame streaming run (so the caches
// are exercised). Divergence localises to a single named block. The
// mixed criterion max(|Δ|≤1e-4, rel≤1e-3) matches the parity contract;
// the decoder path (ONNX real ConvTranspose vs the golden's torch
// Version-2 Conv-flip) carries the ~1e-6 method gap, well inside it.
func TestForwardAllBlocks(t *testing.T) {
	g := loadBlocksGolden(t)
	m, err := NewModel()
	if err != nil {
		t.Fatal(err)
	}
	re, im := whiteNoiseSpec(t, g.Metadata.NFrames)

	const tolAbs, tolRel = 1e-4, 1e-3
	worstAbs := map[string]float64{}
	worstRel := map[string]float64{}

	for fi, fr := range g.Frames {
		captured := map[string][]float32{}
		m.forward(re[fi], im[fi], func(name string, v []float32) {
			captured[name] = append([]float32(nil), v...)
		})
		for _, name := range g.Metadata.BlockOrder {
			gb, ok := fr.Blocks[name]
			if !ok {
				continue
			}
			got, ok := captured[name]
			if !ok {
				t.Fatalf("frame %d: port did not capture block %q", fr.Frame, name)
			}
			want := gb.flat()
			if len(got) != len(want) {
				t.Fatalf("frame %d block %q: len %d vs golden %d", fr.Frame, name, len(got), len(want))
			}
			a, r := blockErr(got, want)
			if a > worstAbs[name] {
				worstAbs[name] = a
			}
			if r > worstRel[name] {
				worstRel[name] = r
			}
		}
	}

	for _, name := range g.Metadata.BlockOrder {
		a, r := worstAbs[name], worstRel[name]
		status := "ok"
		if a > tolAbs && r > tolRel {
			status = "FAIL"
			t.Errorf("block %-9s max|Δ|=%.3e maxRel=%.3e — exceeds max(%.0e,%.0e)", name, a, r, tolAbs, tolRel)
		}
		t.Logf("%-9s max|Δ|=%.3e maxRel=%.3e  %s", name, a, r, status)
	}
}
