//go:build cgo

package benchcmp

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/r128"
)

// Raw ebur128 mode bits (ebur128.h).
const (
	modeM          = 1 << 0
	modeI          = (1 << 2) | modeM // 5
	modeSamplePeak = (1 << 4) | modeM
	modeTruePeak   = (1 << 5) | modeM | modeSamplePeak // 49
)

const (
	benchRate     = 48000
	benchChannels = 2
	benchSeconds  = 60
)

// benchSignal builds the shared 60 s stereo 48 kHz workload once.
func benchSignal() []float64 {
	rng := rand.New(rand.NewPCG(0xB0A7, 0x1770))
	frames := benchRate * benchSeconds
	out := make([]float64, frames*benchChannels)
	for n := 0; n < frames; n++ {
		l := 0.5 * math.Sin(2*math.Pi*440*float64(n)/benchRate)
		r := 0.4*math.Sin(2*math.Pi*554*float64(n)/benchRate) + 0.05*(rng.Float64()*2-1)
		out[n*benchChannels+0] = l
		out[n*benchChannels+1] = r
	}
	return out
}

// goBenchRun mirrors the C bench_run: feed all frames, read integrated (and
// true peak if enabled).
func goBenchRun(mode int, data []float64) float64 {
	st := r128.NewState(benchChannels, benchRate, mode)
	st.AddFrames(data)
	var out float64
	if mode&modeI == modeI {
		out, _ = st.GatedLoudness()
	}
	var tp float64
	if mode&modeTruePeak == modeTruePeak {
		tp, _ = st.TruePeak(0)
	}
	return out + tp
}

var sink float64

func BenchmarkIntegratedGo(b *testing.B) {
	data := benchSignal()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink += goBenchRun(modeI, data)
	}
}

func BenchmarkIntegratedC(b *testing.B) {
	data := benchSignal()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink += cBenchRun(benchChannels, benchRate, modeI, data)
	}
}

func BenchmarkTruePeakGo(b *testing.B) {
	data := benchSignal()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink += goBenchRun(modeI|modeTruePeak, data)
	}
}

func BenchmarkTruePeakC(b *testing.B) {
	data := benchSignal()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink += cBenchRun(benchChannels, benchRate, modeI|modeTruePeak, data)
	}
}
