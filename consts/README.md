# consts

Named numeric constants shared across the toolkit: **sample rates** and
**equal-temperament note frequencies**. The goal is to keep call-site literals
(`48000`, `440`) out of user code so intent reads clearly from the identifier —
`consts.SampleRate48000` instead of a bare `48000`, `consts.FreqNoteA4` instead
of `440`.

This package is pure-Go, dependency-free, and has no engine, no cgo, and no
build tags — it is just declarations.

## Constant groups

### Sample rates — `rates.go`

`int` constants for every rate the toolkit validates against, each named
`SampleRate<N>`:

| Constant | Hz | Typical use |
|---|---|---|
| `SampleRate8000` | 8000 | telephony / narrowband voice |
| `SampleRate16000` | 16000 | speech / wideband voice |
| `SampleRate22050` | 22050 | half-CD |
| `SampleRate24000` | 24000 | half-DAW default; Opus medium band |
| `SampleRate32000` | 32000 | digital broadcast, DAT-lo |
| `SampleRate44100` | 44100 | CD audio |
| `SampleRate48000` | 48000 | DAT / DAW default / professional video |
| `SampleRate88200` | 88200 | 2× CD |
| `SampleRate96000` | 96000 | 2× DAW default |
| `SampleRate176400` | 176400 | 4× CD |
| `SampleRate192000` | 192000 | hi-res mastering |

`CommonSampleRates []int` enumerates the rates the toolkit's algorithms are
matrix-tested against (22050, 32000, 44100, 48000, 88200, 96000, 192000) —
range over it in table-driven tests and format-pickers so a newly-supported rate
is added in one place.

### Note frequencies — `notes.go`

`float64` Hz constants for octaves 2–6 of the equal-tempered scale, named
`FreqNote<Letter>[S]<Octave>`. The number is the octave; an `S` before the
octave marks a sharp — `FreqNoteCS4` is C♯4. Flats are the enharmonic sharp of
the lower note (use `FreqNoteFS4` for G♭4). The reference is **A4 = 440 Hz**
(ISO 16); derive other tunings by multiplying through by the tuning ratio.

| Octave | Constants | Register |
|---|---|---|
| 2 | `FreqNoteC2` … `FreqNoteB2` (65.41 → 123.47 Hz) | sub-bass / low bass |
| 3 | `FreqNoteC3` … `FreqNoteB3` (130.81 → 246.94 Hz) | bass |
| 4 | `FreqNoteC4` … `FreqNoteB4` (261.63 → 493.88 Hz) | middle register |
| 5 | `FreqNoteC5` … `FreqNoteB5` (523.25 → 987.77 Hz) | upper middle |
| 6 | `FreqNoteC6` … `FreqNoteB6` (1046.50 → 1975.53 Hz) | treble |

Each octave runs the full chromatic set `C, CS, D, DS, E, F, FS, G, GS, A, AS,
B`. `FreqNoteA` is a convenience alias of `FreqNoteA4` (the 440 Hz reference)
for code where the specific octave does not matter.

> There are currently **no channel-count constants** in this package — pass the
> channel count as a plain `int` (`1` = mono, `2` = stereo).

## Usage

```go
import "go-mediatoolkit/consts"
import "go-mediatoolkit/generators"

// A 2-second A4 tone at 48 kHz, no magic numbers at the call site.
tone := generators.Sine(consts.FreqNoteA4, 2*time.Second, consts.SampleRate48000)
```

```go
// Validate an algorithm across every supported rate.
for _, rate := range consts.CommonSampleRates {
    // ... exercise the pipeline at `rate` ...
}
```

## License

This package is **MIT** and pure-Go. See [`LICENSING.md`](../LICENSING.md) for
the project-wide map.
