package resample_test

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/resample"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamingEquivalence(t *testing.T) {
	types := []struct {
		ct   resample.ConverterType
		name string
		tol  float64
	}{
		{resample.ZeroOrderHold, "ZOH", 1e-10},
		{resample.Linear, "Linear", 1e-10},
		{resample.SincFastest, "SincFastest", 0.05},
	}

	for _, tt := range types {
		t.Run(tt.name, func(t *testing.T) {
			in := make([]float64, 1000)
			for i := range in {
				in[i] = math.Sin(2.0 * math.Pi * float64(i) / 100.0)
			}
			ratio := resample.Ratio{InputRate: 2, OutputRate: 3}

			oneShot, _ := resample.New(tt.ct, 1)
			defer oneShot.Close()
			oneShotOut := make([]float64, 2000)
			d := &resample.Data{DataIn: in, DataOut: oneShotOut, EndOfInput: true, Ratio: ratio}
			require.NoError(t, oneShot.Process(d))
			oneShotLen := d.OutputFramesGen

			stream, _ := resample.New(tt.ct, 1)
			defer stream.Close()
			var streamOut []float64
			chunkSize := 100
			for offset := 0; offset < len(in); offset += chunkSize {
				end := min(offset+chunkSize, len(in))
				outBuf := make([]float64, int(float64(end-offset)*ratio.Float64())+100)
				sd := &resample.Data{
					DataIn: in[offset:end], DataOut: outBuf,
					EndOfInput: end >= len(in), Ratio: ratio,
				}
				require.NoError(t, stream.Process(sd))
				streamOut = append(streamOut, outBuf[:sd.OutputFramesGen]...)
			}

			assert.InDelta(t, oneShotLen, len(streamOut), 5, "length mismatch")

			minLen := min(oneShotLen, len(streamOut))
			for i := 0; i < minLen; i++ {
				if !assert.InDelta(t, oneShotOut[i], streamOut[i], tt.tol, "sample %d", i) {
					break
				}
			}
		})
	}
}

func TestResetProducesSameOutput(t *testing.T) {
	types := []struct {
		ct   resample.ConverterType
		name string
	}{
		{resample.ZeroOrderHold, "ZOH"},
		{resample.Linear, "Linear"},
		{resample.SincFastest, "SincFastest"},
	}

	for _, tt := range types {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := resample.New(tt.ct, 1)
			defer c.Close()

			in := make([]float64, 500)
			for i := range in {
				in[i] = math.Sin(2.0 * math.Pi * float64(i) / 50.0)
			}

			out1 := make([]float64, 800)
			d := &resample.Data{DataIn: in, DataOut: out1, EndOfInput: true, Ratio: resample.Ratio{InputRate: 2, OutputRate: 3}}
			require.NoError(t, c.Process(d))
			len1 := d.OutputFramesGen

			c.Reset()
			out2 := make([]float64, 800)
			d = &resample.Data{DataIn: in, DataOut: out2, EndOfInput: true, Ratio: resample.Ratio{InputRate: 2, OutputRate: 3}}
			require.NoError(t, c.Process(d))

			require.Equal(t, len1, d.OutputFramesGen)
			assert.Equal(t, out1[:len1], out2[:len1])
		})
	}
}

