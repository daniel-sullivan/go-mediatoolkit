package resample

import "math"

type linearConverter struct {
	converterBase
}

func newLinear(channels int) *linearConverter {
	return &linearConverter{converterBase: newConverterBase(channels)}
}

func (l *linearConverter) Process(d *Data) error {
	if err := validateData(d, l.channels); err != nil {
		return err
	}
	if l.lastRatio == 0 {
		l.lastRatio = d.Ratio.Float64()
	}
	return l.process(d)
}

func (l *linearConverter) process(d *Data) error {
	channels := l.channels
	inputFrames := len(d.DataIn) / channels
	outputFrames := len(d.DataOut) / channels

	if inputFrames <= 0 {
		return nil
	}

	if !l.dirty {
		for ch := 0; ch < channels; ch++ {
			l.lastValue[ch] = d.DataIn[ch]
		}
		l.dirty = true
	}

	inCount := inputFrames * channels
	outCount := outputFrames * channels
	inUsed := 0
	outGen := 0

	srcRatio := l.lastRatio
	if isBadRatio(srcRatio) {
		return ErrBadInternalState
	}

	inputIndex := l.lastPosition

	// Phase 1: interpolate between lastValue and first input sample.
	for inputIndex < 1.0 && outGen < outCount {
		if inUsed+int(float64(channels)*(1.0+inputIndex)) >= inCount {
			break
		}

		if outCount > 0 && math.Abs(l.lastRatio-d.Ratio.Float64()) > minRatioDiff {
			srcRatio = l.lastRatio + float64(outGen)*(d.Ratio.Float64()-l.lastRatio)/float64(outCount)
		}

		for ch := 0; ch < channels; ch++ {
			d.DataOut[outGen] = l.lastValue[ch] + inputIndex*(d.DataIn[ch]-l.lastValue[ch])
			outGen++
		}

		inputIndex += 1.0 / srcRatio
	}

	rem := fmodOne(inputIndex)
	inUsed += channels * lrint(inputIndex-rem)
	inputIndex = rem

	// Phase 2: main processing loop — interpolate between consecutive input samples.
	for outGen < outCount && inUsed+int(float64(channels)*inputIndex) < inCount {
		if outCount > 0 && math.Abs(l.lastRatio-d.Ratio.Float64()) > minRatioDiff {
			srcRatio = l.lastRatio + float64(outGen)*(d.Ratio.Float64()-l.lastRatio)/float64(outCount)
		}

		for ch := 0; ch < channels; ch++ {
			d.DataOut[outGen] = d.DataIn[inUsed-channels+ch] +
				inputIndex*(d.DataIn[inUsed+ch]-d.DataIn[inUsed-channels+ch])
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

	l.lastPosition = inputIndex

	if inUsed > 0 {
		for ch := 0; ch < channels; ch++ {
			l.lastValue[ch] = d.DataIn[inUsed-channels+ch]
		}
	}

	l.lastRatio = srcRatio

	d.InputFramesUsed = inUsed / channels
	d.OutputFramesGen = outGen / channels
	return nil
}

func (l *linearConverter) Reset() {
	l.reset()
}

func (l *linearConverter) Clone() Converter {
	c := newLinear(l.channels)
	c.dirty = l.dirty
	c.lastRatio = l.lastRatio
	c.lastPosition = l.lastPosition
	copy(c.lastValue, l.lastValue)
	return c
}

func (l *linearConverter) Close() {}

func (l *linearConverter) Channels() int {
	return l.channels
}

func (l *linearConverter) SetRatio(ratio Ratio) error {
	return l.setRatio(ratio)
}
