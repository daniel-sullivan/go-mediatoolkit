// This file ports modules/audio_processing/aec3/subtractor.{h,cc}:
// the top-level linear-adaptive-filter stage that runs the refined
// and coarse AdaptiveFirFilters (in parallel) over each capture
// channel and produces the subtraction (echo-cancelled) residual.
//
// Dropped per this port's conventions: ApmDataDumper logging (and the
// coarse-filter impulse-response bookkeeping that existed only to
// feed it), the Aec3Optimization scalar/SIMD dispatch parameter (this
// package is scalar-only), and the
// "WebRTC-Aec3CoarseFilterResetHangoverKillSwitch" field-trial kill
// switch (hardcoded to its default of "not killed", i.e. the coarse
// filter reset hangover is always active, matching upstream's
// out-of-the-box behavior).
package aec3

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
)

// DelayAdjustment mirrors EchoPathVariability::DelayAdjustment.
type DelayAdjustment int

// DelayAdjustment values. C: EchoPathVariability::DelayAdjustment.
const (
	DelayAdjustmentNone DelayAdjustment = iota
	DelayAdjustmentBufferFlush
	DelayAdjustmentNewDetectedDelay
)

// EchoPathVariability describes how the echo path has changed since
// the previous call. Port of echo_path_variability.h's
// EchoPathVariability.
type EchoPathVariability struct {
	GainChange  bool
	DelayChange DelayAdjustment
	ClockDrift  bool
}

// NewEchoPathVariability mirrors EchoPathVariability's C++ constructor.
func NewEchoPathVariability(gainChange bool, delayChange DelayAdjustment, clockDrift bool) EchoPathVariability {
	return EchoPathVariability{GainChange: gainChange, DelayChange: delayChange, ClockDrift: clockDrift}
}

// AudioPathChanged reports whether the audio path changed. C:
// EchoPathVariability::AudioPathChanged.
func (v EchoPathVariability) AudioPathChanged() bool {
	return v.GainChange || v.DelayChange != DelayAdjustmentNone
}

// predictionError computes the time-domain prediction error e = y -
// IFFT(S) (scaled), and optionally the filter output s. C: (anonymous
// namespace) PredictionError in subtractor.cc.
func predictionError(fft *Aec3FFT, s *FFTData, y []float32, e, sOut *[BlockSize]float32) {
	var tmp [FFTLength]float32
	fft.Ifft(s, &tmp)
	const scale = float32(1.0) / FFTLengthBy2
	for k := range e {
		p := mul32(tmp[k+FFTLengthBy2], scale)
		e[k] = sub32(y[k], p)
	}
	if sOut != nil {
		for k := range sOut {
			sOut[k] = scale * tmp[k+FFTLengthBy2]
		}
	}
}

// scaleFilterOutput rescales the filter output s by factor and
// recomputes e = y - s. C: (anonymous namespace) ScaleFilterOutput in
// subtractor.cc.
func scaleFilterOutput(y []float32, factor float32, e, s []float32) {
	for k := range y {
		s[k] *= factor
		e[k] = y[k] - s[k]
	}
}

// filterMisadjustmentNBlocks == FilterMisadjustmentEstimator's
// n_blocks_ (a per-instance const member in C++, but never
// constructed with anything but its default, so it is a package
// constant here).
const filterMisadjustmentNBlocks = 4

// filterMisadjustmentEstimator estimates the misadjustment of the
// refined filter and recommends a corrective scale. Port of
// Subtractor::FilterMisadjustmentEstimator (minus ApmDataDumper
// logging).
type filterMisadjustmentEstimator struct {
	nBlocksAcum      int
	e2Acum           float32
	y2Acum           float32
	invMisadjustment float32
	overhang         int
}

// update updates the misadjustment estimator. C:
// FilterMisadjustmentEstimator::Update.
func (m *filterMisadjustmentEstimator) update(output *SubtractorOutput) {
	m.e2Acum += output.E2RefinedScalar
	m.y2Acum += output.Y2
	m.nBlocksAcum++
	if m.nBlocksAcum == filterMisadjustmentNBlocks {
		if m.y2Acum > filterMisadjustmentNBlocks*200.0*200.0*BlockSize {
			upd := m.e2Acum / m.y2Acum
			if m.e2Acum > filterMisadjustmentNBlocks*7500.0*7500.0*BlockSize {
				// Duration equal to blockSizeMs * n_blocks_ * 4.
				m.overhang = 4
			} else {
				m.overhang--
				if m.overhang < 0 {
					m.overhang = 0
				}
			}

			if upd < m.invMisadjustment || m.overhang > 0 {
				m.invMisadjustment = mla(m.invMisadjustment, 0.1, upd-m.invMisadjustment)
			}
		}
		m.e2Acum = 0
		m.y2Acum = 0
		m.nBlocksAcum = 0
	}
}

