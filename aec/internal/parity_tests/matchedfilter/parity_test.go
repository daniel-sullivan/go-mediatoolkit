//go:build cgo && aec_oracle

package matchedfilter

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

const (
	downSamplingFactor = 4
	subBlockSize       = aec3.BlockSize / downSamplingFactor // 16
	numMatchedFilters  = 3
	excitationLimit    = float32(150)
	smoothingFast      = float32(0.7)
	smoothingSlow      = float32(0.7)
	matchingThreshold  = float32(0.2)
	trueLagSamples     = 200 // within filter 0's window (32*16=512 taps)
)

// TestMatchedFilterParity drives the oracle's MatchedFilter (forced
// scalar via Aec3Optimization::kNone) and this port's MatchedFilter
// with identical render-buffer content and capture sub-blocks --
// including a genuine correlated echo at a fixed lag so the NLMS
// filters converge and exercise the winner/pre-echo code paths -- and
// requires the reported lag estimates to agree bit-for-bit at every
// iteration.
func TestMatchedFilterParity(t *testing.T) {
	renderSize := aec3.GetDownSampledBufferSize(downSamplingFactor, numMatchedFilters)

	rng := lcg(12345)
	renderData := make([]float32, renderSize)
	for i := range renderData {
		renderData[i] = rng.next() * 4000
	}

	goRender := aec3.NewDownsampledRenderBuffer(renderSize)
	copy(goRender.Buffer, renderData)
	cRender := newDrbC(renderSize)
	defer cRender.close()
	cRender.setBuffer(renderData)

	goMF := aec3.NewMatchedFilter(subBlockSize, aec3.MatchedFilterWindowSizeSubBlocks, numMatchedFilters,
		aec3.MatchedFilterAlignmentShiftSizeSubBlocks, excitationLimit, smoothingFast, smoothingSlow,
		matchingThreshold, true)
	cMF := newMatchedFilterC(subBlockSize, aec3.MatchedFilterWindowSizeSubBlocks, numMatchedFilters,
		aec3.MatchedFilterAlignmentShiftSizeSubBlocks, excitationLimit, smoothingFast, smoothingSlow,
		matchingThreshold, true)
	defer cMF.close()

	if got, want := cMF.getMaxFilterLag(), goMF.GetMaxFilterLag(); got != want {
		t.Fatalf("GetMaxFilterLag mismatch: go %d, c %d", want, got)
	}

	noiseRng := lcg(999)
	capture := make([]float32, subBlockSize)

	for iter := 0; iter < 400; iter++ {
		// Correlate capture against renderData at a fixed lag relative to
		// filter 0's window start (alignmentShift=0), matching
		// matchedFilterCore's per-outer-i xStartIndex-i indexing.
		xStart0 := goRender.OffsetIndex(goRender.Read, subBlockSize-1)
		for i := range capture {
			idx := ((xStart0-i+trueLagSamples)%renderSize + renderSize) % renderSize
			noise := noiseRng.next() * 40
			capture[i] = renderData[idx] + noise
		}
		useSlowSmoothing := iter%7 == 0

		goMF.Update(goRender, capture, useSlowSmoothing)
		cMF.update(cRender, capture, useSlowSmoothing)

		goLag := goMF.GetBestLagEstimate()
		cLag, cPreEchoLag, cOK := cMF.getBestLagEstimate()
		if (goLag != nil) != cOK {
			t.Fatalf("iter %d: lag estimate presence mismatch: go %v, c ok=%v", iter, goLag, cOK)
		}
		if goLag != nil {
			if goLag.Lag != cLag || goLag.PreEchoLag != cPreEchoLag {
				t.Fatalf("iter %d: lag estimate mismatch: go {%d,%d}, c {%d,%d}",
					iter, goLag.Lag, goLag.PreEchoLag, cLag, cPreEchoLag)
			}
		}

		// Advance the read index as the real render-delay pipeline does
		// once per capture call.
		goRender.UpdateReadIndex(-subBlockSize)
		cRender.updateReadIndex(-subBlockSize)
		if got, want := cRender.read(), goRender.Read; got != want {
			t.Fatalf("iter %d: render read index diverged: go %d, c %d", iter, want, got)
		}
	}

	// Reset should clear the estimate identically on both sides.
	goMF.Reset(true)
	cMF.reset(true)
	if goLag := goMF.GetBestLagEstimate(); goLag != nil {
		t.Fatalf("Go MatchedFilter.Reset left a lag estimate: %v", goLag)
	}
	if _, _, ok := cMF.getBestLagEstimate(); ok {
		t.Fatalf("C MatchedFilter::Reset left a lag estimate")
	}
}

