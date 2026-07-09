// This file provides AudioBuffer, a fixed-sample-rate, fixed-channel-
// count band-splitting helper used by EchoCanceller3 (echo_canceller3.go)
// to convert between one 10ms full-band audio frame and its
// frequency-band decomposition, mirroring the subset of
// modules/audio_processing/audio_buffer.{h,cc}'s AudioBuffer that
// EchoCanceller3 actually exercises: SplitIntoFrequencyBands and
// MergeFrequencyBands, dispatched (via the real AudioBuffer's
// SplittingFilter member, modules/audio_processing/
// splitting_filter.{h,cc}) to the two-band QMF (32kHz) or three-band
// filter bank (48kHz) primitives already ported in bandsplit.go, or a
// pure identity passthrough at 16kHz.
//
// Deliberately NOT ported (none of these are reachable from
// EchoCanceller3, which always constructs its AudioBuffers with
// input_rate == buffer_rate == output_rate and a fixed channel count
// for the lifetime of the canceller):
//   - Cross-rate resampling (PushSincResampler input_resamplers_/
//     output_resamplers_) and CopyFrom/CopyTo/ExportSplitChannelData/
//     ImportSplitChannelData: AudioBuffer's general-purpose interleave/
//     deinterleave/resample surface for APM's full pipeline. This port
//     only needs the split/merge step; the public aec API (see
//     ../../doc.go and canceller.go) does its own float64<->FloatS16
//     conversion and full-band frame assembly directly.
//   - set_num_channels/RestoreNumChannels and the downmix-to-fewer-
//     channels machinery: channel counts are fixed at construction (see
//     echo_canceller3.go's file header, drop (2)).
//
// 16kHz is a pure single-band passthrough: NumBandsForRate(16000) == 1,
// and upstream's own SplittingFilter constructor RTC_CHECKs
// num_bands_ in {2, 3} -- a real SplittingFilter is never constructed
// at 16kHz (see audio_processing_impl.cc's SampleRateSupportsMultiBand
// guard, which is false for 16000). This port models that as an
// explicit identity-copy case so EchoCanceller3's calling code can
// invoke Split/MergeFrequencyBands uniformly at every supported rate.
package aec3

// twoBandsState holds the four QMF all-pass filter state arrays for
// one channel's 32kHz two-band split/merge. C: splitting_filter.h's
// TwoBandsStates (zero-initialized, matching TwoBandsStates's default
// constructor).
type twoBandsState struct {
	analysisState1  [QMFStateSize]int32
	analysisState2  [QMFStateSize]int32
	synthesisState1 [QMFStateSize]int32
	synthesisState2 [QMFStateSize]int32
}

// AudioBuffer converts one 10ms full-band multichannel audio frame
// (FloatS16 domain, i.e. scaled to the int16 range but stored as
// float32 -- see bandsplit.go's FloatS16ToS16/S16ToFloatS16) into its
// per-band decomposition and back, for a fixed sample rate and channel
// count. C: the SplitIntoFrequencyBands/MergeFrequencyBands subset of
// AudioBuffer, dispatched through SplittingFilter.
type AudioBuffer struct {
	numChannels    int
	numBands       int // NumBandsForRate(sampleRateHz): 1, 2, or 3
	numFrames      int // full-band frame length: sampleRateHz/100
	numSplitFrames int // per-band frame length: numFrames/numBands

	twoBands   []twoBandsState        // len numChannels, used iff numBands == 2
	threeBands []*ThreeBandFilterBank // len numChannels, used iff numBands == 3

	// reuseScratch selects whether SplitIntoFrequencyBands/
	// MergeFrequencyBands return freshly allocated results (false) or
	// reuse splitScratch/mergeScratch across calls (true). Render-side
	// splits are queued (EchoCanceller3.renderQueue) and can have many
	// simultaneously-live results outstanding under the ~1s
	// feed-ahead pacing contract (see canceller.go's FeedFarEnd doc
	// comment), so the render AudioBuffer must keep reuseScratch
	// false. The capture AudioBuffer's split/merge results are only
	// ever used synchronously within one processCapture call and never
	// retained past it, so it's safe to set reuseScratch true there to
	// avoid a make() on every 10ms frame. See NewEchoCanceller3.
	reuseScratch bool
	splitScratch [][][]float32 // used iff reuseScratch; shape [numBands][numChannels][numSplitFrames]
	mergeScratch [][]float32   // used iff reuseScratch; shape [numChannels][numFrames]

	// low16Scratch/high16Scratch/full16Scratch are transient int16
	// conversion buffers for the numBands==2 (32kHz QMF) case, reused
	// across calls and across channels regardless of reuseScratch:
	// each is fully overwritten and consumed (converted back to
	// FloatS16 into split/fullBand) before the next channel's
	// iteration reuses it, so no result value is ever aliased through
	// them -- unlike splitScratch/mergeScratch, there is no queued-
	// frame hazard to guard against.
	low16Scratch  []int16
	high16Scratch []int16
	full16Scratch []int16
}

