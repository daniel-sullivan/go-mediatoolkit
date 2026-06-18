# codec/aac

Streaming AAC codec: AAC **access units** (packets) ↔ interleaved **`float64`** samples in `[-1.0, 1.0]`. This is the `codec.Decoder` / `codec.Encoder` face of AAC, for pipelines that speak `mutations.Audio` (the same convention as `codec/flac`, `codec/opus`, `codec/pcm`).

The full AAC family is complete: **AAC-LC** (decode bit-exact, encode byte-identical, + VBR), **HE-AAC v1** (SBR — decode + encode), and **HE-AAC v2** (parametric stereo — decode + encode). All share the same packet seam below; the profile is selected at encode time with `WithObjectType` and discovered at decode time from the `AudioSpecificConfig`.

## The packet-codec shape decision

AAC has **no single canonical byte-stream framing** — an access unit may be carried in ADTS, LATM/LOAS, or an ISOBMFF/MP4 sample table — so, exactly like [`codec/opus`](../opus) (+ `containers/ogg/opus.go`), this package works on **individual packets** through [`PacketReader`] / [`PacketWriter`] interfaces. Framing is a container concern, not a codec one. Callers supply their own framing implementation; the canonical one for `.m4a`/`.mp4` is [`containers/mp4`](../../containers/mp4), whose `Reader.Packets()` is a `PacketReader` and whose `Writer` is a `PacketWriter`.

Tags and metadata are likewise a **container** concern: AAC carries no artist/title/cover-art in the codec stream, so this package exposes none. Read them via [`containers/mp4`](../../containers/mp4), which projects the iTunes `ilst` atoms onto `containers.StandardTags`.

The decoder takes an out-of-band [`AudioSpecificConfig`](../../libraries/aac) (the MPEG-4 ASC, normally the bytes from the MP4 `esds` box) describing the profile, sample rate, and channel layout. The encoder produces the ASC its container layer must emit. Internally the [Decoder] buffers one decoded frame and carries leftover samples in a remainder across `Read` calls, and the [Encoder] buffers input through a `mutations.StreamChunker` and emits one access unit per full frame — so callers may issue arbitrarily sized `Read`/`Write` requests without minding AAC frame boundaries.

This package is a thin float64 adapter over [`libraries/aac`](../../libraries/aac), which holds the actual codec engine (Fraunhofer FDK-AAC for the cgo backend, plus a 1:1 Go port), the cgo-vs-native routing, the `aacfdk` / `aac_strict` build tags, the FDK license fence, and the bit-exact parity gate. **Read [`libraries/aac/README.md`](../../libraries/aac/README.md) for the packet-codec shape rationale, the cgo-vs-native table, the build tags, the parity discipline, and the per-slice porting status** — this README covers only the float64 streaming seam.

## Layering

| Package | Works with | Use for |
|---|---|---|
| `libraries/aac` | `float64` samples ↔ AAC access units (packets) | direct per-packet encode/decode, lowest overhead |
| **`codec/aac`** | **`float64` samples (`mutations.Audio`) ↔ packets via `PacketReader`/`PacketWriter`** | **streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention)** |
| `containers/mp4` | ISOBMFF box tree + iTunes tag projection | reading/writing `.m4a`/`.mp4`: `esds` ASC, `stsz`/`stsc`/`stco` sample tables, `ilst` metadata |

The AAC library already operates on `float64` in `[-1.0, 1.0]`, so this layer adds streaming/buffering semantics rather than a sample-format conversion: on decode it drains a per-frame scratch buffer into the caller's slice (carrying leftovers in a remainder); on encode it accumulates input in a `StreamChunker` and pads the final partial frame with silence at `Close`.

## Profiles (audio object types)

Pass `WithObjectType(aaclib.AOT…)` to pick the encode profile (default `AOTAACLC`); on decode the profile comes from the `AudioSpecificConfig`. All three are complete — decode is **bit-exact** and encode is **byte-identical** vs the FDK reference.

