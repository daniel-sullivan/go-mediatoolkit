package resample_test

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/resample"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewValidConverters(t *testing.T) {
	types := []resample.ConverterType{
		resample.SincBestQuality,
		resample.SincMediumQuality,
		resample.SincFastest,
		resample.ZeroOrderHold,
		resample.Linear,
	}
	for _, ct := range types {
		c, err := resample.New(ct, 1)
		require.NoError(t, err, "New(%v, 1)", ct)
		assert.Equal(t, 1, c.Channels(), "New(%v, 1).Channels()", ct)
		c.Close()
	}
}

func TestNewInvalidChannels(t *testing.T) {
	_, err := resample.New(resample.Linear, 0)
	assert.ErrorIs(t, err, resample.ErrBadChannelCount)
	_, err = resample.New(resample.Linear, -1)
	assert.ErrorIs(t, err, resample.ErrBadChannelCount)
}

func TestNewInvalidConverterType(t *testing.T) {
	_, err := resample.New(resample.ConverterType(99), 1)
	assert.ErrorIs(t, err, resample.ErrBadConverterType)
}

func TestProcessNilData(t *testing.T) {
	c, _ := resample.New(resample.Linear, 1)
	defer c.Close()
	assert.ErrorIs(t, c.Process(nil), resample.ErrBadData)
}

func TestProcessBadRatio(t *testing.T) {
	c, _ := resample.New(resample.Linear, 1)
	defer c.Close()

	for _, r := range []resample.Ratio{
		{InputRate: 0, OutputRate: 0},
		{InputRate: -1, OutputRate: 1},
		{InputRate: 1, OutputRate: 257},
		{InputRate: 0, OutputRate: 1},
		{InputRate: 1, OutputRate: 0},
	} {
		d := &resample.Data{
			DataIn: make([]float64, 100), DataOut: make([]float64, 100), Ratio: r,
		}
		assert.ErrorIs(t, c.Process(d), resample.ErrBadSrcRatio, "ratio=%v", r)
	}
}

func TestIsValidRatio(t *testing.T) {
	assert.True(t, resample.IsValidRatio(resample.Ratio{InputRate: 1, OutputRate: 1}))
	assert.True(t, resample.IsValidRatio(resample.Ratio{InputRate: 2, OutputRate: 1}))
	assert.True(t, resample.IsValidRatio(resample.Ratio{InputRate: 1, OutputRate: 256}))
	assert.True(t, resample.IsValidRatio(resample.Ratio{InputRate: 256, OutputRate: 1}))
	assert.False(t, resample.IsValidRatio(resample.Ratio{InputRate: 0, OutputRate: 0}))
	assert.False(t, resample.IsValidRatio(resample.Ratio{InputRate: -1, OutputRate: 1}))
	assert.False(t, resample.IsValidRatio(resample.Ratio{InputRate: 1, OutputRate: 257}))
}

func TestConverterTypeString(t *testing.T) {
	assert.Equal(t, "Linear Interpolator", resample.Linear.String())
	assert.Equal(t, "Best Quality Sinc Interpolator", resample.SincBestQuality.String())
}

func TestSetRatio(t *testing.T) {
	c, _ := resample.New(resample.Linear, 1)
	defer c.Close()
	assert.NoError(t, c.SetRatio(resample.Ratio{InputRate: 1, OutputRate: 2}))
	assert.ErrorIs(t, c.SetRatio(resample.Ratio{InputRate: 0, OutputRate: 0}), resample.ErrBadSrcRatio)
}

func TestSimple(t *testing.T) {
	in := make([]float64, 1000)
	for i := range in {
		in[i] = 1.0
	}

	out, err := resample.Simple(in, resample.Linear, 1, resample.Ratio{InputRate: 1, OutputRate: 2})
	require.NoError(t, err)
	assert.InDelta(t, 2000, len(out), 200, "output length")

	for i, v := range out {
		if !assert.InDelta(t, 1.0, v, 0.01, "DC at sample %d", i) {
			break
		}
	}
}

func TestSimpleErrors(t *testing.T) {
	_, err := resample.Simple(nil, resample.Linear, 0, resample.Ratio{InputRate: 1, OutputRate: 1})
	assert.ErrorIs(t, err, resample.ErrBadChannelCount)
	_, err = resample.Simple(nil, resample.Linear, 1, resample.Ratio{InputRate: 0, OutputRate: 0})
	assert.ErrorIs(t, err, resample.ErrBadSrcRatio)
}

func TestProcessZeroInput(t *testing.T) {
	for _, ct := range []resample.ConverterType{resample.ZeroOrderHold, resample.Linear, resample.SincFastest} {
		c, _ := resample.New(ct, 1)
		d := &resample.Data{DataOut: make([]float64, 100), Ratio: resample.Ratio{InputRate: 1, OutputRate: 1}}
		assert.NoError(t, c.Process(d), "%v", ct)
		assert.Equal(t, 0, d.OutputFramesGen, "%v", ct)
		c.Close()
	}
}
