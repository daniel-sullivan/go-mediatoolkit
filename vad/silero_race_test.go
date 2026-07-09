package vad

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/goldensignals"
)

// TestRaceSileroDetector extends the race suite (race_test.go) to the
// Silero engine: Process on one goroutine, every live setter and every
// reader hammered from others, under `go test -race`. No correctness
// assertions — the single-goroutine tests pin behaviour.
func TestRaceSileroDetector(t *testing.T) {
	det, err := NewSileroDetector(SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.35})
	require.NoError(t, err)

	var transitions atomic.Int64
	det.Events().Subscribe(func(ev SpeechEvent) { transitions.Add(1) })

	var sig []float64
	for _, s := range goldensignals.Signals() {
		if s.Name == "speech-pulses" {
			sig = make([]float64, 4*goldensignals.SampleRate)
			for i := range sig {
				sig[i] = float64(s.Samples[i])
			}
		}
	}
	require.NotNil(t, sig)

	var stop atomic.Bool
	var wg sync.WaitGroup

	// Audio goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			processChunks(det, sig, 512, 1)
			if i%4 == 3 {
				det.Reset()
			}
		}
	}()

	// Setter hammer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			_ = det.SetThreshold(0.3 + float64(i%5)*0.1)
			if i%3 == 0 {
				_ = det.SetNegThreshold(0.1 + float64(i%2)*0.05)
			} else if i%3 == 1 {
				_ = det.SetNegThreshold(0) // restore derivation
			}
			_ = det.SetMinSpeech(time.Duration(50+i%400) * time.Millisecond)
			_ = det.SetMinSilence(time.Duration(50+i%300) * time.Millisecond)
			_ = det.SetSpeechPad(time.Duration(1+i%100) * time.Millisecond)
		}
	}()

	// Reader hammers.
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				_ = det.Active()
				_ = det.Probability()
				_, _ = det.LastTransition()
				_ = det.DecisionLatency()
				_ = det.SampleRate()
				_ = det.Channels()
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}
