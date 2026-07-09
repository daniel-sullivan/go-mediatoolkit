// Command duplex demonstrates aec.Canceller wired to a live
// mixer.Mixer output as the far-end (render) reference, rather than a
// bare in-memory slice as in the cancel example. A pink-noise track
// plays through the mixer standing in for whatever the loudspeaker is
// rendering; a synthetic "microphone" signal is built by re-injecting
// that same mixer output with a fixed delay and attenuation (the
// acoustic echo path) plus a separate, uncorrelated near-end signal
// standing in for what the local speaker actually says. Canceller
// removes the echo in place, leaving the near-end signal close to its
// original level.
//
// Fully headless: no audio device is opened.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/aec"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness"
	"github.com/daniel-sullivan/go-mediatoolkit/mixer"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	const sampleRate = 16000
	const delayFrames = 240 // 15ms synthetic acoustic echo path
	const echoGain = 0.6
	const nearEndGain = 0.5
	const duration = 6 * time.Second
	const nearEndStart = 4 * time.Second // let the filter converge echo-only first

	// The far-end reference: broadband pink noise standing in for
	// whatever the mixer is rendering to the loudspeaker (music,
	// a remote call's audio, ...). Broadband, not a tone — see
	// canceller_test.go's TestCanceller_EchoReduction doc comment for
	// why AEC3's delay estimator needs that.
	farClip, err := timeline.LoadClipFromAudio(generators.PinkNoise(duration, sampleRate, 1))
	if err != nil {
		return err
	}
	mx, err := mixer.New(mixer.Config{SampleRate: sampleRate, Channels: 1})
	if err != nil {
		return err
	}
	defer mx.Close()
	if _, err := mx.AddSource(farClip.Playhead()); err != nil {
		return err
	}

	// Drain the mixer's live output into farBuf, paced at roughly each
	// pull's real duration: the mix goroutine only produces into its
	// bounded ring (~200ms by default) as fast as an unpaced consumer
	// drains it, so pulling faster than real time starves the ring and
	// reports spurious underrun silence instead of the track's audio —
	// see loudness/examples/live_leveller's identical pacing note.
	n := int(duration.Seconds() * sampleRate)
	farBuf := make([]float64, n)
	const pullChunk = sampleRate / 10 // 100ms
	const pullDur = 100 * time.Millisecond
	for i := 0; i+pullChunk <= n; i += pullChunk {
		mx.Fill(farBuf[i : i+pullChunk])
		time.Sleep(pullDur)
	}

	// Near-end speech: uncorrelated with the far-end/mixer path
	// entirely, so an ideal canceller passes it through untouched. It
	// only starts at nearEndStart, giving the adaptive filter a clean
	// echo-only run-in to converge on the acoustic path first — the
	// same convergence window canceller_test.go's echo-reduction test
	// relies on — before the double-talk period begins.
	nearEndFrom := int(nearEndStart.Seconds() * sampleRate)
	nearEnd := generators.WhiteNoise(duration, sampleRate, 2)
	for i := range nearEnd.Data {
		if i < nearEndFrom {
			nearEnd.Data[i] = 0
			continue
		}
		nearEnd.Data[i] *= nearEndGain
	}

	// Synthetic "microphone" capture: the far-end signal reflected back
	// delayed and attenuated (the echo), plus the near-end speech.
	mic := make([]float64, n)
	for i := 0; i < n; i++ {
		var echo float64
		if i >= delayFrames {
			echo = echoGain * farBuf[i-delayFrames]
		}
		mic[i] = echo + nearEnd.Data[i]
	}
	micOrig := append([]float64(nil), mic...)

	c, err := aec.NewCanceller(aec.CancellerConfig{SampleRate: sampleRate, CaptureChannels: 1, RenderChannels: 1})
	if err != nil {
		return err
	}
	const chunk = sampleRate / 100 // one 10ms AEC3 frame
	for i := 0; i+chunk <= n; i += chunk {
		if err := c.FeedFarEnd(farBuf[i : i+chunk]); err != nil {
			return err
		}
		c.Process(mic[i : i+chunk])
	}

	// Two comparison windows over the converged tail:
	//  - echoTail: one second of echo-only signal, just before the
	//    near-end talker starts — shows the echo removed.
	//  - talkTail: the last second, where near-end and echo overlap
	//    (double talk) — shows the near-end preserved despite the echo
	//    still running underneath it.
	tail := sampleRate
	echoTail := micOrig[nearEndFrom-tail : nearEndFrom]
	echoTailAfter := mic[nearEndFrom-tail : nearEndFrom]
	talkTail := micOrig[n-tail:]
	talkTailAfter := mic[n-tail:]
	nearOnlyTail := nearEnd.Data[n-tail:]

	echoBeforeDB := mutations.AmplitudeToDecibels(loudness.RMS(echoTail))
	echoAfterDB := mutations.AmplitudeToDecibels(loudness.RMS(echoTailAfter))
	talkBeforeDB := mutations.AmplitudeToDecibels(loudness.RMS(talkTail))
	talkAfterDB := mutations.AmplitudeToDecibels(loudness.RMS(talkTailAfter))
	nearOnlyDB := mutations.AmplitudeToDecibels(loudness.RMS(nearOnlyTail))

	m := c.Metrics()
	fmt.Printf("echo-only window:  before=%.1f dBFS after=%.1f dBFS (echo removed: %.1f dB)\n", echoBeforeDB, echoAfterDB, echoBeforeDB-echoAfterDB)
	fmt.Printf("double-talk window: before=%.1f dBFS after=%.1f dBFS near-end-alone=%.1f dBFS (near-end preserved)\n", talkBeforeDB, talkAfterDB, nearOnlyDB)
	fmt.Printf("metrics: delay=%dms ERLE=%.1fdB clockdrift=%v\n", m.DelayMs, m.EchoReturnLossEnhancement, m.Clockdrift)
	return nil
}