// getMisadjustment returns a recommended scale for the filter so the
// prediction error energy gets closer to the energy seen at the
// microphone input. C:
// FilterMisadjustmentEstimator::GetMisadjustment.
func (m *filterMisadjustmentEstimator) getMisadjustment() float32 {
	return 2.0 / float32(math.Sqrt(float64(m.invMisadjustment)))
}

// isAdjustmentNeeded reports whether the prediction error energy is
// significantly larger than the microphone signal energy. C:
// FilterMisadjustmentEstimator::IsAdjustmentNeeded.
func (m *filterMisadjustmentEstimator) isAdjustmentNeeded() bool { return m.invMisadjustment > 10 }

// reset resets the estimator. C: FilterMisadjustmentEstimator::Reset.
func (m *filterMisadjustmentEstimator) reset() {
	m.e2Acum = 0
	m.y2Acum = 0
	m.nBlocksAcum = 0
	m.invMisadjustment = 0
	m.overhang = 0
}

// Subtractor performs the linear echo cancellation: it runs a refined
// (slow, high-precision) and a coarse (fast) AdaptiveFirFilter per
// capture channel, in parallel, and returns their prediction errors.
// Port of subtractor.{h,cc}'s Subtractor (minus ApmDataDumper).
type Subtractor struct {
	fft                *Aec3FFT
	config             config.Config
	numCaptureChannels int

	refinedFilters               []*AdaptiveFirFilter
	coarseFilter                 []*AdaptiveFirFilter
	refinedGains                 []*RefinedFilterUpdateGain
	coarseGains                  []*CoarseFilterUpdateGain
	filterMisadjustmentEstimator []filterMisadjustmentEstimator
	poorCoarseFilterCounters     []int
	coarseFilterResetHangover    []int
	refinedFrequencyResponses    [][][FFTLengthBy2Plus1]float32
	refinedImpulseResponses      [][]float32
}

// NewSubtractor mirrors Subtractor's C++ constructor.
func NewSubtractor(config config.Config, numRenderChannels, numCaptureChannels int) *Subtractor {
	s := &Subtractor{
		fft:                NewAec3FFT(),
		config:             config,
		numCaptureChannels: numCaptureChannels,

		refinedFilters:               make([]*AdaptiveFirFilter, numCaptureChannels),
		coarseFilter:                 make([]*AdaptiveFirFilter, numCaptureChannels),
		refinedGains:                 make([]*RefinedFilterUpdateGain, numCaptureChannels),
		coarseGains:                  make([]*CoarseFilterUpdateGain, numCaptureChannels),
		filterMisadjustmentEstimator: make([]filterMisadjustmentEstimator, numCaptureChannels),
		poorCoarseFilterCounters:     make([]int, numCaptureChannels),
		coarseFilterResetHangover:    make([]int, numCaptureChannels),
		refinedFrequencyResponses:    make([][][FFTLengthBy2Plus1]float32, numCaptureChannels),
		refinedImpulseResponses:      make([][]float32, numCaptureChannels),
	}

	refinedFilterLength := config.Filter.RefinedInitial.LengthBlocks
	if config.Filter.Refined.LengthBlocks > refinedFilterLength {
		refinedFilterLength = config.Filter.Refined.LengthBlocks
	}
	for ch := 0; ch < numCaptureChannels; ch++ {
		s.refinedFrequencyResponses[ch] = make([][FFTLengthBy2Plus1]float32, refinedFilterLength)
		s.refinedImpulseResponses[ch] = make([]float32, GetTimeDomainLength(refinedFilterLength))
	}

	for ch := 0; ch < numCaptureChannels; ch++ {
		s.refinedFilters[ch] = NewAdaptiveFirFilter(config.Filter.Refined.LengthBlocks, config.Filter.RefinedInitial.LengthBlocks, config.Filter.ConfigChangeDurationBlocks, numRenderChannels)
		s.coarseFilter[ch] = NewAdaptiveFirFilter(config.Filter.Coarse.LengthBlocks, config.Filter.CoarseInitial.LengthBlocks, config.Filter.ConfigChangeDurationBlocks, numRenderChannels)
		s.refinedGains[ch] = NewRefinedFilterUpdateGain(config.Filter.RefinedInitial, config.Filter.ConfigChangeDurationBlocks)
		s.coarseGains[ch] = NewCoarseFilterUpdateGain(config.Filter.CoarseInitial, config.Filter.ConfigChangeDurationBlocks)
	}

	return s
}

