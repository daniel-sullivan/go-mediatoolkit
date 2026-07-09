//go:build cgo && aec_oracle

package erle

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// This slice drives a standalone aec3.ErleEstimator with realistic
// inputs derived from a real Subtractor + RenderDelayBuffer pipeline
// (see shim.h/shim.cc's doc comment for why AecState.Update is never
// called and why the render spectrum, not a reverb-shaped spectrum, is
// used as the avg_render_spectrum_with_reverb input). Two configs are
// exercised: NumSections=1 (default; signal-dependent estimator unset,
// same as the aecstate slice's default-config coverage) and
// NumSections=2 (forces SignalDependentErleEstimator to be
// constructed and driven -- not exercised by any other slice).
//
// FormLinearFilterOutput is used here only in its always-refined,
// same-selection form (config.filter.enable_coarse_filter_output_usage
// is false by default and this slice does not vary it): this always
// takes SignalTransition's identity fast path (from == to), so its
// blend formula -- and the cross-statement-FMA hazard found and fixed
// there in the aecstate slice -- is never exercised here and needs no
// local mul32/add32/sub32 primitives (there is no float arithmetic in
// this slice's own harness code to guard).

// formLinearFilterOutput mirrors this slice's shim.cc FormLinearFilterOutput:
// always selects the refined output (same-to-same, so signalTransition
// takes its identity fast path).
func formLinearFilterOutput(o *aec3.SubtractorOutput) [aec3.BlockSize]float32 {
	return o.ERefinedTime
}

// windowedPaddedFft computes a windowed (square root Hanning) padded
// FFT and updates vOld, mirroring echo_remover.cc's (anonymous
// namespace) WindowedPaddedFft.
func windowedPaddedFft(fft *aec3.Aec3FFT, v []float32, vOld *[aec3.FFTLengthBy2]float32, X *aec3.FFTData) {
	fft.PaddedFft(v, vOld[:], aec3.WindowSqrtHanning, X)
	copy(vOld[:], v)
}

// lcg is the same minimal PRNG the other slices use.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

func requireFloatSliceEqual(t *testing.T, iter int, name string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("iter %d: %s length mismatch: go %d, c %d", iter, name, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("iter %d: %s[%d] mismatch: go %v, c %v", iter, name, i, got[i], want[i])
		}
	}
}

func requireScalarEqual(t *testing.T, iter int, name string, got, want float32) {
	t.Helper()
	if got != want {
		t.Fatalf("iter %d: %s mismatch: go %v, c %v", iter, name, got, want)
	}
}

// scenario drives a single render channel / single capture channel
// run with a correlated linear echo path, checking bit-exactness of
// ErleEstimator's readable outputs every block.
type scenario struct {
	name        string
	numSections int
	delayBlocks int
	attenuation float32
	numBlocks   int
}

func TestErleParity(t *testing.T) {
	scenarios := []scenario{
		{name: "single_section_delay_6blocks", numSections: 1, delayBlocks: 6, attenuation: 0.5, numBlocks: 1100},
		{name: "multi_section_delay_11blocks", numSections: 2, delayBlocks: 11, attenuation: 0.7, numBlocks: 1300},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			runErleScenario(t, sc)
		})
	}
}

