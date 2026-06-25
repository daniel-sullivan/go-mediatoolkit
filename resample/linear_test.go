package resample_test

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/resample"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLinearIdentity(t *testing.T) {
	c, _ := resample.New(resample.Linear, 1)
	defer c.Close()

	in := make([]float64, 256)
	for i := range in {
		in[i] = float64(i) / 256.0
	}
	out := make([]float64, 300)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 1}}
	require.NoError(t, c.Process(d))
	assert.Equal(t, 256, d.InputFramesUsed)
	assert.GreaterOrEqual(t, d.OutputFramesGen, 250)

	for i := 2; i < d.OutputFramesGen-1 && i < len(in); i++ {
		if !assert.InDelta(t, in[i-1], out[i], 1e-10, "sample %d", i) {
			break
		}
	}
}

func TestLinearUpsample(t *testing.T) {
	c, _ := resample.New(resample.Linear, 1)
	defer c.Close()

	in := make([]float64, 100)
	for i := range in {
		in[i] = float64(i)
	}
	out := make([]float64, 250)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
	require.NoError(t, c.Process(d))
	assert.InDelta(t, 200, d.OutputFramesGen, 20)

	for i := 1; i < d.OutputFramesGen; i++ {
		if !assert.GreaterOrEqual(t, out[i], out[i-1]-1e-10, "monotonic at %d", i) {
			break
		}
	}
}

func TestLinearDownsample(t *testing.T) {
	c, _ := resample.New(resample.Linear, 1)
	defer c.Close()

	in := make([]float64, 200)
	for i := range in {
		in[i] = float64(i)
	}
	out := make([]float64, 150)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 2, OutputRate: 1}}
	require.NoError(t, c.Process(d))
	assert.InDelta(t, 100, d.OutputFramesGen, 20)
}

func TestLinearDCPreservation(t *testing.T) {
	c, _ := resample.New(resample.Linear, 1)
	defer c.Close()

	in := make([]float64, 500)
	for i := range in {
		in[i] = 0.75
	}
	out := make([]float64, 1100)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
	require.NoError(t, c.Process(d))

	for i := 0; i < d.OutputFramesGen; i++ {
		if !assert.InDelta(t, 0.75, out[i], 1e-10, "sample %d", i) {
			break
		}
	}
}

func TestLinearStereo(t *testing.T) {
	c, _ := resample.New(resample.Linear, 2)
	defer c.Close()

	in := make([]float64, 200)
	for i := 0; i < len(in); i += 2 {
		in[i] = 1.0
		in[i+1] = -1.0
	}
	out := make([]float64, 400)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
	require.NoError(t, c.Process(d))

	for i := 0; i < d.OutputFramesGen*2; i += 2 {
		if !assert.InDelta(t, 1.0, out[i], 0.01, "L[%d]", i) {
			break
		}
		if !assert.InDelta(t, -1.0, out[i+1], 0.01, "R[%d]", i+1) {
			break
		}
	}
}
