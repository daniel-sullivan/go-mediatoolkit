package timeline

import (
	"io"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Repeat wraps a Source factory into a Source that loops forever.
//
// The factory is called to obtain a fresh Source whenever the
// previous iteration ends. An iteration ends when either:
//
//   - the inner Source returns io.EOF, or
//   - loopDuration frames have been pulled (whichever comes first).
//
// loopDuration = 0 means "loop on natural Source EOF only" — use this
// when the factory's returned Source has the correct loop length
// baked in (e.g. a CachedClip of exactly one loop). A positive
// loopDuration truncates long sources and pads short ones with
// silence up to the loop length.
//
// Repeat is a Source, not a Timeline — it composes into any
// Source-accepting position: scheduled as a Cue on a Timeline, added
// to a Mixer, wrapped by an EffectSource, etc. This covers both
// "loop one clip" (factory returns a fresh Playhead) and "loop an
// arrangement" (factory returns a fresh Timeline).
//
// Repeat is single-consumer: Pull from one goroutine only.
type repeat struct {
	sampleRate int
	channels   int
	loopFrames int64 // 0 = rely on source EOF
	factory    func() Source

	inner    Source
	pulled   int64 // frames emitted in current iteration (source + silence pad)
	innerEOF bool  // current inner reported EOF; remainder of iteration is silence
}

// Repeat constructs a looping Source.
func Repeat(sampleRate, channels int, loopDuration time.Duration, factory func() Source) Source {
	return &repeat{
		sampleRate: sampleRate,
		channels:   channels,
		loopFrames: mutations.DurationToFrames(loopDuration, sampleRate),
		factory:    factory,
	}
}

// Pull reads the next chunk from the current iteration, rolling over
// to a fresh factory-produced Source when the current iteration ends.
func (r *repeat) Pull(dst []float64) (int, error) {
	if len(dst)%r.channels != 0 {
		return 0, ErrPartialFrame
	}
	wantFrames := int64(len(dst) / r.channels)
	written := 0

	for int64(written/r.channels) < wantFrames {
		if r.inner == nil {
			r.inner = r.factory()
			r.pulled = 0
			r.innerEOF = false
			if r.inner == nil {
				// Factory declined — emit silence indefinitely.
				return len(dst), nil
			}
		}

		// How many frames remain in this iteration?
		var remFrames int64
		if r.loopFrames > 0 {
			remFrames = r.loopFrames - r.pulled
			if remFrames <= 0 {
				// Iteration complete by duration — rotate.
				r.inner = nil
				continue
			}
		} else {
			// No explicit loopFrames; keep pulling until inner EOFs.
			remFrames = wantFrames - int64(written/r.channels)
		}

		chunkFrames := wantFrames - int64(written/r.channels)
		if chunkFrames > remFrames {
			chunkFrames = remFrames
		}
		chunkSamples := int(chunkFrames) * r.channels

		if r.innerEOF {
			// Silence pad to end of iteration.
			for i := written; i < written+chunkSamples; i++ {
				dst[i] = 0
			}
			written += chunkSamples
			r.pulled += chunkFrames
			continue
		}

		n, err := r.inner.Pull(dst[written : written+chunkSamples])
		written += n
		r.pulled += int64(n / r.channels)

		if err == io.EOF {
			r.innerEOF = true
			// If loopFrames == 0, treat EOF as end-of-iteration.
			if r.loopFrames == 0 {
				r.inner = nil
				continue
			}
			// Otherwise silence-pad the rest of this chunk call.
			pad := chunkSamples - n
			for i := written; i < written+pad; i++ {
				dst[i] = 0
			}
			written += pad
			r.pulled += int64(pad / r.channels)
			continue
		}
		if err != nil {
			return written, err
		}
		if n < chunkSamples {
			// Live source backpressure — return partial, caller retries.
			return written, nil
		}
	}
	return written, nil
}

func (r *repeat) SampleRate() int         { return r.sampleRate }
func (r *repeat) Channels() int           { return r.channels }
func (r *repeat) Duration() time.Duration { return -1 }
func (r *repeat) Live() bool              { return false }