// TestMatchedFilterLagAggregatorParity drives the oracle's
// MatchedFilterLagAggregator and this port's with an identical
// sequence of MatchedFilter::LagEstimate inputs (including
// std::nullopt gaps), requiring the aggregated DelayEstimate (or its
// absence), ReliableDelayFound and GetDelayAtHighestPeak to agree
// bit-for-bit at every iteration.
func TestMatchedFilterLagAggregatorParity(t *testing.T) {
	const maxFilterLag = 2000
	const headroomSamples = 32
	const thresholdsInitial = 5
	const thresholdsConverged = 20

	delayConfig := config.DelayConfig{
		DownSamplingFactor:       downSamplingFactor,
		DelayHeadroomSamples:     headroomSamples,
		DelaySelectionThresholds: config.DelaySelectionThresholds{Initial: thresholdsInitial, Converged: thresholdsConverged},
		DetectPreEcho:            true,
	}

	goAgg := aec3.NewMatchedFilterLagAggregator(maxFilterLag, delayConfig)
	cAgg := newLagAggregatorC(maxFilterLag, downSamplingFactor, headroomSamples, thresholdsInitial, thresholdsConverged, true)
	defer cAgg.close()

	rng := lcg(54321)

	step := func(iter int, hasEstimate bool, lag, preEchoLag int) {
		var goResult *aec3.DelayEstimate
		if hasEstimate {
			de := goAgg.Aggregate(&aec3.LagEstimate{Lag: lag, PreEchoLag: preEchoLag})
			goResult = de
		} else {
			goResult = goAgg.Aggregate(nil)
		}
		cQuality, cDelay, cOK := cAgg.aggregate(hasEstimate, lag, preEchoLag)

		if (goResult != nil) != cOK {
			t.Fatalf("iter %d: aggregate presence mismatch: go %v, c ok=%v", iter, goResult, cOK)
		}
		if goResult != nil {
			if int(goResult.Quality) != cQuality || goResult.Delay != cDelay {
				t.Fatalf("iter %d: aggregate mismatch: go {%d,%d}, c {%d,%d}",
					iter, goResult.Quality, goResult.Delay, cQuality, cDelay)
			}
		}
		if got, want := cAgg.reliableDelayFound(), goAgg.ReliableDelayFound(); got != want {
			t.Fatalf("iter %d: ReliableDelayFound mismatch: go %v, c %v", iter, want, got)
		}
		if got, want := cAgg.getDelayAtHighestPeak(), goAgg.GetDelayAtHighestPeak(); got != want {
			t.Fatalf("iter %d: GetDelayAtHighestPeak mismatch: go %d, c %d", iter, want, got)
		}
	}

	// Phase 1: a converging run of consistent lag estimates around a
	// fixed candidate (with jitter), to exercise the histogram
	// convergence and pre-echo aggregation paths.
	const candidateLag = 500
	for iter := 0; iter < 600; iter++ {
		jitter := int(rng.next()*6) - 3
		step(iter, true, candidateLag+jitter, candidateLag+jitter+2)
	}

	// Phase 2: gaps (std::nullopt) interleaved with noisy, unrelated
	// lag estimates, exercising the "no estimate this call" branch.
	for iter := 600; iter < 900; iter++ {
		if iter%3 == 0 {
			step(iter, false, 0, 0)
			continue
		}
		lag := int(rng.next() * float32(maxFilterLag-1))
		if lag < 0 {
			lag = -lag
		}
		step(iter, true, lag, lag)
	}

	// A hard reset should clear significantCandidateFound identically.
	goAgg.Reset(true)
	cAgg.reset(true)
	if got, want := cAgg.reliableDelayFound(), goAgg.ReliableDelayFound(); got != want {
		t.Fatalf("post-reset ReliableDelayFound mismatch: go %v, c %v", want, got)
	}
}
