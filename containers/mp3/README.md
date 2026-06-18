# containers/mp3

ID3 metadata around a native MP3 stream: ID3v2 (and ID3v1) tag parse/projection
on top of the raw, self-framed MP3 byte stream. This is the container face of
MP3, mirroring `containers/flac` and `containers/wav`.

MP3 is a **self-framed** codec — the byte stream itself carries frame-sync
headers, side information, and Huffman-coded spectral data in a continuous
sequence, so the audio framing belongs to the codec, not to this layer. An MP3
file is just those frames, optionally bracketed by ID3 metadata: an **ID3v2**
tag (artist, title, album, album art, …) ahead of the audio frames and an
optional fixed-size 128-byte **ID3v1** tag at the end. This package parses and
projects only that metadata; it does not touch the audio framing.

The codec engines, the cgo-vs-native routing, the `mp3lame` / `mp3_strict`
build tags, the LGPL fence on the encoder, and the bit-exact parity gate all
live in [`libraries/mp3`](../../libraries/mp3) — **read
[`libraries/mp3/README.md`](../../libraries/mp3/README.md) for the self-framed
shape decision, the cgo-vs-native table, the build tags, and the parity
discipline.** This README covers only the ID3 metadata seam.

## Layering

| Package | Works with | Use for |
|---|---|---|
| `libraries/mp3` | `int16` samples ↔ MP3 byte stream | direct frame-by-frame encode/decode, lowest overhead |
| `codec/mp3` | `float64` samples (`mutations.Audio`) ↔ MP3 byte stream | streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention) |
| **`containers/mp3`** | **ID3v2 / ID3v1 metadata + tag projection** | **inspecting/writing artist/title/album tags, album art** |

Metadata is normalized onto [`containers.StandardTags`]: ID3 frame IDs (`TPE1`,
`TIT2`, `TALB`, …) map to the Vorbis-comment-style standard tag names used
across the toolkit. Frames with no standard mapping are preserved verbatim in
[`Extras.RawFrames`]; `APIC` album-art frames are preserved in
[`Extras.Pictures`].

## Reading metadata

```go
import (
    ctrmp3 "go-mediatoolkit/containers/mp3"
    "go-mediatoolkit/libraries/mp3"
)

rd, err := ctrmp3.NewReader(r)        // parses the leading ID3v2 (+ trailing ID3v1 if seekable)
hdr := rd.Header()                    // SampleRate/Channels + standard tags + ID3 extras
dec, _ := mp3.NewDecoder(rd.Data())   // rd.Data() replays the ID3 prefix + audio frames unchanged
```

`Reader.Header()` carries the audio parameters too: after skipping any ID3v2
prefix, `NewReader` peeks the first MPEG audio frame's 4-byte header and decodes
its sample rate and channel mode (mono → 1, otherwise 2) into
`Header.SampleRate` / `Header.Channels`, with the MP3-only values (MPEG version,
samples-per-frame, nominal bit rate) in `Extra.StreamInfo`. The audio is never
decoded — only the frame header is read — and if no parseable frame header is
found those fields stay zero rather than erroring (graceful degradation).

`Reader.Data()` yields exactly the bytes the caller would have read from `r`
(ID3v2 prefix, MPEG audio frames, any ID3v1 trailer); the first-frame peek is
buffered and replayed, so the byte stream is intact and the decoder skips ID3
frames itself, so no re-seeking is needed. If `r` also satisfies
[`io.ReadSeeker`], the trailing 128-byte ID3v1 tag is folded into the header
(ID3v2 values win on conflict) and the seek offset is restored.

## Writing metadata

```go
h := ctrmp3.Header{
    SampleRate: 44100,
    Channels:   2,
    Tags: containers.StandardTags{   // typed optional fields; nil = unset
        Title:  new("Song Name"),
        Artist: new("Artist"),
    },
}

w, err := ctrmp3.NewWriter(out, h,    // returns ErrEncoderRequiresLAME unless built -tags mp3lame
    ctrmp3.WithBitRate(192000), ctrmp3.WithQuality(2))

err = w.Encode(samples)               // interleaved int16, length a multiple of Channels
err = w.Close()                       // flushes the final encoder frames; out is not closed
```

`NewWriter` projects the header's tags onto an ID3v2 tag written ahead of the
audio frames, then forwards samples to a [`libraries/mp3.Encoder`]. Because that
encoder is the LAME-derived (**LGPL-2.0-or-later**) path, the write side is only
available under `-tags mp3lame`; without it `NewWriter` surfaces
`libraries/mp3.ErrEncoderRequiresLAME`. Reading metadata is always available
(no encoder, no LGPL). See [`LICENSING.md`](../../LICENSING.md).

Runnable examples: [`examples/tags/`](examples/tags) (parse ID3, project tags);
[`examples/decode/`](examples/decode) (read → `codec/mp3` → PCM, the full
container↔codec split, default build / no LGPL); [`examples/write/`](examples/write)
(write an ID3v2 tag + encode a tone via LAME, degrading gracefully without
`-tags mp3lame`).

## API

### Reader

- `NewReader(r io.Reader) (*Reader, error)`
- `Header() Header` — `SampleRate` / `Channels` plus [`StandardTags`] and the ID3 [`Extras`].
- `Data() io.Reader` — the original byte stream (ID3 + audio), ready for `libraries/mp3.NewDecoder`.

### Writer (audio encoding requires `-tags mp3lame`)

- `NewWriter(w io.Writer, h Header, opts ...WriterOption) (*Writer, error)` — writes the ID3v2 tag from `h.Tags` eagerly; the encoder is constructed lazily on the first `Encode`. Metadata-only use (`NewWriter` + `Close`, no `Encode`) works in a default build with no encoder.
- `Encode(samples []int16) error` — interleaved samples, length a multiple of `Channels`. Constructs the encoder on first call; in a default build this is where `ErrEncoderRequiresLAME` surfaces.
- `Header() Header`, `Encoder() (libraries/mp3.Encoder, error)`, `Close() error` — `Close` flushes the encoder only if one was constructed.
- Options: `WithBitRate(bps int)`, `WithQuality(q int)`, `WithVBR(enable bool)` — forwarded to the encoder.

### Types

- `Header = containers.Header[Extras]` — the generic container header specialised to MP3 `Extras`.
- `Extras` — `StreamInfo` (MPEG version, samples/frame, nominal bit rate), `ID3v2Version`, `HasID3v1`, `RawFrames map[string][]byte` (unmapped frames), `Pictures [][]byte` (raw `APIC` bodies).

### Errors

- `ErrNotMP3` — the stream does not begin with ID3v2 or an MP3 frame sync.
- `ErrInvalidID3` — a malformed ID3v2/ID3v1 tag.
- `ErrUnsupportedFormat` — the `Header` passed to `NewWriter` has an unsupported sample format / channel count.
- `ErrAlreadyClosed` — `Close` called twice.
- `ErrBadArg` — nil reader/writer or other invalid argument.

Encoder backend errors (notably `ErrEncoderRequiresLAME`) surface from
[`libraries/mp3`](../../libraries/mp3) unchanged.

## License

This package is **MIT**; ID3 parsing links no third-party code. The write path
it can reach (via `libraries/mp3` under `-tags mp3lame`) pulls in the
**LGPL-2.0-or-later** LAME-derived encoder; a default, decode/metadata-only
build links no LGPL code. See [`LICENSING.md`](../../LICENSING.md) for the full
file-by-file license map.
