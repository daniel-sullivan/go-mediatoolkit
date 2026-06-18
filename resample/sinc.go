package resample

import (
	"math"
	"runtime"
)

// coeffTable holds a precomputed sinc filter coefficient table.
type coeffTable struct {
	increment int
	coeffs    []float64
}

// maxChannels is the maximum channel count for the sinc converter.
// Matches MAX_CHANNELS in the C implementation.
const maxChannels = 128

type sincConverter struct {
	channels     int
	lastRatio    float64
	lastPosition float64
	isMono       bool

	// Filter parameters.
	coeffs       []float64
	coeffHalfLen int
	indexInc     int

	// Circular buffer.
	buffer   []float64
	bCurrent int
	bEnd     int
	bRealEnd int // -1 until EndOfInput is signaled.
	bLen     int

	// Per-Process counters (shared with prepareData).
	inCount  int
	inUsed   int
	outCount int
	outGen   int

	// Multi-channel accumulators (pre-allocated, reused per output sample).
	leftCalc  []float64
	rightCalc []float64
}

func newSinc(converterType ConverterType, channels int) (*sincConverter, error) {
	if channels > maxChannels {
		return nil, ErrBadChannelCount
	}

	var table *coeffTable
	switch converterType {
	case SincFastest:
		table = &fastestCoeffs
	case SincMediumQuality:
		table = &midQualCoeffs
	case SincBestQuality:
		table = &highQualCoeffs
	default:
		return nil, ErrBadConverterType
	}

	s := &sincConverter{
		channels:     channels,
		isMono:       channels == 1,
		coeffs:       table.coeffs,
		coeffHalfLen: len(table.coeffs) - 2,
		indexInc:     table.increment,
		bRealEnd:     -1,
	}

	// Buffer sizing matches the C implementation exactly.
	bLen := 3 * lrint(float64(s.coeffHalfLen+2)/float64(s.indexInc)*MaxRatio+1)
	if bLen < 4096 {
		bLen = 4096
	}
	bLen *= channels
	bLen += 1

	s.bLen = bLen
	s.buffer = make([]float64, bLen+channels)

	if channels > 1 {
		s.leftCalc = make([]float64, channels)
		s.rightCalc = make([]float64, channels)
	}

	return s, nil
}

func (s *sincConverter) Channels() int { return s.channels }

func (s *sincConverter) SetRatio(ratio Ratio) error {
	r := ratio.Float64()
	if !isValidRatioF(r) {
		return ErrBadSrcRatio
	}
	s.lastRatio = r
	return nil
}

func (s *sincConverter) Process(d *Data) error {
	if err := validateData(d, s.channels); err != nil {
		return err
	}
	if s.lastRatio == 0 {
		s.lastRatio = d.Ratio.Float64()
	}
	// Use parallel processing when multiple CPUs are available and the
	// output is large enough to amortize goroutine overhead.
	outputFrames := len(d.DataOut) / s.channels
	if runtime.GOMAXPROCS(0) > 1 && outputFrames >= parallelThreshold {
		return s.processLoopParallel(d)
	}
	return s.processLoop(d)
}

