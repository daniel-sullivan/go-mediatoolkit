// This file is part of package r128 (see kweight.go for why the DSP core
// lives in this cgo-free internal package). It is the 1:1, bit-exact Go
// port of the remaining meter machinery of libebur128 v1.2.6
// (loudness/libebur128/ebur128.c): the audio_data ring buffer, the
// gating blocks and their channel weighting, the block-energy history
// (list and histogram modes), absolute/relative gating, integrated
// loudness, momentary/short-term/arbitrary-window energy, loudness range
// (LRA), and the sample/true peak bookkeeping.
//
// State mirrors ebur128_state + ebur128_state_internal one-to-one; every
// method here is a transliteration of the correspondingly-named ebur128_*
// function, preserving operation order, expression shape, and precision
// so results are bit-identical to the C oracle. The only deliberate
// structural change is replacing the C STAILQ block lists with Go slices
// used as FIFOs (append at tail, evict at head) — the iteration order,
// and therefore every summation order, is preserved.
//
// FMA policy: as in kweight.go and truepeak.go, every product that feeds
// a +/- is wrapped in an explicit float64() conversion barrier so the Go
// compiler cannot fuse a multiply-add into a single FMA where the C —
// compiled with -ffp-contract=off — does not.
package r128

import (
	"math"
	"sort"
)

// Errcode mirrors libebur128's `enum error` (ebur128.h). State's reader
// and mutator methods return one of these instead of a Go error so the
// port stays a 1:1 transliteration of the C, which returns these codes.
// The root loudness package translates them into its exported sentinel
// errors.
const (
	Success                  = 0
	ErrorNoMem               = 1 // unused in Go (no malloc failures); kept for enum parity
	ErrorInvalidMode         = 2
	ErrorInvalidChannelIndex = 3
	ErrorNoChange            = 4
)

// Mode bits, numerically identical to libebur128's `enum mode`
// (ebur128.h). Kept unexported here; the root package owns the public
// Mode type with the same values.
const (
	modeM          = 1 << 0
	modeS          = (1 << 1) | modeM
	modeI          = (1 << 2) | modeM
	modeLRA        = (1 << 3) | modeS
	modeSamplePeak = (1 << 4) | modeM
	modeTruePeak   = (1 << 5) | modeM | modeSamplePeak
	modeHistogram  = 1 << 6
)

// Channel enum values from ebur128.h that the gating-block weighting and
// the default channel map depend on. Only the values actually referenced
// by the port are named.
const (
	channelUnused        = 0
	channelLeft          = 1
	channelRight         = 2
	channelCenter        = 3
	channelLeftSurround  = 4 // EBUR128_Mp110
	channelRightSurround = 5 // EBUR128_Mm110
	channelDualMono      = 6
	channelMp060         = 9
	channelMm060         = 10
	channelMp090         = 11
	channelMm090         = 12
)

// relativeGate is libebur128's static relative_gate (-10 LU): the
// relative gate sits 10 LU below the ungated mean loudness.
const relativeGate = -10.0

// These mirror libebur128's file-scope statics, computed once in
// ebur128_init from pow(). histogramEnergies / histogramEnergyBoundaries
// are the 1000-bin histogram tables; boundaries[0] doubles as the
// absolute-gate energy (-70 LUFS) in every mode.
//
// In C the histogram tables past index 0 are only filled when a
// histogram-mode state is created (they are process-wide statics).
// Computing them unconditionally here is safe: in list mode their only
// consumers (find_histogram_index in the gated-loudness start-index
// computation) feed values that the list branch never uses, so filling
// them cannot change a list-mode result. In histogram mode they must be
// present, and they are.
var (
	relativeGateFactor        = math.Pow(10.0, relativeGate/10.0)
	minusTwentyDecibels       = math.Pow(10.0, -20.0/10.0)
	histogramEnergies         [1000]float64
	histogramEnergyBoundaries [1001]float64
)

