//go:build cgo && aec_oracle

package delaypipe

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

// goDelayPipe replicates BlockProcessorImpl's BufferRender/
// ProcessCapture orchestration (block_processor.cc) driving this
// port's RenderDelayBuffer + RenderDelayController together, minus the
// echo-remover/metrics/logging side channels this slice has no
// counterpart for -- the same scope the C++ shim (shim.cc) implements.
type goDelayPipe struct {
	renderBuffer           *aec3.RenderDelayBuffer
	delayController        *aec3.RenderDelayController
	renderProperlyStarted  bool
	captureProperlyStarted bool
	renderEvent            aec3.RenderDelayBufferEvent
}

func newGoDelayPipe(config config.Config, sampleRateHz, numRenderChannels, numCaptureChannels int) *goDelayPipe {
	return &goDelayPipe{
		renderBuffer:    aec3.NewRenderDelayBuffer(config, sampleRateHz, numRenderChannels),
		delayController: aec3.NewRenderDelayController(config, sampleRateHz, numCaptureChannels),
		renderEvent:     aec3.RenderDelayBufferEventNone,
	}
}

func (p *goDelayPipe) bufferRender(block *aec3.Block) {
	p.renderEvent = p.renderBuffer.Insert(block)
	p.renderProperlyStarted = true
	p.delayController.LogRenderCall()
}

func (p *goDelayPipe) processCapture(captureBlock *aec3.Block) captureResult {
	if p.renderProperlyStarted {
		if !p.captureProperlyStarted {
			p.captureProperlyStarted = true
			p.renderBuffer.Reset()
			p.delayController.Reset(true)
		}
	} else {
		p.renderBuffer.HandleSkippedCaptureProcessing()
		return captureResult{
			bufferDelay:   p.renderBuffer.Delay(),
			hasClockdrift: p.delayController.HasClockdrift(),
		}
	}

	if p.renderEvent == aec3.RenderDelayBufferEventRenderOverrun && p.renderProperlyStarted {
		p.delayController.Reset(true)
	}
	p.renderEvent = aec3.RenderDelayBufferEventNone

	bufferEvent := p.renderBuffer.PrepareCaptureProcessing()
	if bufferEvent == aec3.RenderDelayBufferEventRenderUnderrun {
		p.delayController.Reset(false)
	}

	estimate := p.delayController.GetDelay(p.renderBuffer.GetDownsampledRenderBuffer(), p.renderBuffer.Delay(), captureBlock)

	delayChanged := false
	if estimate != nil {
		delayChanged = p.renderBuffer.AlignFromDelay(estimate.Delay)
	}

	result := captureResult{
		bufferDelay:   p.renderBuffer.Delay(),
		hasClockdrift: p.delayController.HasClockdrift(),
		delayChanged:  delayChanged,
	}
	if estimate != nil {
		result.hasEstimate = true
		result.quality = int(estimate.Quality)
		result.delayBlocks = estimate.Delay
		result.blocksSinceChange = estimate.BlocksSinceLastChange
		result.blocksSinceUpdate = estimate.BlocksSinceLastUpdate
	}
	return result
}

// scenario describes one delaypipe run: a known bulk delay
// (trueDelayBlocks, in aec3.BlockSize units) is inserted between a
// synthetic render stream and a capture stream built by delaying it,
// then fed through both pipelines using a per-cycle render-insert
// count pattern (1 for strict alternation; varying values to simulate
// render running ahead/behind, exercising the
// overrun/underrun/HandleSkippedCaptureProcessing paths).
type scenario struct {
	name            string
	trueDelayBlocks int
	pattern         []int
	cycles          int
}

func TestDelayPipeParity(t *testing.T) {
	scenarios := []scenario{
		{"delay_1block_4ms", 1, []int{1}, 400},
		{"delay_2blocks_8ms", 2, []int{1}, 400},
		{"delay_8blocks_32ms", 8, []int{1}, 500},
		{"delay_31blocks_124ms", 31, []int{1}, 600},
		{"delay_125blocks_500ms", 125, []int{1}, 900},
		{"drift_8blocks", 8, []int{2, 2, 0, 1, 1, 0, 1}, 700},
		{"drift_31blocks", 31, []int{1, 2, 0, 1, 2, 0, 1, 1, 1}, 900},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			runDelayPipeScenario(t, sc)
		})
	}
}

