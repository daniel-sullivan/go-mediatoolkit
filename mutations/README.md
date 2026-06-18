# mutations

Sample-buffer transforms over interleaved **`float64`** PCM in `[-1, 1]`: the
[`Audio`](#the-audio-struct) value type, sample-format conversion, channel
mixing, interleave/deinterleave, trimming, chunking, looping, gain/fade
envelopes, saturation, and a small family of stateful [`Processor`](#the-processor-effect-model)
effects (biquad filters, echo, reverb, gain).

This is the toolkit's lowest layer for manipulating samples that already live in
memory. It is **pure-Go and device-free** — no engine, no cgo, no build tags —
and is the sample-format seam that the streaming codecs (`codec/pcm`,
`codec/flac`, …) call into. Everything here works on bare `[]float64` and/or the
`Audio` struct; nothing here touches an OS audio backend.

## The `Audio` struct

`Audio` is a PCM buffer paired with its format metadata — the preferred type for
handing audio between packages when the buffer outlives a single streaming pull
(generators, clip loaders, offline renderers, user code).

```go
type Audio struct {
    Data       []float64 // interleaved samples, normalised to [-1, 1]
    SampleRate int       // Hz
    Channels   int       // interleave count: 1 = mono, 2 = stereo, ...
}
```

`Data` is **interleaved** — `[L0, R0, L1, R1, ...]` for stereo. `Audio` is a
small value: copying it copies the header but **shares** the underlying `Data`
slice; use `Clone()` to duplicate the samples. Helpers: `Frames()` (frame count
= `len(Data)/Channels`), `Duration()`.

In-place methods (no length change) mutate `Data` and return the receiver, so
they chain:

```go
audio.ApplyGain(0.5).ApplyFadeIn(50 * time.Millisecond)
```

Length-changing methods (`CrossfadeLoop`, `RenderWithEffects`) allocate a fresh
buffer and return a new `Audio`; the receiver is untouched. The chainable
methods include `ApplyGain`, `ApplyGainEnvelope[Curve]`, `ApplyCustomGain`,
`ApplyFadeIn`, `ApplyFadeOut`, `ApplySaturator`, `ApplyEffect`, `ApplyEffects`.

Every free function below also has a bare-`[]float64` form for the hot path; the
`Audio` methods are thin wrappers that supply `SampleRate`/`Channels`.

## The `Processor` effect model

A `Processor` is a **stateful** audio effect that modifies an interleaved buffer
in place:

```go
type Processor interface {
    Process(samples []float64) // modify samples in place (same channel count it was built for)
    Reset()                    // clear internal state for reuse (after a seek / loop wrap)
}
```

Processors typically carry delay lines or filter state, so a `Processor` is
bound to the stream it was constructed for and is **not** safe to share across
streams or goroutines without synchronisation. Built-in processors:

| Constructor | Effect |
|---|---|
| `NewLowpass(cutoff, q, sampleRate, channels)` | RBJ-cookbook biquad lowpass (`Q = 0.707` = Butterworth). |
| `NewHighpass(cutoff, q, sampleRate, channels)` | Biquad highpass. |
| `NewBandpass(cutoff, q, sampleRate, channels)` | Biquad bandpass, 0 dB peak gain. |
| `NewEcho(delay, sampleRate, channels, feedback, wet)` | Single-tap delay with feedback. |
| `NewReverb(sampleRate, channels, roomSize, damping, wet)` | Schroeder/Freeverb-style reverb. |
| `NewGain(g)` | Stateless constant-gain stage for inline use in a chain. |

Apply one with `audio.ApplyEffect(p)`, a chain with
`audio.ApplyEffects(p1, p2, ...)`, or — for tail-carrying effects rendered
offline — `audio.RenderWithEffects(chain, tail)` / the free `RenderBuffer`,
which extend the buffer with `tail` of silence so echo/reverb decay fully. Cascade
two biquads in one chain for a steeper (≈ 4th-order) slope.

## What's in the box

- **Format conversion** (`format.go`) — `SampleFormat` (`FormatUint8`,
  `FormatInt16`, `FormatInt24`, `FormatInt32`, `FormatFloat32`,
  `FormatFloat64`) plus per-format `…ToFloat64` / `Float64To…` converters and
  the `DecodeSamples` / `EncodeSamples` dispatchers. This is the seam the PCM
  codec wraps.
- **Channels** (`channels.go`) — `DownmixStereoToMono`, `UpmixMonoToStereo`.
- **Interleave** (`interleave.go`) — `Interleave([][]float64)`,
  `Deinterleave(buf, n)`.
- **Trim** (`trim.go`) — `Trim` / `TrimAll` with a predicate, `TrimSilence` /
  `TrimSilenceAll`, and `TrimMode` (`TrimStart`, `TrimEnd`, `TrimBoth`).
- **Chunk** (`chunk.go`) — `Chunk(buf, size)`, `ChunkFunc(buf, size, fn)`.
- **Gain & envelopes** (`gain.go`) — `ApplyGain`, `GainPoint` envelopes with
  `GainCurveLinear` / `GainCurveExponential`, `FadeInEnvelope` /
  `FadeOutEnvelope` (+ `…Exp` variants), `ApplyCustomGain`.
- **Saturation** (`saturate.go`) — `SoftSaturate`, `HardClip`, `TanhSaturate`,
  the `Saturator` func type, `ApplySaturator`.
- **dB conversion** (`decibel.go`) — `Decibels`, `AmplitudeToDecibels`.
- **Looping** (`loop.go`) — `CrossfadeLoop` for seamless loops.
- **Timing** (`timing.go`) — `FramesToDuration`, `DurationToFrames`.
- **Scratch** (`scratch.go`) — `ResizeScratch` for reusing hot-path buffers.

## Usage

### Decibels → linear gain

```go
import "go-mediatoolkit/mutations"

// Attenuate a buffer by 6 dB. Decibels() uses the 20·log10 amplitude
// convention: -6 dB → ≈0.501, so the peak roughly halves.
audio.ApplyGain(mutations.Decibels(-6))
```

### A lowpass filter (a Processor)

```go
// Soften a buffer with a 2 kHz Butterworth lowpass, in place.
lpf := mutations.NewLowpass(2000, 0.707, audio.SampleRate, audio.Channels)
audio.ApplyEffect(lpf)

// Or render through an echo with a tail so the repeats decay fully
// (returns a NEW, longer Audio; the receiver is untouched):
echo := mutations.NewEcho(250*time.Millisecond, audio.SampleRate, audio.Channels, 0.4, 0.3)
wet := audio.RenderWithEffects([]mutations.Processor{echo}, time.Second)
```

### Trim trailing silence

```go
// Drop samples quieter than -60 dB (≈1e-3) from both ends. Returns a
// sub-slice of the input — no allocation.
trimmed := mutations.TrimSilence(audio.Data, mutations.TrimBoth, mutations.Decibels(-60))
audio.Data = trimmed
```

### Stream-convert sample format through the float64 seam

```go
import "encoding/binary"

// int16 PCM bytes → normalised float64 → int24 PCM bytes, all in memory.
in := make([]float64, len(int16Bytes)/2)
n := mutations.Int16ToFloat64(int16Bytes, in, binary.LittleEndian) // bytes → float64

out := make([]byte, n*mutations.FormatInt24.BytesPerSample())
mutations.Float64ToInt24(in[:n], out, binary.LittleEndian)         // float64 → bytes
```

For a *streaming* `io.Reader`/`io.Writer` face of this conversion, use
[`codec/pcm`](../codec/pcm), which wraps these converters in the
`codec.Decoder` / `codec.Encoder` interfaces.

## Layering

| Package | Works with | Use for |
|---|---|---|
| **`mutations`** | **`[]byte` ↔ `[]float64`, and the `Audio` value type** | **direct, in-memory sample transforms and format conversion** |
| `codec/pcm` | raw PCM bytes ↔ `float64` (streaming) | the `codec.Decoder`/`codec.Encoder` convention over PCM |
| `generators` | synthesised `mutations.Audio` | test signals and synthesis feeding these transforms |
| `timeline` / `mixer` | `Source` pulls + `Processor` chains | scheduled playback and live mixing of the same effects |

## License

This package is **MIT** and pure-Go with no third-party engine. See
[`LICENSING.md`](../LICENSING.md) for the project-wide map.
