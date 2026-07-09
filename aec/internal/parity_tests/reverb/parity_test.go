//go:build cgo && aec_oracle

package reverb

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// This slice has two independent parts -- see shim.h's doc comment for
// the full design rationale.
//
// Part A drives a bare aec3.ReverbModel directly with scripted
// power-spectrum/scaling/decay inputs, covering both
// UpdateReverbNoFreqShaping (the single-scalar variant AecState
// actually uses, via computeAvgRenderReverb) and UpdateReverb (the
// per-frequency-bin variant, which has no call site anywhere in this
// port's current scope -- its only real caller upstream,
// ResidualEchoEstimator::UpdateReverb, belongs to EchoRemoverImpl's
// suppression-gain pipeline, out of this task's Phase 4 scope). All
// inputs are generated once as plain []float32/[65]float32 values and
// passed identically (the same underlying values) to both the Go and
// C++ sides, so there is no possibility of independent-recomputation
// FP divergence in this harness's own input generation; only
// ReverbModel's own arithmetic (already wrapped via mul32/add32 in the
// port) is under bit-exact comparison.
//
// Part B drives a standalone aec3.ReverbModelEstimator via a real
// Subtractor + FilterAnalyzer + RenderDelayBuffer pipeline (AecState
// itself is never Update()'d -- only constructed, to satisfy
// Subtractor.Process's signature), with EpStrength.DefaultLen forced
// negative so that ReverbDecayEstimator's adaptive decay-estimation
// path (AnalyzeFilter/EstimateDecay and their helpers) actually runs --
// DefaultConfig's real default (DefaultLen == 0.83, positive) selects
// the constant-decay branch instead, which is all any other slice
// (aecstate's DefaultConfig scenarios) ever exercises.

// lcg is the same minimal PRNG the other slices use.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

func absFloat32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func requireArray65Equal(t *testing.T, iter int, name string, got, want [65]float32) {
	t.Helper()
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("iter %d: %s[%d] mismatch: go %v, c %v", iter, name, i, got[i], want[i])
		}
	}
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

// --- Part A ---

func TestReverbModelParity(t *testing.T) {
	const n = 65

	goModel := new(aec3.ReverbModel)
	cModel := newReverbModelC()
	defer cModel.close()

	// Spans the no-op branch (decay <= 0) and a range of real decays,
	// including values at/near the clamped [0.02, 0.95] range
	// ReverbDecayEstimator::EstimateDecay produces.
	decays := []float32{-0.5, 0, 0.0005, 0.02, 0.3, 0.83, 0.95, 0.999, -1, 1.2}

	rng := lcg(0xf00d)
	for iter := 0; iter < 400; iter++ {
		powerSpectrum := make([]float32, n)
		for k := range powerSpectrum {
			v := rng.next()
			powerSpectrum[k] = v * v * 2e6
		}
		scaling := absFloat32(rng.next()) * 4000
		decay := decays[iter%len(decays)]

		goModel.UpdateReverbNoFreqShaping(powerSpectrum, scaling, decay)
		gotNoFreq := goModel.Reverb()
		wantNoFreq := cModel.updateNoFreqShaping(powerSpectrum, scaling, decay)
		requireArray65Equal(t, iter, "UpdateReverbNoFreqShaping", gotNoFreq, wantNoFreq)

		powerSpectrum2 := make([]float32, n)
		var perBinScaling [65]float32
		for k := range powerSpectrum2 {
			v := rng.next()
			powerSpectrum2[k] = v * v * 1.5e6
			perBinScaling[k] = absFloat32(rng.next()) * 3000
		}
		decay2 := decays[(iter+3)%len(decays)]

		goModel.UpdateReverb(powerSpectrum2, perBinScaling, decay2)
		gotFreq := goModel.Reverb()
		wantFreq := cModel.updateFreq(powerSpectrum2, perBinScaling, decay2)
		requireArray65Equal(t, iter, "UpdateReverb", gotFreq, wantFreq)
	}
}

// --- Part B ---

type scenario struct {
	name        string
	delayBlocks int
	attenuation float32
	numBlocks   int
}

func TestReverbEstimatorParity(t *testing.T) {
	scenarios := []scenario{
		{name: "delay_6blocks", delayBlocks: 6, attenuation: 0.6, numBlocks: 1800},
		{name: "delay_9blocks", delayBlocks: 9, attenuation: 0.4, numBlocks: 2100},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			runReverbEstimatorScenario(t, sc)
		})
	}
}