func runDelayPipeScenario(t *testing.T, sc scenario) {
	const sampleRateHz = 16000
	const blockSize = aec3.BlockSize

	config := config.DefaultConfig()
	goPipe := newGoDelayPipe(config, sampleRateHz, 1, 1)
	cPipe := newPipeC(sampleRateHz, 1, 1)
	defer cPipe.close()

	if got, want := cPipe.maxDelay(), goPipe.renderBuffer.MaxDelay(); got != want {
		t.Fatalf("MaxDelay mismatch: go %d, c %d", want, got)
	}

	delaySamples := sc.trueDelayBlocks * blockSize

	renderRng := lcg(0xC0FFEE ^ uint32(sc.trueDelayBlocks))
	noiseRng := lcg(0xBEEF ^ uint32(sc.trueDelayBlocks)<<8)

	var renderHistory []float32
	captureCursor := 0

	lastBufferDelay := 0

	for cycle := 0; cycle < sc.cycles; cycle++ {
		numRenders := sc.pattern[cycle%len(sc.pattern)]
		for r := 0; r < numRenders; r++ {
			block := make([]float32, blockSize)
			for i := range block {
				block[i] = renderRng.next() * 4000
			}
			renderHistory = append(renderHistory, block...)

			goBlock := aec3.NewBlock(1, 1)
			copy(goBlock.View(0, 0), block)
			goPipe.bufferRender(goBlock)
			cPipe.bufferRender(block)
		}

		capture := make([]float32, blockSize)
		for i := range capture {
			idx := captureCursor + i
			noise := noiseRng.next() * 40
			srcIdx := idx - delaySamples
			if srcIdx >= 0 && srcIdx < len(renderHistory) {
				capture[i] = renderHistory[srcIdx] + noise
			} else {
				capture[i] = noise
			}
		}
		captureCursor += blockSize

		goCaptureBlock := aec3.NewBlock(1, 1)
		copy(goCaptureBlock.View(0, 0), capture)

		goResult := goPipe.processCapture(goCaptureBlock)
		cResult := cPipe.processCapture(capture)

		if goResult.hasEstimate != cResult.hasEstimate {
			t.Fatalf("cycle %d: hasEstimate mismatch: go %v, c %v", cycle, goResult.hasEstimate, cResult.hasEstimate)
		}
		if goResult.hasEstimate {
			if goResult.quality != cResult.quality || goResult.delayBlocks != cResult.delayBlocks ||
				goResult.blocksSinceChange != cResult.blocksSinceChange ||
				goResult.blocksSinceUpdate != cResult.blocksSinceUpdate {
				t.Fatalf("cycle %d: estimate mismatch: go %+v, c %+v", cycle, goResult, cResult)
			}
		}
		if goResult.bufferDelay != cResult.bufferDelay {
			t.Fatalf("cycle %d: bufferDelay mismatch: go %d, c %d", cycle, goResult.bufferDelay, cResult.bufferDelay)
		}
		if goResult.hasClockdrift != cResult.hasClockdrift {
			t.Fatalf("cycle %d: hasClockdrift mismatch: go %v, c %v", cycle, goResult.hasClockdrift, cResult.hasClockdrift)
		}
		if goResult.delayChanged != cResult.delayChanged {
			t.Fatalf("cycle %d: delayChanged mismatch: go %v, c %v", cycle, goResult.delayChanged, cResult.delayChanged)
		}

		lastBufferDelay = goResult.bufferDelay
	}

	// Ground-truth sanity: the buffer's applied delay (in blocks) must
	// have converged to the inserted ground truth, independent of
	// bit-exactness -- this is a correctness check on the algorithm's
	// output, not just a Go/C++ agreement check.
	if diff := lastBufferDelay - sc.trueDelayBlocks; diff < -1 || diff > 1 {
		t.Errorf("%s: ground truth mismatch: inserted %d blocks (%dms), converged buffer delay %d blocks",
			sc.name, sc.trueDelayBlocks, sc.trueDelayBlocks*4, lastBufferDelay)
	}
}

// TestDelayPipeSkippedCapture exercises the narrow
// HandleSkippedCaptureProcessing path: a capture call before any
// render has ever been inserted.
func TestDelayPipeSkippedCapture(t *testing.T) {
	const sampleRateHz = 16000
	const blockSize = aec3.BlockSize

	config := config.DefaultConfig()
	goPipe := newGoDelayPipe(config, sampleRateHz, 1, 1)
	cPipe := newPipeC(sampleRateHz, 1, 1)
	defer cPipe.close()

	capture := make([]float32, blockSize)
	goCaptureBlock := aec3.NewBlock(1, 1)

	goResult := goPipe.processCapture(goCaptureBlock)
	cResult := cPipe.processCapture(capture)

	if goResult.hasEstimate != cResult.hasEstimate {
		t.Fatalf("hasEstimate mismatch: go %v, c %v", goResult.hasEstimate, cResult.hasEstimate)
	}
	if goResult.bufferDelay != cResult.bufferDelay {
		t.Fatalf("bufferDelay mismatch: go %d, c %d", goResult.bufferDelay, cResult.bufferDelay)
	}
	if goResult.hasClockdrift != cResult.hasClockdrift {
		t.Fatalf("hasClockdrift mismatch: go %v, c %v", goResult.hasClockdrift, cResult.hasClockdrift)
	}
}