| Profile | Object type (AOT) | Frame | Output | Notes |
|---|---|---|---|---|
| **AAC-LC** | `AOTAACLC` (2) | 1024 samples/ch | rate & channels as coded | the dominant `.m4a` profile; CBR via `WithBitrate` (VBR via the `libraries/aac` layer's `WithVBR`) |
| **HE-AAC v1** | `AOTSBR` (5) | 2048 samples/ch | **SBR-doubled** rate; channels as coded | AAC-LC core + Spectral Band Replication regenerates the high band |
| **HE-AAC v2** | `AOTPS` (29) | 2048 samples/ch | SBR-doubled rate; **mono core → stereo** | AOT-5 + Parametric Stereo; a mono core is upmixed to 2 channels. Encode **requires a 2-channel input** — a non-stereo input returns `ErrPSRequiresStereo` |

The **output** rate/channels can differ from what the stream was nominally constructed with, because the HE-AAC tools transform the signal: SBR doubles the sample rate (a 22.05 kHz core → a 44.1 kHz output) and PS widens a mono core to stereo. [`AudioSpecificConfig.Output()`](../../libraries/aac) resolves this from the explicit-hierarchical extension signalling in the ASC, so the decoder advertises the **true decoded format up front**, before the first frame flows — and `NewDecoder` sizes its buffers from it (e.g. a stereo scratch for a PS mono core). For an HE-AAC stream `dec.SampleRate()` / `dec.Channels()` are therefore the doubled rate / widened channel count, not the core's.

## cgo vs native (engine routing)

The engine, its routing, and the FDK fence all live in `libraries/aac`; this package inherits them unchanged. **Every AAC decode/encode path goes through the Fraunhofer FDK-AAC engine, which is fenced behind the opt-in `aacfdk` build tag** (and cgo). A default build links **zero** FDK-AAC code, and `NewDecoder` / `NewEncoder` surface `libraries/aac.ErrEngineRequiresFDK` at construction. Under `aacfdk` there are two interchangeable backends — the **vendored FDK-AAC C reference** (via cgo) and a **pure-Go 1:1 port** of it — and the two are equivalent **on the decode/encode data path** for any stream both backends accept: the port is **bit-exact** on decode (it reproduces the C int32 PCM bit-for-bit) and **byte-identical** on encode. FDK-AAC is fixed-point, so this is exact integer equality, not an FP/ULP target. The equivalence is over the sample/bitstream data path; ASC/config validation strictness can differ between the C FDK engine and the native port (the C engine rejects some malformed/synthetic configs the port accepts, and vice versa), so a config one backend errors on is not a parity miss.

| Build | `NewEncoder` / `NewDecoder` reach | Links FDK-AAC? |
|---|---|---|
| default (`go build`) | `libraries/aac` seams → `ErrEngineRequiresFDK` | no |
| `-tags aacfdk` (cgo) | FDK-AAC C reference via cgo | yes (FDK-AAC license) |
| `-tags aacfdk` (cgo disabled) | pure-Go FDK-AAC port (bit-exact decode / byte-identical encode) | yes (FDK-derived Go) |

```sh
go build ./codec/aac/                              # default: no FDK, ErrEngineRequiresFDK at use
go build -tags aacfdk ./codec/aac/                 # FDK-AAC engine (requires CGO_ENABLED=1 for the C path)
go build -tags 'aac_strict aacfdk' ./codec/aac/    # integer-parity assertion mode
```

## Build tags

Inherited from `libraries/aac` (this package adds none of its own):

- **`aacfdk`** — fences the **FDK-AAC** engine (the only AAC engine, used for both decode and encode). Required to build any working AAC path; default builds surface `ErrEngineRequiresFDK`. The C backend additionally requires `CGO_ENABLED=1`; with cgo off, `aacfdk` selects the pure-Go FDK port.
- **`aac_strict`** — un-skips the **integer-parity** assertions and strict-gated tests (flips `nativeaac.StrictMode`). FDK-AAC is FIXED-POINT (int32 Q-format) for both decode and the ported encoder kernels, so parity is **EXACT integer equality** on decode and a **byte-identical bitstream** on encode — there is **no** floating-point/FMA path to make deterministic, hence no `*_fp_strict`/`*_fp_default` split. `aac_strict` does not change arithmetic; it only turns the parity assertions on. Composed with `aacfdk` by the `mise` tasks.

The parity gate, oracle CGO flags, and per-slice status all live in `libraries/aac`; run it via `MISE_EXPERIMENTAL=1 mise run //libraries/aac:parity` (built `-tags 'aac_strict aacfdk'`).

## Usage

### Decoding

```go
import aaccodec "go-mediatoolkit/codec/aac"

// asc is the AudioSpecificConfig — typically the bytes parsed from the MP4 esds
// box (containers/mp4 exposes it as Header.Extra.Config).
dec, err := aaccodec.NewDecoder(packetReader, asc) // packets → mutations.Audio

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

### Encoding (requires `-tags aacfdk`)

```go
enc, err := aaccodec.NewEncoder(packetWriter, 44100, 2, // float64 in, packets out
    aaccodec.WithObjectType(aaclib.AOTAACLC),
    aaccodec.WithBitrate(128000),
)
// NewEncoder surfaces libraries/aac.ErrEngineRequiresFDK unless built -tags aacfdk.

err = enc.Write(audio) // audio.SampleRate / audio.Channels must match the encoder
err = enc.Close()      // flushes the final frame (padded with silence); pw is not closed
```

### With a container

AAC rides in two containers in this repo, and both supply a `PacketReader`/`PacketWriter` plus the `AudioSpecificConfig`, so the codec layer is identical above either:

- **MP4 / `.m4a`** ([`containers/mp4`](../../containers/mp4)) — the ISOBMFF sample table; the ASC is the `esds` box (carries explicit HE-AAC signalling, so SBR/PS streams decode correctly).
- **ADTS / raw `.aac`** ([`containers/adts`](../../containers/adts)) — the self-framed ADTS frame stream; the ASC is synthesised from each frame's 7-byte header.

```go
import "go-mediatoolkit/containers/mp4"

rd, err := mp4.NewReader(r)                                 // parses the ISOBMFF box tree
hdr := rd.Header()                                          // SampleRate/Channels + tags + Extra.Config (ASC)
dec, err := aaccodec.NewDecoder(rd.Packets(), hdr.Extra.Config)
```

```go
import "go-mediatoolkit/containers/adts"

rd, err := adts.NewReader(r)                                // parses the first ADTS header eagerly
dec, err := aaccodec.NewDecoder(rd, rd.ASC())              // adts.Reader is itself a PacketReader

w, err := adts.NewWriter(out, 44100, 2)                    // adts.Writer is a PacketWriter
enc, err := aaccodec.NewEncoder(w, 44100, 2)               // framed access units → playable .aac
```

`rd.Packets()` (MP4) / `rd` (ADTS) is a `PacketReader` over the AAC access units; `hdr.Extra.Config` / `rd.ASC()` is the `AudioSpecificConfig`. ADTS carries no SBR/PS signalling in-band (its 2-bit profile field only spells the core AOT), so explicit HE-AAC decode wants the MP4 `esds` ASC; ADTS round-trips AAC-LC cleanly. See [`containers/mp4`](../../containers/mp4) and [`containers/adts`](../../containers/adts) for the full container stories.

## Examples

Standalone runnable programs in [`examples/`](examples) (each builds in the default build and prints a clear "rebuild with `-tags aacfdk`" message when the FDK engine is fenced out):

- [`decode/`](examples/decode) — decode an `.m4a` (MP4) or `.aac` (ADTS) stream to interleaved float64 PCM.
- [`encode/`](examples/encode) — encode PCM to a playable `.aac` (ADTS) file; `-heaac` selects HE-AAC v1 (SBR) via `WithObjectType`.
- [`heaac/`](examples/heaac) — HE-AAC v2 (AOT-29) encode-then-decode of a stereo signal, showing the PS mono-core → stereo upmix, the `Output()` rate/channel projection, and the `ErrPSRequiresStereo` guard.
- [`roundtrip/`](examples/roundtrip) — encode a tone to access units and decode them back through the codec layer.

Plus [`containers/mp4/examples/readdecode/`](../../containers/mp4/examples/readdecode) (read an `.m4a`'s tags, decode its AAC via this package).

## API

### Decoder — `codec.Decoder`

- `NewDecoder(pr PacketReader, asc libraries/aac.AudioSpecificConfig, opts ...DecoderOption) (codec.Decoder, error)`
- `Read(out []float64) (mutations.Audio, error)` — fills `out` with interleaved float64 in `[-1.0, 1.0]`; loop until `io.EOF`. The wrapper buffers one decoded frame and carries leftover samples across calls, so `out` may be any size.
- `SampleRate() int`, `Channels() int` — taken from the ASC / underlying decoder.

### Encoder — `codec.Encoder` (requires `-tags aacfdk`)

- `NewEncoder(pw PacketWriter, sampleRate, channels int, opts ...EncoderOption) (codec.Encoder, error)`
- `Write(audio mutations.Audio) (int, error)` — the `audio` SampleRate/Channels must match the encoder; returns `ErrFormatMismatch` otherwise. Buffers through a `StreamChunker`; emits one access unit per full frame.
- `Close() error` — flushes the final, silence-padded frame (the `PacketWriter` is not closed).

### Packet I/O

- `PacketReader` / `PacketWriter` — the framing seam (`ReadPacket() ([]byte, error)` / `WritePacket(data []byte) error`).
- `PacketReaderFunc` / `PacketWriterFunc` — function adapters.
- `NewSlicePacketReader(packets [][]byte) PacketReader` — replays a slice of access units, `io.EOF` when exhausted.

### Options

- Decoder: `DecoderOption` — reserved for future decode-time options (e.g. SBR downsample mode).
- Encoder: `WithObjectType(libraries/aac.AudioObjectType)` (default `AOTAACLC`; `AOTSBR` = HE-AAC v1, `AOTPS` = HE-AAC v2), `WithBitrate(bps int)` (default 128000, CBR). VBR (`WithVBR`) is exposed at the [`libraries/aac`](../../libraries/aac) layer; this streaming wrapper drives CBR.

### Errors

- `ErrFormatMismatch` — the `mutations.Audio` passed to `Write` disagrees with the SampleRate/Channels the encoder was constructed with.
- `ErrBadConfig` — the `AudioSpecificConfig` passed to `NewDecoder` is missing or describes an unsupported stream.

Backend errors surface from [`libraries/aac`](../../libraries/aac) unchanged — notably `ErrEngineRequiresFDK` (any encode/decode requested without `-tags aacfdk`), and, for any AAC profile/coding tool the pure-Go port does not yet cover, `ErrUnimplemented`.

## License

This package is **MIT**: the `codec/aac` adapter and the `containers/mp4` box parser link no FDK-AAC code and compile in the default build. The encode/decode paths they can reach (via `libraries/aac` under `-tags aacfdk`) pull in the **Fraunhofer FDK-AAC** engine (SPDX `FDK-AAC`: permissive, non-copyleft, but non-FOSS); a default build of `codec/aac` links no FDK-AAC code and surfaces `ErrEngineRequiresFDK` at use. AAC-LC patents expired in 2017. See [`LICENSING.md`](../../LICENSING.md) for the full file-by-file / build-tag license map, and [`BENCHMARKS.md`](../../BENCHMARKS.md) for native-vs-cgo throughput — notably AAC **encode**, where the pure-Go FDK port runs *faster* than the production C reference.