func runReverbEstimatorScenario(t *testing.T, sc scenario) {
	const sampleRateHz = 16000
	const numRenderChannels = 1
	const numCaptureChannels = 1
	const blockSize = aec3.BlockSize

	// Forces ReverbDecayEstimator's adaptive decay-estimation path --
	// see this file's top-of-file doc comment and shim.h's.
	const defaultLen = -0.83
	const nearendLen = -0.6

	config := config.DefaultConfig()
	config.EpStrength.DefaultLen = defaultLen
	config.EpStrength.NearendLen = nearendLen

	goRb := aec3.NewRenderDelayBuffer(config, sampleRateHz, numRenderChannels)
	goAnalyzer := aec3.NewRenderSignalAnalyzer(config)
	goAecState := aec3.NewAecState(config, numCaptureChannels) // never Update()'d -- see shim.h.
	goSub := aec3.NewSubtractor(config, numRenderChannels, numCaptureChannels)
	goFilterAnalyzer := aec3.NewFilterAnalyzer(config, numCaptureChannels)
	goEstimator := aec3.NewReverbModelEstimator(config, numCaptureChannels)
	goOutputs := make([]aec3.SubtractorOutput, numCaptureChannels)

	cE := newReverbEstC(numRenderChannels, defaultLen, nearendLen)
	defer cE.close()

	renderRng := lcg(0x51a7 ^ uint32(sc.delayBlocks)<<4)
	noiseRng := lcg(0x2c3f ^ uint32(sc.delayBlocks)<<8)
	qualityRng := lcg(0x77aa ^ uint32(sc.delayBlocks)<<12)

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

		// Scripted linear-filter-quality: nil every 37th block (exercising
		// the filterQuality == nil skip branch in both
		// ReverbFrequencyResponse.Update and ReverbDecayEstimator.Update),
		// otherwise a value in (0, 1].
		var quality *float32
		if iter%37 != 36 {
			q := 0.05 + 0.95*absFloat32(qualityRng.next())
			quality = &q
		}

		// Scripted stationary_block: true for a short window, exercising
		// the stationary-signal early return -- otherwise never true in
		// any other slice, since EchoAudibility.UseStationarityProperties
		// defaults false.
		stationaryBlock := iter >= 700 && iter < 720

		// --- Go side: mirrors shim.cc's aec3_reverbest_process exactly. ---
		delay := goAecState.MinDirectPathFilterDelay()
		goAnalyzer.Update(goRb.GetRenderBuffer(), &delay)

		captureBlock := aec3.NewBlock(1, numCaptureChannels)
		copy(captureBlock.View(0, 0), capture)

		goSub.Process(goRb.GetRenderBuffer(), captureBlock, goAnalyzer, goAecState, goOutputs)

		var anyFilterConsistent bool
		var maxEchoPathGain float32
		goFilterAnalyzer.Update(goSub.FilterImpulseResponses(), goRb.GetRenderBuffer(), &anyFilterConsistent, &maxEchoPathGain)

		linearFilterQualities := []*float32{quality}
		usableLinearEstimates := []bool{true}

		// NB: matches AecState::Update's real call site (aec_state.cc):
		// the impulse responses fed here are the highpass-preprocessed
		// filter (FilterAnalyzer.GetAdjustedFilters), not the raw
		// Subtractor.FilterImpulseResponses.
		goEstimator.Update(
			goFilterAnalyzer.GetAdjustedFilters(),
			goSub.FilterFrequencyResponses(),
			linearFilterQualities,
			goFilterAnalyzer.FilterDelaysBlocks(),
			usableLinearEstimates,
			stationaryBlock,
		)

		// --- C++ side: single call replicating the same order. ---
		c := cE.process(capture, quality, stationaryBlock)

		goFreqResponse := goEstimator.GetReverbFrequencyResponse()
		requireFloatSliceEqual(t, iter, "GetReverbFrequencyResponse", goFreqResponse[:], c.freqResponse[:])
		requireScalarEqual(t, iter, "ReverbDecay(false)", goEstimator.ReverbDecay(false), c.decayDefault)
		requireScalarEqual(t, iter, "ReverbDecay(true)", goEstimator.ReverbDecay(true), c.decayMild)
	}
}
