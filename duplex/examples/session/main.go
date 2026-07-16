// Command session demonstrates a headless full-duplex voice session:
// a duplex.Engine plays far-end pink noise (standing in for streamed
// synthesis) while its capture side receives a delayed, attenuated
// copy of that output — the acoustic echo — plus, partway through, a
// genuine near-end tone standing in for the user speaking. The
// engine's echo canceller converges so the echo never registers as
// speech; the tone does, and its utterance streams out as events.
//
// Fully headless: no audio device is opened; the engine is driven by
// its real 10ms ticker.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/duplex"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	const sampleRate = 16000
	const duration = 8 * time.Second
	const echoDelayFrames = 6 // 60ms acoustic path
	const echoGain = 0.5      // −6 dB reflection
	const speechFrom = 6 * time.Second
	const speechDur = time.Second

	det, err := vad.NewEnergyDetector(vad.EnergyConfig{
		SampleRate:      sampleRate,
		Channels:        1,
		AbsoluteFloorDB: -30, // converged residual echo sits below; the near-end tone above
		FloorRise:       time.Second,
	})
	if err != nil {
		return err
	}

	e, err := duplex.New(duplex.Config{
		SampleRate: sampleRate,
		Channels:   1,
		Detector:   det,
		AEC:        new(duplex.AECConfig),
		PreRoll:    300 * time.Millisecond,
	})
	if err != nil {
		return err
	}
	e.SetAudioBufferDelay(echoDelayFrames * 10 * time.Millisecond)

	// The capture side loops the engine's own output back, delayed
	// and attenuated — the echo a speakerphone microphone hears —
	// over a ~−45 dBFS mic noise floor (a real microphone always has
	// one; the energy detector's adaptive floor anchors to it). The
	// output callback runs on the engine's audio goroutine, and Push
	// is safe from any goroutine, so it can feed capture directly.
	var played [][]float64
	var toneSample int
	rng := rand.New(rand.NewSource(2))
	e.SetOutput(func(frame []float64, seq int64) {
		played = append(played, append([]float64(nil), frame...))

		capture := make([]float64, len(frame))
		for i := range capture {
			capture[i] = (rng.Float64()*2 - 1) * 0.01
		}
		if len(played) > echoDelayFrames {
			for i, s := range played[len(played)-1-echoDelayFrames] {
				capture[i] += s * echoGain
			}
		}
		if at := time.Duration(seq) * 10 * time.Millisecond; at >= speechFrom && at < speechFrom+speechDur {
			for i := range capture {
				capture[i] += 0.3 * math.Sin(2*math.Pi*440*float64(toneSample)/sampleRate)
				toneSample++
			}
		}
		if err := e.Push(capture, seq*10); err != nil {
			log.Printf("push: %v", err)
		}
	})

	// Far-end audio: pink noise fed in two chunks with a marked seam,
	// as a streaming synthesizer would deliver it.
	far := generators.PinkNoise(duration, sampleRate, 1)
	for i := range far.Data {
		far.Data[i] *= 0.3
	}
	half := len(far.Data) / 2
	if err := e.FeedChunk(far.Data[:half]); err != nil {
		return err
	}
	e.MarkChunkBoundary()
	if err := e.FeedChunk(far.Data[half:]); err != nil {
		return err
	}

	if err := e.Start(context.Background()); err != nil {
		return err
	}

	go func() {
		time.Sleep(duration)
		e.Stop()
	}()

	var speechFrames int
	for ev := range e.Events() {
		switch ev := ev.(type) {
		case duplex.SpeechStart:
			fmt.Printf("speech start at tag %dms (echo-only playout never triggered one)\n", ev.Timestamp)
		case duplex.AudioFrame:
			speechFrames++
		case duplex.SpeechStop:
			fmt.Printf("speech stop at tag %dms after %d frames (%dms of utterance audio)\n",
				ev.Tag, speechFrames, speechFrames*10)
		}
	}

	m := e.Metrics()
	fmt.Printf("metrics: ERL=%.1fdB ERLE=%.1fdB delay=%dms rendered=%d frames\n",
		m.AEC.EchoReturnLoss, m.AEC.EchoReturnLossEnhancement, m.AEC.DelayMs, m.RenderedFrames)
	return nil
}
