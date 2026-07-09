//go:build cgo && aec_oracle

// Package ec3 is the top-level integration parity slice: it drives
// the REAL, fetched AEC3 C++ oracle's webrtc::EchoCanceller3 (see
// ../../../oracle/VERSION for provenance) directly against this
// port's aec/internal/aec3.EchoCanceller3, frame by frame, across a
// matrix of sample rates, channel counts, and scenarios, and requires
// bit-exact agreement on every processed capture frame and every
// GetMetrics() snapshot -- see parity_test.go for the scenario table,
// the driving loop, and the one documented, intentional exception
// (clockdrift is not compared here; see that file's doc comment for
// why).
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package ec3

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// oracleCanceller wraps one aec3_canceller (the real C++
// webrtc::EchoCanceller3) lifecycle.
type oracleCanceller struct {
	c *C.aec3_canceller
}

func newOracleCanceller(sampleRateHz, numRenderChannels, numCaptureChannels int) (*oracleCanceller, error) {
	c := C.aec3_create(C.int(sampleRateHz), C.int(numRenderChannels), C.int(numCaptureChannels))
	if c == nil {
		return nil, fmt.Errorf("aec3_create failed")
	}
	return &oracleCanceller{c: c}, nil
}

// close releases the underlying EchoCanceller3. Safe to call once;
// idempotent no-op after the first call.
func (o *oracleCanceller) close() {
	if o.c != nil {
		C.aec3_destroy(o.c)
		o.c = nil
	}
}

func (o *oracleCanceller) analyzeRender(renderInterleaved []float32, frameLen int) {
	C.aec3_analyze_render(o.c, (*C.float)(unsafe.Pointer(&renderInterleaved[0])), C.int(frameLen))
}

func (o *oracleCanceller) analyzeCapture(captureInterleaved []float32, frameLen int) {
	C.aec3_analyze_capture(o.c, (*C.float)(unsafe.Pointer(&captureInterleaved[0])), C.int(frameLen))
}

// processCapture removes the estimated echo from captureInterleaved
// in place.
func (o *oracleCanceller) processCapture(captureInterleaved []float32, frameLen int, levelChange bool) {
	lc := C.int(0)
	if levelChange {
		lc = 1
	}
	C.aec3_process_capture(o.c, (*C.float)(unsafe.Pointer(&captureInterleaved[0])), C.int(frameLen), lc)
}

func (o *oracleCanceller) setAudioBufferDelay(delayMs int) {
	C.aec3_set_audio_buffer_delay(o.c, C.int(delayMs))
}

type oracleMetrics struct {
	EchoReturnLoss            float64
	EchoReturnLossEnhancement float64
	DelayMs                   int
}

func (o *oracleCanceller) getMetrics() oracleMetrics {
	var erl, erle C.double
	var delayMs C.int
	C.aec3_get_metrics(o.c, &erl, &erle, &delayMs)
	return oracleMetrics{
		EchoReturnLoss:            float64(erl),
		EchoReturnLossEnhancement: float64(erle),
		DelayMs:                   int(delayMs),
	}
}
