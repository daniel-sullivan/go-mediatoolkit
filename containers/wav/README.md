# containers/wav

Pure-Go reader/writer for **RIFF/WAVE** (`.wav`) — the chunked container that
frames raw PCM and carries metadata. This is the container face of PCM,
mirroring `containers/flac`, `containers/ogg`, `containers/mp4`, and
`containers/mp3`: it parses the RIFF chunk tree, populates a uniform
[`containers.Header`], projects LIST/INFO and broadcast-WAV (`bext`) metadata
onto [`containers.StandardTags`], and hands the `data` chunk payload out as a
plain `io.Reader` of PCM bytes.

WAV is **not a compressed codec** — there is no Huffman/transform stage and no
separate bitstream engine. The split is therefore the cleanest in the toolkit:
**the codec ([`codec/pcm`]) owns the sample bytes** (the int16/int24/float32/…
encoding of each sample), and **the container frames those bytes and carries
the tags**. This package adds the RIFF/`fmt`/`data` framing and the metadata
chunks; it links no third-party code and is pure MIT.

## Layering

| Package | Works with | Use for |
|---|---|---|
| `codec/pcm` | `float64` samples ↔ raw PCM bytes (`mutations.Audio`) | sample ↔ byte conversion (the `codec.Decoder`/`codec.Encoder` convention) |
| **`containers/wav`** | **RIFF chunk tree + INFO/bext tag projection** | **reading/writing `.wav`: `fmt` layout, `data` payload, metadata** |

The `containers/wav` ↔ `codec/pcm` split is the byte-stream analogue of
`containers/flac` ↔ `codec/flac`: the WAV layer locates the `data` chunk and
describes its sample format in `Header`; `codec/pcm` turns those bytes into
`float64` samples (and back). `Reader.Data()` is a byte `io.Reader` you feed to
[`pcm.NewDecoder`]; `Writer.Data()` is a byte `io.Writer` a [`pcm.NewEncoder`]
writes into.

## RIFF chunk structure

A WAV file is a `RIFF` chunk whose form type is `WAVE`, containing a sequence of
length-prefixed sub-chunks — each a four-byte ASCII id, a 32-bit little-endian
size, then the body (padded to an even length). The reader (`reader.go`) walks
those chunks sequentially until it reaches `data`:

```
RIFF <size> WAVE
├─ fmt          PCM layout: format tag, channels, sample rate, bits/sample (chunks.go)
├─ LIST INFO    metadata four-CCs (IART/INAM/ICMT/…) → StandardTags        (info.go)
├─ bext         Broadcast Wave Extension (EBU Tech 3285): description, …   (bext.go)
├─ cue          cue-point table                                           (reader.go)
└─ data         the raw interleaved PCM payload (Reader.Data / Writer.Data)
```

Chunks the reader does not recognise are preserved verbatim in
`Extras.Unknown` (keyed by four-CC) so the writer can round-trip them. The RIFF
chunk size is **not** trusted for EOF — the reader bounds the `data` payload by
its own chunk size and relies on the underlying reader for end-of-stream.

### `fmt` → sample format

The `fmt` chunk (`chunks.go`) carries the WAVEFORMATEX fields. `sampleFormatFor`
maps `(wFormatTag, wBitsPerSample)` onto a [`mutations.SampleFormat`]:

| wFormatTag | bits | SampleFormat |
|---|---|---|
| `WAVE_FORMAT_PCM` (1) | 8 / 16 / 24 / 32 | `FormatUint8` / `FormatInt16` / `FormatInt24` / `FormatInt32` |
| `WAVE_FORMAT_IEEE_FLOAT` (3) | 32 / 64 | `FormatFloat32` / `FormatFloat64` |
| `WAVE_FORMAT_EXTENSIBLE` (0xFFFE) | — | resolved via the SubFormat GUID to PCM or IEEE-float, then as above |

The raw `wFormatTag` and `wBitsPerSample` are retained on `Extras.FormatTag` /
`Extras.BitsPerSample` for callers that need them. `Header.SampleFormat` is the
best-fit format; `Header.BitRate` is the `fmt` byte-rate × 8; `Header.Duration`
is derived from the `data` size and the block alignment.

### Metadata projection

LIST/INFO four-CCs (`info.go`) and the `bext` fields (`bext.go`) are normalised:
INFO ids (`INAM`, `IART`, `IPRD`, `ICMT`, `ICOP`, `ICRD`, `IGNR`, …) map to the
Vorbis-comment-style names on [`containers.StandardTags`]; the `bext` chunk is
exposed verbatim on `Extras.Bext` (broadcast description, originator, coding
history, time reference, UMID). The writer reverses the INFO projection from
`Header.Tags` and emits `bext` / `cue` chunks when present in `Extras`.

## Seekability

Reading is **streaming**: `NewReader` scans chunks sequentially (no seeking), so
any `io.Reader` works and the `data` payload is consumed lazily.

