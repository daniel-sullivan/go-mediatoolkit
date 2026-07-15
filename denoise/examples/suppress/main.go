// Noise-suppression example: run the RNNoise engine over a synthesised
// noisy signal and report how much broadband noise it removes, plus the
// per-frame voice-activity probability RNNoise exposes for free.
//
// Everything is generated and processed offline, so this runs headlessly
// with no audio device. RNNoise is a 48 kHz fullband mono denoiser; the
// engine resamples internally for other rates (see denoise/README.md).
package main

import (
	"flag"
	"fmt"
	"log"
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise"
)

func main() {
	rate := flag.Int("rate", 48000, "sample rate in Hz")
	flag.Parse()

	eng, err := denoise.NewRNNoise(denoise.RNNoiseConfig{SampleRate: *rate})
	if err != nil {
		log.Fatal(err)
	}

	// 2 s of a voiced, formant-like tone complex plus additive white
	// noise. A deterministic LCG keeps the run reproducible.
	n := *rate * 2
	buf := make([]float64, n)
	var seed uint64 = 0x1234567
	for i := range buf {
		t := float64(i) / float64(*rate)
		voice := 0.30*math.Sin(2*math.Pi*160*t) +
			0.15*math.Sin(2*math.Pi*320*t) +
			0.08*math.Sin(2*math.Pi*640*t)
		seed = seed*6364136223846793005 + 1
		noise := ((float64(seed>>40)/float64(1<<24))*2 - 1) * 0.15
		buf[i] = voice + noise
	}
	inRMS := rms(buf)

	// Process mutates the buffer in place; output lags input by Latency.
	eng.Process(buf)
	outRMS := rms(buf)

	fmt.Printf("rate:              %d Hz\n", eng.SampleRate())
	fmt.Printf("engine latency:    %v\n", eng.Latency())
	fmt.Printf("input RMS:         %.4f\n", inRMS)
	fmt.Printf("output RMS:        %.4f\n", outRMS)
	fmt.Printf("VAD probability:   %.3f (last frame)\n", eng.Probability())
}

func rms(x []float64) float64 {
	var s float64
	for _, v := range x {
		s += v * v
	}
	return math.Sqrt(s / float64(len(x)))
}
