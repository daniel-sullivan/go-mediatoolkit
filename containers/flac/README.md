# containers/flac

Pure-Go reader/writer for the **native FLAC stream** container — the `fLaC`
magic + metadata-block chain that frames FLAC audio frames. This is the
container face of FLAC, mirroring `containers/wav`, `containers/ogg`,
`containers/mp4`, and `containers/mp3`: it parses the metadata chain, populates a
uniform [`containers.Header`], projects VORBIS_COMMENT onto
[`containers.StandardTags`], and exposes the whole byte stream so it can be piped
into the FLAC decoder.

FLAC is **self-framed**, like MP3: the `fLaC` magic, the metadata blocks, and the
audio frames all live in one continuous byte stream, so the audio framing belongs
to the codec, not to this layer. The split is therefore: **the codec
([`libraries/flac`] / [`codec/flac`]) owns the sample bytes** (the LPC residual,
Rice coding, frame headers), and **the container parses the leading metadata
blocks and projects tags**. Unlike a re-muxing container, this layer adds **no
bytes of its own** — the `Reader` hands back the original stream verbatim, and the
`Writer` delegates every byte (magic, STREAMINFO, VORBIS_COMMENT, frames) to the
encoder.

The FLAC bitstream engine, the cgo-vs-native routing (libFLAC under `cgo`, a
bit-exact pure-Go 1:1 port otherwise), and the parity gate all live in
[`libraries/flac`](../../libraries/flac) — **read
[`libraries/flac/README.md`](../../libraries/flac/README.md) for the backend
table, build tags, and parity discipline.** This README covers only the metadata
seam. For Ogg-encapsulated FLAC, see
[`containers/ogg.NewFLACReader`](../ogg).

## Layering

| Package | Works with | Use for |
|---|---|---|
| `libraries/flac` | `int32` samples ↔ FLAC byte stream | direct frame-by-frame encode/decode, lowest overhead |
| `codec/flac` | `float64` samples (`mutations.Audio`) ↔ FLAC byte stream | streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention) |
| **`containers/flac`** | **STREAMINFO + VORBIS_COMMENT + tag projection** | **inspecting/writing native FLAC: stream info, tags, metadata blocks** |

`Reader.Data()` is a byte `io.Reader` you hand straight to
[`codec/flac.NewDecoder`] (or [`libraries/flac.NewDecoder`]); the `Writer` wraps
a `libraries/flac.Encoder` and projects `Header.Tags` onto its VORBIS_COMMENT.

## FLAC stream structure

A native FLAC stream (RFC 9639) is the four-byte magic `fLaC`, then a chain of
length-prefixed **metadata blocks** (a 1-byte type + `is_last` flag, a 24-bit
big-endian body length, then the body), then the audio frames:

```
fLaC                          4-byte magic
metadata blocks:
├─ STREAMINFO   (type 0)      mandatory, first: block/frame size bounds, sample rate,
│                             channels, bits/sample, total samples, MD5         (metadata.go)
├─ VORBIS_COMMENT (type 4)    vendor string + KEY=value tags → StandardTags     (metadata.go)
├─ SEEKTABLE    (type 3)      seek points (placeholders dropped)                (metadata.go)
├─ APPLICATION  (type 2)      registered-id payloads → Extras.Application
├─ PICTURE      (type 6)      album art → Extras.Pictures (raw bodies)
├─ CUESHEET     (type 5)      → Extras.Cuesheet (raw)
└─ PADDING      (type 1)      → Extras.Padding (byte total)
                              … (last block has is_last set) …
audio frames                  decoded by codec/flac (not parsed here)
```

The reader walks the chain via an `io.TeeReader`, so `Reader.Data()` can replay
the buffered metadata prefix and then continue from the live reader — yielding the
**exact original bytes** with no re-seek. STREAMINFO is mandatory and must come
first (`ErrMissingStreamInfo` otherwise). The block bodies populate
`Header.SampleRate` / `Channels` / `Duration` (from STREAMINFO) and
`Header.Tags` (from VORBIS_COMMENT); the FLAC-only fields stay on `Extras`.

### Metadata projection

VORBIS_COMMENT (`metadata.go`) is parsed into the vendor string (`Extras.Vendor`)
and `KEY=value` entries, which `containers.StandardTagsFromMap` normalises onto
[`containers.StandardTags`] (TITLE/ARTIST/ALBUM/…). The remaining block types are
preserved structurally on `Extras`: `SeekTable []SeekPoint`, `Padding int`,
`Pictures [][]byte` (raw PICTURE bodies), `Application map[[4]byte][]byte`,
`Cuesheet []byte`. On the write side the `Writer` projects `Header.Tags` back into
VORBIS_COMMENT via the encoder's `WithTags` option.

## Seekability

Reading is **streaming**: `NewReader` buffers only the metadata prefix and then
streams frames from the underlying reader, so any `io.Reader` works.
`Reader`/`Writer` are not safe for concurrent use, and `Writer.Close` flushes the
encoder's final frame + trailing metadata but does **not** close the underlying
writer.

## Integration with the FLAC codec