func init() {
	histogramEnergyBoundaries[0] = math.Pow(10.0, (-70.0+0.691)/10.0)
	for i := 0; i < 1000; i++ {
		histogramEnergies[i] = math.Pow(10.0, (float64(i)/10.0-69.95+0.691)/10.0)
	}
	for i := 1; i < 1001; i++ {
		histogramEnergyBoundaries[i] = math.Pow(10.0, (float64(i)/10.0-70.0+0.691)/10.0)
	}
}

// State is the pure-Go port of ebur128_state (+ ebur128_state_internal):
// one loudness measurement in progress. Fields are named after their C
// counterparts. Not safe for concurrent use.
type State struct {
	// ebur128_state (public in C):
	mode       int
	channels   int
	sampleRate int

	// ebur128_state_internal:
	audioData       []float64 // ring buffer, len == audioDataFrames*channels
	audioDataFrames int
	audioDataIndex  int // in samples (frame*channels units), as in C
	neededFrames    int
	channelMap      []int
	samplesIn100ms  int

	kfilter *KFilter // holds b[5]/a[5]/v[ch][5]; drives the recurrence

	// Block-energy history. FIFO slices replace libebur128's STAILQs;
	// len() is the C block_list_size, and the *Max fields are the caps.
	blockList          []float64
	blockListMax       uint64
	shortTermBlockList []float64
	stBlockListMax     uint64

	useHistogram                  bool
	blockEnergyHistogram          [1000]uint64
	shortTermBlockEnergyHistogram [1000]uint64

	shortTermFrameCounter int

	samplePeak     []float64
	prevSamplePeak []float64
	truePeak       []float64
	prevTruePeak   []float64

	interp *TruePeaker // nil at >=192 kHz (bypass)

	window  uint64 // ms
	history uint64 // ms; math.MaxUint64 == libebur128's ULONG_MAX default
}

// NewState builds the meter core for the given channel count, sample
// rate, and mode bitmask (raw libebur128 mode bits). It is the port of
// ebur128_init's double path. Inputs are assumed already validated by the
// caller (the root loudness.NewMeter checks sample rate, channel count,
// and that the mode carries at least the momentary bit), mirroring how
// NewKFilter/NewTruePeaker take no error return.
func NewState(channels, sampleRate, mode int) *State {
	s := &State{
		mode:       mode,
		channels:   channels,
		sampleRate: sampleRate,
	}

	s.initChannelMap()

	s.samplePeak = make([]float64, channels)
	s.prevSamplePeak = make([]float64, channels)
	s.truePeak = make([]float64, channels)
	s.prevTruePeak = make([]float64, channels)

	s.useHistogram = mode&modeHistogram != 0
	s.history = math.MaxUint64 // ULONG_MAX
	s.samplesIn100ms = (sampleRate + 5) / 10

	// Window default: 3000 ms if short-term is available, else 400 ms.
	if mode&modeS == modeS {
		s.window = 3000
	} else {
		s.window = 400
	}
	s.allocAudioData()

	s.kfilter = NewKFilter(sampleRate, channels)

	s.blockListMax = s.history / 100
	s.stBlockListMax = s.history / 3000
	s.shortTermFrameCounter = 0

	s.interp = NewTruePeaker(sampleRate, channels)

	// The first block needs 400 ms of audio; start at the buffer head.
	s.neededFrames = s.samplesIn100ms * 4
	s.audioDataIndex = 0

	return s
}

// allocAudioData (re)allocates the zeroed ring buffer for the current
// window, sizing it exactly as ebur128_init:
//
//	audio_data_frames = samplerate * window / 1000, rounded UP to a
//	multiple of samples_in_100ms.
func (s *State) allocAudioData() {
	frames := s.sampleRate * int(s.window) / 1000
	if frames%s.samplesIn100ms != 0 {
		frames = (frames + s.samplesIn100ms) - (frames % s.samplesIn100ms)
	}
	s.audioDataFrames = frames
	s.audioData = make([]float64, frames*s.channels)
}

