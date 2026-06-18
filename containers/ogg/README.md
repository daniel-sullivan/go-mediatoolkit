# containers/ogg

Pure-Go reader/writer for the **Ogg** container — the page/packet framing that
carries Opus, Vorbis, and FLAC bitstreams (`.opus`, `.ogg`). This is the
container face of the packet codecs, mirroring `containers/mp4` (the other
packet-oriented container) and the byte-stream containers `containers/flac`,
`containers/wav`, and `containers/mp3`.

Ogg is **codec-agnostic framing**: it slices each logical bitstream into
**pages**, and reassembles pages into **packets** — but it knows nothing about
what is inside a packet. So the split is sharp: **the codec owns the sample
bytes** (Opus/Vorbis/FLAC encode each packet's audio), and **the container
frames those packets, demultiplexes logical streams, and carries the
codec's header packets** (which is where the tags live). The generic
[`Reader`]/[`Writer`] handle only pages + packets; the codec-specific helpers
([`OpusReader`]/[`NewOpusWriter`], [`FLACReader`]/[`NewFLACWriter`]) parse the
codec's BOS header packets to fill in sample rate, channels, and tags.

The Ogg page/packet state machine lives in [`libraries/ogg`](../../libraries/ogg)
(a pure-Go port of libogg's `Sync`/`Decoder`/`Encoder`); this package wraps it
in the uniform container API. It links no C and compiles in a default
`CGO_ENABLED=0` build.

## Layering

| Package | Works with | Use for |
|---|---|---|
| `libraries/ogg` | raw pages ↔ packets (`Sync`/`Decoder`/`Encoder`) | low-level page demux/mux |
| **`containers/ogg`** (generic) | logical streams ↔ `containers.PacketReader`/`PacketWriter` | codec-agnostic packet demux/mux |
| **`containers/ogg`** (Opus / FLAC helpers) | a fully-populated `Header` + codec packets | reading/writing `.opus` / Ogg-FLAC with tags |
| `codec/opus`, `codec/flac` | `float64` samples ↔ packets / byte stream | the streaming `codec.Decoder`/`codec.Encoder` seam |

The generic `Reader.Stream(serial).Packets()` is a [`containers.PacketReader`];
each `StreamWriter` is a [`containers.PacketWriter`] — structurally compatible
with the `codec/opus` `PacketReader`/`PacketWriter` interfaces, so a stream can
be piped straight into `codec/opus.NewDecoder` / `NewEncoder`.

## Ogg page/packet structure

An Ogg physical bitstream is a sequence of **pages**. Each page belongs to one
**logical bitstream** identified by a 32-bit serial number, carries a granule
position (a codec-defined time stamp), and is flagged **BOS** (beginning) on the
first page of its stream and **EOS** (end) on the last. A page's body is a run
of **segments** whose lacing bytes reassemble into **packets** — a packet may
span multiple pages. Per RFC 3533, every Ogg file opens with a contiguous run of
BOS pages, one per logical stream.

```
physical bitstream  ──►  pages (serial-tagged, granule-stamped, BOS/EOS flags)
pages               ──►  packets (lacing-reassembled, may span pages)
packets (stream 0)  ──►  codec packets:  [BOS header(s)]  [audio packets…]
```

The [`Reader`] demultiplexes by serial number: `NewReader` drains pages until the
first non-BOS page (so every stream's first packet is queued), then exposes one
[`Stream`] per logical bitstream. A best-effort `CodecHint` (`"opus"`,
`"vorbis"`, `"flac"`) is sniffed from each BOS packet. The [`Writer`] reverses
this: `AddStream(serial)` registers a logical stream whose `WritePacket` calls
are laced into pages, with BOS stamped on the first packet and EOS on the last
(buffered one packet ahead so the final packet can be flagged at `Close`).

## Opus-in-Ogg (`OpusReader` / `OpusWriter`)

`opus.go` adds the Opus mapping (RFC 7845): the first logical stream's two BOS
header packets are **OpusHead** (`OpusHead` magic + version, channel count,
pre-skip, input sample rate, output gain, channel-mapping table) and
**OpusTags** (vendor string + Vorbis-comment entries). `NewOpusReader` parses
both, fills the uniform `Header` (sample rate defaults to 48000 — Opus always
decodes at 48 kHz — channels from `OpusHead`, tags from `OpusTags`), and exposes
the audio packets through a [`containers.PacketReader`]:

```go
import (
    "go-mediatoolkit/codec/opus"
    "go-mediatoolkit/containers/ogg"
)

rd, err := ogg.NewOpusReader(r)              // parses OpusHead + OpusTags
h := rd.Header()                             // SampleRate/Channels + StandardTags + Extra.Head
dec, err := opus.NewDecoder(rd, h.SampleRate, h.Channels)  // rd is a PacketReader
// dec.Read(...) → mutations.Audio (interleaved float64 in [-1.0, 1.0])
```

`NewOpusWriter` writes OpusHead + OpusTags (each forced onto its own page, per
RFC 7845), then accepts encoded packets and keeps the 48 kHz granule counter
accurate (`WritePacket` assumes a 20 ms / 960-sample frame; use
`WritePacketWithFrames` for other durations). A `codec/opus.Encoder` satisfies
the writer's `PacketWriter` shape, so the codec produces packets and the Ogg
writer frames them:

```go
ow, err := ogg.NewOpusWriter(out, channels, ogg.WithOpusTags(tags))
enc, err := opus.NewEncoder(ow, 48000, channels, opus.WithBitrate(64000))
_, err = enc.Write(input); err = enc.Close(); err = ow.Close()
```

Options: `WithOpusSerialNo`, `WithOpusPreSkip`, `WithOpusInputSampleRate`,
`WithOpusOutputGain`, `WithOpusVendor`, `WithOpusTags`.

## FLAC-in-Ogg (`FLACReader` / `FLACWriter`)

`flac.go` adds the xiph "Ogg encapsulation for FLAC" mapping: the BOS packet is a
`0x7F"FLAC"` mapping header (version, `numOtherHeaders`, the native `fLaC` magic,
and the 34-byte STREAMINFO), followed by one metadata block per packet, then one
FLAC frame per packet. `NewFLACReader` parses the mapping header and metadata
packets (projecting VORBIS_COMMENT onto `StandardTags`), then `Data()` rebuilds a
**synthetic native FLAC byte stream** — re-prepending `fLaC` + the metadata
blocks (with `is_last` correctly stamped) ahead of the concatenated frames — so
it can be handed straight to [`libraries/flac.NewDecoder`] (or
[`codec/flac.NewDecoder`]). The `FLACWriter` wraps a `libraries/flac.Encoder`,
splits its native output back into the BOS/metadata/frame packets (locating
frame boundaries by scanning for the FLAC sync code and validating each via the
mandated CRC-16 footer), and keeps the granule position accurate per frame.

## Generic (codec-agnostic) demux/mux

For non-audio or unknown codecs, use the generic [`Reader`]/[`Writer`] directly:
`Reader.Streams()` lists logical streams, `Stream.Packets()` iterates a stream's
packets, and the container-level `Header` leaves `SampleRate`/`Channels` zero
(only a codec helper can fill them). On write, `Writer.AddStream(serial)` returns
a `StreamWriter`; `SetGranule`/`SetEOS` control the per-page granule and the EOS
flag.

Runnable examples (all commands run from the repo root, where `go.mod` lives;
each takes a file-path argument, so **write a file first** and read/inspect that
same file):

```sh
go run ./containers/ogg/examples/write /tmp/tone.opus     # encode a tone through codec/opus, mux into Ogg
go run ./containers/ogg/examples/read /tmp/tone.opus      # print header + tags + per-stream summary, decode via codec/opus
go run ./containers/ogg/examples/inspect /tmp/tone.opus   # generic demux — streams, codec hint, header/data packet counts
```

[`examples/write/`](examples/write) (encode a tone through `codec/opus` and mux
the packets into Ogg); [`examples/read/`](examples/read) (open an `.opus`, print
header + tags + per-stream summary, decode via `codec/opus`);
[`examples/inspect/`](examples/inspect) (generic demux — list every logical
stream, its codec hint, header packets, and packet count, no codec engine
required).

## API

### Reader (generic)

- `NewReader(r io.Reader) (*Reader, error)` — demultiplexes BOS pages into logical streams.
- `Header() Header` — container-level header (`SampleRate`/`Channels` zero; `Extra.Streams` lists each stream).
- `Streams() []*Stream` / `Stream(serialNo int32) *Stream` — the logical streams.
- `Stream.Packets() containers.PacketReader` / `Stream.ReadPacket() ([]byte, error)` — packet iteration.

### Writer (generic)

- `NewWriter(w io.Writer) *Writer` / `AddStream(serialNo int32) (*StreamWriter, error)` / `Streams()` / `Close()`.
- `StreamWriter.WritePacket([]byte) error`, `SetGranule(int64)`, `SetEOS()` — BOS auto-stamped on the first packet; EOS applied to the last at `Close`.

### Opus helpers

- `NewOpusReader(r io.Reader) (*OpusReader, error)`, `Header() OpusHeader`, `ReadPacket() ([]byte, error)`.
- `NewOpusWriter(w io.Writer, channels int, opts ...OpusWriterOption) (*OpusWriter, error)`, `WritePacket`, `WritePacketWithFrames`, `Close`.

### FLAC helpers

- `NewFLACReader(r io.Reader) (*FLACReader, error)`, `Header() FLACHeader`, `Data() io.Reader` (synthetic native FLAC stream for `libraries/flac.NewDecoder`).
- `NewFLACWriter(w io.Writer, sampleRate, channels int, opts ...FLACWriterOption) (*FLACWriter, error)`, `Encode([]int32)`, `Close`.

### Types

- `Header = containers.Header[Extras]`; `Extras.Streams []StreamInfo` (`SerialNo`, `CodecHint`, `HeaderPackets`).
- `OpusHeader = containers.Header[OpusExtras]` (`Head OpusHead`, `Vendor`, `SerialNo`).
- `FLACHeader = containers.Header[FLACExtras]` (`Head FLACHead`, `Vendor`, `SerialNo`, `MetadataBlocks`).

### Errors

- `ErrNoStreams` — no logical streams found.
- `ErrUnknownStream` — `AddStream` serial collision / unknown serial.
- `ErrAlreadyClosed` — a writer method called after `Close`.
- `ErrNoOpusStream` / `ErrBadOpusHead` / `ErrBadOpusTags` — Opus mapping not found / malformed headers.
- `ErrNoFLACStream` / `ErrBadFLACHead` / `ErrBadFLACMetadata` — FLAC mapping not found / malformed mapping header or metadata block.

## License

This package is **MIT**: the Ogg page/packet machinery (`libraries/ogg`) and the
Opus/FLAC mapping parsers link **no third-party code** and compile in a default
build. Decoding the packets a `Reader` yields goes through `codec/opus` /
`codec/flac`; the FLAC write path reaches `libraries/flac` (cgo libFLAC or the
pure-Go port). See [`LICENSING.md`](../../LICENSING.md) for the full license map.
