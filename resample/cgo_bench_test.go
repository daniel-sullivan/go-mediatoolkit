//go:build cgo

// CGo-side benchmarks for the libsamplerate-backed Converter. Pair with
// the native benchmarks in streaming_test.go so the README can show a
// like-for-like Go vs C comparison without a custom build tag.

package resample_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/resample"
)

func BenchmarkZOH_C(b *testing.B) {
	benchmarkLibsamplerate(b, resample.ZeroOrderHold, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}
func BenchmarkLinear_C(b *testing.B) {
	benchmarkLibsamplerate(b, resample.Linear, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}
func BenchmarkSincFastest_C(b *testing.B) {
	benchmarkLibsamplerate(b, resample.SincFastest, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}
func BenchmarkSincMedium_C(b *testing.B) {
	benchmarkLibsamplerate(b, resample.SincMediumQuality, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}
func BenchmarkSincBest_C(b *testing.B) {
	benchmarkLibsamplerate(b, resample.SincBestQuality, 1, resample.Ratio{InputRate: 1, OutputRate: 2}, 48000)
}

func benchmarkLibsamplerate(b *testing.B, ct resample.ConverterType, channels int, ratio resample.Ratio, frames int) {
	b.Helper()
	c, err := resample.NewLibsamplerate(ct, channels)
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