// NewAudioBuffer constructs an AudioBuffer for sampleRateHz (16000,
// 32000, or 48000) and numChannels channels, with freshly zeroed
// filter-bank state. C: AudioBuffer's constructor (the split_data_/
// splitting_filter_ initialization only; see the file header for what
// is intentionally not ported).
//
// reuseScratch selects the allocation strategy for
// SplitIntoFrequencyBands/MergeFrequencyBands -- see the AudioBuffer.
// reuseScratch field doc. Callers whose split/merge results must
// outlive the call that produced them (e.g. a queue of buffered
// frames) must pass false.
func NewAudioBuffer(sampleRateHz, numChannels int, reuseScratch bool) *AudioBuffer {
	numBands := NumBandsForRate(sampleRateHz)
	numFrames := sampleRateHz / 100
	numSplitFrames := numFrames / numBands
	ab := &AudioBuffer{
		numChannels:    numChannels,
		numBands:       numBands,
		numFrames:      numFrames,
		numSplitFrames: numSplitFrames,
		reuseScratch:   reuseScratch,
	}
	switch numBands {
	case 2:
		ab.twoBands = make([]twoBandsState, numChannels)
		ab.low16Scratch = make([]int16, numSplitFrames)
		ab.high16Scratch = make([]int16, numSplitFrames)
		ab.full16Scratch = make([]int16, numFrames)
	case 3:
		ab.threeBands = make([]*ThreeBandFilterBank, numChannels)
		for i := range ab.threeBands {
			ab.threeBands[i] = NewThreeBandFilterBank()
		}
	}
	if reuseScratch {
		ab.splitScratch = make([][][]float32, numBands)
		for band := range ab.splitScratch {
			ab.splitScratch[band] = make([][]float32, numChannels)
			for ch := range ab.splitScratch[band] {
				ab.splitScratch[band][ch] = make([]float32, numSplitFrames)
			}
		}
		ab.mergeScratch = make([][]float32, numChannels)
		for ch := range ab.mergeScratch {
			ab.mergeScratch[ch] = make([]float32, numFrames)
		}
	}
	return ab
}

// NumBands returns the number of frequency bands (1, 2, or 3).
func (ab *AudioBuffer) NumBands() int { return ab.numBands }

// NumFrames returns the full-band frame length in samples
// (sampleRateHz/100).
func (ab *AudioBuffer) NumFrames() int { return ab.numFrames }

// NumSplitFrames returns the per-band frame length in samples
// (NumFrames()/NumBands()).
func (ab *AudioBuffer) NumSplitFrames() int { return ab.numSplitFrames }