// initChannelMap ports ebur128_init_channel_map: the default speaker
// assignment by channel count.
func (s *State) initChannelMap() {
	s.channelMap = make([]int, s.channels)
	switch s.channels {
	case 4:
		s.channelMap[0] = channelLeft
		s.channelMap[1] = channelRight
		s.channelMap[2] = channelLeftSurround
		s.channelMap[3] = channelRightSurround
	case 5:
		s.channelMap[0] = channelLeft
		s.channelMap[1] = channelRight
		s.channelMap[2] = channelCenter
		s.channelMap[3] = channelLeftSurround
		s.channelMap[4] = channelRightSurround
	default:
		for i := 0; i < s.channels; i++ {
			switch i {
			case 0:
				s.channelMap[i] = channelLeft
			case 1:
				s.channelMap[i] = channelRight
			case 2:
				s.channelMap[i] = channelCenter
			case 3:
				s.channelMap[i] = channelUnused
			case 4:
				s.channelMap[i] = channelLeftSurround
			case 5:
				s.channelMap[i] = channelRightSurround
			default:
				s.channelMap[i] = channelUnused
			}
		}
	}
}

// Mode reports the raw mode bitmask.
func (s *State) Mode() int { return s.mode }

// Channels reports the channel count.
func (s *State) Channels() int { return s.channels }

// SampleRate reports the sample rate.
func (s *State) SampleRate() int { return s.sampleRate }

// filter is the port of the double variant of EBUR128_FILTER: the
// per-buffer sample-peak scan, the true-peak oversampling, and the
// K-weighting IIR recurrence that writes filtered samples into the ring
// buffer at audioDataIndex. src must hold exactly `frames` interleaved
// frames (len(src) == frames*channels).
//
// The three passes are independent (they read src and write disjoint
// state), so their relative order does not affect results; they are kept
// in the C's order for clarity.
func (s *State) filter(src []float64, frames int) {
	ch := s.channels
	base := s.audioDataIndex // audio_data + audio_data_index

	// Sample-peak scan. scaling_factor for the double path is 1.0, so
	// the C's `max /= scaling_factor` is a no-op and is elided.
	if s.mode&modeSamplePeak == modeSamplePeak {
		for c := 0; c < ch; c++ {
			max := 0.0
			for i := 0; i < frames; i++ {
				cur := src[i*ch+c]
				m := cur // EBUR128_MAX(cur, -cur)
				if -cur > m {
					m = -cur
				}
				if m > max {
					max = m
				}
			}
			if max > s.prevSamplePeak[c] {
				s.prevSamplePeak[c] = max
			}
		}
	}

	// True-peak oversampling. TruePeaker.Process is exactly
	// ebur128_check_true_peak composed with the (float) narrowing of the
	// input (scaling_factor == 1.0). It is a no-op on a nil (>=192 kHz
	// bypass) interpolator, matching the `st->d->interp` non-null guard.
	if s.mode&modeTruePeak == modeTruePeak && s.interp != nil {
		s.interp.Process(src, s.prevTruePeak)
	}

	// K-weighting recurrence, per channel, skipping UNUSED channels
	// exactly as the C filter loop does.
	f := s.kfilter
	a1, a2, a3, a4 := f.A[1], f.A[2], f.A[3], f.A[4]
	b0, b1, b2, b3, b4 := f.B[0], f.B[1], f.B[2], f.B[3], f.B[4]
	for c := 0; c < ch; c++ {
		if s.channelMap[c] == channelUnused {
			continue
		}
		st := &f.v[c]
		v0, v1, v2, v3, v4 := st[0], st[1], st[2], st[3], st[4]
		for i := 0; i < frames; i++ {
			idx := i*ch + c
			v0 = src[idx] - float64(a1*v1) - float64(a2*v2) - float64(a3*v3) - float64(a4*v4)
			s.audioData[base+idx] = float64(b0*v0) + float64(b1*v1) + float64(b2*v2) + float64(b3*v3) + float64(b4*v4)
			v4, v3, v2, v1 = v3, v2, v1, v0
		}
		// FLUSH_MANUALLY: the non-SSE2 denormal guard (v0 is deliberately
		// left un-flushed; it is overwritten before it is next read).
		if math.Abs(v1) < dblMin {
			v1 = 0.0
		}
		if math.Abs(v2) < dblMin {
			v2 = 0.0
		}
		if math.Abs(v3) < dblMin {
			v3 = 0.0
		}
		if math.Abs(v4) < dblMin {
			v4 = 0.0
		}
		st[0], st[1], st[2], st[3], st[4] = v0, v1, v2, v3, v4
	}
}

