package vad

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The race suite drives Process on one goroutine while other
// goroutines hammer every live setter and every reader, under
// `go test -race`. There are no correctness assertions beyond "does
// not race / does not panic" — behavioural correctness is pinned by
// the single-goroutine tests.

func TestRaceEnergyDetector(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)

	// A subscriber that reads state from inside the callback (runs on
	// the audio goroutine, but exercises bus vs setter interleaving).
	var transitions atomic.Int64
	det.Events().Subscribe(func(ev SpeechEvent) { transitions.Add(1) })

	var sig []float64
	sig = appendSilence(sig, rate/4, 1)
	sig = appendTone(sig, 1000, ampForDBFS(-23), rate/4, rate, 1)

	var stop atomic.Bool
	var wg sync.WaitGroup

	// Audio goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			processChunks(det, sig, 160, 1)
			if i%8 == 7 {
				det.Reset()
			}
		}
	}()

	// Setter hammers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			_ = det.SetThresholdsDB(float64(4+i%6), 2)
			_ = det.SetAbsoluteFloorDB(-50 - float64(i%20))
			_ = det.SetOnset(time.Duration(10+i%40) * time.Millisecond)
			_ = det.SetHangover(time.Duration(100+i%200) * time.Millisecond)
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

func TestRaceWebRTCDetector(t *testing.T) {
	const rate = 16000
	det, err := NewWebRTCDetector(WebRTCConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)

	var transitions atomic.Int64
	det.Events().Subscribe(func(ev SpeechEvent) { transitions.Add(1) })

	var sig []float64
	sig = appendSilence(sig, rate/4, 1)
	sig = appendSpeechLike(sig, rate/2, rate, 1)

	var stop atomic.Bool
	var wg sync.WaitGroup

	// Audio goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			processChunks(det, sig, 160, 1)
			if i%8 == 7 {
				det.Reset()
			}
		}
	}()

	// Setter hammers (SetMode races the audio goroutine's per-frame
	// mode application — the exact path the appliedMode handoff guards).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			_ = det.SetMode(i % 4)
			_ = det.SetOnset(time.Duration(10+i%60) * time.Millisecond)
			_ = det.SetHangover(time.Duration(100+i%300) * time.Millisecond)
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

func TestRaceGate(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	g, err := NewGate(GateConfig{
		SampleRate: rate, Channels: 1, Detector: det,
		Lookahead: 5 * time.Millisecond,
	})
	require.NoError(t, err)

	var sig []float64
	sig = appendSilence(sig, rate/4, 1)
	sig = appendTone(sig, 1000, ampForDBFS(-23), rate/4, rate, 1)

	var stop atomic.Bool
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]float64, 160)
		for off := 0; !stop.Load(); off = (off + 160) % len(sig) {
			copy(buf, sig[off:off+160])
			g.Process(buf)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			_ = g.SetFloorDB(-float64(20 + i%40))
			_ = g.SetAttack(time.Duration(1+i%10) * time.Millisecond)
			_ = g.SetRelease(time.Duration(50+i%100) * time.Millisecond)
			_ = g.GainDB()
			_ = g.Latency()
			_ = g.LatencyFrames()
		}
	}()

	time.Sleep(300 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}

func TestRaceDucker(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	d, err := NewDucker(DuckerConfig{SampleRate: rate, Channels: 1, Detector: det})
	require.NoError(t, err)

	var voice []float64
	voice = appendSilence(voice, rate/4, 1)
	voice = appendTone(voice, 1000, ampForDBFS(-23), rate/4, rate, 1)

	var stop atomic.Bool
	var wg sync.WaitGroup

	// The documented cross-track topology under race: one goroutine
	// feeds the detector (voice chain), a DIFFERENT goroutine runs the
	// ducker (bed chain) reading it, a third hammers setters.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for off := 0; !stop.Load(); off = (off + 160) % len(voice) {
			det.Process(voice[off : off+160])
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		bed := make([]float64, 160)
		for !stop.Load() {
			for i := range bed {
				bed[i] = 1
			}
			d.Process(bed)
			_ = d.GainDB()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			_ = d.SetDepthDB(-float64(6 + i%18))
			_ = d.SetAttack(time.Duration(10+i%50) * time.Millisecond)
			_ = d.SetRelease(time.Duration(100+i%400) * time.Millisecond)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}
