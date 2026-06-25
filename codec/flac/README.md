# codec/flac

Streaming FLAC codec: a native FLAC byte stream ↔ interleaved **`float64`** samples in `[-1.0, 1.0]`. This is the `codec.Decoder` / `codec.Encoder` face of FLAC, for pipelines that speak `mutations.Audio` (the same convention as `codec/mp3`, `codec/opus`, `codec/pcm`).

FLAC is a **self-framed, lossless** codec — the byte stream carries the `fLaC` magic, metadata blocks (STREAMINFO, VORBIS_COMMENT, …), and audio frames in a single continuous sequence, so there is no separate framing layer at this level. The [Decoder] reads a native FLAC stream from a single continuous [io.Reader]; the [Encoder] writes one to a single continuous [io.Writer]. Use [`containers/flac`](../../containers/flac) for metadata-block inspection / tag projection on top of the raw stream, or [`containers/ogg`](../../containers/ogg) for Ogg-FLAC encapsulation.

Because FLAC is lossless, an encode→decode round-trip of the *integer* samples is bit-exact. This float64 seam is the only lossy step: scaling to/from `[-1.0, 1.0]` quantizes to the configured bit depth on encode and divides by full-scale on decode.

This package is a thin float64 adapter over [`libraries/flac`](../../libraries/flac), which holds the actual codec engine (the reference C libFLAC for the cgo backend, plus a bit-exact 1:1 Go port), the cgo-vs-native routing, the `flac_strict` build tag, and the bit-exact parity gate. **Read [`libraries/flac/README.md`](../../libraries/flac/README.md) for the engine, the cgo-vs-native table, the build tag, the parity discipline, and the benchmarks** — this README covers only the float64 streaming seam.

## Layering

| Package | Works with | Use for |
|---|---|---|
| `libraries/flac` | `int32` samples ↔ FLAC byte stream | direct block-by-block encode/decode, lowest overhead |
| **`codec/flac`** | **`float64` samples (`mutations.Audio`) ↔ FLAC byte stream** | **streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention)** |
| `containers/flac` | metadata-block chain + tag projection | inspecting/writing VORBIS_COMMENT (artist/title/album) |

`codec/flac` wraps `libraries/flac`'s `int32` engine and handles the conversion: on decode it divides each integer sample by `2^(BitsPerSample-1)-1`; on encode it scales `float64` by the same factor and **saturates at the bit-depth limits** so values just above `±1.0` don't wrap on overflow.

## cgo vs native (engine routing)

The engine and its routing live in `libraries/flac`; this package inherits them unchanged. Both paths are always available — there is **no opt-in license fence** (FLAC is BSD-licensed), unlike `codec/mp3` (`mp3lame`) or `codec/aac` (`aacfdk`). A default `codec/flac` build works out of the box.

| Build | `libraries/flac.NewEncoder` / `NewDecoder` reach |
|---|---|
| default (`go build`, cgo on) | C libFLAC via cgo |
| `CGO_ENABLED=0 go build` | bit-exact pure-Go libFLAC port (the `NewNative*` path) |

`codec/flac.NewDecoder` / `NewEncoder` always select `libraries/flac.NewDecoder` / `NewEncoder`, so they follow the cgo toggle above. To force the pure-Go port regardless of cgo, use `libraries/flac`'s `NewNativeDecoder` / `NewNativeEncoder` directly.

> **Tags on decode.** VORBIS_COMMENT parsing (`Decoder.Vendor()`/`Tags()`) lives in `libraries/flac`'s **cgo** backend; the native decode path length-skips the comment block. To read tags portably, use [`containers/flac`](../../containers/flac), which parses the metadata chain on either path.

```sh
go build ./codec/flac/                              # C libFLAC via cgo (default)
CGO_ENABLED=0 go build ./codec/flac/                # bit-exact pure-Go port
go build -tags=flac_strict ./codec/flac/            # FMA-free bit-exact parity mode
```

## Build tags

Inherited from `libraries/flac` (this package adds none of its own):

- **`flac_strict`** — FMA-free floating-point in the encoder analysis, matching the `-ffp-contract=off` C reference bit-for-bit. Slower; used for the parity gate. **Lossless decode output is bit-exact in either build** — the tag only affects which predictor/partition the *encoder* chooses, never the losslessness of the result.

