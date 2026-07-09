//go:build cgo && aec_oracle

// Package adaptivefir is the bit-exact parity slice for
// aec/internal/aec3's adaptive_fir_filter.go (AdaptiveFirFilter,
// scalar path) and adaptive_fir_filter_erl.go (ComputeErl) against the
// fetched C++ oracle. The oracle's AdaptiveFirFilter is exercised with
// Aec3Optimization::kNone, matching this port's scalar-only scope
// (see adaptive_fir_filter.go's package doc comment). Env/link setup
// is shared with the other slices via ../run.sh -- see the bandsplit
// slice's cgo.go for the full rationale.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package adaptivefir

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"
import "unsafe"

// filterC wraps the oracle's webrtc::AdaptiveFirFilter plus the
// webrtc::RenderDelayBuffer that feeds it.
type filterC struct {
	h *C.aec3_adaptivefir
}

func newFilterC(maxSizePartitions, initialSizePartitions, sizeChangeDurationBlocks, numRenderChannels int) *filterC {
	return &filterC{h: C.aec3_adaptivefir_create(
		C.int(maxSizePartitions), C.int(initialSizePartitions),
		C.int(sizeChangeDurationBlocks), C.int(numRenderChannels))}
}

func (f *filterC) close() {
	C.aec3_adaptivefir_destroy(f.h)
	f.h = nil
}

func (f *filterC) insertRenderBlock(samples []float32) {
	C.aec3_adaptivefir_insert_render_block(f.h, (*C.float)(&samples[0]))
}

func (f *filterC) handleEchoPathChange() {
	C.aec3_adaptivefir_handle_echo_path_change(f.h)
}

func (f *filterC) sizePartitions() int {
	return int(C.aec3_adaptivefir_size_partitions(f.h))
}

func (f *filterC) setSizePartitions(size int, immediateEffect bool) {
	C.aec3_adaptivefir_set_size_partitions(f.h, C.int(size), boolToC(immediateEffect))
}

func (f *filterC) maxFilterSizePartitions() int {
	return int(C.aec3_adaptivefir_max_filter_size_partitions(f.h))
}

func (f *filterC) filter() (re, im [65]float32) {
	C.aec3_adaptivefir_filter(f.h, (*C.float)(unsafe.Pointer(&re[0])), (*C.float)(unsafe.Pointer(&im[0])))
	return re, im
}

func (f *filterC) adapt(gRe, gIm [65]float32, impulseResponseCap int) []float32 {
	buf := make([]float32, impulseResponseCap)
	var n C.int
	C.aec3_adaptivefir_adapt(f.h, (*C.float)(unsafe.Pointer(&gRe[0])), (*C.float)(unsafe.Pointer(&gIm[0])),
		(*C.float)(unsafe.Pointer(&buf[0])), &n)
	return buf[:int(n)]
}

func (f *filterC) adaptNoIR(gRe, gIm [65]float32) {
	C.aec3_adaptivefir_adapt_no_ir(f.h, (*C.float)(unsafe.Pointer(&gRe[0])), (*C.float)(unsafe.Pointer(&gIm[0])))
}

func (f *filterC) computeFrequencyResponse(maxPartitions int) [][65]float32 {
	buf := make([]float32, maxPartitions*65)
	n := C.aec3_adaptivefir_compute_frequency_response(f.h, (*C.float)(unsafe.Pointer(&buf[0])))
	out := make([][65]float32, int(n))
	for p := 0; p < int(n); p++ {
		copy(out[p][:], buf[p*65:(p+1)*65])
	}
	return out
}

func (f *filterC) scaleFilter(factor float32) {
	C.aec3_adaptivefir_scale_filter(f.h, C.float(factor))
}

func computeErlC(h2 [][65]float32) [65]float32 {
	flat := make([]float32, len(h2)*65)
	for p, row := range h2 {
		copy(flat[p*65:(p+1)*65], row[:])
	}
	var erl [65]float32
	if len(flat) == 0 {
		return erl
	}
	C.aec3_adaptivefir_compute_erl((*C.float)(unsafe.Pointer(&flat[0])), C.int(len(h2)),
		(*C.float)(unsafe.Pointer(&erl[0])))
	return erl
}

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