// HandleEchoPathChange takes action in the case of a known echo path
// change. C: Subtractor::HandleEchoPathChange.
func (s *Subtractor) HandleEchoPathChange(echoPathVariability EchoPathVariability) {
	fullReset := func() {
		for ch := 0; ch < s.numCaptureChannels; ch++ {
			s.refinedFilters[ch].HandleEchoPathChange()
			s.coarseFilter[ch].HandleEchoPathChange()
			s.refinedGains[ch].HandleEchoPathChange(echoPathVariability)
			s.coarseGains[ch].HandleEchoPathChange()
			s.refinedGains[ch].SetConfig(s.config.Filter.RefinedInitial, true)
			s.coarseGains[ch].SetConfig(s.config.Filter.CoarseInitial, true)
			s.refinedFilters[ch].SetSizePartitions(s.config.Filter.RefinedInitial.LengthBlocks, true)
			s.coarseFilter[ch].SetSizePartitions(s.config.Filter.CoarseInitial.LengthBlocks, true)
		}
	}

	if echoPathVariability.DelayChange != DelayAdjustmentNone {
		fullReset()
	}

	if echoPathVariability.GainChange {
		for ch := 0; ch < s.numCaptureChannels; ch++ {
			s.refinedGains[ch].HandleEchoPathChange(echoPathVariability)
		}
	}
}

// ExitInitialState exits the initial state. C:
// Subtractor::ExitInitialState.
func (s *Subtractor) ExitInitialState() {
	for ch := 0; ch < s.numCaptureChannels; ch++ {
		s.refinedGains[ch].SetConfig(s.config.Filter.Refined, false)
		s.coarseGains[ch].SetConfig(s.config.Filter.Coarse, false)
		s.refinedFilters[ch].SetSizePartitions(s.config.Filter.Refined.LengthBlocks, false)
		s.coarseFilter[ch].SetSizePartitions(s.config.Filter.Coarse.LengthBlocks, false)
	}
}

// FilterFrequencyResponses returns the block-wise frequency responses
// for the refined adaptive filters. C:
// Subtractor::FilterFrequencyResponses.
func (s *Subtractor) FilterFrequencyResponses() [][][FFTLengthBy2Plus1]float32 {
	return s.refinedFrequencyResponses
}

// FilterImpulseResponses returns the estimates of the impulse
// responses for the refined adaptive filters. C:
// Subtractor::FilterImpulseResponses.
func (s *Subtractor) FilterImpulseResponses() [][]float32 { return s.refinedImpulseResponses }

