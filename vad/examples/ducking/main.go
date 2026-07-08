// Sidechain ducking example: the classic radio "music dips under the
// presenter" effect, wired through a real mixer. Two tracks play:
//
//   - a VOICE track whose timeline.EffectSource chain contains the
//     EnergyDetector (pass-through — it only listens), and
//   - a music BED track whose chain contains a Ducker READING that
//     detector.
//
// The ducker never feeds the detector; the goroutine-safe atomic
// readers are what make the cross-track read legal. Output is pulled
// headlessly via Mixer.Fill (no audio device), printing the ducker's
// gain as the voice comes and goes.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/mixer"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
)

func main() {
	const (
		sampleRate = consts.SampleRate16000
		channels   = 1
	)

	det, err := vad.NewEnergyDetector(vad.EnergyConfig{
		SampleRate: sampleRate,
		Channels:   channels,
	})
	if err != nil {
		log.Fatal(err)
	}
	duck, err := vad.NewDucker(vad.DuckerConfig{
		SampleRate: sampleRate,
		Channels:   channels,
		Detector:   det, // read-only sidechain; fed by the voice track below
	})
	if err != nil {
		log.Fatal(err)
	}

	// Voice: 1 s of silence, 2 s of "speech" (an A3 tone at -18 dB),
	// then 2 s of silence so the hangover and release play out.
	voice := generators.Sine(consts.FreqNoteA3, 2*time.Second, sampleRate)
	mutations.ApplyGain(voice.Data, mutations.Decibels(-18))
	silence := func(d time.Duration) []float64 {
		return make([]float64, int(mutations.DurationToFrames(d, sampleRate)))
	}
	voiceData := append(silence(time.Second), voice.Data...)
	voiceData = append(voiceData, silence(2*time.Second)...)
	voiceClip, err := timeline.LoadClipFromPCM(voiceData, sampleRate, channels)
	if err != nil {
		log.Fatal(err)
	}

	// Bed: 5 s of music stand-in (E2), quiet enough not to trip the
	// detector even if it WERE listening — but it isn't: only the
	// voice chain feeds det.
	bed := generators.Sine(consts.FreqNoteE2, 5*time.Second, sampleRate)
	mutations.ApplyGain(bed.Data, mutations.Decibels(-14))
	bedClip, err := timeline.LoadClipFromPCM(bed.Data, sampleRate, channels)
	if err != nil {
		log.Fatal(err)
	}

	mx, err := mixer.New(mixer.Config{SampleRate: sampleRate, Channels: channels})
	if err != nil {
		log.Fatal(err)
	}
	defer mx.Close()

	// The wiring: detector in the voice chain, ducker in the bed chain.
	if _, err := mx.AddSource(timeline.NewEffectSource(voiceClip.Playhead(), det)); err != nil {
		log.Fatal(err)
	}
	if _, err := mx.AddSource(timeline.NewEffectSource(bedClip.Playhead(), duck)); err != nil {
		log.Fatal(err)
	}

	// Pull headlessly in 100 ms steps, paced against the mixer's
	// real-time model, reading the ducker's gain from OUTSIDE the mix
	// goroutine — safe because GainDB (like Detector.Active) is an
	// atomic reader.
	const step = 100 * time.Millisecond
	buf := make([]float64, int(mutations.DurationToFrames(step, sampleRate))*channels)
	for i := 0; i < 50; i++ {
		mx.Fill(buf)
		time.Sleep(step)
		if i%5 == 4 {
			fmt.Printf("t=%3.1fs  voice speaking=%-5v  bed gain=%6.1f dB\n",
				(time.Duration(i+1) * step).Seconds(), det.Active(), duck.GainDB())
		}
	}
}