// SplitIntoFrequencyBands splits fullBand -- one 10ms full-band frame
// per channel (fullBand[channel], each of length NumFrames(),
// FloatS16 domain) -- into its frequency bands, returned as
// split[band][channel] (each of length NumSplitFrames()). C:
// AudioBuffer::SplitIntoFrequencyBands (=> SplittingFilter::Analysis).
func (ab *AudioBuffer) SplitIntoFrequencyBands(fullBand [][]float32) [][][]float32 {
	var split [][][]float32
	if ab.reuseScratch {
		split = ab.splitScratch
	} else {
		split = make([][][]float32, ab.numBands)
		for band := range split {
			split[band] = make([][]float32, ab.numChannels)
			for ch := range split[band] {
				split[band][ch] = make([]float32, ab.numSplitFrames)
			}
		}
	}

	switch ab.numBands {
	case 1:
		// 16kHz: no SplittingFilter is ever constructed upstream; the
		// single band IS the full band (see file header).
		for ch := 0; ch < ab.numChannels; ch++ {
			copy(split[0][ch], fullBand[ch])
		}
	case 2:
		// C: SplittingFilter::TwoBandsAnalysis.
		full16, low16, high16 := ab.full16Scratch, ab.low16Scratch, ab.high16Scratch
		for ch := 0; ch < ab.numChannels; ch++ {
			for i, v := range fullBand[ch] {
				full16[i] = FloatS16ToS16(v)
			}
			st := &ab.twoBands[ch]
			AnalysisQMF(full16, low16, high16, st.analysisState1[:], st.analysisState2[:])
			for i, v := range low16 {
				split[0][ch][i] = S16ToFloatS16(v)
			}
			for i, v := range high16 {
				split[1][ch][i] = S16ToFloatS16(v)
			}
		}
	case 3:
		// C: SplittingFilter::ThreeBandsAnalysis.
		for ch := 0; ch < ab.numChannels; ch++ {
			var in [FullBandSize]float32
			copy(in[:], fullBand[ch])
			var bandsOut [ThreeBandFilterNumBands][]float32
			for b := range bandsOut {
				bandsOut[b] = split[b][ch]
			}
			ab.threeBands[ch].Analysis(&in, &bandsOut)
		}
	}
	return split
}

// MergeFrequencyBands recombines split (split[band][channel], each of
// length NumSplitFrames()) into one 10ms full-band frame per channel,
// returned as fullBand[channel] (each of length NumFrames(), FloatS16
// domain). C: AudioBuffer::MergeFrequencyBands (=>
// SplittingFilter::Synthesis).
func (ab *AudioBuffer) MergeFrequencyBands(split [][][]float32) [][]float32 {
	var fullBand [][]float32
	if ab.reuseScratch {
		fullBand = ab.mergeScratch
	} else {
		fullBand = make([][]float32, ab.numChannels)
		for ch := range fullBand {
			fullBand[ch] = make([]float32, ab.numFrames)
		}
	}

	switch ab.numBands {
	case 1:
		for ch := 0; ch < ab.numChannels; ch++ {
			copy(fullBand[ch], split[0][ch])
		}
	case 2:
		// C: SplittingFilter::TwoBandsSynthesis.
		low16, high16, full16 := ab.low16Scratch, ab.high16Scratch, ab.full16Scratch
		for ch := 0; ch < ab.numChannels; ch++ {
			for i, v := range split[0][ch] {
				low16[i] = FloatS16ToS16(v)
			}
			for i, v := range split[1][ch] {
				high16[i] = FloatS16ToS16(v)
			}
			st := &ab.twoBands[ch]
			SynthesisQMF(low16, high16, full16, st.synthesisState1[:], st.synthesisState2[:])
			for i, v := range full16 {
				fullBand[ch][i] = S16ToFloatS16(v)
			}
		}
	case 3:
		// C: SplittingFilter::ThreeBandsSynthesis.
		for ch := 0; ch < ab.numChannels; ch++ {
			var bandsIn [ThreeBandFilterNumBands][]float32
			for b := range bandsIn {
				bandsIn[b] = split[b][ch]
			}
			var out [FullBandSize]float32
			ab.threeBands[ch].Synthesis(&bandsIn, &out)
			copy(fullBand[ch], out[:])
		}
	}
	return fullBand
}
