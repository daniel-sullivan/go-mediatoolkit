# Sample pipeline — resample, mutations, generators, timeline, mixer, buffers

All pure Go, no build tags, no cgo. Everything operates on interleaved `float64` in `[-1, 1]`.

## resample — sample-rate conversion (libsamplerate port)

```go
import "github.com/daniel-sullivan/go-mediatoolkit/resample"

// One-shot:
ratio := resample.Ratio{InputRate: 44100, OutputRate: 48000}
output, err := resample.Simple(input, resample.SincFastest, channels, ratio)

// Streaming:
conv, err := resample.New(resample.SincMediumQuality, channels)
defer conv.Close()
outBuf := make([]float64, 4096)
d := &resample.Data{DataIn: chunk, DataOut: outBuf, EndOfInput: last, Ratio: ratio}
err = conv.Process(d)
// use outBuf[:d.OutputFramesGen]; d.InputFramesUsed reports frames consumed
```

- Converters (quality → speed): `SincBestQuality` (144 dB SNR), `SincMediumQuality` (121 dB), `SincFastest` (97 dB), `Linear`, `ZeroOrderHold`. Sinc converters auto-parallelise on large buffers.
- Ratio must be within [1/256, 256] (`ErrBadSrcRatio`); `Ratio.Float64()` = OutputRate/InputRate. The ratio may change between `Process` calls (pitch bend / drift matching) — the converter interpolates smoothly.
- `Converter` is **not** goroutine-safe; `Clone()` for concurrent streams. `Reset()` reuses one for a new stream. Zero-allocation when buffers are reused.
- `NewLibsamplerate(...)` (cgo builds only) exposes the vendored C library under the same interface — only for byte-parity with C-based software.

## mutations — in-memory transforms + the Processor effect model

The `Audio` value type and free functions over bare `[]float64`:

- **Format conversion**: `Int16ToFloat64(src []byte, dst []float64, order binary.ByteOrder)`, `Float64ToInt24`, ... plus dispatchers `DecodeSamples`/`EncodeSamples` over `SampleFormat` (`FormatUint8/Int16/Int24/Int32/Float32/Float64`; `BytesPerSample()`).
- **Channels**: `DownmixStereoToMono`, `UpmixMonoToStereo`. **Interleave**: `Interleave([][]float64)`, `Deinterleave(buf, n)`.
- **Trim**: `TrimSilence(buf, mutations.TrimBoth, mutations.Decibels(-60))` — returns a sub-slice, no allocation. Modes `TrimStart`/`TrimEnd`/`TrimBoth`.
- **Chunking**: `Chunk(buf, size)`, `ChunkFunc`, and the stateful `StreamChunker` (accumulates arbitrary writes, emits fixed-size chunks).
- **Gain/dB**: `Decibels(db)` (dB → linear, 20·log10 convention), `AmplitudeToDecibels(amp)`; envelopes via `GainPoint` + `GainCurveLinear`/`GainCurveExponential`; `ApplyGain`, fades.
- **Saturation**: `SoftSaturate`, `HardClip`, `TanhSaturate`, `ApplySaturator`.
- **Timing**: `FramesToDuration(frames, rate)`, `DurationToFrames(d, rate)`.

`Audio` methods chain in place: `audio.ApplyGain(mutations.Decibels(-6)).ApplyFadeIn(50 * time.Millisecond)`. Length-changing methods (`CrossfadeLoop`, `RenderWithEffects`) return a **new** `Audio`.

Stateful effects implement `Processor` — the toolkit-wide seam (loudness, vad, aec all implement it):

```go
type Processor interface {
    Process(samples []float64) // in place, same channel count it was built for
    Reset()
}
```

Built-ins: `NewLowpass(cutoff, q, sampleRate, channels)` / `NewHighpass` / `NewBandpass` (RBJ biquads; q = 0.707 is Butterworth), `NewEcho(delay, sampleRate, channels, feedback, wet)`, `NewReverb(sampleRate, channels, roomSize, damping, wet)`, `NewGain(g)`. A `Processor` carries state — never share one across streams or goroutines. For tail-carrying effects rendered offline:

```go
echo := mutations.NewEcho(250*time.Millisecond, audio.SampleRate, audio.Channels, 0.4, 0.3)
wet := audio.RenderWithEffects([]mutations.Processor{echo}, time.Second) // extends with 1s tail
```

## generators — test signals & synthesis

All return **mono** `mutations.Audio` (upmix if needed); `*Into` variants are zero-alloc into caller buffers.

