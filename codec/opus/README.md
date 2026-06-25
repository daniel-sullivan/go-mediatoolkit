# codec/opus

Streaming Opus codec: Opus **packets** ↔ interleaved **`float64`** samples in `[-1.0, 1.0]`. This is the `codec.Decoder` / `codec.Encoder` face of Opus, for pipelines that speak `mutations.Audio` (the same convention as `codec/flac`, `codec/mp3`, `codec/pcm`).

## The packet-codec shape decision

Opus has **no single canonical byte-stream framing** — a packet may be carried in Ogg, WebM, RTP, or a length-prefixed blob — so, exactly like [`codec/aac`](../aac), this package works on **individual packets** through [`PacketReader`] / [`PacketWriter`] interfaces. Framing is a container concern, not a codec one. Callers supply their own framing implementation; the canonical one for `.opus`/Ogg is [`containers/ogg`](../../containers/ogg), whose `OpusReader` is a `PacketReader` and whose `OpusWriter` is a `PacketWriter`.

Tags and metadata are likewise a **container** concern: Opus carries its artist/title in the `OpusTags` header packet (an Ogg/`containers/ogg` concept), not in the codec packets this layer sees, so this package exposes none.

Internally the [Decoder] buffers one decoded frame and carries leftover samples in a remainder across `Read` calls, and the [Encoder] buffers input through a `mutations.StreamChunker` and emits one packet per full frame — so callers may issue arbitrarily sized `Read`/`Write` requests without minding Opus frame boundaries. The final partial frame is padded with silence at `Close`.

This package is a thin float64-streaming adapter over [`libraries/opus`](../../libraries/opus), which holds the actual codec engine (a pure-Go RFC 6716 implementation plus an optional cgo libopus backend), the cgo-vs-native routing, the `opus_strict` / `opus_nosimd` build tags, and the bit-exact parity gate. **Read [`libraries/opus/README.md`](../../libraries/opus/README.md) for the SILK/CELT engine, the cgo-vs-native table, the build tags, the parity discipline, and the benchmarks** — this README covers only the float64 streaming seam.

## Layering

| Package | Works with | Use for |
|---|---|---|
| `libraries/opus` | `float64` samples ↔ Opus packets | direct per-packet encode/decode, lowest overhead |
| **`codec/opus`** | **`float64` samples (`mutations.Audio`) ↔ packets via `PacketReader`/`PacketWriter`** | **streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention)** |
| `containers/ogg` | Ogg page muxing + `OpusHead`/`OpusTags` headers | reading/writing `.opus` files: pre-skip, gain, vendor/tag projection |

The Opus library already operates on `float64` in `[-1.0, 1.0]`, so this layer adds streaming/buffering semantics rather than a sample-format conversion: on decode it drains a per-frame scratch buffer into the caller's slice (carrying leftovers in a remainder); on encode it accumulates input in a `StreamChunker` and pads the final partial frame with silence at `Close`.

## cgo vs native (engine routing)

The engine and its routing live in `libraries/opus`; this package inherits them unchanged. Both paths are always available — there is **no opt-in license fence** (Opus is BSD-licensed), unlike `codec/mp3` (`mp3lame`) or `codec/aac` (`aacfdk`). A default `codec/opus` build works out of the box.

| Build | `libraries/opus.NewEncoder` / `NewDecoder` reach |
|---|---|
| default (`go build`, cgo on) | C libopus via cgo (NEON/SSE where available) |
| `CGO_ENABLED=0 go build` | pure-Go RFC 6716 port (the `NewNative*` path) |

`codec/opus.NewDecoder` / `NewEncoder` always select `libraries/opus.NewDecoder` / `NewEncoder`, so they follow the cgo toggle above. To force the pure-Go port regardless of cgo, use the `libraries/opus` `NewNativeDecoder` / `NewNativeEncoder` directly.

```sh
go build ./codec/opus/                              # C libopus via cgo (default)
CGO_ENABLED=0 go build ./codec/opus/                # pure-Go port
go build -tags=opus_strict ./codec/opus/            # FMA-free bit-exact parity mode
```

## Build tags

Inherited from `libraries/opus` (this package adds none of its own):

- **`opus_strict`** — FMA-free, bit-exact-against-the-C-reference floating-point on the hot paths, SIMD disabled. Slower; used for the parity gate. The default build is within PSNR noise (~117 dB).
- **`opus_nosimd`** — disables the NEON/SSE kernels, forcing scalar Go (still FMA-fused unless combined with `opus_strict`).

The parity gate, oracle CGO flags, and benchmarks all live in `libraries/opus`; run the fast unit-level gate via `mise run //libraries/opus:parity:benchcmp`.

