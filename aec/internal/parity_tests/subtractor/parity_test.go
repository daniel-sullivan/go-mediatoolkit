//go:build cgo && aec_oracle

package subtractor

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// lcg is the same PRNG the other slices use; both sides consume
// identical Go-generated inputs, so quality is irrelevant.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

// goRenderHarness drives a Go RenderDelayBuffer the same minimal way
// the C++ shim drives its webrtc::RenderDelayBuffer (see shim.cc):
// Insert then PrepareCaptureProcessing every block, with no delay
// controller/estimation involved -- Subtractor does not depend on
// delay estimation quality.
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
		t.Fatalf("iter %d: %s length mismatch: go %d, c %d", iter, name, len(want), len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("iter %d: %s[%d] mismatch: go %v, c %v", iter, name, i, want[i], got[i])
		}
	}
}

// scenario drives a single render channel / single capture channel run
// with a genuine correlated linear echo path (a single-tap delay +
// attenuation + noise), so the adaptive filters actually converge.
// Bit-exactness between the Go port and the C++ oracle is checked
// every block; a non-bit-exact echo-reduction sanity assertion (e2 <<
// y2 after convergence) is checked at the end.
type scenario struct {
	name        string
	delayBlocks int
	attenuation float32
	numBlocks   int
	exitInitAt  int // block index at which to call ExitInitialState (0 = never)
	// expectConvergence gates the end-of-run echo-reduction sanity
	// assertion. The render harness applies the config's 5-block
	// default delay (no delay controller runs), so an echo path
	// shorter than that is anti-causal relative to the aligned render
	// and the linear filters cannot converge on it -- on either side.
	// Such scenarios still provide full bit-exactness coverage (and
	// heavily exercise the poor-coarse-filter reset path), they just
	// cannot assert convergence.
	expectConvergence bool
}

func TestSubtractorParity(t *testing.T) {
	scenarios := []scenario{
		{"delay_2blocks", 2, 0.6, 900, 300, false},
		{"delay_6blocks", 6, 0.5, 1100, 300, true},
		{"delay_11blocks", 11, 0.7, 1300, 300, true},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			runSubtractorScenario(t, sc)
		})
	}
}

