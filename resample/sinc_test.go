package resample_test

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/resample"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSincFastestIdentity(t *testing.T) {
	c, err := resample.New(resample.SincFastest, 1)
	require.NoError(t, err)
	defer c.Close()

	in := make([]float64, 512)
	for i := range in {
		in[i] = float64(i) / 512.0
	}
	out := make([]float64, 600)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 1}}
	require.NoError(t, c.Process(d))
	assert.Equal(t, 512, d.InputFramesUsed)

	margin := 20
	for i := margin; i < d.OutputFramesGen-margin && i < len(in)-margin; i++ {
		if !assert.InDelta(t, in[i], out[i], 0.02, "sample %d", i) {
			break
		}
	}
}

func TestSincDCPreservation(t *testing.T) {
	types := []struct {
		ct   resample.ConverterType
		name string
	}{
		{resample.SincFastest, "Fastest"},
		{resample.SincMediumQuality, "Medium"},
		{resample.SincBestQuality, "Best"},
	}

	for _, tt := range types {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := resample.New(tt.ct, 1)
			defer c.Close()

			in := make([]float64, 2000)
			for i := range in {
				in[i] = 1.0
			}
			out := make([]float64, 4500)

			d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
			require.NoError(t, c.Process(d))

			margin := max(d.OutputFramesGen/10, 50)
			for i := margin; i < d.OutputFramesGen-margin; i++ {
				if !assert.InDelta(t, 1.0, out[i], 0.01, "sample %d", i) {
					break
				}
			}
		})
	}
}

func TestSincUpsample(t *testing.T) {
	c, _ := resample.New(resample.SincFastest, 1)
	defer c.Close()

	in := make([]float64, 500)
	for i := range in {
		in[i] = math.Sin(2.0 * math.Pi * float64(i) / 50.0)
	}
	out := make([]float64, 1200)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
	require.NoError(t, c.Process(d))
	assert.InDelta(t, 1000, d.OutputFramesGen, 100)
}

func TestSincDownsample(t *testing.T) {
	c, _ := resample.New(resample.SincFastest, 1)
	defer c.Close()

	in := make([]float64, 1000)
	for i := range in {
		in[i] = math.Sin(2.0 * math.Pi * float64(i) / 100.0)
	}
	out := make([]float64, 600)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 2, OutputRate: 1}}
	require.NoError(t, c.Process(d))
	assert.InDelta(t, 500, d.OutputFramesGen, 100)
}

func TestSincStereo(t *testing.T) {
	c, _ := resample.New(resample.SincFastest, 2)
	defer c.Close()

	in := make([]float64, 2000)
	for i := 0; i < len(in); i += 2 {
		in[i] = 1.0
		in[i+1] = -1.0
	}
	out := make([]float64, 4500)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
	require.NoError(t, c.Process(d))

	margin := d.OutputFramesGen / 10
	for i := margin; i < d.OutputFramesGen-margin; i++ {
		if !assert.InDelta(t, 1.0, out[i*2], 0.02, "L frame %d", i) {
			break
		}
		if !assert.InDelta(t, -1.0, out[i*2+1], 0.02, "R frame %d", i) {
			break
		}
	}
}