func (s *sincConverter) processLoop(d *Data) error {
	channels := s.channels
	inputFrames := len(d.DataIn) / channels
	outputFrames := len(d.DataOut) / channels

	s.inCount = inputFrames * channels
	s.outCount = outputFrames * channels
	s.inUsed = 0
	s.outGen = 0

	srcRatio := s.lastRatio
	if isBadRatio(srcRatio) {
		return ErrBadInternalState
	}

	// Calculate the required number of samples on either side of the filter center.
	count := float64(s.coeffHalfLen+2) / float64(s.indexInc)
	minRatio := math.Min(s.lastRatio, d.Ratio.Float64())
	if minRatio < 1.0 {
		count /= minRatio
	}
	halfFilterChanLen := channels * (lrint(count) + 1)

	// Restore fractional position from previous call.
	inputIndex := s.lastPosition
	rem := fmodOne(inputIndex)
	s.bCurrent = (s.bCurrent + channels*lrint(inputIndex-rem)) % s.bLen
	inputIndex = rem

	terminate := 1.0/srcRatio + 1e-20

	outCount := s.outCount
	indexInc := s.indexInc

	// Constant-ratio fast path: hoist invariants out of the loop.
	constantRatio := math.Abs(s.lastRatio-d.Ratio.Float64()) <= 1e-10
	var constFloatInc float64
	var constInc increment
	var constScale float64
	var constInvRatio float64
	if constantRatio {
		r := srcRatio
		if r > 1.0 {
			r = 1.0
		}
		constFloatInc = float64(indexInc) * r
		constInc = doubleToFP(constFloatInc)
		constScale = constFloatInc / float64(indexInc)
		constInvRatio = 1.0 / srcRatio
	}

	for s.outGen < outCount {
		// Check if the buffer has enough data for the filter.
		samplesInHand := s.bEnd - s.bCurrent
		if samplesInHand < 0 {
			samplesInHand += s.bLen
		}

		if samplesInHand <= halfFilterChanLen {
			if err := s.prepareData(d, halfFilterChanLen); err != nil {
				return err
			}
			samplesInHand = s.bEnd - s.bCurrent
			if samplesInHand < 0 {
				samplesInHand += s.bLen
			}
			if samplesInHand <= halfFilterChanLen {
				break
			}
		}

		// Termination condition.
		if s.bRealEnd >= 0 {
			if s.isMono {
				if float64(s.bCurrent)+inputIndex+terminate > float64(s.bRealEnd) {
					break
				}
			} else {
				if float64(s.bCurrent)+inputIndex+terminate >= float64(s.bRealEnd) {
					break
				}
			}
		}

		var inc increment
		var startFilterIndex increment
		var scale float64
		var invRatio float64

		if constantRatio {
			inc = constInc
			startFilterIndex = doubleToFP(inputIndex * constFloatInc)
			scale = constScale
			invRatio = constInvRatio
		} else {
			if outCount > 0 {
				srcRatio = s.lastRatio + float64(s.outGen)*(d.Ratio.Float64()-s.lastRatio)/float64(outCount)
			}
			r := srcRatio
			if r > 1.0 {
				r = 1.0
			}
			floatIncrement := float64(indexInc) * r
			inc = doubleToFP(floatIncrement)
			startFilterIndex = doubleToFP(inputIndex * floatIncrement)
			scale = floatIncrement / float64(indexInc)
			invRatio = 1.0 / srcRatio
		}

		if s.isMono {
			s.calcOutputMono(s.bCurrent, inc, startFilterIndex, scale, d.DataOut[s.outGen:])
		} else {
			s.calcOutputMulti(s.bCurrent, inc, startFilterIndex, scale, d.DataOut[s.outGen:], s.leftCalc, s.rightCalc)
		}
		s.outGen += channels

		// Advance input position. Must match libsamplerate's
		// `lrint(input_index - fmod_one(input_index))`, which is the
		// FLOOR of input_index, not RoundToEven — for fractional parts
		// in (0.5, 1.0) the two disagree by 1, causing the loop to
		// skip an input sample every ~6 iterations at downsample
		// ratios near 0.91875 (e.g. 48k→44.1k) and underproduce
		// ~30% of expected output. Verified against the
		// libsamplerate parity oracle.
		inputIndex += invRatio
		floor := math.Floor(inputIndex)
		inputIndex -= floor
		// Conditional wrap instead of modulo — wrapping is rare.
		s.bCurrent += channels * int(floor)
		if s.bCurrent >= s.bLen {
			s.bCurrent -= s.bLen
		}
	}

	s.lastPosition = inputIndex
	s.lastRatio = srcRatio

	d.InputFramesUsed = s.inUsed / channels
	d.OutputFramesGen = s.outGen / channels
	return nil
}

