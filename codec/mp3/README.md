# codec/mp3

Streaming MP3 codec: a native MP3 byte stream ↔ interleaved **`float64`** samples in `[-1.0, 1.0]`. This is the `codec.Decoder` / `codec.Encoder` face of MP3, for pipelines that speak `mutations.Audio` (the same convention as `codec/flac`, `codec/opus`, `codec/pcm`).

MP3 is a **self-framed** codec — the byte stream itself carries frame-sync headers, side information, and Huffman-coded spectral data in a continuous sequence, so there is no separate framing layer at this level. The [Decoder] reads raw MP3 frames from a single continuous [io.Reader]; the [Encoder] writes raw MP3 frames to a single continuous [io.Writer]. Use [`containers/mp3`](../../containers/mp3) for ID3 metadata inspection and tag projection on top of the raw stream.

This package is a thin float64 adapter over [`libraries/mp3`](../../libraries/mp3), which holds the actual codec engines (minimp3 for decode, a LAME port for encode), the cgo-vs-native routing, the `mp3_strict` / `mp3lame` build tags, and the bit-exact parity gate. **Read [`libraries/mp3/README.md`](../../libraries/mp3/README.md) for the self-framed shape decision, the cgo-vs-native table, the build tags, the parity discipline, and the authoritative per-slice porting status** — this README covers only the float64 streaming seam.

## Layering

| Package | Works with | Use for |
|---|---|---|
| `libraries/mp3` | `int16` samples ↔ MP3 byte stream | direct frame-by-frame encode/decode, lowest overhead |
| **`codec/mp3`** | **`float64` samples (`mutations.Audio`) ↔ MP3 byte stream** | **streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention)** |
| `containers/mp3` | ID3v2 / ID3v1 metadata + tag projection | inspecting/writing artist/title/album tags |

`codec/mp3` wraps `libraries/mp3`'s `int16` engine and handles the conversion: on decode it divides the signed 16-bit samples by `2^15-1`; on encode it scales `float64` by `2^15-1`, saturates values past `±1.0`, and rounds to 16-bit before handing the buffer to the backend.

## Usage

### Decoding

```go
import codecmp3 "go-mediatoolkit/codec/mp3"

dec, err := codecmp3.NewDecoder(r) // io.Reader of MP3 bytes → mutations.Audio

buf := make([]float64, 8192)
for {
    audio, err := dec.Read(buf) // audio.Data is interleaved float64 in [-1.0, 1.0]
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    // audio.SampleRate / audio.Channels populate once the first frame header parses.
}
```

`SampleRate()` and `Channels()` return zero until the first frame header has been parsed (MP3 frames are self-describing, so the format is not known up front); they are surfaced as soon as `Read` returns its first samples.

### Encoding (requires `-tags mp3lame`, LGPL)

```go
enc, err := codecmp3.NewEncoder(w, 44100, 2,   // float64 in, MP3 out
    codecmp3.WithBitRate(192000),
    codecmp3.WithQuality(2),
)
// NewEncoder surfaces libraries/mp3.ErrEncoderRequiresLAME unless built -tags mp3lame.

err = enc.Write(audio)  // audio.SampleRate / audio.Channels must match the encoder
err = enc.Close()       // flushes the final frames; the underlying writer is not closed
```

The encoder is a derivative of **LAME** and is therefore **LGPL-2.0-or-later**. It is compiled in only when the `mp3lame` build tag is set; without it, `NewEncoder` returns `libraries/mp3.ErrEncoderRequiresLAME` and the binary links no LGPL code. Decoding (minimp3, CC0) is always available. See [`LICENSING.md`](../../LICENSING.md) and the [License](../../libraries/mp3/README.md#license) section of the libraries README.

### With a container (ID3)

```go
import ctrmp3 "go-mediatoolkit/containers/mp3"

rd, err := ctrmp3.NewReader(r)           // parses the leading ID3v2 (+ trailing ID3v1 if seekable)
dec, err := codecmp3.NewDecoder(rd.Data()) // rd.Data() replays ID3 prefix + audio frames
```

Runnable examples: [`examples/streaming/`](examples/streaming) (container → streaming float64 decode) and [`examples/metadata/`](examples/metadata) (ID3 round-trip).

## Build tags

Inherited from `libraries/mp3` (this package adds none of its own):

- **`mp3lame`** — fences the **LGPL** encoder. Required to build any encode path; default builds are decode-only and `NewEncoder` returns `ErrEncoderRequiresLAME`.
- **`mp3_strict`** — FMA-free, bit-exact-against-the-C-reference floating-point on the decode/encode hot paths (no `a*b+c` fusion). Slower; used for the parity gate. The default build is within PSNR noise.

```sh
go build ./codec/mp3/                              # decode-only, FMA-fused
go build -tags mp3lame ./codec/mp3/                # + LGPL encoder
go build -tags 'mp3lame mp3_strict' ./codec/mp3/   # bit-exact parity mode
```

The parity gate, oracle CGO flags, and per-slice status all live in `libraries/mp3`; run it via `mise run //libraries/mp3:parity`.

## API

### Decoder — `codec.Decoder`

- `NewDecoder(r io.Reader, opts ...DecoderOption) (codec.Decoder, error)`
- `Read(out []float64) (mutations.Audio, error)` — fills `out` with interleaved float64 in `[-1.0, 1.0]`; loop until `io.EOF`. The wrapper buffers one decoded frame internally so callers may issue arbitrarily small `Read` requests.
- `SampleRate() int`, `Channels() int` — zero until the first frame header is parsed.

### Encoder — `codec.Encoder` (requires `-tags mp3lame`)

- `NewEncoder(w io.Writer, sampleRate, channels int, opts ...EncoderOption) (codec.Encoder, error)`
- `Write(audio mutations.Audio) (int, error)` — the `audio` SampleRate/Channels must match the encoder; returns `ErrFormatMismatch` otherwise.
- `Close() error` — flushes the final frames (the underlying writer is not closed).

### Options

- `WithBitRate(bps int)` — target CBR, in bits/s (default 128000; ignored under VBR).
- `WithQuality(q int)` — LAME quality, `0`–`9`, `0` = highest quality / slowest (default 3).
- `WithVBR(enable bool)` — variable bit rate; `WithQuality` then selects the VBR target and `WithBitRate` is ignored.

### Errors

- `ErrBadArg` — nil reader/writer, or a `mutations.Audio` whose `Data` length is not a multiple of `Channels`.
- `ErrFormatMismatch` — the `mutations.Audio` passed to `Write` disagrees with the SampleRate/Channels the encoder was constructed with.

Backend errors surface from [`libraries/mp3`](../../libraries/mp3) unchanged — notably `ErrEncoderRequiresLAME` (encoder requested without `-tags mp3lame`).

## License

This package is **MIT**. The encode path it can reach (via `libraries/mp3` under `-tags mp3lame`) pulls in the **LGPL-2.0-or-later** LAME-derived encoder; decode (minimp3, CC0) is MIT-clean. A default build of `codec/mp3` links no LGPL code. See [`LICENSING.md`](../../LICENSING.md) for the full file-by-file / build-tag license map.