func TestCloneIndependence(t *testing.T) {
	c, _ := resample.New(resample.Linear, 1)
	defer c.Close()

	in := make([]float64, 200)
	for i := range in {
		in[i] = float64(i)
	}

	out := make([]float64, 400)
	d := &resample.Data{DataIn: in[:100], DataOut: out, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
	require.NoError(t, c.Process(d))

	c2 := c.Clone()
	defer c2.Close()

	out1 := make([]float64, 400)
	d1 := &resample.Data{DataIn: in[100:], DataOut: out1, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}
	out2 := make([]float64, 400)
	d2 := &resample.Data{DataIn: in[100:], DataOut: out2, EndOfInput: true, Ratio: resample.Ratio{InputRate: 1, OutputRate: 2}}

	require.NoError(t, c.Process(d1))
	require.NoError(t, c2.Process(d2))

	require.Equal(t, d1.OutputFramesGen, d2.OutputFramesGen)
	assert.Equal(t, out1[:d1.OutputFramesGen], out2[:d2.OutputFramesGen])
}

func TestMultiChannelMatchesMono(t *testing.T) {
	types := []struct {
		ct   resample.ConverterType
		name string
	}{
		{resample.ZeroOrderHold, "ZOH"},
		{resample.Linear, "Linear"},
	}

	for _, tt := range types {
		t.Run(tt.name, func(t *testing.T) {
			monoL, _ := resample.New(tt.ct, 1)
			monoR, _ := resample.New(tt.ct, 1)
			stereo, _ := resample.New(tt.ct, 2)
			defer monoL.Close()
			defer monoR.Close()
			defer stereo.Close()

			frames := 200
			ratio := resample.Ratio{InputRate: 10, OutputRate: 17}
			inL := make([]float64, frames)
			inR := make([]float64, frames)
			inStereo := make([]float64, frames*2)
			for i := 0; i < frames; i++ {
				inL[i] = math.Sin(float64(i) * 0.1)
				inR[i] = math.Cos(float64(i) * 0.1)
				inStereo[i*2] = inL[i]
				inStereo[i*2+1] = inR[i]
			}

			outFrames := int(float64(frames)*ratio.Float64()) + 50
			outL := make([]float64, outFrames)
			outR := make([]float64, outFrames)
			outStereo := make([]float64, outFrames*2)

			dL := &resample.Data{DataIn: inL, DataOut: outL, EndOfInput: true, Ratio: ratio}
			dR := &resample.Data{DataIn: inR, DataOut: outR, EndOfInput: true, Ratio: ratio}
			dS := &resample.Data{DataIn: inStereo, DataOut: outStereo, EndOfInput: true, Ratio: ratio}

			monoL.Process(dL)
			monoR.Process(dR)
			stereo.Process(dS)

			assert.InDelta(t, dL.OutputFramesGen, dS.OutputFramesGen, 1, "frame count")

			minFrames := min(dL.OutputFramesGen, dS.OutputFramesGen)
			for i := 0; i < minFrames; i++ {
				if !assert.InDelta(t, outL[i], outStereo[i*2], 1e-10, "L frame %d", i) {
					break
				}
				if !assert.InDelta(t, outR[i], outStereo[i*2+1], 1e-10, "R frame %d", i) {
					break
				}
			}
		})
	}
}

func BenchmarkZOH(b *testing.B) {
	benchmarkConverter(b, resample.ZeroOrderHold, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}
func BenchmarkLinear(b *testing.B) {
	benchmarkConverter(b, resample.Linear, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}
func BenchmarkSincFastest(b *testing.B) {
	benchmarkConverter(b, resample.SincFastest, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}
func BenchmarkSincMedium(b *testing.B) {
	benchmarkConverter(b, resample.SincMediumQuality, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}
func BenchmarkSincBest(b *testing.B) {
	benchmarkConverter(b, resample.SincBestQuality, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}

func benchmarkConverter(b *testing.B, ct resample.ConverterType, channels int, ratio resample.Ratio, frames int) {
	b.Helper()
	c, err := resample.New(ct, channels)
	require.NoError(b, err)
	defer c.Close()

	in := make([]float64, frames*channels)
	for i := range in {
		in[i] = math.Sin(float64(i) * 0.01)
	}
	out := make([]float64, (int(float64(frames)*ratio.Float64())+100)*channels)

	b.ResetTimer()
	b.SetBytes(int64(frames * channels * 8))
	for i := 0; i < b.N; i++ {
		c.Reset()
		d := &resample.Data{DataIn: in, DataOut: out, EndOfInput: true, Ratio: ratio}
		require.NoError(b, c.Process(d))
	}
}