func runSubtractorScenario(t *testing.T, sc scenario) {
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

	cSub := newSubtractorC(numRenderChannels, numCaptureChannels)
	defer cSub.close()

	renderRng := lcg(0x1234 ^ uint32(sc.delayBlocks))
	noiseRng := lcg(0x9999 ^ uint32(sc.delayBlocks)<<8)

	var renderHistory []float32
	delaySamples := sc.delayBlocks * blockSize

	var sumY2, sumMinE2 float64
	const tailWindow = 100

	for iter := 0; iter < sc.numBlocks; iter++ {
		render := make([]float32, blockSize)
		for i := range render {
			render[i] = renderRng.next() * 4000
		}
		renderHistory = append(renderHistory, render...)

		goHarness.insertBlock(render, numRenderChannels)
		cSub.insertRenderBlock(render)

		goAnalyzer.Update(goHarness.rb.GetRenderBuffer(), nil)
		cSub.updateRenderSignalAnalyzer()

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

		if sc.exitInitAt != 0 && iter == sc.exitInitAt {
			goSub.ExitInitialState()
			cSub.exitInitialState()
		}
		if iter == sc.exitInitAt/2 && sc.exitInitAt != 0 {
			goAecState.UpdateCaptureSaturation(true)
			cSub.setSaturatedCapture(true)
		}
		if iter == sc.exitInitAt/2+50 && sc.exitInitAt != 0 {
			goAecState.UpdateCaptureSaturation(false)
			cSub.setSaturatedCapture(false)
		}
		if iter == sc.exitInitAt+200 {
			goSub.HandleEchoPathChange(aec3.NewEchoPathVariability(true, aec3.DelayAdjustmentNone, false))
			cSub.handleEchoPathChange(true, 0, false)
		}

		captureBlock := aec3.NewBlock(1, numCaptureChannels)
		copy(captureBlock.View(0, 0), capture)

		goSub.Process(goHarness.rb.GetRenderBuffer(), captureBlock, goAnalyzer, goAecState, goOutputs)
		cOutputs := cSub.process(capture)

		for ch := 0; ch < numCaptureChannels; ch++ {
			g := &goOutputs[ch]
			c := cOutputs[ch]
			requireFloatSliceEqual(t, iter, "SRefined", g.SRefined[:], c.sRefined[:])
			requireFloatSliceEqual(t, iter, "SCoarse", g.SCoarse[:], c.sCoarse[:])
			requireFloatSliceEqual(t, iter, "ERefinedTime", g.ERefinedTime[:], c.eRefined[:])
			requireFloatSliceEqual(t, iter, "ECoarseTime", g.ECoarseTime[:], c.eCoarse[:])
			requireFloatSliceEqual(t, iter, "ERefined.Re", g.ERefined.Re[:], c.eRefinedRe[:])
			requireFloatSliceEqual(t, iter, "ERefined.Im", g.ERefined.Im[:], c.eRefinedIm[:])
			requireFloatSliceEqual(t, iter, "E2Refined", g.E2Refined[:], c.e2Refined[:])
			requireFloatSliceEqual(t, iter, "E2Coarse", g.E2Coarse[:], c.e2Coarse[:])
			if g.S2Refined != c.s2Refined || g.S2Coarse != c.s2Coarse ||
				g.E2RefinedScalar != c.e2RefinedScalar || g.E2CoarseScalar != c.e2CoarseScalar ||
				g.Y2 != c.y2 || g.SRefinedMaxAbs != c.sRefinedMaxAbs || g.SCoarseMaxAbs != c.sCoarseMaxAbs {
				t.Fatalf("iter %d ch %d: scalar output mismatch: go %+v, c %+v", iter, ch, g, c)
			}

			if iter >= sc.numBlocks-tailWindow {
				minE2 := g.E2RefinedScalar
				if g.E2CoarseScalar < minE2 {
					minE2 = g.E2CoarseScalar
				}
				sumY2 += float64(g.Y2)
				sumMinE2 += float64(minE2)
			}
		}

		// State comparison: impulse responses + frequency responses,
		// checked every 25 blocks to keep the loop cheap.
		if iter%25 == 0 {
			for ch := 0; ch < numCaptureChannels; ch++ {
				goIR := goSub.FilterImpulseResponses()[ch]
				cIR := cSub.impulseResponse(ch)
				requireFloatSliceEqual(t, iter, "ImpulseResponse", goIR, cIR)

				goFR := goSub.FilterFrequencyResponses()[ch]
				cFR := cSub.frequencyResponse(ch)
				if len(goFR) != len(cFR) {
					t.Fatalf("iter %d ch %d: FrequencyResponse partition count mismatch: go %d, c %d", iter, ch, len(goFR), len(cFR))
				}
				for p := range goFR {
					requireFloatSliceEqual(t, iter, "FrequencyResponse", goFR[p][:], cFR[p][:])
				}
			}
		}
	}

	avgY2 := sumY2 / tailWindow
	avgMinE2 := sumMinE2 / tailWindow
	var reductionDB float64
	if avgMinE2 > 0 && avgY2 > 0 {
		reductionDB = 10 * math.Log10(avgY2/avgMinE2)
	}
	t.Logf("%s: tail-window avg y2=%.1f, avg min(e2_refined,e2_coarse)=%.1f, echo reduction=%.1f dB",
		sc.name, avgY2, avgMinE2, reductionDB)

	// Echo-reduction sanity: this is a correctness check on the port's
	// convergence behavior, independent of bit-exactness -- the linear
	// filter must have substantially reduced the (purely linear,
	// single-tap) echo by the end of the run. Skipped for anti-causal
	// scenarios (see scenario.expectConvergence).
	if sc.expectConvergence && avgMinE2 >= 0.1*avgY2 {
		t.Errorf("%s: insufficient echo reduction: avg y2=%.1f, avg min(e2)=%.1f (want min(e2) < 0.1*y2)",
			sc.name, avgY2, avgMinE2)
	}
}