```go
import (
    "github.com/daniel-sullivan/go-mediatoolkit/consts"
    "github.com/daniel-sullivan/go-mediatoolkit/generators"
)

tone  := generators.Sine(consts.FreqNoteA4, time.Second, consts.SampleRate48000)
sweep := generators.SineSweep(20, 20000, 2*time.Second, 48000) // log sweep, full-band test
noise := generators.WhiteNoise(time.Second, 48000, 42)          // seeded, reproducible
pink  := generators.PinkNoise(time.Second, 48000, 42)
song  := generators.MaryHadALittleLamb(consts.SampleRate44100)  // demo melody
```

Also: `Chord(freqs, dur, rate)`, `Note` (ADSR envelope, click-free), `Melody([]MelodyNote, rate)` (`Freq <= 0` is a rest), `Beep`, `Pluck`.

## timeline — scheduled playback (device-free)

Everything playable is a `Source`: `Pull(dst []float64) (int, error)` + `SampleRate()/Channels()/Duration()/Live()`. Clips, live inputs, effect wrappers, and Timelines themselves are all `Source`s and nest freely.

```go
import "github.com/daniel-sullivan/go-mediatoolkit/timeline"

tl, err := timeline.NewTimeline(48000, 1)
defer tl.Close()

clip, err := timeline.LoadClipFromAudio(tone) // *CachedClip: decoded PCM in memory
_, err = tl.Schedule(timeline.Cue{
    Source:    clip.Playhead(), // independent seekable cursor per playback
    Start:     500 * time.Millisecond,
    Transform: timeline.NewFadeIn(20 * time.Millisecond),
})

buf := make([]float64, 1024) // len must be a multiple of channels
for tl.Position() < tl.ScheduledDuration() {
    n, err := tl.Pull(buf) // silence where nothing is scheduled; io.EOF only after Close
    _ = n; _ = err
}
```

- **Clip loading**: `LoadClip(dec codec.Decoder)` (drains a decoder), `LoadClipFromAudio`, `LoadClipFromPCM`, `MustCacheClip`; `OpenClip(dec, duration)` returns a single-use `StreamingClip` for long-form material.
- **Live input**: `NewInputSource(sampleRate, channels, bufferFrames)` — feed it from a `devices.InputCallback`; partial `Pull` is the backpressure signal.
- **Looping**: `Repeat(sampleRate, channels, loopDuration, factory func() Source)`.
- **Effects on a stream**: `NewEffectSource(src, processors...)` runs a `mutations.Processor` chain; `.WithTail(d)` appends `d` of silence so reverb/limiter/VAD tails drain before `io.EOF`.
- **Transforms** (declarative per-cue gain): `Transform{Gain []EnvelopePoint, GainCurve, GainFunc}`; helpers `NewFadeIn/NewFadeOut/NewFadeInLog/NewFadeOutLog`. `Crossfade(tl, fromCue, toCue, fade)` helper.
- **Scheduling**: `Schedule` (explicit `Start`; overlapping cues sum), `Append`/`AppendAudio` (back-to-back); both return a `Handle` (cancellation + completion). `NewTimelineWith(timeline.Config{KeepHistory: true})` enables `Seek` rewind.
- A `Timeline` has a **fixed format**; cues with a different rate/channels are rejected at `Schedule` time — the **mixer** is the layer that adapts formats.

## mixer — summing sources for a device callback

```go
import "github.com/daniel-sullivan/go-mediatoolkit/mixer"

mx, err := mixer.New(mixer.Config{
    SampleRate: 48000, Channels: 2,
    // RingFrames (default ~200ms), ChunkFrames (~10ms), Processors: master-bus chain
})
handle, err := mx.AddSource(tl)  // any timeline.Source; rate/channel adapted automatically
// Bind to a device: the callback side is a pure memcpy.
stream, err := sys.OpenOutput(dev, devices.StreamFormat{SampleRate: 48000, Channels: 2}, mx.Fill)
stream.Start()
```

- A mix goroutine runs ahead of realtime, pulling tracks, summing with per-track gain, soft-saturating, and writing into an SPSC ring; `Fill` (the device callback) just copies out, zero-filling and counting `Underruns()` on underrun.
- Tracks are adapted transparently: resampling + mono↔stereo (other channel topologies → `ErrUnsupportedChannels`).
- `Config.Processors` is the master-bus `mutations.Processor` chain — the place for a `loudness.Monitor`, `Leveller`, or an `aec` far-end tap.
- Latency ≈ RingFrames/SampleRate; shrink `RingFrames` for tighter latency at underrun risk.

## buffers — lock-free SPSC ring

`buffers.NewRing(n)` — a single-producer/single-consumer `float64` ring synchronised with atomics: one goroutine writes, one reads (e.g. bridging a device callback and a processing goroutine). It is layout-agnostic: push/pop multiples of the channel count yourself.