// Process performs the echo subtraction. capture is a Block with
// s.numCaptureChannels channels; outputs must have the same length.
// C: Subtractor::Process.
func (s *Subtractor) Process(renderBuffer *RenderBuffer, capture *Block, renderSignalAnalyzer *RenderSignalAnalyzer, aecState *AecState, outputs []SubtractorOutput) {
	// Compute the render powers.
	sameFilterSizes := s.refinedFilters[0].SizePartitions() == s.coarseFilter[0].SizePartitions()
	var x2Refined [FFTLengthBy2Plus1]float32
	var x2CoarseData [FFTLengthBy2Plus1]float32
	x2Coarse := &x2CoarseData
	if sameFilterSizes {
		x2Coarse = &x2Refined
		renderBuffer.SpectralSum(s.refinedFilters[0].SizePartitions(), x2Refined[:])
	} else if s.refinedFilters[0].SizePartitions() > s.coarseFilter[0].SizePartitions() {
		renderBuffer.SpectralSums(s.coarseFilter[0].SizePartitions(), s.refinedFilters[0].SizePartitions(), x2Coarse[:], x2Refined[:])
	} else {
		renderBuffer.SpectralSums(s.refinedFilters[0].SizePartitions(), s.coarseFilter[0].SizePartitions(), x2Refined[:], x2Coarse[:])
	}

	// Process all capture channels.
	for ch := 0; ch < s.numCaptureChannels; ch++ {
		output := &outputs[ch]
		y := capture.View(0, ch)
		eRefined := &output.ERefinedTime
		eCoarse := &output.ECoarseTime

		// sg aliases the same FftData buffer the C++ uses for both S and
		// (later, reassigned) G.
		sg := &FFTData{}

		// Form the outputs of the refined and coarse filters.
		s.refinedFilters[ch].Filter(renderBuffer, sg)
		predictionError(s.fft, sg, y, eRefined, &output.SRefined)

		s.coarseFilter[ch].Filter(renderBuffer, sg)
		predictionError(s.fft, sg, y, eCoarse, &output.SCoarse)

		// Compute the signal powers in the subtractor output.
		output.ComputeMetrics(y)

		// Adjust the filter if needed.
		refinedFiltersAdjusted := false
		s.filterMisadjustmentEstimator[ch].update(output)
		if s.filterMisadjustmentEstimator[ch].isAdjustmentNeeded() {
			scale := s.filterMisadjustmentEstimator[ch].getMisadjustment()
			s.refinedFilters[ch].ScaleFilter(scale)
			for i := range s.refinedImpulseResponses[ch] {
				s.refinedImpulseResponses[ch][i] *= scale
			}
			scaleFilterOutput(y, scale, eRefined[:], output.SRefined[:])
			s.filterMisadjustmentEstimator[ch].reset()
			refinedFiltersAdjusted = true
		}

		// Compute the FFTs of the refined and coarse filter outputs.
		eCoarseFFT := &FFTData{}
		s.fft.ZeroPaddedFft(eRefined[:], WindowHanning, &output.ERefined)
		s.fft.ZeroPaddedFft(eCoarse[:], WindowHanning, eCoarseFFT)

		// Compute spectra for future use.
		eCoarseFFT.Spectrum(output.E2Coarse[:])
		output.ERefined.Spectrum(output.E2Refined[:])

		// Update the refined filter.
		if !refinedFiltersAdjusted {
			// Do not allow the performance of the coarse filter to affect
			// the adaptation speed of the refined filter just after the
			// coarse filter has been reset.
			disallowLeakageDiverged := s.coarseFilterResetHangover[ch] > 0

			var erl [FFTLengthBy2Plus1]float32
			ComputeErl(s.refinedFrequencyResponses[ch], erl[:])
			s.refinedGains[ch].Compute(&x2Refined, renderSignalAnalyzer, output, erl[:], s.refinedFilters[ch].SizePartitions(), aecState.SaturatedCapture(), disallowLeakageDiverged, sg)
		} else {
			sg.Re = [FFTLengthBy2Plus1]float32{}
			sg.Im = [FFTLengthBy2Plus1]float32{}
		}
		s.refinedFilters[ch].AdaptAndUpdateImpulseResponse(renderBuffer, sg, &s.refinedImpulseResponses[ch])
		s.refinedFilters[ch].ComputeFrequencyResponse(&s.refinedFrequencyResponses[ch])

		// Update the coarse filter.
		if output.E2RefinedScalar < output.E2CoarseScalar {
			s.poorCoarseFilterCounters[ch]++
		} else {
			s.poorCoarseFilterCounters[ch] = 0
		}
		if s.poorCoarseFilterCounters[ch] < 5 {
			s.coarseGains[ch].Compute(x2Coarse, renderSignalAnalyzer, eCoarseFFT, s.coarseFilter[ch].SizePartitions(), aecState.SaturatedCapture(), sg)
			s.coarseFilterResetHangover[ch]--
			if s.coarseFilterResetHangover[ch] < 0 {
				s.coarseFilterResetHangover[ch] = 0
			}
		} else {
			s.poorCoarseFilterCounters[ch] = 0
			s.coarseFilter[ch].SetFilter(s.refinedFilters[ch].SizePartitions(), s.refinedFilters[ch].GetFilter())
			s.coarseGains[ch].Compute(x2Coarse, renderSignalAnalyzer, &output.ERefined, s.coarseFilter[ch].SizePartitions(), aecState.SaturatedCapture(), sg)
			s.coarseFilterResetHangover[ch] = s.config.Filter.CoarseResetHangoverBlocks
		}

		// Not tracking coarse impulse responses: that bookkeeping only fed
		// ApmDataDumper in the oracle, which this port drops.
		s.coarseFilter[ch].Adapt(renderBuffer, sg)

		for k := range eRefined {
			v := eRefined[k]
			if !(v >= -32768) { // also catches NaN, matching rtc::SafeClamp
				v = -32768
			} else if v > 32767 {
				v = 32767
			}
			eRefined[k] = v
		}
	}
}