// calcGatingBlock ports ebur128_calc_gating_block. It sums the squared
// K-weighted samples over the trailing framesPerBlock frames (wrapping
// the ring buffer), applies the BS.1770 channel weights, divides by the
// block length, and either writes the block energy to optionalOutput (if
// non-nil) or records it into the block-energy history / histogram.
func (s *State) calcGatingBlock(framesPerBlock int, optionalOutput *float64) {
	ch := s.channels
	sum := 0.0
	for c := 0; c < ch; c++ {
		if s.channelMap[c] == channelUnused {
			continue
		}
		channelSum := 0.0
		if s.audioDataIndex < framesPerBlock*ch {
			for i := 0; i < s.audioDataIndex/ch; i++ {
				d := s.audioData[i*ch+c]
				channelSum += float64(d * d)
			}
			for i := s.audioDataFrames - (framesPerBlock - s.audioDataIndex/ch); i < s.audioDataFrames; i++ {
				d := s.audioData[i*ch+c]
				channelSum += float64(d * d)
			}
		} else {
			for i := s.audioDataIndex/ch - framesPerBlock; i < s.audioDataIndex/ch; i++ {
				d := s.audioData[i*ch+c]
				channelSum += float64(d * d)
			}
		}
		switch s.channelMap[c] {
		case channelLeftSurround, channelRightSurround, channelMp060, channelMm060, channelMp090, channelMm090:
			channelSum *= 1.41
		case channelDualMono:
			channelSum *= 2.0
		}
		sum += channelSum
	}

	sum /= float64(framesPerBlock)

	if optionalOutput != nil {
		*optionalOutput = sum
		return
	}

	if sum >= histogramEnergyBoundaries[0] {
		if s.useHistogram {
			s.blockEnergyHistogram[findHistogramIndex(sum)]++
		} else {
			if uint64(len(s.blockList)) == s.blockListMax {
				s.blockList = s.blockList[1:] // reuse head (FIFO eviction)
			}
			s.blockList = append(s.blockList, sum)
		}
	}
}

// findHistogramIndex ports find_histogram_index: the binary search over
// histogramEnergyBoundaries for the bin containing energy.
func findHistogramIndex(energy float64) int {
	indexMin := 0
	indexMax := 1000
	for {
		indexMid := (indexMin + indexMax) / 2
		if energy >= histogramEnergyBoundaries[indexMid] {
			indexMin = indexMid
		} else {
			indexMax = indexMid
		}
		if indexMax-indexMin == 1 {
			break
		}
	}
	return indexMin
}

// energyToLoudness ports ebur128_energy_to_loudness:
// 10 * (log(energy) / log(10)) - 0.691. The product `10 * quotient` is
// barriered against fusion with the trailing subtract. log/log is spelled
// exactly as the C (not log10), so the transcendental path matches.
func (s *State) energyToLoudness(energy float64) float64 {
	return float64(10*(math.Log(energy)/math.Log(10.0))) - 0.691
}

