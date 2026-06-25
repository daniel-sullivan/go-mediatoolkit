# containers/adts

Pure-Go reader/writer for **ADTS** (Audio Data Transport Stream) — the per-frame framing used by standalone `.aac` files and broadcast AAC. This is one of the two container faces of AAC (the other being `containers/mp4` for `.m4a`/`.mp4`): it adds a 7-byte header (9 with CRC) in front of each raw AAC access unit and carries the decoder configuration *inline in every frame header* rather than out-of-band.

The container layer is **pure Go and adds no audio bytes of its own**: it only wraps/locates the existing AAC access units. The AAC bitstream engine, the cgo-vs-native routing, the `aacfdk` / `aac_strict` build tags, the FDK license fence, and the parity gate all live in [`libraries/aac`](../../libraries/aac); the streaming float64 seam lives in [`codec/aac`](../../codec/aac). **This README covers only the ADTS framing layer.**

## Codec vs. container

ADTS is a pure **framing** container: each AAC access unit is prefixed with a fixed+variable header that re-states the sync pattern, profile, sample-rate index, channel configuration, and the total frame length. It performs **no compression of its own** — exactly as `containers/ogg` frames Opus packets into pages, ADTS frames AAC access units. This is the codec-vs-container split mandated by the `add-audio-format` skill: framing belongs to the container, the bitstream to `libraries/aac`, and the float64 streaming seam to `codec/aac`.

| Package | Works with | Use for |
|---|---|---|
| `libraries/aac` | `float64` samples ↔ AAC access units (packets) | direct per-packet encode/decode, lowest overhead |
| `codec/aac` | `float64` samples (`mutations.Audio`) ↔ packets via `PacketReader`/`PacketWriter` | streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention) |
| **`containers/adts`** | **per-frame ADTS headers over raw AAC access units** | **reading/writing standalone `.aac` / broadcast streams** |

The `containers/adts` ↔ `codec/aac` split is identical to `containers/ogg` ↔ `codec/opus`: the AAC library is packet-oriented, so framing belongs to this container. `Reader` is a `codec/aac.PacketReader`; `Writer` is a `codec/aac.PacketWriter`.

