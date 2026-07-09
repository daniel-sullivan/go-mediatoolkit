// This file ports modules/audio_processing/aec3/
// erl_estimator.{h,cc}: ErlEstimator, which estimates the echo return
// loss based on the signal spectra.
package aec3

const (
	erlEstimatorMinErl = 0.01
	erlEstimatorMaxErl = 1000.0
)

// ErlEstimator estimates the echo return loss (ERL). C:
// erl_estimator.{h,cc}'s ErlEstimator.
type ErlEstimator struct {
	startupPhaseLengthBlocks int
	erl                      [FFTLengthBy2Plus1]float32
	holdCounters             [FFTLengthBy2Minus1]int
	erlTimeDomain            float32
	holdCounterTimeDomain    int
	blocksSinceReset         int
}

// NewErlEstimator constructs an ErlEstimator that stays at its
// (kMaxErl) initial estimate for startupPhaseLengthBlocks blocks. C:
// ErlEstimator::ErlEstimator.
func NewErlEstimator(startupPhaseLengthBlocks int) *ErlEstimator {
	e := &ErlEstimator{startupPhaseLengthBlocks: startupPhaseLengthBlocks}
	for k := range e.erl {
		e.erl[k] = erlEstimatorMaxErl
	}
	e.erlTimeDomain = erlEstimatorMaxErl
	return e
}

// Reset resets the ERL estimator. C: ErlEstimator::Reset.
func (e *ErlEstimator) Reset() {
	e.blocksSinceReset = 0
}

// Erl returns the estimated ERL per frequency bin. C:
// ErlEstimator::Erl.
func (e *ErlEstimator) Erl() [FFTLengthBy2Plus1]float32 {
	return e.erl
}

// ErlTimeDomain returns the fullband, time-domain, ERL estimate. C:
// ErlEstimator::ErlTimeDomain.
func (e *ErlEstimator) ErlTimeDomain() float32 {
	return e.erlTimeDomain
}

// Update updates the ERL estimate. renderSpectra is the per-render-
// channel power spectrum ([]float32, length FFTLengthBy2Plus1, as
// returned by RenderBuffer.Spectrum); captureSpectra is the
// per-capture-channel power spectrum; convergedFilters flags which
// capture channels have converged filters. C: ErlEstimator::Update.
func (e *ErlEstimator) Update(convergedFilters []bool, renderSpectra [][]float32, captureSpectra [][FFTLengthBy2Plus1]float32) {
	const x2Min float32 = 44015068.0

	numCaptureChannels := len(convergedFilters)
	firstConverged := -1
	for ch, c := range convergedFilters {
		if c {
			firstConverged = ch
			break
		}
	}
	anyFilterConverged := firstConverged >= 0

	e.blocksSinceReset++
	if e.blocksSinceReset < e.startupPhaseLengthBlocks || !anyFilterConverged {
		return
	}

	maxCaptureSpectrum := captureSpectra[0]
	if numCaptureChannels > 1 {
		maxCaptureSpectrumData := captureSpectra[firstConverged]
		for ch := firstConverged + 1; ch < numCaptureChannels; ch++ {
			if !convergedFilters[ch] {
				continue
			}
			for k := range maxCaptureSpectrumData {
				if captureSpectra[ch][k] > maxCaptureSpectrumData[k] {
					maxCaptureSpectrumData[k] = captureSpectra[ch][k]
				}
			}
		}
		maxCaptureSpectrum = maxCaptureSpectrumData
	}

	numRenderChannels := len(renderSpectra)
	var maxRenderSpectrum [FFTLengthBy2Plus1]float32
	copy(maxRenderSpectrum[:], renderSpectra[0])
	if numRenderChannels > 1 {
		for ch := 1; ch < numRenderChannels; ch++ {
			for k := range maxRenderSpectrum {
				if renderSpectra[ch][k] > maxRenderSpectrum[k] {
					maxRenderSpectrum[k] = renderSpectra[ch][k]
				}
			}
		}
	}

	x2 := maxRenderSpectrum
	y2 := maxCaptureSpectrum

	for k := 1; k < FFTLengthBy2; k++ {
		if x2[k] > x2Min {
			newErl := y2[k] / x2[k]
			if newErl < e.erl[k] {
				e.holdCounters[k-1] = 1000
				e.erl[k] = mla(e.erl[k], 0.1, sub32(newErl, e.erl[k]))
				e.erl[k] = max32(e.erl[k], erlEstimatorMinErl)
			}
		}
	}

	for k := range e.holdCounters {
		e.holdCounters[k]--
	}

	for k := 1; k < FFTLengthBy2; k++ {
		if e.holdCounters[k-1] > 0 {
			continue
		}
		e.erl[k] = minFloat32(erlEstimatorMaxErl, mul32(2.0, e.erl[k]))
	}

	e.erl[0] = e.erl[1]
	e.erl[FFTLengthBy2] = e.erl[FFTLengthBy2-1]

	// std::accumulate uses a float (not double) running sum, matching
	// the plain float32 loop below.
	var x2Sum float32
	for k := range x2 {
		x2Sum += x2[k]
	}

	if x2Sum > mul32(x2Min, float32(len(x2))) {
		var y2Sum float32
		for k := range y2 {
			y2Sum += y2[k]
		}
		newErl := y2Sum / x2Sum
		if newErl < e.erlTimeDomain {
			e.holdCounterTimeDomain = 1000
			e.erlTimeDomain = mla(e.erlTimeDomain, 0.1, sub32(newErl, e.erlTimeDomain))
			e.erlTimeDomain = max32(e.erlTimeDomain, erlEstimatorMinErl)
		}
	}

	e.holdCounterTimeDomain--
	if e.holdCounterTimeDomain <= 0 {
		e.erlTimeDomain = minFloat32(erlEstimatorMaxErl, mul32(2.0, e.erlTimeDomain))
	}
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
