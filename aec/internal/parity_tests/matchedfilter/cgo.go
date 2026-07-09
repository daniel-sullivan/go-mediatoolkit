//go:build cgo && aec_oracle

// Package matchedfilter is the bit-exact parity slice for
// aec/internal/aec3's matched_filter.go (MatchedFilter, scalar path)
// and matched_filter_lag_aggregator.go (MatchedFilterLagAggregator)
// against the fetched C++ oracle. The oracle's MatchedFilter is
// exercised with Aec3Optimization::kNone, matching this port's
// scalar-only scope (see matched_filter.go's package doc comment).
// Env/link setup is shared with the other slices via ../run.sh -- see
// the bandsplit slice's cgo.go for the full rationale.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package matchedfilter

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
*/
import "C"

// drbC wraps the oracle's webrtc::DownsampledRenderBuffer.
type drbC struct {
	h *C.aec3_drb
}

func newDrbC(size int) *drbC {
	return &drbC{h: C.aec3_drb_create(C.int(size))}
}

func (b *drbC) close() {
	C.aec3_drb_destroy(b.h)
	b.h = nil
}

func (b *drbC) setBuffer(data []float32) {
	C.aec3_drb_set_buffer(b.h, (*C.float)(&data[0]), C.int(len(data)))
}

func (b *drbC) setRead(read int) {
	C.aec3_drb_set_read(b.h, C.int(read))
}

func (b *drbC) updateReadIndex(offset int) {
	C.aec3_drb_update_read_index(b.h, C.int(offset))
}

func (b *drbC) offsetIndex(index, offset int) int {
	return int(C.aec3_drb_offset_index(b.h, C.int(index), C.int(offset)))
}

func (b *drbC) read() int {
	return int(C.aec3_drb_read(b.h))
}

// matchedFilterC wraps the oracle's webrtc::MatchedFilter, forced to
// the scalar (Aec3Optimization::kNone) path.
type matchedFilterC struct {
	h *C.aec3_matched_filter
}

func newMatchedFilterC(subBlockSize, windowSizeSubBlocks, numMatchedFilters, alignmentShiftSubBlocks int, excitationLimit, smoothingFast, smoothingSlow, matchingFilterThreshold float32, detectPreEcho bool) *matchedFilterC {
	return &matchedFilterC{h: C.aec3_matched_filter_create(
		C.int(subBlockSize), C.int(windowSizeSubBlocks), C.int(numMatchedFilters),
		C.int(alignmentShiftSubBlocks), C.float(excitationLimit), C.float(smoothingFast),
		C.float(smoothingSlow), C.float(matchingFilterThreshold), boolToC(detectPreEcho),
	)}
}

func (m *matchedFilterC) close() {
	C.aec3_matched_filter_destroy(m.h)
	m.h = nil
}

func (m *matchedFilterC) update(render *drbC, capture []float32, useSlowSmoothing bool) {
	C.aec3_matched_filter_update(m.h, render.h, (*C.float)(&capture[0]), C.int(len(capture)), boolToC(useSlowSmoothing))
}

func (m *matchedFilterC) reset(fullReset bool) {
	C.aec3_matched_filter_reset(m.h, boolToC(fullReset))
}

// getBestLagEstimate returns (lag, preEchoLag, ok).
func (m *matchedFilterC) getBestLagEstimate() (int, int, bool) {
	var lag, preEchoLag C.int
	ok := C.aec3_matched_filter_get_best_lag_estimate(m.h, &lag, &preEchoLag)
	return int(lag), int(preEchoLag), ok != 0
}

func (m *matchedFilterC) getMaxFilterLag() int {
	return int(C.aec3_matched_filter_get_max_filter_lag(m.h))
}

// lagAggregatorC wraps the oracle's webrtc::MatchedFilterLagAggregator.
type lagAggregatorC struct {
	h *C.aec3_lag_aggregator
}

func newLagAggregatorC(maxFilterLag, downSamplingFactor, delayHeadroomSamples, thresholdsInitial, thresholdsConverged int, detectPreEcho bool) *lagAggregatorC {
	return &lagAggregatorC{h: C.aec3_lag_aggregator_create(
		C.int(maxFilterLag), C.int(downSamplingFactor), C.int(delayHeadroomSamples),
		C.int(thresholdsInitial), C.int(thresholdsConverged), boolToC(detectPreEcho),
	)}
}

func (a *lagAggregatorC) close() {
	C.aec3_lag_aggregator_destroy(a.h)
	a.h = nil
}

func (a *lagAggregatorC) reset(hardReset bool) {
	C.aec3_lag_aggregator_reset(a.h, boolToC(hardReset))
}

// aggregate returns (quality, delay, ok); hasLagEstimate selects
// std::nullopt (false) vs a populated MatchedFilter::LagEstimate.
func (a *lagAggregatorC) aggregate(hasLagEstimate bool, lag, preEchoLag int) (int, int, bool) {
	var quality, delay C.int
	ok := C.aec3_lag_aggregator_aggregate(a.h, boolToC(hasLagEstimate), C.int(lag), C.int(preEchoLag), &quality, &delay)
	return int(quality), int(delay), ok != 0
}

func (a *lagAggregatorC) reliableDelayFound() bool {
	return C.aec3_lag_aggregator_reliable_delay_found(a.h) != 0
}

func (a *lagAggregatorC) getDelayAtHighestPeak() int {
	return int(C.aec3_lag_aggregator_get_delay_at_highest_peak(a.h))
}

func boolToC(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