// calcOutputMono computes one mono output sample by applying the sinc filter
// symmetrically around bCurrent. Safe for concurrent use — reads only from
// the shared (immutable during processing) buffer and coefficients.
func (s *sincConverter) calcOutputMono(bCurrent int, inc increment, startFilterIndex increment, scale float64, out []float64) {
	coeffs := s.coeffs
	buffer := s.buffer
	maxFilterIndex := intToFP(s.coeffHalfLen)

	// Left half of the filter: walk backward from the current position.
	filterIndex := startFilterIndex
	coeffCount := int((maxFilterIndex - filterIndex) / inc)
	filterIndex = filterIndex + increment(coeffCount)*inc
	dataIndex := bCurrent - coeffCount

	if dataIndex < 0 {
		steps := -dataIndex
		filterIndex -= inc * increment(steps)
		dataIndex += steps
	}

	// BCE hints: prove max indices to the compiler so it elides bounds checks in the loop.
	if filterIndex >= 0 {
		maxCoeffIdx := fpToInt(filterIndex) + 1
		_ = coeffs[maxCoeffIdx]
		_ = buffer[dataIndex]
	}

	left := 0.0
	for filterIndex >= 0 {
		fraction := fpToDouble(filterIndex)
		indx := fpToInt(filterIndex)
		icoeff := coeffs[indx] + fraction*(coeffs[indx+1]-coeffs[indx])
		left += icoeff * buffer[dataIndex]
		filterIndex -= inc
		dataIndex++
	}

	// Right half of the filter: walk forward from the next position.
	filterIndex = inc - startFilterIndex
	coeffCount = int((maxFilterIndex - filterIndex) / inc)
	filterIndex = filterIndex + increment(coeffCount)*inc
	dataIndex = bCurrent + 1 + coeffCount

	// BCE hints for right half.
	if filterIndex > 0 {
		maxCoeffIdx := fpToInt(filterIndex) + 1
		_ = coeffs[maxCoeffIdx]
		_ = buffer[dataIndex]
	}

	right := 0.0
	for {
		fraction := fpToDouble(filterIndex)
		indx := fpToInt(filterIndex)
		icoeff := coeffs[indx] + fraction*(coeffs[indx+1]-coeffs[indx])
		right += icoeff * buffer[dataIndex]
		filterIndex -= inc
		dataIndex--
		if filterIndex <= 0 {
			break
		}
	}

	out[0] = scale * (left + right)
}

// calcOutputMulti computes one frame (all channels) of output by applying
// the sinc filter symmetrically around bCurrent. The caller provides scratch
// slices (leftCalc, rightCalc) of length >= channels. Safe for concurrent use
// when each goroutine provides its own scratch slices.
func (s *sincConverter) calcOutputMulti(bCurrent int, inc increment, startFilterIndex increment, scale float64, out []float64, leftCalc, rightCalc []float64) {
	coeffs := s.coeffs
	buffer := s.buffer
	channels := s.channels
	maxFilterIndex := intToFP(s.coeffHalfLen)

	for ch := 0; ch < channels; ch++ {
		leftCalc[ch] = 0
		rightCalc[ch] = 0
	}

	// Left half.
	filterIndex := startFilterIndex
	coeffCount := int((maxFilterIndex - filterIndex) / inc)
	filterIndex = filterIndex + increment(coeffCount)*inc
	dataIndex := bCurrent - channels*coeffCount

	if dataIndex < 0 {
		steps := (-dataIndex + channels - 1) / channels
		filterIndex -= inc * increment(steps)
		dataIndex += steps * channels
	}

	// BCE hints.
	if filterIndex >= 0 {
		_ = coeffs[fpToInt(filterIndex)+1]
		_ = buffer[dataIndex+channels-1]
	}

	for filterIndex >= 0 {
		fraction := fpToDouble(filterIndex)
		indx := fpToInt(filterIndex)
		icoeff := coeffs[indx] + fraction*(coeffs[indx+1]-coeffs[indx])
		for ch := 0; ch < channels; ch++ {
			leftCalc[ch] += icoeff * buffer[dataIndex+ch]
		}
		filterIndex -= inc
		dataIndex += channels
	}

	// Right half.
	filterIndex = inc - startFilterIndex
	coeffCount = int((maxFilterIndex - filterIndex) / inc)
	filterIndex = filterIndex + increment(coeffCount)*inc
	dataIndex = bCurrent + channels*(1+coeffCount)

	// BCE hints.
	if filterIndex > 0 {
		_ = coeffs[fpToInt(filterIndex)+1]
		_ = buffer[dataIndex+channels-1]
	}

	for {
		fraction := fpToDouble(filterIndex)
		indx := fpToInt(filterIndex)
		icoeff := coeffs[indx] + fraction*(coeffs[indx+1]-coeffs[indx])
		for ch := 0; ch < channels; ch++ {
			rightCalc[ch] += icoeff * buffer[dataIndex+ch]
		}
		filterIndex -= inc
		dataIndex -= channels
		if filterIndex <= 0 {
			break
		}
	}

	for ch := 0; ch < channels; ch++ {
		out[ch] = scale * (leftCalc[ch] + rightCalc[ch])
	}
}

