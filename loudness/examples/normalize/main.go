// Normalize example: measure a synthesised clip, then normalise it to
// loudness.TargetPodcast (-16 LUFS) once in NormalizeClamp mode and
// once in NormalizeLimit mode, printing the gain applied and the
// before/after peaks so the two modes' trade-off is visible.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// buildClip synthesises a fresh, mildly peaky mono clip: a sustained
// tone under a short, much louder transient (like a quiet verse
// followed by a hit or a shout), so the naive gain-to-target and the
// ceiling genuinely compete.
func buildClip() mutations.Audio {
	const sampleRate = consts.SampleRate48000

	body := generators.Sine(consts.FreqNoteA4, 3*time.Second, sampleRate)
	mutations.ApplyGain(body.Data, mutations.Decibels(-28))

	hit := generators.Sine(consts.FreqNoteA4, 200*time.Millisecond, sampleRate)
	mutations.ApplyGain(hit.Data, mutations.Decibels(-3))

	return mutations.Audio{
		SampleRate: sampleRate,
		Channels:   1,
		Data:       append(append([]float64{}, body.Data...), hit.Data...),
	}
}

func run(mode loudness.NormalizeMode, label string) {
	clip := buildClip()

	res, err := loudness.Normalize(clip, loudness.NormalizeOptions{
		Target: loudness.TargetPodcast,
		Mode:   mode,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("--- %s ---\n", label)
	fmt.Printf("gain applied:     %.2f dB\n", res.GainDB)
	fmt.Printf("clamped/limited:  clamped=%v limited=%v\n", res.Clamped, res.Limited)
	fmt.Printf("before: I=%.1f LUFS  TP=%.2f dBTP\n", res.Input.Integrated, res.Input.TruePeak)
	fmt.Printf("after:  I=%.1f LUFS  TP=%.2f dBTP\n", res.Output.Integrated, res.Output.TruePeak)
}

func main() {
	run(loudness.NormalizeClamp, "NormalizeClamp")
	run(loudness.NormalizeLimit, "NormalizeLimit")
}