The parity gate, oracle CGO flags, and per-slice status all live in `libraries/flac`; run it via `mise run //libraries/flac:parity`.

## Usage

### Decoding

```go
import flaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/flac"

dec, err := flaccodec.NewDecoder(r) // io.Reader of FLAC bytes → mutations.Audio

buf := make([]float64, 8192)
for {
    audio, err := dec.Read(buf) // audio.Data is interleaved float64 in [-1.0, 1.0]
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    // audio.SampleRate / audio.Channels populate once STREAMINFO is parsed.
}
```

`SampleRate()` and `Channels()` return zero until STREAMINFO has been parsed (surfaced as soon as the first `Read` returns samples).

### Encoding

```go
enc, err := flaccodec.NewEncoder(w, 44100, 2,   // float64 in, FLAC out
    flaccodec.WithBitsPerSample(16),
    flaccodec.WithCompressionLevel(8),          // 0 (fastest) .. 8 (smallest)
    flaccodec.WithTotalSamples(uint64(n)),      // lets libFLAC finalize STREAMINFO
)

err = enc.Write(audio) // audio.SampleRate / audio.Channels must match the encoder
err = enc.Close()      // flushes the final frame + trailing metadata; w is not closed
```

### With a container (tags)

```go
import ctrflac "github.com/daniel-sullivan/go-mediatoolkit/containers/flac"

rd, err := ctrflac.NewReader(r)            // parses the metadata-block chain (tags, STREAMINFO)
dec, err := flaccodec.NewDecoder(rd.Data()) // rd.Data() replays magic + metadata + frames
```

## Examples

Runnable, standalone programs under [`examples/`](examples). Run them from the
repo root (where `go.mod` lives); the `encode` example takes a required output
path:

```sh
go run ./codec/flac/examples/encode out.flac   # encode a generated tone, writing out.flac
```

- [`encode/`](examples/encode) — encode a generated tone to a native FLAC byte stream (takes `<output.flac>`).
- [`decode/`](examples/decode) — drive the `codec.Decoder` loop over a FLAC stream back to float64 PCM.
- [`roundtrip/`](examples/roundtrip) — encode then decode, reporting compression ratio and recovered peak amplitude.
- [`compression/`](examples/compression) — FLAC's signature strength: encode the same signal at every compression level (0–8) and compare the resulting sizes.

## API

### Decoder — `codec.Decoder`

- `NewDecoder(r io.Reader, opts ...DecoderOption) (codec.Decoder, error)`
- `Read(out []float64) (mutations.Audio, error)` — fills `out` with interleaved float64 in `[-1.0, 1.0]`; loop until `io.EOF`. Buffers one decoded block, so `out` may be any size.
- `SampleRate() int`, `Channels() int` — zero until STREAMINFO is parsed.

### Encoder — `codec.Encoder`

- `NewEncoder(w io.Writer, sampleRate, channels int, opts ...EncoderOption) (codec.Encoder, error)`
- `Write(audio mutations.Audio) (int, error)` — the `audio` SampleRate/Channels must match the encoder; returns `ErrFormatMismatch` otherwise.
- `Close() error` — flushes the final frame and trailing metadata (the underlying writer is not closed).

### Options

- Decoder: `WithMD5Check(bool)` — verify the STREAMINFO MD5 against the decoded samples.
- Encoder: `WithBitsPerSample(int)` (default 16, range [4, 32]), `WithCompressionLevel(0–8)` (default 5), `WithVerify(bool)`, `WithBlockSize(int)`, `WithTotalSamples(uint64)`, `WithTag(key, value string)`, `WithVendor(string)`.

### Errors

- `ErrBadArg` — nil reader/writer, or a `mutations.Audio` whose `Data` length is not a multiple of `Channels`.
- `ErrFormatMismatch` — the `mutations.Audio` passed to `Write` disagrees with the encoder's SampleRate/Channels.

Backend errors surface from [`libraries/flac`](../../libraries/flac) unchanged.

## License

This package is **MIT**; the engine it wraps (`libraries/flac`, BSD-licensed libFLAC / the pure-Go port) carries no copyleft or opt-in fence — FLAC encode and decode are available in every build. See [`LICENSING.md`](../../LICENSING.md) for the full map and [`BENCHMARKS.md`](../../BENCHMARKS.md) for native-vs-cgo throughput.
