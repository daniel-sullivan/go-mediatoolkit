# generators

Audio **signal generators** for testing, examples, and synthesis: sines,
chords, noise, pitched notes, melodies, sweeps, and short SFX bursts. Every
generator returns a [`mutations.Audio`](../mutations) — interleaved `float64`
samples in `[-1, 1]` paired with its sample rate and channel count — so its
output drops straight into the rest of the toolkit (`mutations` effects,
`codec/*` encoders, `containers/*` writers, `timeline`, `mixer`).

All generators are **pure-Go and device-free**: they synthesise a buffer in
memory and never touch `devices` or an OS audio backend. There is no engine, no
cgo, and no build tag. Every generated buffer is **mono** (`Channels: 1`); upmix
with [`mutations.UpmixMonoToStereo`](../mutations) if you need stereo.

The `*Into` variants write into a caller-owned `[]float64` and return the sample
count — zero-allocation forms for streaming into a ring or a pre-sized scratch
buffer.

## Generators

### Tones — `sine.go`

| Function | One-liner |
|---|---|
| `Sine(freq float64, duration time.Duration, sampleRate int) mutations.Audio` | Mono sine wave at `freq`, full ±1 amplitude. |
| `SineInto(buf []float64, freq float64, sampleRate int) int` | Zero-alloc: write a sine into `buf`, return samples written. |
| `Chord(freqs []float64, duration time.Duration, sampleRate int) mutations.Audio` | Sum of unit sines at every `freqs` entry, normalised to ≈ ±0.5 — pads and drones. |

### Pitched notes & melodies — `note.go`, `melody.go`

| Function / type | One-liner |
|---|---|
| `Note(freq float64, duration time.Duration, sampleRate int) mutations.Audio` | A single note with a piano-ish ADSR envelope; both endpoints are exactly 0 (no click). `freq <= 0` is a rest. |
| `MelodyNote{Freq float64; Duration time.Duration}` | One entry in a `Melody` sequence; `Freq <= 0` marks a rest. |
| `Melody(notes []MelodyNote, sampleRate int) mutations.Audio` | Renders a sequence of notes (and rests) end-to-end, seam-free, into one buffer. |
| `MaryHadALittleLamb(sampleRate int) mutations.Audio` | A ready-made recognisable tune (C major, 120 BPM) for examples. |

### Noise — `noise.go`

| Function | One-liner |
|---|---|
| `WhiteNoise(duration time.Duration, sampleRate int, seed int64) mutations.Audio` | Uniform white noise in ±1; `seed` makes it reproducible. |
| `WhiteNoiseInto(buf []float64, seed int64) int` | Zero-alloc white noise into `buf`. |
| `PinkNoise(duration time.Duration, sampleRate int, seed int64) mutations.Audio` | 1/f "pink" noise (Paul Kellet's 7-tap filter), peak ≈ ±0.5. |
| `PinkNoiseInto(buf []float64, seed int64) int` | Zero-alloc pink noise into `buf`, filter state carried across the buffer. |

### Sweeps — `sweep.go`

| Function | One-liner |
|---|---|
| `SineSweep(startHz, endHz float64, duration time.Duration, sampleRate int) mutations.Audio` | Logarithmic sine sweep `startHz → endHz`, amplitude 1.0 — a full-band test signal. |
| `SineSweepInto(buf []float64, startHz, endHz float64, sampleRate int) int` | Zero-alloc sweep into `buf` (`buf[0]` = `startHz`, last sample = `endHz`). |

### SFX bursts — `burst.go`

| Function | One-liner |
|---|---|
| `Beep(freq float64, duration time.Duration, sampleRate int) mutations.Audio` | Short sine cue at amp 0.4 with a 30 ms fade-out so it ends clean — UI / game SFX. |
| `Pluck(freq float64, duration time.Duration, sampleRate int) mutations.Audio` | Sine with exponential decay (τ = duration/3) — a cheap plucked-string. |

## Usage

```go
import "github.com/daniel-sullivan/go-mediatoolkit/consts"
import "github.com/daniel-sullivan/go-mediatoolkit/generators"

// A 1-second A4 tone at 48 kHz — a mono mutations.Audio, no device involved.
tone := generators.Sine(consts.FreqNoteA4, time.Second, consts.SampleRate48000)
log.Printf("%d frames, %s", tone.Frames(), tone.Duration())
```

Because the output is a `mutations.Audio`, generator output composes directly
with the rest of the toolkit:

```go
// Pink noise → softened with a 2 kHz lowpass, in place.
n := generators.PinkNoise(2*time.Second, consts.SampleRate48000, 42)
n.ApplyEffect(mutations.NewLowpass(2000, 0.707, n.SampleRate, n.Channels))
```

```go
// A recognisable backing tune, ready for an encoder or file writer.
song := generators.MaryHadALittleLamb(consts.SampleRate44100)
```

## License

This package is **MIT** and pure-Go with no third-party engine. See
[`LICENSING.md`](../LICENSING.md) for the project-wide map.
