# timeline

The playback-layer foundation: schedule audio along a monotonically advancing
frame cursor and read back a single interleaved **`float64`** stream that a
[`mixer`](../mixer) or a [`devices`](../devices) callback consumes. Everything
playable is a [`Source`] — `Pull(dst []float64) (n int, err error)` plus
`SampleRate`/`Channels`/`Duration`/`Live` — so the pieces compose freely: a
[`CachedClip`] playhead, a [`StreamingClip`] over a `codec.Decoder`, a live
[`InputSource`] fed by a microphone callback, a [`Repeat`] that loops any
factory forever, an [`EffectSource`] that runs a `mutations.Processor` chain,
and a [`Timeline`] itself are all `Source`s — and a `Timeline` can be scheduled
as a [`Cue`] on another `Timeline`. A `Cue` pairs a `Source` with a start time
and an optional [`Transform`] (gain envelope, curve, or per-frame `GainFunc`);
`Timeline.Schedule` places it on the frame axis (parallel cues sum),
`Append`/`AppendAudio` place it back-to-back, and either returns a [`Handle`]
for cancellation and completion. All audio is interleaved `float64` in `[-1, 1]`
at the `Timeline`'s fixed sample rate and channel count; cues whose format
differs are rejected at schedule time (the `mixer` is what adapts format).

## Minimal example (device-free)

Build a `Timeline`, schedule a `Cue`, and drain it into a `[]float64` — no audio
hardware involved, just the `Source` interface:

```go
package main

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/generators"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

func main() {
	const (
		sampleRate = 48000
		channels   = 1
	)

	tl, err := timeline.NewTimeline(sampleRate, channels)
	if err != nil {
		log.Fatal(err)
	}
	defer tl.Close()

	// Cache a half-second 440 Hz tone and schedule it at t=0.5s with a
	// short fade-in. generators.Sine returns a mutations.Audio matching
	// the timeline's format.
	tone := generators.Sine(440, 500*time.Millisecond, sampleRate)
	clip, err := timeline.LoadClipFromAudio(tone)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := tl.Schedule(timeline.Cue{
		Source:    clip.Playhead(),
		Start:     500 * time.Millisecond,
		Transform: timeline.NewFadeIn(20 * time.Millisecond),
	}); err != nil {
		log.Fatal(err)
	}

	// Pull the mixed stream into a []float64, frame by frame. A Timeline
	// emits silence where no cue overlaps and never returns io.EOF until
	// Close, so stop once the cursor passes the scheduled span. dst must
	// hold whole frames (len(dst) % channels == 0).
	buf := make([]float64, 1024)
	var pulled int64
	for tl.Position() < tl.ScheduledDuration() {
		n, err := tl.Pull(buf)
		pulled += int64(n / channels)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("pulled %d frames (%.2fs)\n", pulled, float64(pulled)/float64(sampleRate))
}
```

For real-time output, hand the `Timeline` (or any `Source`) to a `mixer.Mixer`
and bind `mixer.Fill` as a `devices` output callback — that is exactly what the
examples below do.

## Building blocks

- **[`CachedClip`]** — fully-decoded PCM in memory; call `Playhead()` for an
  independent, seekable `Source` cursor. Build via `LoadClip` (drains a
  `codec.Decoder`), `LoadClipFromAudio`, `LoadClipFromPCM`, or `MustCacheClip`.
  Best for short, frequently-replayed audio.
- **[`StreamingClip`]** (`OpenClip`) — single-use `Source` that decodes a
  `codec.Decoder` on demand; for long-form material.
- **[`InputSource`]** (`NewInputSource`) — live `Source` (`Live() == true`) fed
  by a `devices.InputCallback` through an SPSC ring; partial `Pull` is the
  backpressure signal.
- **[`Repeat`]** — wraps a `func() Source` factory into a forever-looping
  `Source` (loop on natural EOF, or truncate/pad to a fixed loop duration).
- **[`EffectSource`]** (`NewEffectSource`) — runs an ordered
  `mutations.Processor` chain over a wrapped `Source`; `WithTail` extends the
  output so reverb/echo tails decay cleanly.
- **[`Transform`]** — declarative gain on a `Cue`: a `Gain` envelope, a
  `GainCurve` (linear / exponential), or a per-frame `GainFunc`. Helpers
  `NewFadeIn` / `NewFadeOut` / `NewFadeInLog` / `NewFadeOutLog`.
- **[`Timeline`]** — `NewTimeline` (realtime, no history) or `NewTimelineWith`
  (`Config{KeepHistory: true}` retains played cues so `Seek` can rewind).
  `Schedule` (explicit start, parallel cues sum), `Append`/`AppendAudio`
  (back-to-back), `Seek`, `Position`, `ScheduledDuration`, `Pull`, `Close`.

## Examples

Runnable, standalone programs under [`examples/`](examples). All open the
default audio output and run until Ctrl-C unless noted; **karaoke** additionally
needs a microphone, and **playlist** / **streaming** can self-terminate.

| Example | Command | Needs | Lifetime |
|---|---|---|---|
| [`playlist`](examples/playlist) | `go run ./timeline/examples/playlist` | output device | `Timeline.Append` of faded clips; exits when the last clip finishes or on Ctrl-C |
| [`game_sfx`](examples/game_sfx) | `go run ./timeline/examples/game_sfx` | output device (device picker) | `Schedule`-at-future-offset SFX; runs until Ctrl-C |
| [`multitrack`](examples/multitrack) | `go run ./timeline/examples/multitrack` | output device | three balanced `Mixer` tracks; runs until Ctrl-C |
| [`ambient_loop`](examples/ambient_loop) | `go run ./timeline/examples/ambient_loop` | output device | a `Repeat` loop with an LFO `GainFunc`; runs until Ctrl-C |
| [`streaming`](examples/streaming) | `go run ./timeline/examples/streaming` | output device | packet-at-a-time `AppendAudio` with optional `Seek`-back; runs for `-run` (default 30s) or until Ctrl-C |
| [`karaoke`](examples/karaoke) | `go run ./timeline/examples/karaoke` | **input (mic) + output** (device picker) | mic → `EffectSource` → `Mixer` over a backing loop; runs until Ctrl-C |

## License

This package is **MIT** and links no codec engines — see
[`LICENSING.md`](../LICENSING.md). For the toolkit-wide `float64` streaming
convention and the surrounding packages (`mixer`, `devices`, `mutations`,
`generators`), see the top-level [README](../README.md).
