//go:build cgo && aec_oracle

package aecstate

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// This slice drives AecState::Update with the same orchestration
// EchoRemoverImpl::ProcessCapture uses (echo_remover.cc), without
// porting or wrapping EchoRemover itself (out of scope -- see
// shim.h's doc comment). The E2/Y2 spectrum-forming glue
// (FormLinearFilterOutput, WindowedPaddedFft, SignalTransition) is
// reimplemented here as test-only harness code on the Go side (mirrored
// verbatim in shim.cc on the C++ side), since it is not part of any
// aec3 package file and echo_remover.cc's own copies have internal
// linkage. LinearEchoPower is omitted on both sides: its result only
// feeds suppression-gain computation, which AecState::Update never
// consults.

// mul32/add32/sub32 are local re-implementations of aec3's unexported
// fp_strict.go primitives (this test package cannot import
// unexported symbols from aec3). Since this package is only ever
// compiled together with -tags=aec_strict via run.sh, these are
// unconditionally "strict" (no default/fast variant needed): the
// //go:noinline barrier forces the multiply and the add/sub to round
// separately, matching the oracle's -ffp-contract=off build and this
// port's established cross-statement-FMA discipline.
//
//go:noinline
func mul32(a, b float32) float32 { return a * b }

//go:noinline
func add32(a, b float32) float32 { return a + b }

//go:noinline
func sub32(a, b float32) float32 { return a - b }

// signalTransition fades between two input signals using a
// fix-sized transition, mirroring echo_remover.cc's (anonymous
// namespace) SignalTransition exactly, including its fast path when
// from and to are the same underlying selection (see this file's use
// below): that fast path is not just an optimization, it avoids a
// rounding difference the blend formula would otherwise introduce.
func signalTransition(same bool, from, to *[aec3.BlockSize]float32, out *[aec3.BlockSize]float32) {
	if same {
		*out = *to
		return
	}
	const transitionSize = 30
	const oneByTransitionSizePlusOne = 1.0 / (transitionSize + 1)
	for k := 0; k < transitionSize; k++ {
		// a feeds a later subtraction (oneMinusA) in a separate
		// statement, so it must be computed via the noinline mul32
		// primitive -- otherwise the Go compiler can fuse this
		// multiply into that subtraction as a cross-statement FMA,
		// diverging from the oracle's -ffp-contract=off build (which
		// rounds a to float32 before subtracting it from 1).
		a := mul32(float32(k+1), oneByTransitionSizePlusOne)
		oneMinusA := sub32(1.0, a)
		out[k] = add32(mul32(a, to[k]), mul32(oneMinusA, from[k]))
	}
	copy(out[transitionSize:], to[transitionSize:])
}

