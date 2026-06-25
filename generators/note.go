package generators

import (
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// notePeak is the maximum sample amplitude produced by Note. Set well
// below unity so multiple notes layered into chords or summed against
// a melody backing don't push the mixer's saturator into action.
const notePeak = 0.4

// Note renders a single pitched note with a piano-ish ADSR envelope:
// a fast linear attack, a gentle decay to a sustain plateau, then a
// release that fades back to silence by the end of the buffer. The
// envelope guarantees both endpoints are exactly zero, so notes
// concatenate in Melody without click artefacts.
//
// freq <= 0 is treated as a rest and produces silence at the requested
// duration. Returns a mono Audio at the given sample rate.
func Note(freq float64, duration time.Duration, sampleRate int) mutations.Audio {
	n := int(duration.Seconds() * float64(sampleRate))
	data := make([]float64, n)
	if n > 0 && freq > 0 {
		renderNote(data, freq, sampleRate)
	}
	return mutations.Audio{Data: data, SampleRate: sampleRate, Channels: 1}
}

// renderNote writes a single ADSR note into buf in place. Used by Note
// directly and by Melody to fill a sub-slice of a longer buffer
// without intermediate allocations.
func renderNote(buf []float64, freq float64, sampleRate int) {
	n := len(buf)
	if n == 0 {
		return
	}
	angular := 2.0 * math.Pi * freq / float64(sampleRate)

	// Target envelope segment lengths (samples). Clamped below so a
	// 50ms note still gets a clean attack + release rather than
	// starting or ending mid-cycle, and so attack+release never
	// overruns the buffer.
	attack := int(0.015 * float64(sampleRate))  // 15 ms
	release := int(0.080 * float64(sampleRate)) // 80 ms
	decay := int(0.060 * float64(sampleRate))   // 60 ms
	if attack > n/4 {
		attack = n / 4
	}
	if release > n/3 {
		release = n / 3
	}
	if attack < 1 {
		attack = 1
	}
	if release < 1 {
		release = 1
	}
	if decay > (n-attack-release)/2 {
		decay = (n - attack - release) / 2
	}
	if decay < 0 {
		decay = 0
	}

	const sustainLevel = 0.65
	decayEnd := attack + decay
	releaseStart := n - release

	for i := 0; i < n; i++ {
		var env float64
		switch {
		case i < attack:
			env = float64(i) / float64(attack)
		case i < decayEnd:
			t := float64(i-attack) / float64(decay)
			env = 1 + t*(sustainLevel-1)
		case i < releaseStart:
			env = sustainLevel
		default:
			// Release ramps sustain → 0 inclusive of both endpoints,
			// so buf[n-1] == 0 exactly. release-1 denominator avoids
			// the fence-post error that would leave a ~1e-5 residue
			// at the seam (audible click on extreme repetition).
			if release > 1 {
				env = sustainLevel * float64(n-1-i) / float64(release-1)
			} else {
				env = 0
			}
		}
		buf[i] = notePeak * env * math.Sin(angular*float64(i))
	}
}