Writing requires an **`io.WriteSeeker`**: `NewWriter` emits the RIFF/`fmt`/
metadata chunks with placeholder sizes, then `Close` seeks back to **backpatch**
the RIFF and `data` chunk sizes (and writes a trailing pad byte for an odd-length
`data` chunk). A `Reader`/`Writer` is not safe for concurrent use, and `Close`
does **not** close the underlying stream.

## Integration with `codec/pcm`

`Reader.Data()` yields the raw PCM bytes; `Header.SampleRate` / `Channels` /
`SampleFormat` describe their layout. Feed all four straight into
[`codec/pcm.NewDecoder`]:

```go
import (
    "github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
    "github.com/daniel-sullivan/go-mediatoolkit/containers/wav"
)

rd, err := wav.NewReader(r)        // parses the RIFF chunk tree
h := rd.Header()                   // SampleRate/Channels/SampleFormat + Duration + StandardTags
dec, err := pcm.NewDecoder(rd.Data(), h.SampleRate, h.Channels, h.SampleFormat)
// dec.Read(...) → mutations.Audio (interleaved float64 in [-1.0, 1.0])
```

On the write side, construct a `Writer` from a populated `Header`, then wrap
`Writer.Data()` with a [`pcm.NewEncoder`] that matches the header's format:

```go
title := "Tone"
h := wav.Header{SampleRate: 48000, Channels: 1, SampleFormat: mutations.FormatInt24,
    Tags: containers.StandardTags{Title: &title}} // StandardTags fields are *string
w, err := wav.NewWriter(out, h)            // out must be an io.WriteSeeker
enc, err := pcm.NewEncoder(w.Data(), h.SampleRate, h.Channels, h.SampleFormat)
_, err = enc.Write(samples); err = enc.Close()
err = w.Close()                            // backpatches RIFF/data sizes; out is not closed
```

Runnable examples (all paths relative to the repo root, where `go.mod` lives;
each takes a file-path argument, so **write a file first** and read/decode that
same file):

```sh
go run ./containers/wav/examples/write tone.wav    # generate a sweep, write a 24-bit WAV with INFO + bext metadata
go run ./containers/wav/examples/read tone.wav      # parse the chunk tree, print the header + tags
go run ./containers/wav/examples/decode tone.wav    # full read -> codec/pcm -> PCM split, reporting peak amplitude
```

[`examples/write/`](examples/write) (generate a sweep, write a 24-bit WAV with
INFO + `bext` metadata); [`examples/read/`](examples/read) (parse the chunk tree,
print the header + tags); [`examples/decode/`](examples/decode) (the full
read → `codec/pcm` → PCM split, reporting peak amplitude).

## API

### Reader

- `NewReader(r io.Reader) (*Reader, error)` — parses the RIFF/`fmt`/metadata chunks; positions reads at the `data` payload.
- `Header() Header` — `SampleRate` / `Channels` / `SampleFormat` / `BitRate` / `Duration` plus [`StandardTags`] and the WAV [`Extras`].
- `Data() io.Reader` — the raw PCM bytes of the `data` chunk, ready for `codec/pcm.NewDecoder`.

### Writer (requires `io.WriteSeeker`)

- `NewWriter(w io.WriteSeeker, h Header) (*Writer, error)` — `Header.SampleRate`/`Channels`/`SampleFormat` required; emits RIFF/`fmt`/metadata chunks up front.
- `Data() io.Writer` — the `data`-chunk payload sink, for `codec/pcm.NewEncoder`.
- `Header() Header`, `Close() error` — `Close` pads and backpatches the RIFF/`data` sizes (`w` is not closed).

### Types

- `Header = containers.Header[Extras]` — the generic container header specialised to WAV `Extras`; `Format` is `"wav"`.
- `Extras` — `Bext *BroadcastExt`, `Cues []CuePoint`, `Unknown map[string][]byte` (round-tripped chunks), `FormatTag uint16`, `BitsPerSample uint16`.
- `BroadcastExt` — the `bext` chunk (description, originator, origination date/time, time reference, version, UMID, coding history).
- `CuePoint` — one `cue ` entry (id, position, data-chunk id, chunk/block start, sample offset).
- `DurationFromSamples(frames, sampleRate int) time.Duration` — frame count → duration helper.

### Errors

- `ErrNotRIFF` / `ErrNotWAVE` — the stream is not a RIFF file / its form type is not `WAVE`.
- `ErrMissingFmt` — a `data` chunk arrived before `fmt`.
- `ErrMissingData` — the stream ended without a `data` chunk.
- `ErrUnsupportedFormat` — the `fmt` layout (or the `Header` passed to `NewWriter`) is not a supported PCM/IEEE-float form.
- `ErrBadChunkSize` — a chunk declared an invalid size.
- `ErrNeedSeeker` — the writer was given a non-seekable destination.
- `ErrAlreadyClosed` — a `Writer` method called after `Close`.

## License

This package is **MIT**: the RIFF chunk parser, `fmt`/`data`/INFO/`bext`/`cue`
handling, and tag projection link **no third-party code** and compile in a
default `CGO_ENABLED=0` build. Turning the `data` bytes into samples goes through
[`codec/pcm`], which is likewise pure-Go MIT. See
[`LICENSING.md`](../../LICENSING.md) for the full license map.
