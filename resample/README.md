# resample

A pure-Go sample rate converter, ported from [libsamplerate](https://github.com/libsndfile/libsamplerate). Supports five conversion algorithms at different quality/speed tradeoffs, streaming and one-shot conversion, and automatic parallel processing for the sinc converters.

## Converter types

| Type | Quality | Use case |
|---|---|---|
| `SincBestQuality` | 144 dB SNR, 96% bandwidth | Mastering, archival |
| `SincMediumQuality` | 121 dB SNR, 90% bandwidth | General purpose |
| `SincFastest` | 97 dB SNR, 80% bandwidth | Real-time with quality |
| `Linear` | Low | Preview, non-critical |
| `ZeroOrderHold` | Lowest | Testing, effects |

Sinc converters automatically parallelize across available CPUs when processing large buffers.

## Install

Part of the `go-mediatoolkit` module — import it as `go-mediatoolkit/resample`; it is not separately `go get`-able (the module has no domain path).

```go
import "go-mediatoolkit/resample"
```

## Quick start

### One-shot conversion

Convert an entire buffer in a single call:

```go
ratio := resample.Ratio{InputRate: 44100, OutputRate: 48000}
output, err := resample.Simple(input, resample.SincFastest, 1, ratio)
```

[Full example](examples/simple/main.go)

### Streaming conversion

Create a converter and call `Process` repeatedly with chunks of audio:

```go
conv, err := resample.New(resample.SincMediumQuality, 1)
if err != nil {
    return err
}
defer conv.Close()

ratio := resample.Ratio{InputRate: 44100, OutputRate: 48000}
outBuf := make([]float64, 4096)

for {
    chunk, endOfInput := readNextChunk()

    d := &resample.Data{
        DataIn:     chunk,
        DataOut:    outBuf,
        EndOfInput: endOfInput,
        Ratio:      ratio,
    }

    if err := conv.Process(d); err != nil {
        return err
    }

    // Use outBuf[:d.OutputFramesGen] — write to file, device, etc.

    if endOfInput {
        break
    }
}
```

[Full example](examples/streaming/main.go)

### Stereo and multi-channel

Audio samples are interleaved: `[L0, R0, L1, R1, ...]`. Specify the channel count when creating the converter:

```go
conv, err := resample.New(resample.SincBestQuality, 2)
```

[Full example](examples/stereo/main.go)

### Variable ratio

Change the `Ratio` between `Process` calls for pitch bending, doppler effects, or adaptive rate matching. The converter smoothly interpolates between the old and new ratio within each call:

```go
d := &resample.Data{
    DataIn:     chunk,
    DataOut:    outBuf,
    EndOfInput: false,
    Ratio:      resample.Ratio{InputRate: 44100, OutputRate: 88200},
}
conv.Process(d)
```

[Full example](examples/variable_ratio/main.go)

## API reference

### Types

```go
// Ratio represents a conversion ratio as input/output sample rates.
type Ratio struct {
    InputRate  int
    OutputRate int
}

func (r Ratio) Float64() float64  // Returns OutputRate / InputRate.

// ConverterType selects the resampling algorithm.
type ConverterType int

const (
    SincBestQuality   ConverterType = iota
    SincMediumQuality
    SincFastest
    ZeroOrderHold
    Linear
)

// Data carries I/O buffers for a single Process call.
type Data struct {
    DataIn          []float64 // Input samples (interleaved).
    DataOut         []float64 // Output buffer (interleaved).
    InputFramesUsed int       // Frames consumed (set by Process).
    OutputFramesGen int       // Frames produced (set by Process).
    EndOfInput      bool      // Set true on the final buffer.
    Ratio           Ratio     // Conversion ratio.
}

// Converter performs streaming sample rate conversion.
type Converter interface {
    Process(d *Data) error
    Reset()
    Clone() Converter
    Close()
    Channels() int
    SetRatio(ratio Ratio) error
}
```

### Functions

```go
// New creates a converter for the given algorithm and channel count.
func New(converterType ConverterType, channels int) (Converter, error)

// Simple converts an entire buffer in one call.
func Simple(in []float64, converterType ConverterType, channels int, ratio Ratio) ([]float64, error)

// IsValidRatio reports whether a ratio is in [1/256, 256].
func IsValidRatio(ratio Ratio) bool
```

### Errors

| Error | Meaning |
|---|---|
| `ErrBadSrcRatio` | Ratio outside [1/256, 256] |
| `ErrBadChannelCount` | Channel count < 1 |
| `ErrBadConverterType` | Unknown converter type |
| `ErrBadData` | Nil data argument |
| `ErrBadInternalState` | Corrupted converter state |

## Data format

All audio is represented as `[]float64` with interleaved channels. A "frame" is one sample per channel:

```
Mono:   [S0, S1, S2, ...]
Stereo: [L0, R0, L1, R1, ...]
5.1:    [FL0, FR0, C0, LFE0, BL0, BR0, FL1, ...]
```

The ratio is specified as input and output sample rates:

```go
resample.Ratio{InputRate: 44100, OutputRate: 48000}  // Upsample (~1.089x)
resample.Ratio{InputRate: 96000, OutputRate: 44100}  // Downsample (~0.459x)
```

## Implementation

The default `New(...)` constructor is the pure-Go port. An optional `NewLibsamplerate(...)` constructor is built whenever the toolchain has cgo enabled, exposing the vendored libsamplerate (`resample/libsamplerate/`) under the same `Converter` interface — useful when you need byte-for-byte parity with audio software written against the C library, or as a regression oracle.

| Constructor | Cgo enabled | Cgo disabled |
|---|---|---|
| `New` | Native Go | Native Go |
| `NewLibsamplerate` | C libsamplerate (via Cgo) | _unavailable_ |

To force the pure-Go path:

```sh
CGO_ENABLED=0 go build ./resample/
```

No custom build tag is needed for the C path — the cgo discipline alone gates it (matches `libraries/opus` and `libraries/ogg`).

> The benchmark and parity commands below build the vendored C libsamplerate oracle via cgo, so they require `CGO_ENABLED=1` and a C compiler. The pure-Go suite (`go test ./resample/`) needs no C toolchain.

### Benchmarks (Apple M3 Pro, arm64)

48000 mono frames, 2× upsample. Native Go vs C libsamplerate (called via Cgo), side-by-side from `streaming_test.go` and `cgo_bench_test.go`:

| Converter | Native Go | C libsamplerate (via Cgo) | Go/C |
|---|---|---|---|
| `ZeroOrderHold` | 263 µs (1457 MB/s) | 946 µs (406 MB/s) | **3.6×** |
| `Linear` | 261 µs (1472 MB/s) | 929 µs (413 MB/s) | **3.6×** |
| `SincFastest` | 1.54 ms (250 MB/s) | 4.66 ms (82 MB/s) | **3.0×** |
| `SincMediumQuality` | 2.70 ms (142 MB/s) | 10.0 ms (38 MB/s) | **3.7×** |
| `SincBestQuality` | 6.36 ms (60 MB/s) | 30.6 ms (12 MB/s) | **4.8×** |

Native Go is faster across every converter. Two reasons: it operates directly on the caller's `float64` buffers (libsamplerate forces a `float64 → float32 → float64` round-trip through the Cgo boundary), and the sinc converters auto-parallelise when `GOMAXPROCS > 1` and the output buffer is ≥ 256 frames.

Reproduce:

```sh
go test ./resample/ -bench='Benchmark(ZOH|Linear|Sinc).*' -benchmem -benchtime=2s
```

### Native ↔ C parity

Every native converter is held against C libsamplerate (called via Cgo) as the oracle. `cgo_test.go` compares output frame counts and sample values across the full converter × ratio matrix:

```sh
go test ./resample/ -run 'NativeMatchesLibsamplerate|LibsamplerateAPISuite' -count=1
```

| Layer | What it checks |
|---|---|
| `TestNativeMatchesLibsamplerate` | 5 converters × 9 ratios × 1+2 channels — frame counts must match exactly; sample values within 0.05 abs (float32 vs float64 cumulative drift; well below the 16-bit quantisation floor). |
| `TestLibsamplerateAPISuite` | Same constructor / `Process` / error-handling contract checks the native suite runs in `resample_test.go`, applied to the C path. |
| `TestNewLibsamplerate*` | Smoke tests on the C path in isolation (round-trip, upsample, bad-input rejection). |

The frame-count assertion is the regression net for the round-to-even position-advance bug fixed in `sinc.go`.

### Zero-allocation streaming

The native `Process` API is zero-allocation when the caller reuses buffers (the C path always allocates a per-call float32 scratch buffer for the cgo boundary):

```go
// Allocate once.
ratio := resample.Ratio{InputRate: 44100, OutputRate: 48000}
outBuf := make([]float64, 4096)
d := &resample.Data{DataOut: outBuf, Ratio: ratio}

// Reuse every call — no allocations on the hot path.
for {
    d.DataIn = nextChunk()
    d.EndOfInput = isLast()
    conv.Process(d)
    consume(outBuf[:d.OutputFramesGen])
}
```

## Thread safety

A single `Converter` is **not** safe for concurrent use. For concurrent processing, use `Clone()` to create independent copies:

```go
conv, _ := resample.New(resample.SincFastest, 2)
clone := conv.Clone()

go func() { clone.Process(d1) }()
go func() { conv.Process(d2) }()
```

## Examples

| Example | Description |
|---|---|
| [simple](examples/simple/main.go) | One-shot conversion with `Simple()` |
| [streaming](examples/streaming/main.go) | Chunked streaming with `Process()` |
| [stereo](examples/stereo/main.go) | Multi-channel downsampling |
| [variable_ratio](examples/variable_ratio/main.go) | Changing ratio between calls |

Run any example:

```
go run ./resample/examples/simple/
```

## Acknowledgments

Port of [libsamplerate](https://github.com/libsndfile/libsamplerate) by Erik de Castro Lopo. Sinc filter coefficients are used under the [2-clause BSD license](https://github.com/libsndfile/libsamplerate/blob/master/COPYING).