// formLinearFilterOutput selects/transitions between the coarse and
// refined filter outputs, mirroring echo_remover.cc's (anonymous
// namespace) EchoRemoverImpl::FormLinearFilterOutput. lastSelected is
// the harness's persistent refined_filter_output_last_selected_
// state (mutated in place).
func formLinearFilterOutput(useCoarseFilterOutput bool, lastSelected *bool, o *aec3.SubtractorOutput) [aec3.BlockSize]float32 {
	useRefinedOutput := true
	if useCoarseFilterOutput {
		if o.E2CoarseScalar < 0.9*o.E2RefinedScalar &&
			o.Y2 > 30.0*30.0*aec3.BlockSize &&
			(o.S2Refined > 60.0*60.0*aec3.BlockSize || o.S2Coarse > 60.0*60.0*aec3.BlockSize) {
			useRefinedOutput = false
		} else if o.E2CoarseScalar < o.E2RefinedScalar && o.Y2 < o.E2RefinedScalar {
			useRefinedOutput = false
		}
	}

	var from, to *[aec3.BlockSize]float32
	if *lastSelected {
		from = &o.ERefinedTime
	} else {
		from = &o.ECoarseTime
	}
	if useRefinedOutput {
		to = &o.ERefinedTime
	} else {
		to = &o.ECoarseTime
	}

	var out [aec3.BlockSize]float32
	signalTransition(*lastSelected == useRefinedOutput, from, to, &out)
	*lastSelected = useRefinedOutput
	return out
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

type goRenderHarness struct {
	rb *aec3.RenderDelayBuffer
}

func newGoRenderHarness(config config.Config, sampleRateHz, numRenderChannels int) *goRenderHarness {
	return &goRenderHarness{rb: aec3.NewRenderDelayBuffer(config, sampleRateHz, numRenderChannels)}
}

func (h *goRenderHarness) insertBlock(samples []float32, numRenderChannels int) {
	block := aec3.NewBlock(1, numRenderChannels)
	for ch := 0; ch < numRenderChannels; ch++ {
		copy(block.View(0, ch), samples[ch*aec3.BlockSize:(ch+1)*aec3.BlockSize])
	}
	h.rb.Insert(block)
	h.rb.PrepareCaptureProcessing()
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

func boolToFloat32(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// scenario drives a single render channel / single capture channel
// run with a correlated linear echo path, scripted saturation/echo-
// path-change/external-delay events, checking bit-exactness of both
// SubtractorOutput and every AecState-readable output every block.
type scenario struct {
	name        string
	delayBlocks int
	attenuation float32
	numBlocks   int
	exitInitAt  int // block index to call ExitInitialState-triggering events around (0 = never)

	gainChangeAt  int // block index at which to fire a gain-only echo path change (0 = never)
	delayChangeAt int // block index at which to fire a full-reset (delay change) echo path change (0 = never)

	externalDelayAt    int // block index from which an external delay estimate is reported (0 = never)
	externalDelayValue int
}

func TestAecStateParity(t *testing.T) {
	scenarios := []scenario{
		{
			name:               "delay_6blocks",
			delayBlocks:        6,
			attenuation:        0.5,
			numBlocks:          1100,
			exitInitAt:         300,
			gainChangeAt:       500,
			delayChangeAt:      800,
			externalDelayAt:    150,
			externalDelayValue: 5,
		},
		{
			name:               "delay_11blocks",
			delayBlocks:        11,
			attenuation:        0.7,
			numBlocks:          1300,
			exitInitAt:         300,
			gainChangeAt:       600,
			delayChangeAt:      950,
			externalDelayAt:    0,
			externalDelayValue: 0,
		},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			runAecStateScenario(t, sc)
		})
	}
}

func runAecStateScenario(t *testing.T, sc scenario) {
	const sampleRateHz = 16000
	const numRenderChannels = 1
	const numCaptureChannels = 1
	const blockSize = aec3.BlockSize

	config := config.DefaultConfig()

	goHarness := newGoRenderHarness(config, sampleRateHz, numRenderChannels)
	goAnalyzer := aec3.NewRenderSignalAnalyzer(config)
	goAecState := aec3.NewAecState(config, numCaptureChannels)
	goSub := aec3.NewSubtractor(config, numRenderChannels, numCaptureChannels)
	goOutputs := make([]aec3.SubtractorOutput, numCaptureChannels)

	goFft := aec3.NewAec3FFT()
	var eOld, yOld [aec3.FFTLengthBy2]float32
	lastSelected := true
	useCoarseFilterOutput := config.Filter.EnableCoarseFilterOutputUsage

	cAS := newAecStateC(numRenderChannels)
	defer cAS.close()

	renderRng := lcg(0x2468 ^ uint32(sc.delayBlocks))
	noiseRng := lcg(0x1357 ^ uint32(sc.delayBlocks)<<8)

	var renderHistory []float32
	delaySamples := sc.delayBlocks * blockSize

	for iter := 0; iter < sc.numBlocks; iter++ {
		render := make([]float32, blockSize)
		for i := range render {
			render[i] = renderRng.next() * 4000
		}
		renderHistory = append(renderHistory, render...)

		goHarness.insertBlock(render, numRenderChannels)
		cAS.insertRenderBlock(render)

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

		captureSaturation := sc.exitInitAt != 0 && iter >= sc.exitInitAt/2 && iter < sc.exitInitAt/2+50

		gainChange := sc.gainChangeAt != 0 && iter == sc.gainChangeAt
		delayChange := aec3.DelayAdjustmentNone
		delayChangeInt := 0
		if sc.delayChangeAt != 0 && iter == sc.delayChangeAt {
			delayChange = aec3.DelayAdjustmentNewDetectedDelay
			delayChangeInt = 2
		}
		clockDrift := false

		var externalDelay *aec3.DelayEstimate
		ext := externalDelayArgs{}
		if sc.externalDelayAt != 0 && iter >= sc.externalDelayAt {
			d := aec3.NewDelayEstimate(aec3.DelayEstimateQualityRefined, sc.externalDelayValue)
			externalDelay = &d
			ext = externalDelayArgs{has: true, quality: 1, value: sc.externalDelayValue}
		}

		// --- Go side: replicate EchoRemoverImpl::ProcessCapture's
		// orchestration order exactly (see echo_remover.cc). ---
		goAecState.UpdateCaptureSaturation(captureSaturation)

		variability := aec3.NewEchoPathVariability(gainChange, delayChange, clockDrift)
		if variability.AudioPathChanged() {
			goSub.HandleEchoPathChange(variability)
			goAecState.HandleEchoPathChange(variability)
		}

		delay := goAecState.MinDirectPathFilterDelay()
		goAnalyzer.Update(goHarness.rb.GetRenderBuffer(), &delay)

		if goAecState.TransitionTriggered() {
			goSub.ExitInitialState()
		}

		captureBlock := aec3.NewBlock(1, numCaptureChannels)
		copy(captureBlock.View(0, 0), capture)

		goSub.Process(goHarness.rb.GetRenderBuffer(), captureBlock, goAnalyzer, goAecState, goOutputs)

		e := formLinearFilterOutput(useCoarseFilterOutput, &lastSelected, &goOutputs[0])

		var Y, E aec3.FFTData
		windowedPaddedFft(goFft, captureBlock.View(0, 0), &yOld, &Y)
		windowedPaddedFft(goFft, e[:], &eOld, &E)

		var Y2, E2 [aec3.FFTLengthBy2Plus1]float32
		Y.Spectrum(Y2[:])
		E.Spectrum(E2[:])

		goAecState.Update(externalDelay, goSub.FilterFrequencyResponses(), goSub.FilterImpulseResponses(),
			goHarness.rb.GetRenderBuffer(), [][aec3.FFTLengthBy2Plus1]float32{E2}, [][aec3.FFTLengthBy2Plus1]float32{Y2}, goOutputs)

		// --- C++ side: single call replicating the same order. ---
		result := cAS.process(capture, captureSaturation, gainChange, delayChangeInt, clockDrift, ext)

		g := &goOutputs[0]
		c := result.subtractor
		requireFloatSliceEqual(t, iter, "SRefined", g.SRefined[:], c.sRefined[:])
		requireFloatSliceEqual(t, iter, "SCoarse", g.SCoarse[:], c.sCoarse[:])
		requireFloatSliceEqual(t, iter, "ERefinedTime", g.ERefinedTime[:], c.eRefined[:])
		requireFloatSliceEqual(t, iter, "ECoarseTime", g.ECoarseTime[:], c.eCoarse[:])
		requireFloatSliceEqual(t, iter, "ERefined.Re", g.ERefined.Re[:], c.eRefinedRe[:])
		requireFloatSliceEqual(t, iter, "ERefined.Im", g.ERefined.Im[:], c.eRefinedIm[:])
		requireFloatSliceEqual(t, iter, "E2Refined", g.E2Refined[:], c.e2Refined[:])
		requireFloatSliceEqual(t, iter, "E2Coarse", g.E2Coarse[:], c.e2Coarse[:])
		requireScalarEqual(t, iter, "S2Refined", g.S2Refined, c.s2Refined)
		requireScalarEqual(t, iter, "S2Coarse", g.S2Coarse, c.s2Coarse)
		requireScalarEqual(t, iter, "E2RefinedScalar", g.E2RefinedScalar, c.e2RefinedScalar)
		requireScalarEqual(t, iter, "E2CoarseScalar", g.E2CoarseScalar, c.e2CoarseScalar)
		requireScalarEqual(t, iter, "Y2", g.Y2, c.y2)
		requireScalarEqual(t, iter, "SRefinedMaxAbs", g.SRefinedMaxAbs, c.sRefinedMaxAbs)
		requireScalarEqual(t, iter, "SCoarseMaxAbs", g.SCoarseMaxAbs, c.sCoarseMaxAbs)

		// AecState-readable state, checked every block.
		as := result.aecState
		goErleNoOnset := goAecState.Erle(false)
		requireFloatSliceEqual(t, iter, "Erle(false)", goErleNoOnset[0][:], as.erleNoOnset[:])
		goErleOnset := goAecState.Erle(true)
		requireFloatSliceEqual(t, iter, "Erle(true)", goErleOnset[0][:], as.erleOnset[:])
		goErleUnbounded := goAecState.ErleUnbounded()
		requireFloatSliceEqual(t, iter, "ErleUnbounded", goErleUnbounded[0][:], as.erleUnbounded[:])
		goErl := goAecState.Erl()
		requireFloatSliceEqual(t, iter, "Erl", goErl[:], as.erl[:])
		goReverbFreq := goAecState.GetReverbFrequencyResponse()
		requireFloatSliceEqual(t, iter, "ReverbFrequencyResponse", goReverbFreq[:], as.reverbFreqResponse[:])

		requireScalarEqual(t, iter, "FullBandErleLog2", goAecState.FullBandErleLog2(), as.fullbandErleLog2)
		requireScalarEqual(t, iter, "ErlTimeDomain", goAecState.ErlTimeDomain(), as.erlTimeDomain)
		requireScalarEqual(t, iter, "MinDirectPathFilterDelay", float32(goAecState.MinDirectPathFilterDelay()), as.minDirectPathFilterDelay)
		requireScalarEqual(t, iter, "SaturatedCapture", boolToFloat32(goAecState.SaturatedCapture()), as.saturatedCapture)
		requireScalarEqual(t, iter, "SaturatedEcho", boolToFloat32(goAecState.SaturatedEcho()), as.saturatedEcho)
		requireScalarEqual(t, iter, "TransparentModeActive", boolToFloat32(goAecState.TransparentModeActive()), as.transparentModeActive)
		requireScalarEqual(t, iter, "ReverbDecay(false)", goAecState.ReverbDecay(false), as.reverbDecayDefault)
		requireScalarEqual(t, iter, "ReverbDecay(true)", goAecState.ReverbDecay(true), as.reverbDecayMild)
		requireScalarEqual(t, iter, "TransitionTriggered", boolToFloat32(goAecState.TransitionTriggered()), as.transitionTriggered)
		requireScalarEqual(t, iter, "FilterLengthBlocks", float32(goAecState.FilterLengthBlocks()), as.filterLengthBlocks)
		requireScalarEqual(t, iter, "UsableLinearEstimate", boolToFloat32(goAecState.UsableLinearEstimate()), as.usableLinearEstimate)
		requireScalarEqual(t, iter, "UseLinearFilterOutput", boolToFloat32(goAecState.UseLinearFilterOutput()), as.useLinearFilterOutput)
		requireScalarEqual(t, iter, "ActiveRender", boolToFloat32(goAecState.ActiveRender()), as.activeRender)
	}
}