## Usage

### Decoding

```go
import opuscodec "github.com/daniel-sullivan/go-mediatoolkit/codec/opus"

// pr is any PacketReader — e.g. containers/ogg's OpusReader, or
// opuscodec.NewSlicePacketReader(packets) over an in-memory slice.
dec, err := opuscodec.NewDecoder(pr, 48000, 2) // packets → mutations.Audio

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

Supported sample rates: 8000, 12000, 16000, 24000, 48000 Hz. Supported channels: 1 (mono) or 2 (stereo).

### Encoding

```go
enc, err := opuscodec.NewEncoder(pw, 48000, 1, // float64 in, packets out
    opuscodec.WithBitrate(64000),
    opuscodec.WithFrameDuration(20), // ms per packet
)

err = enc.Write(audio) // audio.SampleRate / audio.Channels must match the encoder
err = enc.Close()      // flushes the final (silence-padded) frame; pw is not closed
```

### With a container (`.opus` / Ogg)

```go
import "github.com/daniel-sullivan/go-mediatoolkit/containers/ogg"

rd, err := ogg.NewOpusReader(r)            // parses OpusHead / OpusTags, demuxes pages
dec, err := opuscodec.NewDecoder(rd, rd.Header().SampleRate, rd.Header().Channels)
```

`ogg.OpusReader` is a `PacketReader` over the Opus packets demuxed from the Ogg pages; `ogg.OpusWriter` is a `PacketWriter`. See [`containers/ogg`](../../containers/ogg) for the full Ogg/Opus story.

## Examples

Runnable, standalone programs under [`examples/`](examples):

- [`decode/`](examples/decode) — encode a tone to packets, then drive the `codec.Decoder` loop back to float64 PCM.
- [`encode/`](examples/encode) — encode a generated tone to Opus packets via a `PacketWriter`, reporting packet count and bitrate.
- [`roundtrip/`](examples/roundtrip) — encode then decode, measuring recovered peak amplitude.
- [`pipeline/`](examples/pipeline) — a compress-then-save pipeline: Opus-encode a sweep, decode, and re-encode as int16 PCM, reporting the compression ratio.

## API

### Decoder — `codec.Decoder`

- `NewDecoder(pr PacketReader, sampleRate, channels int, opts ...DecoderOption) (codec.Decoder, error)`
- `Read(out []float64) (mutations.Audio, error)` — fills `out` with interleaved float64 in `[-1.0, 1.0]`; loop until `io.EOF`. Buffers one decoded frame and carries leftovers across calls, so `out` may be any size.
- `SampleRate() int`, `Channels() int` — the values passed at construction.

### Encoder — `codec.Encoder`

- `NewEncoder(pw PacketWriter, sampleRate, channels int, opts ...EncoderOption) (codec.Encoder, error)`
- `Write(audio mutations.Audio) (int, error)` — the `audio` SampleRate/Channels must match the encoder; returns `ErrFormatMismatch` otherwise. Buffers through a `StreamChunker`; emits one packet per full frame.
- `Close() error` — flushes the final, silence-padded frame (the `PacketWriter` is not closed).

### Packet I/O

- `PacketReader` / `PacketWriter` — the framing seam (`ReadPacket() ([]byte, error)` / `WritePacket(data []byte) error`).
- `PacketReaderFunc` / `PacketWriterFunc` — function adapters.
- `NewSlicePacketReader(packets [][]byte) PacketReader` — replays a slice of packets, `io.EOF` when exhausted.

### Options

- Decoder: `WithGain(dB float64)` — applies a fixed output gain in dB.
- Encoder: `WithBitrate(bps int)` (default 64000), `WithComplexity(0–10)` (default 10), `WithApplication(libraries/opus.Application)` (default `AppAudio`), `WithFrameDuration(ms float64)` — one of 2.5, 5, 10, 20, 40, 60 (default 20).

### Errors

- `ErrBadSampleRate` — sample rate not one of 8000, 12000, 16000, 24000, 48000.
- `ErrBadChannels` — channels not 1 or 2.
- `ErrFormatMismatch` — the `mutations.Audio` passed to `Write` disagrees with the encoder's SampleRate/Channels.

Backend errors surface from [`libraries/opus`](../../libraries/opus) unchanged.

## License

This package is **MIT**; the engine it wraps (`libraries/opus`, BSD-licensed libopus / the pure-Go RFC 6716 port) carries no copyleft or opt-in fence — Opus encode and decode are available in every build. See [`LICENSING.md`](../../LICENSING.md) for the full file-by-file map and [`BENCHMARKS.md`](../../BENCHMARKS.md) for native-vs-cgo throughput.
