//go:build cgo

// Command gen regenerates vad/testdata/silero_golden.json: it runs
// the vendored silero_vad_16k_op15.onnx through onnxruntime over the
// shared goldensignals set and records the per-window probabilities
// the default-CI golden test (vad/silero_golden_test.go) pins the
// pure-Go port against.
//
// It needs the same opt-in environment as the parity slice:
//
//	ONNXRUNTIME_SHARED_LIB=/path/to/libonnxruntime.{dylib,so} \
//	  go run ./internal/parity_tests/silero_ort/gen   # from vad/
//
// or, via mise: MISE_EXPERIMENTAL=1 mise run //vad:golden:gen.
// The silero-parity workflow runs this with -verify — a fresh oracle
// pass that fails if the committed goldens drift beyond the parity
// tolerance — so the committed values cannot diverge from the oracle.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/goldensignals"
	silero_ort "github.com/daniel-sullivan/go-mediatoolkit/vad/internal/parity_tests/silero_ort"
)

func main() {
	model := flag.String("model", "internal/parity_tests/silero_ort/testdata/silero_vad_16k_op15.onnx",
		"path to the vendored oracle onnx model")
	out := flag.String("out", "testdata/silero_golden.json",
		"golden JSON output path")
	verify := flag.Bool("verify", false,
		"verify the committed golden file against a fresh oracle run instead of writing: "+
			"per-window |Δp| must be ≤ 1e-4 and the metadata must match the pins. "+
			"A byte-diff would flake — fp32 oracle outputs differ at the ~1e-6 level "+
			"across CPU ISAs (NEON vs AVX vs AVX-512), so goldens generated on one "+
			"machine never byte-match another's regeneration.")
	flag.Parse()

	if err := run(*model, *out, *verify); err != nil {
		log.Fatal(err)
	}
}

func run(modelPath, outPath string, verify bool) error {
	if err := silero_ort.InitEnvironment(); err != nil {
		return err
	}

	raw, err := os.ReadFile(modelPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if digest != silero_ort.ModelSHA256 {
		return fmt.Errorf("model %s hashes to %s, want the VERSION pin %s",
			modelPath, digest, silero_ort.ModelSHA256)
	}

	oracle, err := silero_ort.NewOracle(modelPath)
	if err != nil {
		return err
	}
	defer oracle.Destroy()

	golden := goldensignals.GoldenFile{
		OnnxRuntimeVersion: ort.GetVersion(),
		ModelSHA256:        digest,
		SampleRate:         goldensignals.SampleRate,
		WindowSize:         goldensignals.WindowSize,
		Probabilities:      map[string][]float64{},
	}
	for _, sig := range goldensignals.Signals() {
		probs, err := oracle.Run(sig.Samples)
		if err != nil {
			return fmt.Errorf("running %s: %w", sig.Name, err)
		}
		golden.Probabilities[sig.Name] = probs
		log.Printf("%s: %d windows", sig.Name, len(probs))
	}

	if verify {
		return verifyAgainst(outPath, &golden)
	}

	blob, err := json.MarshalIndent(&golden, "", "\t")
	if err != nil {
		return err
	}
	blob = append(blob, '\n')
	if err := os.WriteFile(outPath, blob, 0o644); err != nil {
		return err
	}
	log.Printf("wrote %s (onnxruntime %s)", outPath, golden.OnnxRuntimeVersion)
	return nil
}

// verifyTolerance bounds committed-vs-fresh-oracle drift. Same bound
// (and justification) as the parity slice — see the silero_ort
// package doc; observed cross-ISA oracle self-drift is ~1e-6.
const verifyTolerance = 1e-4

// verifyAgainst checks the committed golden file at path against a
// freshly generated one.
func verifyAgainst(path string, fresh *goldensignals.GoldenFile) error {
	committed, err := goldensignals.LoadGolden(path)
	if err != nil {
		return err
	}
	if committed.OnnxRuntimeVersion != fresh.OnnxRuntimeVersion {
		return fmt.Errorf("%s records onnxruntime %s, this oracle is %s — regenerate",
			path, committed.OnnxRuntimeVersion, fresh.OnnxRuntimeVersion)
	}
	if committed.ModelSHA256 != fresh.ModelSHA256 ||
		committed.SampleRate != fresh.SampleRate ||
		committed.WindowSize != fresh.WindowSize {
		return fmt.Errorf("%s metadata does not match the pinned model/geometry — regenerate", path)
	}
	if len(committed.Probabilities) != len(fresh.Probabilities) {
		return fmt.Errorf("%s has %d signals, fresh run has %d — regenerate",
			path, len(committed.Probabilities), len(fresh.Probabilities))
	}
	worst := 0.0
	for name, want := range fresh.Probabilities {
		got, ok := committed.Probabilities[name]
		if !ok {
			return fmt.Errorf("%s is missing signal %q — regenerate", path, name)
		}
		if len(got) != len(want) {
			return fmt.Errorf("%s signal %q has %d windows, fresh run has %d — regenerate",
				path, name, len(got), len(want))
		}
		for w := range want {
			d := got[w] - want[w]
			if d < 0 {
				d = -d
			}
			if d > worst {
				worst = d
			}
			if d > verifyTolerance {
				return fmt.Errorf("%s signal %q window %d: committed %.9f vs fresh oracle %.9f (|Δ|=%.3e > %g) — regenerate",
					path, name, w, got[w], want[w], d, verifyTolerance)
			}
		}
	}
	log.Printf("%s verified against a fresh oracle run: max |Δp| = %.3e (tolerance %g)",
		path, worst, verifyTolerance)
	return nil
}