`Reader.Data()` yields the whole native FLAC stream; pass it straight to the
codec:

```go
import (
    "github.com/daniel-sullivan/go-mediatoolkit/codec/flac"
    ctrflac "github.com/daniel-sullivan/go-mediatoolkit/containers/flac"
)

rd, err := ctrflac.NewReader(r)        // parses fLaC + metadata chain
h := rd.Header()                       // SampleRate/Channels + Duration + StandardTags + Extra.StreamInfo
dec, err := flac.NewDecoder(rd.Data()) // rd.Data() replays magic+metadata+frames unchanged
// dec.Read(...) → mutations.Audio (interleaved float64 in [-1.0, 1.0])
```

The decode path uses the **pure-Go** FLAC port by default (no cgo), so this
compiles and runs in a default `CGO_ENABLED=0` build; building with `cgo` routes
through libFLAC instead. On the write side, construct a `Writer` from a populated
`Header` and feed interleaved **int32** samples:

```go
h := ctrflac.Header{SampleRate: 48000, Channels: 1,
    Extra: ctrflac.Extras{StreamInfo: ctrflac.StreamInfo{BitsPerSample: 16}},
    Tags:  containers.StandardTags{Title: new("Tone")}}
w, err := ctrflac.NewWriter(out, h, ctrflac.WithCompressionLevel(5))
err = w.Encode(samples)   // interleaved int32, sign-extended per BitsPerSample
err = w.Close()           // flushes final frame + metadata; out is not closed
```

Runnable examples (all commands run from the repo root, where `go.mod` lives;
each reads a `.flac` file, so **generate one first** with the `codec/flac` encode
example):

```sh
go run ./codec/flac/examples/encode out.flac        # generate a native FLAC file to read
go run ./containers/flac/examples/read out.flac      # parse the metadata chain, print STREAMINFO + tags + block summary
go run ./containers/flac/examples/inspect out.flac   # dump every metadata block's type and size
go run ./containers/flac/examples/decode out.flac    # full read -> codec/flac -> PCM split, reporting peak amplitude (pure-Go)
```

[`examples/read/`](examples/read) (parse the metadata chain, print STREAMINFO +
tags + block summary); [`examples/inspect/`](examples/inspect) (dump every
metadata block's type and size); [`examples/decode/`](examples/decode) (the full
read → `codec/flac` → PCM split, reporting peak amplitude — pure-Go, default
build).

## API

### Reader

- `NewReader(r io.Reader) (*Reader, error)` — parses the `fLaC` magic and metadata chain.
- `Header() Header` — `SampleRate` / `Channels` / `Duration` plus [`StandardTags`] and the FLAC [`Extras`].
- `Data() io.Reader` — the original byte stream (magic + metadata + frames), ready for `codec/flac.NewDecoder`.

### Writer

- `NewWriter(w io.Writer, h Header, opts ...WriterOption) (*Writer, error)` — `Header.SampleRate`/`Channels` required; bit depth from `Header.Extra.StreamInfo.BitsPerSample` (default 16).
- `Encode(samples []int32) error` — interleaved int32 samples (see `libraries/flac.Encoder.Encode` for buffer shape).
- `Header() Header`, `Encoder() libraries/flac.Encoder`, `Close() error` — `Close` flushes the encoder (`w` is not closed).
- Options: `WithCompressionLevel(level int)` (0–8, default 5), `WithVerify(bool)`, `WithBlockSize(samples int)`.

### Types

- `Header = containers.Header[Extras]` — the generic container header specialised to FLAC `Extras`; `Format` is `"flac"`.
- `Extras` — `StreamInfo`, `Vendor`, `SeekTable []SeekPoint`, `Padding int`, `Pictures [][]byte`, `Application map[[4]byte][]byte`, `Cuesheet []byte`.
- `StreamInfo` — `Min/MaxBlockSize`, `Min/MaxFrameSize`, `SampleRate`, `Channels`, `BitsPerSample`, `TotalSamples`, `MD5Signature` (fields match `libraries/flac.StreamInfo`).
- `SeekPoint` — `SampleNumber`, `StreamOffset`, `FrameSamples`.

### Errors

- `ErrNotFLAC` — the stream does not begin with the `fLaC` magic.
- `ErrMissingStreamInfo` — the first metadata block was not STREAMINFO.
- `ErrInvalidMetadata` — a malformed/truncated metadata block.
- `ErrUnsupportedFormat` — the `Header` passed to `NewWriter` does not describe a supportable FLAC stream.
- `ErrAlreadyClosed` — a `Writer` method called after `Close`.
- `ErrBadArg` — a nil destination writer or other invalid argument.

## License

This package is **MIT**: the metadata-chain parser and tag projection link **no
third-party code** and compile in a default build. The decode path runs the
pure-Go FLAC port by default (MIT); building with `cgo` routes encode/decode
through **libFLAC** (BSD-3-Clause) instead. See
[`LICENSING.md`](../../LICENSING.md) for the full license map and
[`libraries/flac/README.md`](../../libraries/flac/README.md) for the backend /
parity details.
