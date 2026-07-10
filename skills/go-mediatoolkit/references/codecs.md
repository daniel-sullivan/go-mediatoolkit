# Audio codecs — codec/aac, codec/mp3, codec/flac, codec/opus, codec/pcm

All five expose the same streaming face: `codec.Decoder` / `codec.Encoder` over interleaved `float64` in `[-1.0, 1.0]`, carried in `mutations.Audio`. What differs is the constructor (framing shape, options) and the build-tag fence.

Shared decoder loop (identical for every codec):

```go
buf := make([]float64, 8192)
for {
    audio, err := dec.Read(buf) // audio.Data aliases buf[:n], interleaved float64
    // ... consume audio.Data ...
    if err == io.EOF {
        break // a partial read + io.EOF is valid; samples above were consumed
    }
    if err != nil {
        return err
    }
}
```

Shared encoder contract: `Write(audio)` requires `audio.SampleRate`/`Channels` to equal the encoder's (else `ErrFormatMismatch`); `Close()` flushes the final frame (padding partial frames with silence where the codec is frame-based) and does **not** close the underlying writer. `codec.ReadFull(dec, buf)` is a helper that fills `buf` completely.

## FLAC — `codec/flac` (byte-stream, lossless, no fence)

```go
import flaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/flac"

dec, err := flaccodec.NewDecoder(r) // io.Reader of native FLAC bytes
// dec.SampleRate()/Channels() are 0 until STREAMINFO parses (first Read)

enc, err := flaccodec.NewEncoder(w, 44100, 2,
    flaccodec.WithBitsPerSample(16),      // default 16, range [4, 32]
    flaccodec.WithCompressionLevel(8),    // 0 fastest .. 8 smallest, default 5
    flaccodec.WithTotalSamples(uint64(n)), // lets the encoder finalise STREAMINFO
)
```

- Decoder options: `WithMD5Check(bool)`. Encoder options also: `WithVerify(bool)`, `WithBlockSize(int)`, `WithTag(key, value)`, `WithVendor(string)`.
- Lossless: integer round-trips are bit-exact; the float64 seam is the only quantisation step (scale by `2^(bits-1)-1`, saturating).
- Engine routing: cgo build → C libFLAC; `CGO_ENABLED=0` → bit-exact pure-Go port. Decode output is bit-exact either way.
- Tag reading portably: use `containers/flac` (the native decode path length-skips VORBIS_COMMENT; `Decoder.Vendor()/Tags()` only work on the cgo backend).

## Opus — `codec/opus` (packet codec, no fence)

```go
import (
    opuscodec "github.com/daniel-sullivan/go-mediatoolkit/codec/opus"
    opuslib "github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
)

dec, err := opuscodec.NewDecoder(pr, 48000, 2) // pr is a PacketReader

enc, err := opuscodec.NewEncoder(pw, 48000, 1, // pw is a PacketWriter
    opuscodec.WithBitrate(64000),        // default 64000
    opuscodec.WithComplexity(10),        // 0–10, default 10
    opuscodec.WithApplication(opuslib.AppAudio), // AppVoIP | AppAudio | AppLowDelay
    opuscodec.WithFrameDuration(20),     // ms: 2.5, 5, 10, 20 (default), 40, 60
)
```

- **Hard limits**: sample rate must be 8000, 12000, 16000, 24000, or 48000 (`ErrBadSampleRate`); channels 1 or 2 (`ErrBadChannels`). 44.1 kHz input must be resampled first.
- Decoder option: `WithGain(dB float64)`.
- Packet seam: `PacketReader`/`PacketWriter` interfaces (`ReadPacket() ([]byte, error)` / `WritePacket([]byte) error`), plus `PacketReaderFunc`/`PacketWriterFunc` adapters and `NewSlicePacketReader(packets [][]byte)` for in-memory replay. The canonical file framing is `containers/ogg` (`.opus`).
- Engine routing mirrors FLAC: cgo → libopus, `CGO_ENABLED=0` → pure-Go RFC 6716 port (~117 dB PSNR vs the reference in the default fast build).

## MP3 — `codec/mp3` (byte-stream; decode free, encode LGPL-fenced)

```go
import codecmp3 "github.com/daniel-sullivan/go-mediatoolkit/codec/mp3"

dec, err := codecmp3.NewDecoder(r) // always available (minimp3 port, CC0)
// SampleRate()/Channels() are 0 until the first frame header parses

// Encoder requires: go build -tags mp3lame   (LAME port, LGPL-2.0-or-later)
enc, err := codecmp3.NewEncoder(w, 44100, 2,
    codecmp3.WithBitRate(192000), // CBR bits/s, default 128000 (ignored under VBR)
    codecmp3.WithQuality(2),      // LAME quality 0 (best/slowest) – 9, default 3
    codecmp3.WithVBR(true),       // VBR: WithQuality selects the target, WithBitRate ignored
)
// without -tags mp3lame: NewEncoder returns libraries/mp3.ErrEncoderRequiresLAME
```