// AddFrames ports ebur128_add_frames_double: it filters the interleaved
// float64 input in needed_frames-sized steps, emits gating blocks (100 ms
// hop after the first 400 ms block), records short-term energies at the
// 1 s LRA hop, and maintains the per-add sample/true peak maxima. src must
// be a whole number of frames (len(src) % channels == 0); the caller
// validates this.
func (s *State) AddFrames(src []float64) {
	ch := s.channels
	for c := 0; c < ch; c++ {
		s.prevSamplePeak[c] = 0.0
		s.prevTruePeak[c] = 0.0
	}

	frames := len(src) / ch
	srcIndex := 0
	for frames > 0 {
		if frames >= s.neededFrames {
			s.filter(src[srcIndex:srcIndex+s.neededFrames*ch], s.neededFrames)
			srcIndex += s.neededFrames * ch
			frames -= s.neededFrames
			s.audioDataIndex += s.neededFrames * ch
			if s.mode&modeI == modeI {
				s.calcGatingBlock(s.samplesIn100ms*4, nil)
			}
			if s.mode&modeLRA == modeLRA {
				s.shortTermFrameCounter += s.neededFrames
				if s.shortTermFrameCounter == s.samplesIn100ms*30 {
					var stEnergy float64
					if s.energyShortterm(&stEnergy) == Success && stEnergy >= histogramEnergyBoundaries[0] {
						if s.useHistogram {
							s.shortTermBlockEnergyHistogram[findHistogramIndex(stEnergy)]++
						} else {
							if uint64(len(s.shortTermBlockList)) == s.stBlockListMax {
								s.shortTermBlockList = s.shortTermBlockList[1:]
							}
							s.shortTermBlockList = append(s.shortTermBlockList, stEnergy)
						}
					}
					s.shortTermFrameCounter = s.samplesIn100ms * 20
				}
			}
			// 100 ms are needed for every block after the first.
			s.neededFrames = s.samplesIn100ms
			if s.audioDataIndex == s.audioDataFrames*ch {
				s.audioDataIndex = 0
			}
		} else {
			s.filter(src[srcIndex:srcIndex+frames*ch], frames)
			s.audioDataIndex += frames * ch
			if s.mode&modeLRA == modeLRA {
				s.shortTermFrameCounter += frames
			}
			s.neededFrames -= frames
			frames = 0
		}
	}

	for c := 0; c < ch; c++ {
		if s.prevSamplePeak[c] > s.samplePeak[c] {
			s.samplePeak[c] = s.prevSamplePeak[c]
		}
		if s.prevTruePeak[c] > s.truePeak[c] {
			s.truePeak[c] = s.prevTruePeak[c]
		}
	}
}

// calcRelativeThreshold ports ebur128_calc_relative_threshold for a single
// state: it accumulates the ungated block energies and their count, from
// either the histogram or the block list.
func (s *State) calcRelativeThreshold() (aboveThreshCounter int, relativeThreshold float64) {
	if s.useHistogram {
		for i := 0; i < 1000; i++ {
			relativeThreshold += float64(float64(s.blockEnergyHistogram[i]) * histogramEnergies[i])
			aboveThreshCounter += int(s.blockEnergyHistogram[i])
		}
	} else {
		for _, z := range s.blockList {
			aboveThreshCounter++
			relativeThreshold += z
		}
	}
	return aboveThreshCounter, relativeThreshold
}

// GatedLoudness ports ebur128_gated_loudness / ebur128_loudness_global for
// a single state: the two-pass gated integration (absolute gate, then
// relative gate at ungated mean − 10 LU). Returns -Inf when no block
// passes either gate.
func (s *State) GatedLoudness() (float64, int) {
	if s.mode&modeI != modeI {
		return 0, ErrorInvalidMode
	}
	aboveThreshCounter, relativeThreshold := s.calcRelativeThreshold()
	if aboveThreshCounter == 0 {
		return math.Inf(-1), Success
	}

	relativeThreshold /= float64(aboveThreshCounter)
	relativeThreshold *= relativeGateFactor

	var gatedLoudness float64
	aboveThreshCounter = 0
	var startIndex int
	if relativeThreshold < histogramEnergyBoundaries[0] {
		startIndex = 0
	} else {
		startIndex = findHistogramIndex(relativeThreshold)
		if relativeThreshold > histogramEnergies[startIndex] {
			startIndex++
		}
	}

	if s.useHistogram {
		for j := startIndex; j < 1000; j++ {
			gatedLoudness += float64(float64(s.blockEnergyHistogram[j]) * histogramEnergies[j])
			aboveThreshCounter += int(s.blockEnergyHistogram[j])
		}
	} else {
		for _, z := range s.blockList {
			if z >= relativeThreshold {
				aboveThreshCounter++
				gatedLoudness += z
			}
		}
	}

	if aboveThreshCounter == 0 {
		return math.Inf(-1), Success
	}
	gatedLoudness /= float64(aboveThreshCounter)
	return s.energyToLoudness(gatedLoudness), Success
}

