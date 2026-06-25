# containers/mp4

Pure-Go reader/writer for the **ISO Base Media File Format** (ISOBMFF / MP4) — the framing used by `.m4a` and `.mp4` audio files. This is the container face of AAC, mirroring `containers/flac`, `containers/ogg`, and `containers/wav`: it parses the box tree, recovers the AAC [`AudioSpecificConfig`] and sample tables, slices the media data into AAC access units, and projects iTunes metadata onto `containers.StandardTags`.

The container layer is **pure Go and adds no bytes of its own to the audio**: it only locates the existing AAC access units and hands them out — re-framing nothing. The AAC bitstream engine, the cgo-vs-native routing, the `aacfdk` / `aac_strict` build tags, the FDK license fence, and the parity gate all live in [`libraries/aac`](../../libraries/aac); the streaming float64 seam lives in [`codec/aac`](../../codec/aac). **This README covers only the ISOBMFF / metadata layer.**

## Layering

| Package | Works with | Use for |
|---|---|---|
| `libraries/aac` | `float64` samples ↔ AAC access units (packets) | direct per-packet encode/decode, lowest overhead |
| `codec/aac` | `float64` samples (`mutations.Audio`) ↔ packets via `PacketReader`/`PacketWriter` | streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention) |
| **`containers/mp4`** | **ISOBMFF box tree + iTunes tag projection** | **reading/writing `.m4a`/`.mp4`: `esds` ASC, sample tables, `ilst` metadata** |

The `containers/mp4` ↔ `codec/aac` split is identical to `containers/ogg` ↔ `codec/opus`: the AAC library is packet-oriented, so framing and tags belong to this container, not to the codec. `Reader.Packets()` is a `codec/aac.PacketReader`; `Writer` is a `codec/aac.PacketWriter`.