Note the capitalisation: MP3 uses `WithBitRate`; AAC/Opus use `WithBitrate`.

Internally MP3 is an int16 engine — the codec layer scales by `2^15-1` both ways. Distribution of an `mp3lame` binary carries LGPL obligations (source availability + relink); see `LICENSING.md`.

## AAC — `codec/aac` (packet codec, whole engine fenced behind `aacfdk`)

Everything — decode **and** encode — needs `go build -tags aacfdk` (Fraunhofer FDK-AAC license: permissive, non-copyleft, non-FOSS; AAC-LC patents expired 2017). Without the tag, `NewDecoder`/`NewEncoder` return `libraries/aac.ErrEngineRequiresFDK`. With the tag: cgo on → vendored FDK-AAC C; cgo off → pure-Go FDK port (bit-exact decode, byte-identical encode).

```go
import (
    aaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/aac"
    aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
)

// Decode: needs the out-of-band AudioSpecificConfig (ASC) — from the MP4 esds
// box (containers/mp4 Header().Extra.Config) or derived by containers/adts (rd.ASC()).
dec, err := aaccodec.NewDecoder(packetReader, asc)

enc, err := aaccodec.NewEncoder(packetWriter, 44100, 2,
    aaccodec.WithObjectType(aaclib.AOTAACLC), // default AAC-LC
    aaccodec.WithBitrate(128000),             // default 128000, CBR
)
```

Profiles via `WithObjectType`:

| Profile | AOT | Behaviour |
|---|---|---|
| AAC-LC | `aaclib.AOTAACLC` (2) | 1024 samples/ch per access unit; the dominant `.m4a` profile |
| HE-AAC v1 | `aaclib.AOTSBR` (5) | SBR: output sample rate is **double** the core's |
| HE-AAC v2 | `aaclib.AOTPS` (29) | SBR + Parametric Stereo: mono core → stereo output; **encode requires 2-channel input** (`ErrPSRequiresStereo`) |

On decode, `dec.SampleRate()`/`Channels()` report the **true decoded output** (SBR-doubled / PS-widened), resolved from the ASC before the first frame — trust them, not the nominal core values. `WithVBR` lives at the `libraries/aac` layer; the streaming wrapper drives CBR.

Packet seam is identical in shape to Opus: `PacketReader`/`PacketWriter`, `PacketReaderFunc`/`PacketWriterFunc`, `NewSlicePacketReader`. Containers: `containers/mp4` (`.m4a`, out-of-band esds ASC — required for explicit HE-AAC signalling) and `containers/adts` (`.aac`, config restated per frame; AAC-LC round-trips cleanly, no SBR/PS signalling in-band).

## PCM — `codec/pcm` (headerless, pure Go, no engine)

Raw PCM carries no header, so format/rate/channels are constructor arguments:

```go
import (
    "github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
    "github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

dec, err := pcm.NewDecoder(r, 44100, 2, mutations.FormatInt16)
enc, err := pcm.NewEncoder(w, 44100, 2, mutations.FormatInt16)
```

- Formats: `mutations.FormatUint8`, `FormatInt16`, `FormatInt24`, `FormatInt32`, `FormatFloat32`, `FormatFloat64`. Fixed for the codec's lifetime — to convert widths, decode to float64 and re-encode.
- Byte order options: `pcm.WithByteOrder(binary.BigEndian)` (decoder), `pcm.WithEncoderByteOrder(...)` (encoder); default little-endian.
- Pair with `containers/wav`: the `fmt` chunk supplies `Header.SampleRate/Channels/SampleFormat`, `Reader.Data()` supplies the bytes.

## Build-tag summary

| Tag | Enables | License | Default-build behaviour |
|---|---|---|---|
| `mp3lame` | MP3 **encoder** | LGPL-2.0-or-later | `NewEncoder` → `ErrEncoderRequiresLAME`; decode unaffected |
| `aacfdk` | AAC engine (decode + encode) | FDK-AAC | `NewDecoder`/`NewEncoder` → `ErrEngineRequiresFDK` |
| (cgo on/off) | C reference vs pure-Go port for FLAC/Opus (and FDK under `aacfdk`) | per-engine | pure-Go ports run when `CGO_ENABLED=0` |
| `opus_strict` / `flac_strict` / `mp3_strict` / `aac_strict` | bit-exact parity-gate builds (FMA-free/SIMD-off) | — | **development-only correctness gates, never ship** |

A `CGO_ENABLED=0 go build ./...` with no tags links only MIT + permissive code. `go test ./...` enables cgo by default and needs a C compiler (clang/gcc) — that is the repo's own test posture, not a consumer requirement.