// RelativeThreshold ports ebur128_relative_threshold: the gated-loudness
// relative gate expressed in LUFS. Returns -70 LUFS when no block passes
// the absolute gate.
func (s *State) RelativeThreshold() (float64, int) {
	if s.mode&modeI != modeI {
		return 0, ErrorInvalidMode
	}
	aboveThreshCounter, relativeThreshold := s.calcRelativeThreshold()
	if aboveThreshCounter == 0 {
		return -70.0, Success
	}
	relativeThreshold /= float64(aboveThreshCounter)
	relativeThreshold *= relativeGateFactor
	return s.energyToLoudness(relativeThreshold), Success
}

// energyInInterval ports ebur128_energy_in_interval: the block energy over
// the trailing intervalFrames frames, or ErrorInvalidMode if the interval
// exceeds the ring buffer.
func (s *State) energyInInterval(intervalFrames int, out *float64) int {
	if intervalFrames > s.audioDataFrames {
		return ErrorInvalidMode
	}
	s.calcGatingBlock(intervalFrames, out)
	return Success
}

// energyShortterm ports ebur128_energy_shortterm (the 3 s interval).
func (s *State) energyShortterm(out *float64) int {
	return s.energyInInterval(s.samplesIn100ms*30, out)
}

// LoudnessMomentary ports ebur128_loudness_momentary (trailing 400 ms).
func (s *State) LoudnessMomentary() (float64, int) {
	var energy float64
	if e := s.energyInInterval(s.samplesIn100ms*4, &energy); e != Success {
		return 0, e
	}
	if energy <= 0.0 {
		return math.Inf(-1), Success
	}
	return s.energyToLoudness(energy), Success
}

// LoudnessShortterm ports ebur128_loudness_shortterm (trailing 3 s). It
// returns ErrorInvalidMode when the ring buffer is shorter than 3 s (i.e.
// the meter lacks the short-term window), matching the C's implicit
// interval check.
func (s *State) LoudnessShortterm() (float64, int) {
	var energy float64
	if e := s.energyShortterm(&energy); e != Success {
		return 0, e
	}
	if energy <= 0.0 {
		return math.Inf(-1), Success
	}
	return s.energyToLoudness(energy), Success
}

// LoudnessWindow ports ebur128_loudness_window: loudness over an arbitrary
// trailing window (in ms), bounded by the configured max window.
func (s *State) LoudnessWindow(window uint64) (float64, int) {
	if window > s.window {
		return 0, ErrorInvalidMode
	}
	intervalFrames := int(uint64(s.sampleRate) * window / 1000)
	var energy float64
	if e := s.energyInInterval(intervalFrames, &energy); e != Success {
		return 0, e
	}
	if energy <= 0.0 {
		return math.Inf(-1), Success
	}
	return s.energyToLoudness(energy), Success
}

