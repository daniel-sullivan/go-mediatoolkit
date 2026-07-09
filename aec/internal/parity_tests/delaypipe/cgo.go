//go:build cgo && aec_oracle

// Package delaypipe is the integrated bit-exact parity slice for
// aec/internal/aec3's render/delay pipeline (RenderDelayBuffer +
// RenderDelayController driven together, replicating
// BlockProcessorImpl's BufferRender/ProcessCapture orchestration from
// block_processor.cc) against the fetched C++ oracle. Env/link setup
// is shared with the other slices via ../run.sh -- see the bandsplit
// slice's cgo.go for the full rationale.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package delaypipe

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"

// pipeC wraps the oracle's paired
// webrtc::RenderDelayBuffer/webrtc::RenderDelayController, driven
// through BlockProcessorImpl's orchestration sequence.
type pipeC struct {
	h *C.aec3_delaypipe
}

func newPipeC(sampleRateHz, numRenderChannels, numCaptureChannels int) *pipeC {
	return &pipeC{h: C.aec3_delaypipe_create(
		C.int(sampleRateHz), C.int(numRenderChannels), C.int(numCaptureChannels))}
}

func (p *pipeC) close() {
	C.aec3_delaypipe_destroy(p.h)
	p.h = nil
}

func (p *pipeC) maxDelay() int {
	return int(C.aec3_delaypipe_max_delay(p.h))
}

func (p *pipeC) bufferRender(samples []float32) {
	C.aec3_delaypipe_buffer_render(p.h, (*C.float)(&samples[0]))
}

// captureResult mirrors the fields filled by
// aec3_delaypipe_process_capture.
type captureResult struct {
	hasEstimate       bool
	quality           int
	delayBlocks       int
	blocksSinceChange int
	blocksSinceUpdate int
	bufferDelay       int
	hasClockdrift     bool
	delayChanged      bool
}

func (p *pipeC) processCapture(samples []float32) captureResult {
	var hasEstimate, quality, delayBlocks, blocksSinceChange, blocksSinceUpdate C.int
	var bufferDelay, hasClockdrift C.int
	changed := C.aec3_delaypipe_process_capture(p.h, (*C.float)(&samples[0]),
		&hasEstimate, &quality, &delayBlocks, &blocksSinceChange, &blocksSinceUpdate,
		&bufferDelay, &hasClockdrift)
	return captureResult{
		hasEstimate:       hasEstimate != 0,
		quality:           int(quality),
		delayBlocks:       int(delayBlocks),
		blocksSinceChange: int(blocksSinceChange),
		blocksSinceUpdate: int(blocksSinceUpdate),
		bufferDelay:       int(bufferDelay),
		hasClockdrift:     hasClockdrift != 0,
		delayChanged:      changed != 0,
	}
}