This package compiles in a **default build** and links **no FDK-AAC code** — it is MIT/untagged. Decoding the access units it yields goes through the FDK-AAC engine and so requires `-tags aacfdk`; see [License](#license).

## ADTS frame structure

Every frame begins with a header (ISO/IEC 13818-7 / 14496-3). The fixed header (the first 28 bits) repeats every frame; the variable header carries the per-frame length and buffer fullness:

```
byte 0   1111 1111                              syncword high 8 bits (0xFF)
byte 1   1111  ID  LL  P                        sync low 4 | MPEG ver(1) | layer(2)=00 | protection_absent(1)
byte 2   PP FFFF R C                            profile(2) | samplingFrequencyIndex(4) | private(1) | chan_hi(1)
byte 3   CC OO H YY  LL                         chan_lo(2) | orig/home/copyright(4) | frame_length high 2
byte 4   LLLL LLLL                              frame_length middle 8
byte 5   LLL  BBBBB                             frame_length low 3 | buffer_fullness high 5
byte 6   BBBBBB  NN                             buffer_fullness low 6 | num_raw_data_blocks(2)
[byte 7..8   CRC]                               present iff protection_absent == 0
```

- **syncword** — 12 bits, always `0xFFF`; the resync anchor.
- **profile** — 2 bits, the `AudioObjectType` minus one (`1` ⇒ AAC-LC).
- **samplingFrequencyIndex** — 4 bits, indexing the MPEG-4 sampling-frequency table (same table as `containers/mp4`).
- **channelConfiguration** — 3 bits, the channel count (`1`..`7`; `7` ⇒ 7.1 / eight channels).
- **aac_frame_length** — 13 bits, header + CRC + payload in bytes (≤ 8191).
- **protection_absent** — when `0`, a 2-byte CRC follows the fixed header.

A canonical 44.1 kHz stereo AAC-LC frame header (no CRC) opens `FF F1 50 80 …`; with CRC the second byte's low bit clears to `FF F0 …` and the header grows to 9 bytes.

### Deriving the AudioSpecificConfig

Unlike MP4 (which carries an `esds` `AudioSpecificConfig` out-of-band), ADTS re-states the config in **every** frame header. `Reader` projects the **first** header onto an [`libraries/aac.AudioSpecificConfig`] (`AOT = profile + 1`, the resolved sample rate, the channel count) and freshly packs the two-byte AAC-LC `Raw` ASC — so callers can feed `codec/aac` exactly as they would with an MP4 `esds`, or re-mux an ADTS stream into MP4 byte-for-byte.

## Resync & seekability

ADTS parsing is **streaming**, not random-access: a `Reader` walks frames front-to-back over any `io.Reader` (buffering only what `bufio.Reader` needs). On corrupt or mis-aligned input it **resyncs** — scanning forward over non-syncword bytes (up to a 64 KiB window) and re-validating each candidate by parsing the fixed header — so leading junk, inter-frame padding, or a torn frame does not derail the stream. The optional CRC is recognised (header sized 9 instead of 7) and stripped along with the header; `Reader` does not currently *verify* the CRC. A `Reader`/`Writer` is not safe for concurrent use.

## Integration with `codec/aac`

`Reader` is itself a `codec/aac.PacketReader`, and `Reader.ASC()` returns the config derived from the first frame — so an ADTS stream pipes straight into `codec/aac.NewDecoder` with **no separate config record**:

```go
import (
    "github.com/daniel-sullivan/go-mediatoolkit/containers/adts"
    aaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/aac"
)

rd, err := adts.NewReader(r)       // parses the first frame; resyncs as needed
hdr := rd.Header()                 // SampleRate/Channels + the derived AudioSpecificConfig
dec, err := aaccodec.NewDecoder(rd, rd.ASC())
// dec.Read(...) → mutations.Audio (interleaved float64 in [-1.0, 1.0])
// NewDecoder/Read reach the FDK-AAC engine → require -tags aacfdk.
```

On the write side, `Writer` is a `codec/aac.PacketWriter`: point a `codec/aac` streaming **encoder** at a `Writer` to emit a standalone `.aac` stream. Framing pre-encoded access units (a pure re-mux, e.g. MP4 → ADTS) needs **no AAC engine** and stays in a default build:

```go
w, err := adts.NewWriter(out, 44100, 2, adts.WithObjectType(aaclib.AOTAACLC))
err = w.WritePacket(au)            // wrap one raw AAC access unit in an ADTS header
```

Runnable examples: [`examples/read/`](examples/read) (parse an `.aac` stream → frames + derived config); [`examples/write/`](examples/write) (wrap raw AAC AUs in ADTS, round-trip); [`examples/demux/`](examples/demux) (ADTS → `codec/aac` decode to PCM, needs `-tags aacfdk`).

## API

### Reader

- `NewReader(r io.Reader) (*Reader, error)` — wraps `r`, parses the first frame header (resyncing to the syncword), and populates `Header`/`ASC` eagerly; the first frame is buffered, not lost.
- `Header() Header` — `Format` (`"adts"`), `SampleRate` / `Channels`, and the ADTS [`Extras`]; `Extra.Frames` updates as the stream is read.
- `ASC() libraries/aac.AudioSpecificConfig` — the config derived from the first frame, ready for `codec/aac.NewDecoder`.
- `ReadPacket() ([]byte, error)` — the next AAC access unit (header + CRC stripped); resyncs past garbage; `io.EOF` when exhausted. Implements `codec/aac.PacketReader`.
- `AccessUnits() ([][]byte, error)` — drain the rest of the stream into a slice of access units.

### Writer

- `NewWriter(w io.Writer, sampleRate, channels int, opts ...WriterOption) (*Writer, error)` — `sampleRate`/`channels` must map to an MPEG-4 sampling-frequency index and channel configuration.
- `WritePacket(au []byte) error` — wrap one raw AAC access unit in an ADTS header (+ CRC when enabled). Implements `codec/aac.PacketWriter`.
- `Frames() int`, `ASC() libraries/aac.AudioSpecificConfig` — frames written; the config the stream implies (for an out-of-band description / re-mux).
- Options: `WithObjectType(libraries/aac.AudioObjectType)` (default AAC-LC), `WithMPEGVersion(int)` (0 = MPEG-4, default; 1 = MPEG-2), `WithCRC(bool)` (default off).

### Header parsing (low level)

- `ParseHeader(buf []byte) (FrameHeader, error)` — decode an ADTS fixed header from the start of `buf`.
- `EncodeHeader(dst []byte, h FrameHeader, payloadLen int) (int, error)` — serialise a header for a `payloadLen`-byte access unit (CRC bytes are zero placeholders).
- `FrameHeader` — `MPEGVersion`, `Profile`, `SampleRateIndex`, `ChannelConfiguration`, `CRCPresent`, `FrameLength`, `RawDataBlocks`; methods `ObjectType()`, `SampleRate()`, `Channels()`, `HeaderSize()`, `AudioSpecificConfig()`.
- Constants: `HeaderLen` (7), `HeaderLenCRC` (9), `SyncWord` (0xFFF), `MaxFrameLen` (0x1FFF).

### Types

- `Header = containers.Header[Extras]` — the generic container header specialised to ADTS `Extras`; `Format` is `"adts"`.
- `Extras` — `Config` (`AudioSpecificConfig`, incl. `Raw`), `MPEGVersion`, `Profile`, `SampleRateIndex`, `ChannelConfiguration`, `CRCPresent`, `Frames`.

### Errors

- `ErrShortHeader` — a buffer too small to hold a header (7, or 9 with CRC).
- `ErrBadSyncword` — the 0xFFF syncword was absent at the parse position.
- `ErrBadFrameLength` — `aac_frame_length` smaller than its own header, or above the 13-bit max.
- `ErrNoSync` — the resync window was scanned without finding a valid frame header.
- `ErrUnsupportedSampleRate` / `ErrUnsupportedChannels` — a rate/channel count with no ADTS header mapping.
- `ErrPacketTooLarge` — a framed access unit exceeding the 13-bit `aac_frame_length` maximum.
- `ErrBadArg` — a nil reader/writer.

## License

This package is **MIT**: the ADTS header parser/encoder, resync scanner, and `AudioSpecificConfig` projection link **no FDK-AAC code** and compile in a default build. Decoding the access units it yields (`codec/aac.NewDecoder`) reaches the **Fraunhofer FDK-AAC** engine via `libraries/aac`, which is fenced behind the opt-in `aacfdk` build tag; without that tag the decode path surfaces `libraries/aac.ErrEngineRequiresFDK`, and a pure framing/re-mux build links no FDK-AAC code. AAC-LC patents expired in 2017. See [`LICENSING.md`](../../LICENSING.md) for the full file-by-file / build-tag license map.