// LoudnessRange ports ebur128_loudness_range_multiple for a single state:
// the EBU Tech 3342 loudness range from the short-term block energies,
// in both list and histogram modes.
func (s *State) LoudnessRange() (float64, int) {
	if s.mode&modeLRA != modeLRA {
		return 0, ErrorInvalidMode
	}

	if s.useHistogram {
		var hist [1000]uint64
		var stlSize int
		var stlPower float64
		for j := 0; j < 1000; j++ {
			hist[j] += s.shortTermBlockEnergyHistogram[j]
			stlSize += int(s.shortTermBlockEnergyHistogram[j])
			stlPower += float64(float64(s.shortTermBlockEnergyHistogram[j]) * histogramEnergies[j])
		}
		if stlSize == 0 {
			return 0.0, Success
		}
		stlPower /= float64(stlSize)
		stlIntegrated := minusTwentyDecibels * stlPower

		var index int
		if stlIntegrated < histogramEnergyBoundaries[0] {
			index = 0
		} else {
			index = findHistogramIndex(stlIntegrated)
			if stlIntegrated > histogramEnergies[index] {
				index++
			}
		}
		stlSize = 0
		for j := index; j < 1000; j++ {
			stlSize += int(hist[j])
		}
		if stlSize == 0 {
			return 0.0, Success
		}

		percentileLow := int(float64(float64(stlSize-1)*0.1) + 0.5)
		percentileHigh := int(float64(float64(stlSize-1)*0.95) + 0.5)

		stlSize = 0
		j := index
		for stlSize <= percentileLow {
			stlSize += int(hist[j])
			j++
		}
		lEn := histogramEnergies[j-1]
		for stlSize <= percentileHigh {
			stlSize += int(hist[j])
			j++
		}
		hEn := histogramEnergies[j-1]

		return s.energyToLoudness(hEn) - s.energyToLoudness(lEn), Success
	}

	stlSize := len(s.shortTermBlockList)
	if stlSize == 0 {
		return 0.0, Success
	}
	stlVector := make([]float64, stlSize)
	copy(stlVector, s.shortTermBlockList)
	sort.Float64s(stlVector) // ascending, matching qsort + ebur128_double_cmp

	var stlPower float64
	for i := 0; i < stlSize; i++ {
		stlPower += stlVector[i]
	}
	stlPower /= float64(stlSize)
	stlIntegrated := minusTwentyDecibels * stlPower

	// Relative gate: drop the lowest energies below the integrated
	// threshold, exactly as the C advances stl_relgated from the front.
	relStart := 0
	relSize := stlSize
	for relSize > 0 && stlVector[relStart] < stlIntegrated {
		relStart++
		relSize--
	}

	if relSize != 0 {
		hEn := stlVector[relStart+int(float64(float64(relSize-1)*0.95)+0.5)]
		lEn := stlVector[relStart+int(float64(float64(relSize-1)*0.1)+0.5)]
		return s.energyToLoudness(hEn) - s.energyToLoudness(lEn), Success
	}
	return 0.0, Success
}

// SetChannel ports ebur128_set_channel: reassign channelNumber's
// weighting role. DUAL_MONO is only valid on a single-channel meter's
// channel 0.
func (s *State) SetChannel(channelNumber, value int) int {
	if channelNumber < 0 || channelNumber >= s.channels {
		return ErrorInvalidChannelIndex
	}
	if value == channelDualMono && (s.channels != 1 || channelNumber != 0) {
		return ErrorInvalidChannelIndex
	}
	s.channelMap[channelNumber] = value
	return Success
}

// SetMaxWindow ports ebur128_set_max_window: grow/shrink the maximum
// window available to LoudnessWindow, reallocating (and clearing) the ring
// buffer and resetting the block accounting.
//
// NOTE — this reproduces a genuine v1.2.6 sizing quirk: the new buffer is
// sized samplerate*window frames (NOT samplerate*window/1000), i.e. ~1000x
// larger than the millisecond window implies. This is faithful to the
// vendored oracle (verified against upstream v1.2.6) and is required for
// bit-exact parity; callers should pass modest windows.
func (s *State) SetMaxWindow(window uint64) int {
	if s.mode&modeS == modeS && window < 3000 {
		window = 3000
	} else if s.mode&modeM == modeM && window < 400 {
		window = 400
	}
	if window == s.window {
		return ErrorNoChange
	}

	newAudioDataFrames := int(uint64(s.sampleRate) * window)
	if newAudioDataFrames%s.samplesIn100ms != 0 {
		newAudioDataFrames = (newAudioDataFrames + s.samplesIn100ms) -
			(newAudioDataFrames % s.samplesIn100ms)
	}

	s.window = window
	s.audioData = make([]float64, newAudioDataFrames*s.channels)
	s.audioDataFrames = newAudioDataFrames

	s.neededFrames = s.samplesIn100ms * 4
	s.audioDataIndex = 0
	s.shortTermFrameCounter = 0
	return Success
}

