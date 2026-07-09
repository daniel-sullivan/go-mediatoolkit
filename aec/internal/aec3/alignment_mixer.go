// This file ports modules/audio_processing/aec3/alignment_mixer.{h,cc}:
// mixes multiple render/capture channels down to one channel (fixed,
// adaptive-select, or downmix) for delay estimation.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// AlignmentMixerMixingVariant is AlignmentMixer::MixingVariant.
type AlignmentMixerMixingVariant int

const (
	// AlignmentMixerDownmix == AlignmentMixer::MixingVariant::kDownmix.
	AlignmentMixerDownmix AlignmentMixerMixingVariant = iota
	// AlignmentMixerAdaptive == AlignmentMixer::MixingVariant::kAdaptive.
	AlignmentMixerAdaptive
	// AlignmentMixerFixed == AlignmentMixer::MixingVariant::kFixed.
	AlignmentMixerFixed
)

// blocksToChooseLeftOrRight == SelectChannel's
// kBlocksToChooseLeftOrRight (0.5f * kNumBlocksPerSecond, truncated).
const blocksToChooseLeftOrRight = int(0.5 * NumBlocksPerSecond)

// numBlocksBeforeEnergySmoothing == SelectChannel's
// kNumBlocksBeforeEnergySmoothing (60 * kNumBlocksPerSecond).
const numBlocksBeforeEnergySmoothing = 60 * NumBlocksPerSecond

func chooseMixingVariant(downmix, adaptiveSelection bool, numChannels int) AlignmentMixerMixingVariant {
	if numChannels == 1 {
		return AlignmentMixerFixed
	}
	if downmix {
		return AlignmentMixerDownmix
	}
	if adaptiveSelection {
		return AlignmentMixerAdaptive
	}
	return AlignmentMixerFixed
}

// AlignmentMixer performs channel conversion to mono for the purpose
// of providing a decent mono input for the delay estimation. Port of
// AlignmentMixer.
type AlignmentMixer struct {
	numChannels               int
	oneByNumChannels          float32
	excitationEnergyThreshold float32
	preferFirstTwoChannels    bool
	selectionVariant          AlignmentMixerMixingVariant
	strongBlockCounters       [2]int
	cumulativeEnergies        []float32
	selectedChannel           int
	blockCounter              int
}

// NewAlignmentMixer mirrors AlignmentMixer's C++ constructor (the
// 5-argument overload; the config.AlignmentMixingConfig overload is just a
// thin field-spread wrapper around it upstream).
func NewAlignmentMixer(numChannels int, downmix, adaptiveSelection bool, activityPowerThreshold float32, preferFirstTwoChannels bool) *AlignmentMixer {
	m := &AlignmentMixer{
		numChannels:               numChannels,
		oneByNumChannels:          1.0 / float32(numChannels),
		excitationEnergyThreshold: float32(BlockSize) * activityPowerThreshold,
		preferFirstTwoChannels:    preferFirstTwoChannels,
		selectionVariant:          chooseMixingVariant(downmix, adaptiveSelection, numChannels),
	}
	if m.selectionVariant == AlignmentMixerAdaptive {
		m.cumulativeEnergies = make([]float32, numChannels)
	}
	return m
}

// NewAlignmentMixerFromConfig builds an AlignmentMixer from a
// config.AlignmentMixingConfig. C: AlignmentMixer(size_t, const
// EchoCanceller3Config::Delay::AlignmentMixing&).
func NewAlignmentMixerFromConfig(numChannels int, config config.AlignmentMixingConfig) *AlignmentMixer {
	return NewAlignmentMixer(numChannels, config.Downmix, config.AdaptiveSelection, config.ActivityPowerThreshold, config.PreferFirstTwoChannels)
}

// ProduceOutput mixes x (numChannels channels, band 0) down into y
// (length BlockSize). C: AlignmentMixer::ProduceOutput.
func (m *AlignmentMixer) ProduceOutput(x *Block, y []float32) {
	if m.selectionVariant == AlignmentMixerDownmix {
		m.downmix(x, y)
		return
	}

	ch := 0
	if m.selectionVariant != AlignmentMixerFixed {
		ch = m.selectChannel(x)
	}

	copy(y, x.View(0, ch))
}

// downmix averages all channels of x (band 0) into y. C:
// AlignmentMixer::Downmix.
func (m *AlignmentMixer) downmix(x *Block, y []float32) {
	copy(y, x.View(0, 0))
	for ch := 1; ch < m.numChannels; ch++ {
		xCh := x.View(0, ch)
		for i := range y {
			y[i] += xCh[i]
		}
	}
	for i := range y {
		y[i] *= m.oneByNumChannels
	}
}

// selectChannel picks the strongest channel via an energy-hysteresis
// heuristic. C: AlignmentMixer::SelectChannel.
func (m *AlignmentMixer) selectChannel(x *Block) int {
	goodSignalInLeftOrRight := m.preferFirstTwoChannels &&
		(m.strongBlockCounters[0] > blocksToChooseLeftOrRight || m.strongBlockCounters[1] > blocksToChooseLeftOrRight)

	numChToAnalyze := m.numChannels
	if goodSignalInLeftOrRight {
		numChToAnalyze = 2
	}

	m.blockCounter++

	for ch := 0; ch < numChToAnalyze; ch++ {
		var x2Sum float32
		xCh := x.View(0, ch)
		for i := range xCh {
			x2Sum = mla(x2Sum, xCh[i], xCh[i])
		}

		if ch < 2 && x2Sum > m.excitationEnergyThreshold {
			m.strongBlockCounters[ch]++
		}

		if m.blockCounter <= numBlocksBeforeEnergySmoothing {
			m.cumulativeEnergies[ch] += x2Sum
		} else {
			const kSmoothing = 1.0 / (10 * float32(NumBlocksPerSecond))
			diff := sub32(x2Sum, m.cumulativeEnergies[ch])
			m.cumulativeEnergies[ch] = mla(m.cumulativeEnergies[ch], kSmoothing, diff)
		}
	}

	if m.blockCounter == numBlocksBeforeEnergySmoothing {
		kOneByNumBlocksBeforeEnergySmoothing := float32(1.0) / float32(numBlocksBeforeEnergySmoothing)
		for ch := 0; ch < numChToAnalyze; ch++ {
			m.cumulativeEnergies[ch] *= kOneByNumBlocksBeforeEnergySmoothing
		}
	}

	strongestCh := 0
	for ch := 0; ch < numChToAnalyze; ch++ {
		if m.cumulativeEnergies[ch] > m.cumulativeEnergies[strongestCh] {
			strongestCh = ch
		}
	}

	if (goodSignalInLeftOrRight && m.selectedChannel > 1) ||
		m.cumulativeEnergies[strongestCh] > 2.0*m.cumulativeEnergies[m.selectedChannel] {
		m.selectedChannel = strongestCh
	}

	return m.selectedChannel
}
