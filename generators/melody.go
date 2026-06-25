package generators

import (
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// MelodyNote is one entry in a Melody sequence. Freq <= 0 marks a rest
// and produces silence for Duration; otherwise the note is rendered
// with the same ADSR envelope as Note.
type MelodyNote struct {
	Freq     float64
	Duration time.Duration
}

// Melody renders a sequence of notes (and rests) end-to-end into a
// single mono Audio buffer. Each note's ADSR release reaches zero
// before the next begins, so notes concatenate seam-free without any
// per-boundary crossfade. Returns Audio whose duration is the sum of
// the note durations.
func Melody(notes []MelodyNote, sampleRate int) mutations.Audio {
	total := 0
	frames := make([]int, len(notes))
	for i, n := range notes {
		f := int(n.Duration.Seconds() * float64(sampleRate))
		frames[i] = f
		total += f
	}
	data := make([]float64, total)
	cursor := 0
	for i, n := range notes {
		f := frames[i]
		if n.Freq > 0 && f > 0 {
			renderNote(data[cursor:cursor+f], n.Freq, sampleRate)
		}
		cursor += f
	}
	return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
}

// MaryHadALittleLamb returns the melody of "Mary Had a Little Lamb" in
// C major at 120 BPM, rendered as a single Audio buffer. Useful as a
// backing track in examples where you want a recognisable tune rather
// than a static drone.
func MaryHadALittleLamb(sampleRate int) mutations.Audio {
	const (
		quarter = 500 * time.Millisecond // 120 BPM
		half    = 2 * quarter
		whole   = 4 * quarter
	)
	c := consts.FreqNoteC4
	d := consts.FreqNoteD4
	e := consts.FreqNoteE4
	g := consts.FreqNoteG4

	notes := []MelodyNote{
		// "Mary had a little lamb,"
		{e, quarter}, {d, quarter}, {c, quarter}, {d, quarter},
		{e, quarter}, {e, quarter}, {e, half},
		// "little lamb, little lamb,"
		{d, quarter}, {d, quarter}, {d, half},
		{e, quarter}, {g, quarter}, {g, half},
		// "Mary had a little lamb,"
		{e, quarter}, {d, quarter}, {c, quarter}, {d, quarter},
		{e, quarter}, {e, quarter}, {e, quarter}, {e, quarter},
		// "its fleece was white as snow."
		{d, quarter}, {d, quarter}, {e, quarter}, {d, quarter},
		{c, whole},
	}
	return Melody(notes, sampleRate)
}
