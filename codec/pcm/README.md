# codec/pcm

Streaming PCM codec: raw **headerless** PCM bytes ↔ interleaved **`float64`** samples in `[-1.0, 1.0]`. This is the `codec.Decoder` / `codec.Encoder` face of linear PCM, for pipelines that speak `mutations.Audio` (the same convention as `codec/flac`, `codec/opus`, `codec/mp3`).

## The headerless-codec shape decision

Raw PCM **has no framing and no header** — it is just a flat run of sample bytes — so unlike a self-framed codec (`codec/mp3`, `codec/flac`) or a packet codec (`codec/opus`, `codec/aac`), the [Decoder] takes a plain [io.Reader] and the [Encoder] a plain [io.Writer], and the **sample format, sample rate, and channel count must be supplied at construction** (there is nothing in the byte stream to discover them from). The byte order is configurable (default little-endian).

Because PCM is uncompressed, this package is **self-contained**: there is no `libraries/pcm` engine behind it. It is a thin streaming wrapper over the per-format sample-conversion routines in [`mutations`](../../mutations) (`Int16ToFloat64`, `Float64ToInt16`, …). For a PCM stream wrapped in a header/chunk container, use [`containers/wav`](../../containers/wav), whose `Reader.Data()` / `Writer.Data()` give you the raw PCM `io.Reader` / `io.Writer` to pair with this codec.

## Layering

| Package | Works with | Use for |
|---|---|---|
| `mutations` | `[]byte` ↔ `[]float64` (one buffer at a time) | direct, allocation-free sample-format conversion |
| **`codec/pcm`** | **raw PCM bytes ↔ `float64` (`mutations.Audio`)** | **streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention)** |
| `containers/wav` | RIFF/WAVE chunk parsing + `fmt`/`data`/cue/bext | reading/writing `.wav`: format auto-detection, metadata |

On decode this layer reads raw bytes and divides each integer sample by its full-scale value to land in `[-1.0, 1.0]`; on encode it scales `float64` by full-scale, saturates values past `±1.0`, and rounds to the target width.

## Supported sample formats

Any [`mutations.SampleFormat`](../../mutations): `FormatUint8`, `FormatInt16`, `FormatInt24`, `FormatInt32`, `FormatFloat32`, `FormatFloat64`. The format is fixed for the life of a Decoder/Encoder; to change width, decode to float64 and re-encode (see the format-conversion example).

## cgo vs native / build tags

**None.** PCM is pure-Go in every build — there is no engine, no cgo backend, and no license-fenced build tag. `CGO_ENABLED=0 go build` and a default build are identical.

## Usage

### Decoding

```go
import "io"
import "log"
import "github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
import "github.com/daniel-sullivan/go-mediatoolkit/mutations"

// format, sampleRate, channels are not in the stream — supply them.
dec, err := pcm.NewDecoder(r, 44100, 2, mutations.FormatInt16) // raw bytes → mutations.Audio

buf := make([]float64, 8192)
for {
    audio, err := dec.Read(buf) // audio.Data is interleaved float64 in [-1.0, 1.0]
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
}
```

Use `pcm.WithByteOrder(binary.BigEndian)` for big-endian streams.

### Encoding

```go
enc, err := pcm.NewEncoder(w, 44100, 2, mutations.FormatInt16) // float64 in, raw bytes out

err = enc.Write(audio) // any size; not required to align to a frame boundary
err = enc.Close()       // flushes buffered bytes; the underlying writer is not closed
```

### With a container (`.wav`)

```go
import "github.com/daniel-sullivan/go-mediatoolkit/containers/wav"

rd, err := wav.NewReader(r)        // parses the fmt chunk → format/rate/channels
hdr := rd.Header()
dec, err := pcm.NewDecoder(rd.Data(), hdr.SampleRate, hdr.Channels, hdr.Extra.Format)
```

`rd.Data()` is the raw PCM `io.Reader` sliced from the `data` chunk; the `fmt` chunk supplies the format/rate/channels the headerless codec needs. See [`containers/wav`](../../containers/wav) for the full RIFF story.

## Examples

Runnable, standalone programs under [`examples/`](examples):

- [`decode/`](examples/decode) — decode raw int16 PCM bytes to float64, driving the `codec.Decoder` loop.
- [`encode/`](examples/encode) — encode a generated tone to raw int16 PCM bytes.
- [`roundtrip/`](examples/roundtrip) — encode then decode int16, confirming the recovered peak amplitude.
- [`format_conversion/`](examples/format_conversion) — PCM's signature strength: stream-convert int16 PCM to float32 PCM through the float64 seam.

## API

### Decoder — `codec.Decoder`

- `NewDecoder(r io.Reader, sampleRate, channels int, format mutations.SampleFormat, opts ...DecoderOption) (codec.Decoder, error)`
- `Read(out []float64) (mutations.Audio, error)` — fills `out` with interleaved float64 in `[-1.0, 1.0]`; loop until `io.EOF`.
- `SampleRate() int`, `Channels() int` — the values passed at construction.

### Encoder — `codec.Encoder`

- `NewEncoder(w io.Writer, sampleRate, channels int, format mutations.SampleFormat, opts ...EncoderOption) (codec.Encoder, error)`
- `Write(audio mutations.Audio) (int, error)` — the `audio` SampleRate/Channels must match the encoder; returns `ErrFormatMismatch` otherwise.
- `Close() error` — flushes buffered bytes (the underlying writer is not closed).

### Options

- Decoder: `WithByteOrder(binary.ByteOrder)` — default `binary.LittleEndian`.
- Encoder: `WithEncoderByteOrder(binary.ByteOrder)` — default `binary.LittleEndian`.

### Errors

- `ErrUnsupportedFormat` — the `mutations.SampleFormat` has no byte width (zero `BytesPerSample`).
- `ErrFormatMismatch` — the `mutations.Audio` passed to `Write` disagrees with the encoder's SampleRate/Channels.
- `ErrShortWrite` — the underlying writer accepted fewer bytes than offered.
- `codec.ErrBadChannelCount` / `codec.ErrBadSampleRate` — non-positive channels or sample rate at construction.

## License

This package is **MIT** and pure-Go with no third-party engine. See [`LICENSING.md`](../../LICENSING.md) for the project-wide map.