func runErleScenario(t *testing.T, sc scenario) {
	const sampleRateHz = 16000
	const numRenderChannels = 1
	const numCaptureChannels = 1
	const blockSize = aec3.BlockSize

	config := config.DefaultConfig()
	config.Erle.NumSections = sc.numSections

	goRb := aec3.NewRenderDelayBuffer(config, sampleRateHz, numRenderChannels)
	goAnalyzer := aec3.NewRenderSignalAnalyzer(config)
	goAecState := aec3.NewAecState(config, numCaptureChannels) // never Update()'d -- see shim.h.
	goSub := aec3.NewSubtractor(config, numRenderChannels, numCaptureChannels)
	goOutputAnalyzer := aec3.NewSubtractorOutputAnalyzer(numCaptureChannels)
	goErle := aec3.NewErleEstimator(2*aec3.NumBlocksPerSecond, config, numCaptureChannels)
	goOutputs := make([]aec3.SubtractorOutput, numCaptureChannels)

	goFft := aec3.NewAec3FFT()
	var eOld, yOld [aec3.FFTLengthBy2]float32

	cE := newErleC(numRenderChannels, sc.numSections)
	defer cE.close()

	renderRng := lcg(0x35ac ^ uint32(sc.delayBlocks)<<4)
	noiseRng := lcg(0x9e21 ^ uint32(sc.delayBlocks)<<8)

	var renderHistory []float32
	delaySamples := sc.delayBlocks * blockSize

	for iter := 0; iter < sc.numBlocks; iter++ {
		render := make([]float32, blockSize)
		for i := range render {
			render[i] = renderRng.next() * 4000
		}
		renderHistory = append(renderHistory, render...)

		block := aec3.NewBlock(1, numRenderChannels)
		copy(block.View(0, 0), render)
		goRb.Insert(block)
		goRb.PrepareCaptureProcessing()
		cE.insertRenderBlock(render)

		captureCursor := iter * blockSize
		capture := make([]float32, blockSize)
		for i := range capture {
			idx := captureCursor + i
			noise := noiseRng.next() * 20
			srcIdx := idx - delaySamples
			if srcIdx >= 0 && srcIdx < len(renderHistory) {
				capture[i] = sc.attenuation*renderHistory[srcIdx] + noise
			} else {
				capture[i] = noise
			}
		}

		// --- Go side: mirrors shim.cc's aec3_erle_process exactly. ---
		delay := goAecState.MinDirectPathFilterDelay()
		goAnalyzer.Update(goRb.GetRenderBuffer(), &delay)

		captureBlock := aec3.NewBlock(1, numCaptureChannels)
		copy(captureBlock.View(0, 0), capture)

		goSub.Process(goRb.GetRenderBuffer(), captureBlock, goAnalyzer, goAecState, goOutputs)

		var anyFilterConverged, anyCoarseFilterConverged, allFiltersDiverged bool
		goOutputAnalyzer.Update(goOutputs, &anyFilterConverged, &anyCoarseFilterConverged, &allFiltersDiverged)

		e := formLinearFilterOutput(&goOutputs[0])

		var Y, E aec3.FFTData
		windowedPaddedFft(goFft, captureBlock.View(0, 0), &yOld, &Y)
		windowedPaddedFft(goFft, e[:], &eOld, &E)

		var Y2, E2 [aec3.FFTLengthBy2Plus1]float32
		Y.Spectrum(Y2[:])
		E.Spectrum(E2[:])

		x2Reverb := goRb.GetRenderBuffer().Spectrum(sc.delayBlocks)[0]
		var x2Array [aec3.FFTLengthBy2Plus1]float32
		copy(x2Array[:], x2Reverb)

		goErle.Update(goRb.GetRenderBuffer(), goSub.FilterFrequencyResponses(), x2Array,
			[][aec3.FFTLengthBy2Plus1]float32{Y2}, [][aec3.FFTLengthBy2Plus1]float32{E2},
			goOutputAnalyzer.ConvergedFilters())

		// --- C++ side: single call replicating the same order. ---
		c := cE.process(capture, sc.delayBlocks)

		goErleNoOnset := goErle.Erle(false)
		requireFloatSliceEqual(t, iter, "Erle(false)", goErleNoOnset[0][:], c.erleNoOnset[:])
		goErleOnset := goErle.Erle(true)
		requireFloatSliceEqual(t, iter, "Erle(true)", goErleOnset[0][:], c.erleOnset[:])
		goErleUnbounded := goErle.ErleUnbounded()
		requireFloatSliceEqual(t, iter, "ErleUnbounded", goErleUnbounded[0][:], c.erleUnbounded[:])
		goErleDuringOnsets := goErle.ErleDuringOnsets()
		requireFloatSliceEqual(t, iter, "ErleDuringOnsets", goErleDuringOnsets[0][:], c.erleDuringOnsets[:])
		requireScalarEqual(t, iter, "FullbandErleLog2", goErle.FullbandErleLog2(), c.fullbandErleLog2)

		goQuality := goErle.GetInstLinearQualityEstimates()
		if (goQuality[0] != nil) != c.qualityValid {
			t.Fatalf("iter %d: quality validity mismatch: go %v, c %v", iter, goQuality[0] != nil, c.qualityValid)
		}
		if goQuality[0] != nil {
			requireScalarEqual(t, iter, "InstLinearQualityEstimate", *goQuality[0], c.qualityValue)
		}
	}
}
