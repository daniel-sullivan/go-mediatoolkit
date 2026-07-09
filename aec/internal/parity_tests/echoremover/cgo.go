//go:build cgo && aec_oracle

// Package echoremover is the integrated bit-exact parity slice for
// aec/internal/aec3's echo_remover.go against the fetched AEC3 C++
// oracle, driving the oracle's real webrtc::EchoRemover directly (see
// shim.h for why this needs no hand-reimplemented orchestration,
// unlike the aecstate slice).
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package echoremover

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"

// blockFloats mirrors AEC3_ECHOREMOVER_BLOCK_FLOATS.
const blockFloats = 64

type echoRemoverC struct {
	h *C.aec3_echoremover
}

func newEchoRemoverC() *echoRemoverC {
	return &echoRemoverC{h: C.aec3_echoremover_create()}
}

func (e *echoRemoverC) close() {
	C.aec3_echoremover_destroy(e.h)
	e.h = nil
}

func (e *echoRemoverC) insertRenderBlock(samples []float32) {
	C.aec3_echoremover_insert_render_block(e.h, (*C.float)(&samples[0]))
}

// externalDelayArgs mirrors the aecstate slice's own struct of the
// same name/shape.
type externalDelayArgs struct {
	has     bool
	quality int // 0=kCoarse, 1=kRefined
	value   int
}

type processResult struct {
	capture      [blockFloats]float32
	linearOutput [blockFloats]float32
	erl          float64
	erle         float64
}

func (e *echoRemoverC) process(capture []float32, captureSaturation, gainChange bool, delayChange int, clockDrift bool, ext externalDelayArgs) processResult {
	var captureBuf [blockFloats]float32
	copy(captureBuf[:], capture)

	var linearOutput [blockFloats]float32
	var metrics [2]float64

	C.aec3_echoremover_process(
		e.h,
		(*C.float)(&captureBuf[0]),
		boolToC(captureSaturation),
		boolToC(gainChange),
		C.int(delayChange),
		boolToC(clockDrift),
		boolToC(ext.has),
		C.int(ext.quality),
		C.int(ext.value),
		(*C.float)(&linearOutput[0]),
		(*C.double)(&metrics[0]),
	)

	return processResult{
		capture:      captureBuf,
		linearOutput: linearOutput,
		erl:          metrics[0],
		erle:         metrics[1],
	}
}

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