// SetMaxHistory ports ebur128_set_max_history: bound the stored block and
// short-term-block history and trim any excess from the front (oldest).
func (s *State) SetMaxHistory(history uint64) int {
	if s.mode&modeLRA == modeLRA && history < 3000 {
		history = 3000
	} else if s.mode&modeM == modeM && history < 400 {
		history = 400
	}
	if history == s.history {
		return ErrorNoChange
	}
	s.history = history
	s.blockListMax = s.history / 100
	s.stBlockListMax = s.history / 3000
	for uint64(len(s.blockList)) > s.blockListMax {
		s.blockList = s.blockList[1:]
	}
	for uint64(len(s.shortTermBlockList)) > s.stBlockListMax {
		s.shortTermBlockList = s.shortTermBlockList[1:]
	}
	return Success
}

// SamplePeak ports ebur128_sample_peak.
func (s *State) SamplePeak(channelNumber int) (float64, int) {
	if s.mode&modeSamplePeak != modeSamplePeak {
		return 0, ErrorInvalidMode
	}
	if channelNumber < 0 || channelNumber >= s.channels {
		return 0, ErrorInvalidChannelIndex
	}
	return s.samplePeak[channelNumber], Success
}

// PrevSamplePeak ports ebur128_prev_sample_peak.
func (s *State) PrevSamplePeak(channelNumber int) (float64, int) {
	if s.mode&modeSamplePeak != modeSamplePeak {
		return 0, ErrorInvalidMode
	}
	if channelNumber < 0 || channelNumber >= s.channels {
		return 0, ErrorInvalidChannelIndex
	}
	return s.prevSamplePeak[channelNumber], Success
}

// TruePeak ports ebur128_true_peak: max(true_peak, sample_peak).
func (s *State) TruePeak(channelNumber int) (float64, int) {
	if s.mode&modeTruePeak != modeTruePeak {
		return 0, ErrorInvalidMode
	}
	if channelNumber < 0 || channelNumber >= s.channels {
		return 0, ErrorInvalidChannelIndex
	}
	v := s.truePeak[channelNumber]
	if s.samplePeak[channelNumber] > v {
		v = s.samplePeak[channelNumber]
	}
	return v, Success
}

// PrevTruePeak ports ebur128_prev_true_peak:
// max(prev_true_peak, prev_sample_peak).
func (s *State) PrevTruePeak(channelNumber int) (float64, int) {
	if s.mode&modeTruePeak != modeTruePeak {
		return 0, ErrorInvalidMode
	}
	if channelNumber < 0 || channelNumber >= s.channels {
		return 0, ErrorInvalidChannelIndex
	}
	v := s.prevTruePeak[channelNumber]
	if s.prevSamplePeak[channelNumber] > v {
		v = s.prevSamplePeak[channelNumber]
	}
	return v, Success
}

// Reset returns the meter to a fresh-stream state while preserving its
// configuration (channel map, window, history, mode, coefficients).
// libebur128 has no reset entry point; this clears exactly the
// measurement state a re-init would, without reallocating the ring buffer
// (the window, and therefore its size, is unchanged).
func (s *State) Reset() {
	for i := range s.audioData {
		s.audioData[i] = 0.0
	}
	s.audioDataIndex = 0
	s.neededFrames = s.samplesIn100ms * 4
	s.shortTermFrameCounter = 0

	s.blockList = nil
	s.shortTermBlockList = nil
	for i := range s.blockEnergyHistogram {
		s.blockEnergyHistogram[i] = 0
		s.shortTermBlockEnergyHistogram[i] = 0
	}

	for c := 0; c < s.channels; c++ {
		s.samplePeak[c] = 0.0
		s.prevSamplePeak[c] = 0.0
		s.truePeak[c] = 0.0
		s.prevTruePeak[c] = 0.0
	}

	s.kfilter.Reset()
	s.interp.Reset()
}