This package compiles in a **default build** and links **no FDK-AAC code** — it is MIT/untagged. Decoding the access units it yields, or encoding new ones via `Writer.WriteAudio`, goes through the FDK-AAC engine and so requires `-tags aacfdk`; see [License](#license).

## ISOBMFF box structure

An MP4 file is a tree of length-prefixed **boxes** (atoms): a 32-bit big-endian size, a four-byte type, then a body that may itself contain child boxes (`size == 1` selects a 64-bit largesize; `size == 0` runs to the end of the enclosing body). The box reader (`box.go`) walks that tree with zero C code. The relevant hierarchy for an audio `.m4a` is:

```
ftyp                              file-type / brand box ("M4A ", "mp42", …)
moov                              movie (metadata) box
├─ trak → mdia                    audio track
│  ├─ mdhd                        media timescale + duration (v0 32-bit / v1 64-bit)
│  └─ minf → stbl                 sample table box
│     ├─ stsd → mp4a → esds       sample description → AudioSpecificConfig
│     ├─ stsz                     sample-size table (per-AU byte lengths)
│     ├─ stsc                     sample-to-chunk runs
│     ├─ stco / co64              chunk-offset table (32-bit / 64-bit)
│     └─ stts                     time-to-sample (decoding-time deltas → duration)
└─ udta → meta → ilst            iTunes metadata item list
mdat                              media-data box (the raw AAC access units)
```

`Reader` requires a leading `ftyp` (non-MP4 input is rejected as `ErrNotMP4`) and a `moov`; it walks the first `trak` whose `stbl` carries an `mp4a` sample entry.

### `esds` → AudioSpecificConfig

The `esds` box (`esds.go`) is a chain of MPEG-4 descriptors (ISO/IEC 14496-1): an `ES_Descriptor` (tag `0x03`) → `DecoderConfigDescriptor` (`0x04`) → `DecoderSpecificInfo` (`0x05`), the last carrying the raw **AudioSpecificConfig** bytes (ISO/IEC 14496-3 §1.6.2.1). The ASC bit string is decoded into the typed [`libraries/aac.AudioSpecificConfig`]:

- `audioObjectType` — 5 bits (escape 31 → +6 bits) → the AAC profile (`AOTAACLC`, `AOTSBR`, …).
- `samplingFrequencyIndex` — 4 bits, indexing the MPEG-4 sampling-frequency table (15 → an explicit 24-bit frequency).
- `channelConfiguration` — 4 bits → channel count.

The verbatim ASC bytes are preserved in `Config.Raw` so a re-muxer can copy them byte-for-byte (the `esds` writer emits `Raw` when present, otherwise packs the standard AAC-LC two-byte form).

### Sample tables (locating access units)

The `stbl` sub-tables (`sampletable.go`) together address each AAC access unit inside `mdat`:

- **`stsz`** — the byte length of each sample (access unit). A single constant `sampleSize` is expanded to one entry per sample for uniform indexing.
- **`stsc`** — sample-to-chunk runs: each run gives `samplesPerChunk` from its `firstChunk` until the next run begins.
- **`stco` / `co64`** — the absolute file offset of each chunk's first sample (the 64-bit `co64` variant is widened into the same slice).

`resolveSampleOffsets` expands these into one absolute `(offset, size)` per sample — each chunk's samples laid out contiguously from the chunk offset — and `sliceAccessUnits` cuts `mdat` accordingly. The decoded tables are exposed on `Extras.SampleTable` (`SampleSizes`, `ChunkOffsets`, `SampleToChunk`).

### `ilst` tag projection

iTunes metadata lives at `moov → udta → meta → ilst` (`meta` is a FullBox, so its 4-byte version+flags prefix is skipped). Each `ilst` item atom's name is its box type and its value is the first nested `data` atom (`type(4) | locale(4) | value`). `metadata.go` projects the well-known `©`-prefixed atoms onto `containers.StandardTags`:

| ilst atom | Standard tag | | ilst atom | Standard tag |
|---|---|---|---|---|
| `©nam` | TITLE | | `©wrt` | COMPOSER |
| `©ART` | ARTIST | | `cprt` | COPYRIGHT |
| `aART` | ALBUMARTIST | | `©too` | ENCODER |
| `©alb` | ALBUM | | `desc` | DESCRIPTION |
| `©day` | DATE | | `trkn` | TRACKNUMBER (binary `reserved/track/total`) |
| `©gen` / `gnre` | GENRE | | `covr` | → `Extras.CoverArt` |
| `©cmt` | COMMENT | | *(unmapped)* | → `Extras.FreeformTags` |

Atoms with no standard mapping are preserved verbatim in `Extras.FreeformTags` (keyed by atom name); `covr` artwork bodies are preserved in `Extras.CoverArt`. The writer (`buildIlst`) reverses the projection, emitting a UTF-8 `data` atom per standard tag, the binary 8-byte form for `trkn`, and a `covr` atom per image (JPEG/PNG sniffed from the magic bytes).

## Seekability

MP4 parsing is **random-access**, not streaming: chunk offsets in `stco`/`co64` are absolute file positions, so `NewReader` buffers the **entire stream into memory** (`io.ReadAll`) rather than streaming it. A `Reader` is therefore self-contained once constructed and needs no `io.Seeker`; it exposes the access units as an in-memory slice (`AccessUnits()`) and as a streaming `PacketReader` (`Packets()`). The `Writer` likewise buffers all encoded access units and emits the whole file in `Close`, because the `moov` sample/offset tables can only be written once every access unit's size and the `mdat` layout are known. A `Reader`/`Writer` is not safe for concurrent use.

## Integration with `codec/aac`

`Reader.Packets()` yields a `codec/aac.PacketReader` over the access units in decode order; `Header().Extra.Config` is the parsed `esds` `AudioSpecificConfig`. Feed both straight into `codec/aac.NewDecoder`:

```go
import (
    "github.com/daniel-sullivan/go-mediatoolkit/containers/mp4"
    aaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/aac"
)

rd, err := mp4.NewReader(r)        // parses the ISOBMFF box tree; buffers the file
hdr := rd.Header()                 // SampleRate/Channels + Duration + StandardTags + Extra
dec, err := aaccodec.NewDecoder(rd.Packets(), hdr.Extra.Config)
// dec.Read(...) → mutations.Audio (interleaved float64 in [-1.0, 1.0])
// NewDecoder/Read reach the FDK-AAC engine → require -tags aacfdk.
```

On the write side, `Writer` wraps a `codec/aac` streaming encoder: `WriteAudio` takes interleaved float64 PCM (any sample count), and `Close` assembles the box tree and projects `Header.Tags` onto `ilst`. A pure **re-mux** that only calls `WritePacket` (copying existing access units byte-for-byte) needs no AAC engine and stays in a default build; `WriteAudio` constructs the encoder lazily and is the only path that requires `-tags aacfdk`.

```go
h := mp4.Header{SampleRate: 44100, Channels: 2,
    Tags: containers.StandardTags{Title: new("Song"), Artist: new("Artist")}}
w, err := mp4.NewWriter(out, h, mp4.WithBitrate(128000))
err = w.WriteAudio(pcm)   // float64 PCM → AAC (requires -tags aacfdk)
err = w.Close()           // assembles ftyp/moov/mdat; out is not closed
```

Runnable example: [`examples/readdecode/`](examples/readdecode) (read an `.m4a`'s tags + decode its AAC via `codec/aac`); [`examples/muxinspect/`](examples/muxinspect) (mux PCM → `.m4a` then re-open and inspect).

## API

### Reader

- `NewReader(r io.Reader) (*Reader, error)` — buffers and parses the whole MP4 stream.
- `Header() Header` — `SampleRate` / `Channels` / `Duration` plus [`StandardTags`] and the MP4 [`Extras`].
- `Packets() *PacketReader` — streaming `codec/aac.PacketReader` over the AAC access units (decode order).
- `AccessUnits() [][]byte` — the same access units as a slice, for random access.

### Writer

- `NewWriter(w io.Writer, h Header, opts ...WriterOption) (*Writer, error)` — `Header.SampleRate`/`Channels` required; encoder constructed lazily on first `WriteAudio`.
- `WriteAudio(samples []float64) error` — interleaved float64 PCM, encoded to AAC (reaches the FDK engine; needs `-tags aacfdk`).
- `WritePacket(pkt []byte) error` — append a pre-encoded AAC access unit verbatim (pure re-mux; no engine).
- `Header() Header`, `Close() error` — `Close` assembles and writes the whole file (`w` is not closed).
- Options: `WithBitrate(bps int)` (default 128000), `WithObjectType(libraries/aac.AudioObjectType)` (default AAC-LC).

### Types

- `Header = containers.Header[Extras]` — the generic container header specialised to MP4 `Extras`; `Format` is `FormatM4A` (`"mp4"`).
- `Extras` — `MajorBrand`, `CompatibleBrands`, `Config` (`AudioSpecificConfig`, incl. `Raw`), `SampleTable`, `FreeformTags map[string][]string`, `CoverArt [][]byte`.
- `SampleTable` — `SampleSizes []uint32`, `ChunkOffsets []uint64`, `SampleToChunk []SampleToChunkEntry`.
- `BoxType [4]byte` — a four-character-code atom identifier; `BoxFtyp`, `BoxMoov`, `BoxMdat`, `BoxEsds`, `BoxStsz`, `BoxStsc`, `BoxStco`, `BoxIlst`, `BoxCo64`, `BoxStts` are predeclared.

### Errors

- `ErrNotMP4` — no leading `ftyp` box, or a corrupt initial box header.
- `ErrInvalidBox` — a malformed box header/body (size smaller than its header, body overrunning its parent).
- `ErrMissingMoov` — no `moov` box.
- `ErrMissingEsds` — the audio sample entry has no `esds` (no recoverable `AudioSpecificConfig`).
- `ErrInvalidSampleTable` — inconsistent or truncated `stsz` / `stsc` / `stco` tables.
- `ErrUnsupportedCodec` — the track's sample entry is not AAC (`mp4a`).
- `ErrBadArg` — nil reader/writer or other invalid argument.
- `ErrAlreadyClosed` — a `Writer` method called after `Close`.
- `ErrStreamTooLong` — the muxed track duration overflows the 32-bit `mdhd`/`stts` box fields.
- `ErrOffsetTooLarge` — the first access unit lands beyond the 32-bit `stco` range (a `co64` widening would be required).

## License

This package is **MIT**: the ISOBMFF box parser, sample-table decoder, `esds`/ASC parser, and `ilst` tag projection link **no FDK-AAC code** and compile in a default build. Decoding the access units it yields (`codec/aac.NewDecoder`) or encoding new ones (`Writer.WriteAudio`) reaches the **Fraunhofer FDK-AAC** engine via `libraries/aac`, which is fenced behind the opt-in `aacfdk` build tag (SPDX `FDK-AAC`: permissive, non-copyleft, non-FOSS); without that tag those paths surface `libraries/aac.ErrEngineRequiresFDK`, and a pure metadata/re-mux build links no FDK-AAC code. AAC-LC patents expired in 2017. See [`LICENSING.md`](../../LICENSING.md) for the full file-by-file / build-tag license map.