// prepareData loads input samples into the circular buffer.
func (s *sincConverter) prepareData(d *Data, halfFilterChanLen int) error {
	if s.bRealEnd >= 0 {
		return nil
	}

	channels := s.channels
	var length int

	if s.bCurrent == 0 {
		// Initial state: leave space for the left side of the filter.
		length = s.bLen - 2*halfFilterChanLen
		s.bCurrent = halfFilterChanLen
		s.bEnd = halfFilterChanLen
	} else if s.bEnd+halfFilterChanLen+channels < s.bLen {
		// Space available: append after current data.
		length = s.bLen - s.bCurrent - halfFilterChanLen
		if length < 0 {
			length = 0
		}
	} else {
		// No space: move data back to start of buffer.
		srcStart := s.bCurrent - halfFilterChanLen
		dataLen := s.bEnd - srcStart
		copy(s.buffer, s.buffer[srcStart:srcStart+dataLen])
		s.bCurrent = halfFilterChanLen
		s.bEnd = s.bCurrent + (dataLen - halfFilterChanLen)
		length = s.bLen - s.bCurrent - halfFilterChanLen
		if length < 0 {
			length = 0
		}
	}

	// Clamp to available input.
	available := s.inCount - s.inUsed
	if length > available {
		length = available
	}
	// Align to channel boundaries.
	length -= length % channels

	if length < 0 || s.bEnd+length > s.bLen {
		return errSincPrepareData
	}

	if length > 0 {
		copy(s.buffer[s.bEnd:s.bEnd+length], d.DataIn[s.inUsed:s.inUsed+length])
		s.bEnd += length
		s.inUsed += length
	}

	// Handle end-of-input: pad with zeros so the filter can read past the last sample.
	if s.inUsed == s.inCount && d.EndOfInput && s.bEnd-s.bCurrent < 2*halfFilterChanLen {
		if s.bLen-s.bEnd < halfFilterChanLen+5 {
			// Not enough room to pad — move data back first.
			srcStart := s.bCurrent - halfFilterChanLen
			dataLen := s.bEnd - srcStart
			copy(s.buffer, s.buffer[srcStart:srcStart+dataLen])
			s.bCurrent = halfFilterChanLen
			s.bEnd = s.bCurrent + (dataLen - halfFilterChanLen)
		}
		s.bRealEnd = s.bEnd
		padLen := halfFilterChanLen + 5
		if s.bEnd+padLen > len(s.buffer) {
			padLen = len(s.buffer) - s.bEnd
		}
		clear(s.buffer[s.bEnd : s.bEnd+padLen])
		s.bEnd += padLen
	}

	return nil
}

func (s *sincConverter) Reset() {
	s.lastRatio = 0
	s.lastPosition = 0
	s.bCurrent = 0
	s.bEnd = 0
	s.bRealEnd = -1
	s.inCount = 0
	s.inUsed = 0
	s.outCount = 0
	s.outGen = 0
	clear(s.buffer)
	// Write sentinel bytes past bLen for debug (matching C behavior).
	for i := s.bLen; i < len(s.buffer); i++ {
		s.buffer[i] = math.Float64frombits(0xAAAAAAAAAAAAAAAA)
	}
}

func (s *sincConverter) Clone() Converter {
	c := &sincConverter{
		channels:     s.channels,
		isMono:       s.isMono,
		lastRatio:    s.lastRatio,
		lastPosition: s.lastPosition,
		coeffs:       s.coeffs, // Shared read-only data.
		coeffHalfLen: s.coeffHalfLen,
		indexInc:     s.indexInc,
		bCurrent:     s.bCurrent,
		bEnd:         s.bEnd,
		bRealEnd:     s.bRealEnd,
		bLen:         s.bLen,
		inCount:      s.inCount,
		inUsed:       s.inUsed,
		outCount:     s.outCount,
		outGen:       s.outGen,
	}
	c.buffer = make([]float64, len(s.buffer))
	copy(c.buffer, s.buffer)
	if s.channels > 1 {
		c.leftCalc = make([]float64, s.channels)
		c.rightCalc = make([]float64, s.channels)
	}
	return c
}

func (s *sincConverter) Close() {
	s.buffer = nil
	s.leftCalc = nil
	s.rightCalc = nil
}
