package resample_test

import (
	"math"
	"runtime"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/resample"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParallelMatchesSequential verifies the parallel path produces the same
// output as sequential by comparing GOMAXPROCS=1 vs default.
func TestParallelMatchesSequential(t *testing.T) {
	types := []struct {
		ct   resample.ConverterType
		name string
	}{
		{resample.SincFastest, "Fastest"},
		{resample.SincMediumQuality, "Medium"},
		{resample.SincBestQuality, "Best"},
	}

	channelCounts := []int{1, 2, 6}
	ratios := []resample.Ratio{
		{InputRate: 2, OutputRate: 1},
		{InputRate: 1, OutputRate: 1},
		{InputRate: 1, OutputRate: 2},
		{InputRate: 10, OutputRate: 37},
	}

	for _, tt := range types {
		for _, ch := range channelCounts {
			for _, ratio := range ratios {
				t.Run(tt.name, func(t *testing.T) {
					frames := 2000
					in := make([]float64, frames*ch)
					for i := range in {
						in[i] = math.Sin(float64(i) * 0.037)
					}
					outFrames := int(float64(frames)*ratio.Float64()) + 100

					// Force sequential via GOMAXPROCS(1).
					prev := runtime.GOMAXPROCS(1)
					seq, _ := resample.New(tt.ct, ch)
					seqOut := make([]float64, outFrames*ch)
					dSeq := &resample.Data{DataIn: in, DataOut: seqOut, EndOfInput: true, Ratio: ratio}
					require.NoError(t, seq.Process(dSeq))
					seq.Close()
					runtime.GOMAXPROCS(prev)

					// Parallel path with default GOMAXPROCS.
					par, _ := resample.New(tt.ct, ch)
					parOut := make([]float64, outFrames*ch)
					dPar := &resample.Data{DataIn: in, DataOut: parOut, EndOfInput: true, Ratio: ratio}
					require.NoError(t, par.Process(dPar))
					par.Close()

					require.Equal(t, dSeq.OutputFramesGen, dPar.OutputFramesGen, "frame count")
					require.Equal(t, dSeq.InputFramesUsed, dPar.InputFramesUsed, "input used")
					assert.Equal(t, seqOut[:dSeq.OutputFramesGen*ch], parOut[:dPar.OutputFramesGen*ch])
				})
			}
		}
	}
}

func TestParallelLargeBuffer(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("need GOMAXPROCS >= 2")
	}

	c, _ := resample.New(resample.SincFastest, 2)
	defer c.Close()

	frames := 50000
	in := make([]float64, frames*2)
	for i := range in {
		in[i] = math.Sin(float64(i) * 0.01)
	}
	out := make([]float64, (frames*2+1000)*2)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
	require.NoError(t, c.Process(d))
	assert.Greater(t, d.OutputFramesGen, frames*2-1000)

	for i := 0; i < d.OutputFramesGen*2; i++ {
		require.False(t, math.IsNaN(out[i]) || math.IsInf(out[i], 0), "sample %d is NaN/Inf", i)
	}
}

func TestParallelSmallBufferFallsBack(t *testing.T) {
	c, _ := resample.New(resample.SincFastest, 1)
	defer c.Close()

	in := make([]float64, 100)
	for i := range in {
		in[i] = float64(i)
	}
	out := make([]float64, 250)

	d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
	require.NoError(t, c.Process(d))
	assert.InDelta(t, 200, d.OutputFramesGen, 50)
}
