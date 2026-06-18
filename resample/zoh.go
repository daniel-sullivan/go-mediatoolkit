package resample

import "math"

type zohConverter struct {
	converterBase
}

func newZOH(channels int) *zohConverter {
	return &zohConverter{converterBase: newConverterBase(channels)}
}

func (z *zohConverter) Process(d *Data) error {
	if err := validateData(d, z.channels); err != nil {
		return err
	}
	if z.lastRatio == 0 {
		z.lastRatio = d.Ratio.Float64()
	}
	return z.process(d)
}

func (z *zohConverter) process(d *Data) error {
	channels := z.channels
	inputFrames := len(d.DataIn) / channels
	outputFrames := len(d.DataOut) / channels

	if inputFrames <= 0 {
		return nil
	}

	if !z.dirty {
		for ch := 0; ch < channels; ch++ {
			z.lastValue[ch] = d.DataIn[ch]
		}
		z.dirty = true
	}

	inCount := inputFrames * channels
	outCount := outputFrames * channels
	inUsed := 0
	outGen := 0

	srcRatio := z.lastRatio
	if isBadRatio(srcRatio) {
		return ErrBadInternalState
	}

	inputIndex := z.lastPosition

	// Phase 1: output last-held values while inputIndex < 1.0.
	for inputIndex < 1.0 && outGen < outCount {
		// Float compare matches libsamplerate's
		// `in_used + channels * input_index >= in_count`. Casting
		// channels*inputIndex to int loses the fractional position
		// for channels > 1, letting the loop continue past the C
		// boundary on stereo non-integer ratios.
		if float64(inUsed)+float64(channels)*inputIndex >= float64(inCount) {
			break
		}

		if outCount > 0 && math.Abs(z.lastRatio-d.Ratio.Float64()) > minRatioDiff {
			srcRatio = z.lastRatio + float64(outGen)*(d.Ratio.Float64()-z.lastRatio)/float64(outCount)
		}

		for ch := 0; ch < channels; ch++ {
			d.DataOut[outGen] = z.lastValue[ch]
			outGen++
		}

		inputIndex += 1.0 / srcRatio
	}

	rem := fmodOne(inputIndex)
	inUsed += channels * lrint(inputIndex-rem)
	inputIndex = rem

	// Phase 2: main processing loop. Float compare for the same
	// reason as Phase 1.
	for outGen < outCount && float64(inUsed)+float64(channels)*inputIndex <= float64(inCount) {
		if outCount > 0 && math.Abs(z.lastRatio-d.Ratio.Float64()) > minRatioDiff {
			srcRatio = z.lastRatio + float64(outGen)*(d.Ratio.Float64()-z.lastRatio)/float64(outCount)
		}

		for ch := 0; ch < channels; ch++ {
			d.DataOut[outGen] = d.DataIn[inUsed-channels+ch]
			outGen++
		}

		inputIndex += 1.0 / srcRatio
		rem = fmodOne(inputIndex)
		inUsed += channels * lrint(inputIndex-rem)
		inputIndex = rem
	}

	if inUsed > inCount {
		inputIndex += float64(inUsed-inCount) / float64(channels)
		inUsed = inCount
	}

	z.lastPosition = inputIndex

	if inUsed > 0 {
		for ch := 0; ch < channels; ch++ {
			z.lastValue[ch] = d.DataIn[inUsed-channels+ch]
		}
	}

	z.lastRatio = srcRatio

	d.InputFramesUsed = inUsed / channels
	d.OutputFramesGen = outGen / channels
	return nil
}

func (z *zohConverter) Reset() {
	z.reset()
}

func (z *zohConverter) Clone() Converter {
	c := newZOH(z.channels)
	c.dirty = z.dirty
	c.lastRatio = z.lastRatio
	c.lastPosition = z.lastPosition
	copy(c.lastValue, z.lastValue)
	return c
}

func (z *zohConverter) Close() {}

func (z *zohConverter) Channels() int {
	return z.channels
}

func (z *zohConverter) SetRatio(ratio Ratio) error {
	return z.setRatio(ratio)
}

func isBadRatio(ratio float64) bool {
	return !isValidRatioF(ratio)
}
