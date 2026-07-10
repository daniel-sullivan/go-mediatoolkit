# Containers — wav, ogg, flac, mp3, mp4, adts

Containers handle **framing and metadata only** — they never touch the audio bitstream (that's the codec's job). All are pure Go, compile in a default `CGO_ENABLED=0` build, and expose a uniform generic header:

```go
type Header[E any] struct {
    Format       string                  // "wav", "flac", "adts", "mp4", ...
    SampleRate   int                     // 0 if unknown at container level
    Channels     int
    SampleFormat mutations.SampleFormat  // best-fit PCM representation
    BitRate      int
    Duration     time.Duration           // 0 if unknown/unseekable
    Tags         containers.StandardTags
    Extra        E                       // format-specific extras struct
}
```

`containers.StandardTags` fields are `*string` (`Title`, `Artist`, `Album`, `AlbumArtist`, `Date`, `Genre`, `TrackNumber`, `Comment`, `Composer`, `Performer`, `Copyright`, `Encoder`, `Description`, `Organization`, `License`, `ISRC`) — nil means unset; extra/multi-value tags live in `AdditionalTags` (Vorbis-comment-style upper-case keys). With Go 1.26, `Title: new("Song")` builds a `*string` inline.

`containers.PacketReader` / `containers.PacketWriter` are structurally identical to the `codec/opus` and `codec/aac` packet interfaces, so container streams pipe straight into those codecs.

## WAV — `containers/wav` (+ `codec/pcm`)

```go
import (
    "github.com/daniel-sullivan/go-mediatoolkit/codec/pcm"
    "github.com/daniel-sullivan/go-mediatoolkit/containers"
    "github.com/daniel-sullivan/go-mediatoolkit/containers/wav"
)

// Read (streaming, any io.Reader):
rd, err := wav.NewReader(r)
h := rd.Header() // SampleRate/Channels/SampleFormat + Duration + Tags + Extras (bext, cues)
dec, err := pcm.NewDecoder(rd.Data(), h.SampleRate, h.Channels, h.SampleFormat)

// Write (requires io.WriteSeeker — Close backpatches RIFF/data sizes):
hdr := wav.Header{SampleRate: 48000, Channels: 1, SampleFormat: mutations.FormatInt24,
    Tags: containers.StandardTags{Title: new("Tone")}}
w, err := wav.NewWriter(out, hdr)
enc, err := pcm.NewEncoder(w.Data(), hdr.SampleRate, hdr.Channels, hdr.SampleFormat)
// enc.Write(...); enc.Close(); then:
err = w.Close() // backpatches sizes; out is not closed
```

- `fmt` mapping: PCM 8/16/24/32-bit → `FormatUint8/Int16/Int24/Int32`; IEEE float 32/64 → `FormatFloat32/Float64`; WAVE_FORMAT_EXTENSIBLE resolved via SubFormat GUID. Raw tag/bits on `Extras.FormatTag`/`Extras.BitsPerSample`.
- Metadata: LIST/INFO → `StandardTags`; `bext` (broadcast WAV) on `Extras.Bext`; cue points on `Extras.Cues`; unknown chunks round-trip via `Extras.Unknown`.
- `ErrNeedSeeker` if the writer destination can't seek.

## Ogg — `containers/ogg` (Opus + FLAC mappings, generic demux)

Opus (`.opus`, RFC 7845):

```go
import (
    opuscodec "github.com/daniel-sullivan/go-mediatoolkit/codec/opus"
    "github.com/daniel-sullivan/go-mediatoolkit/containers/ogg"
)

rd, err := ogg.NewOpusReader(r)  // parses OpusHead + OpusTags
h := rd.Header()                 // SampleRate (48000 — Opus always decodes at 48 kHz), Channels, Tags, Extra.Head
dec, err := opuscodec.NewDecoder(rd, h.SampleRate, h.Channels) // rd is a PacketReader

// Write: codec encoder feeds the Ogg writer (an ogg.OpusWriter is a PacketWriter):
ow, err := ogg.NewOpusWriter(out, channels, ogg.WithOpusTags(tags))
enc, err := opuscodec.NewEncoder(ow, 48000, channels, opuscodec.WithBitrate(64000))
// enc.Write(...); enc.Close(); ow.Close()
```

- `OpusWriter.WritePacket` assumes 20 ms / 960-sample frames for the granule counter; use `WritePacketWithFrames` for other frame durations.
- Writer options: `WithOpusSerialNo`, `WithOpusPreSkip`, `WithOpusInputSampleRate`, `WithOpusOutputGain`, `WithOpusVendor`, `WithOpusTags`.

FLAC-in-Ogg: `ogg.NewFLACReader(r)` parses the mapping and `Data()` rebuilds a **synthetic native FLAC byte stream** you can hand to `codec/flac.NewDecoder`; `ogg.NewFLACWriter(w, sampleRate, channels, opts...)` has `Encode([]int32)`.

Generic demux (any codec): `ogg.NewReader(r)`, `Streams()`, `Stream(serial)`, `Stream.Packets()` (a `containers.PacketReader`); each BOS packet is sniffed into a best-effort `CodecHint` (`"opus"`, `"vorbis"`, `"flac"`). Write side: `ogg.NewWriter(w)`, `AddStream(serial)`, `StreamWriter.WritePacket`/`SetGranule`/`SetEOS`.

## Native FLAC — `containers/flac`

```go
import ctrflac "github.com/daniel-sullivan/go-mediatoolkit/containers/flac"

rd, err := ctrflac.NewReader(r)  // parses fLaC magic + metadata chain (streaming, no seek)
h := rd.Header()                 // SampleRate/Channels/Duration + Tags + Extra.StreamInfo
dec, err := flaccodec.NewDecoder(rd.Data()) // Data() replays magic+metadata+frames verbatim
```

Write: `ctrflac.NewWriter(out, h, ctrflac.WithCompressionLevel(5))` wraps a FLAC encoder — `Encode(samples []int32)` takes **interleaved int32** (bit depth from `h.Extra.StreamInfo.BitsPerSample`, default 16); it projects `h.Tags` into VORBIS_COMMENT. Options: `WithCompressionLevel(0–8)`, `WithVerify(bool)`, `WithBlockSize(int)`. Extras carry `StreamInfo`, `Vendor`, `SeekTable`, `Padding`, `Pictures`, `Application`, `Cuesheet`.

This is the portable way to read FLAC tags (works on the pure-Go decode path).

## MP3 / ID3 — `containers/mp3`

```go
import ctrmp3 "github.com/daniel-sullivan/go-mediatoolkit/containers/mp3"

rd, err := ctrmp3.NewReader(r)  // parses leading ID3v2 (+ trailing ID3v1 if r is an io.ReadSeeker)
h := rd.Header()                // Tags + SampleRate/Channels peeked from the first frame header
dec, err := codecmp3.NewDecoder(rd.Data()) // Data() replays ID3 prefix + audio unchanged
```

- `SampleRate`/`Channels` come from peeking the first MPEG frame header (audio is not decoded); they stay zero on unparseable streams rather than erroring.
- Unmapped ID3 frames → `Extras.RawFrames`; `APIC` album art → `Extras.Pictures`.
- Write: `ctrmp3.NewWriter(out, h, ctrmp3.WithBitRate(192000), ctrmp3.WithQuality(2))` writes the ID3v2 tag eagerly, then `Encode(samples []int16)` (interleaved int16) through the LAME encoder — the encode path needs `-tags mp3lame` and surfaces `ErrEncoderRequiresLAME` on first `Encode` otherwise. Metadata-only use (`NewWriter` + `Close`, no `Encode`) works in a default build.

## MP4 / M4A — `containers/mp4` (+ `codec/aac`)

```go
import (
    aaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/aac"
    "github.com/daniel-sullivan/go-mediatoolkit/containers/mp4"
)

rd, err := mp4.NewReader(r)   // buffers the WHOLE stream (ISOBMFF is random-access)
hdr := rd.Header()            // SampleRate/Channels/Duration + iTunes tags + Extra.Config (esds ASC)
dec, err := aaccodec.NewDecoder(rd.Packets(), hdr.Extra.Config) // decode needs -tags aacfdk
```

- `Packets()` is a `codec/aac.PacketReader` over the access units; `AccessUnits()` gives the same as a slice.
- iTunes `ilst` atoms project onto `StandardTags` (`©nam`→Title, `©ART`→Artist, `©alb`→Album, `trkn`→TrackNumber, ...); `covr` art → `Extras.CoverArt`; unmapped atoms → `Extras.FreeformTags`.
- Write: `mp4.NewWriter(out, h, mp4.WithBitrate(128000), mp4.WithObjectType(...))`. `WriteAudio(pcm []float64)` encodes via AAC (needs `-tags aacfdk`); `WritePacket(pkt)` re-muxes pre-encoded access units byte-for-byte and works in a **default build** (no engine). `Close` assembles the whole file (writer buffers everything).
- The container itself is MIT/untagged — parsing/re-muxing never needs `aacfdk`.

## ADTS — `containers/adts` (standalone `.aac`)

```go
import "github.com/daniel-sullivan/go-mediatoolkit/containers/adts"

rd, err := adts.NewReader(r)               // streaming; resyncs past garbage (64 KiB window)
dec, err := aaccodec.NewDecoder(rd, rd.ASC()) // rd is a PacketReader; ASC derived from frame 1

w, err := adts.NewWriter(out, 44100, 2, adts.WithObjectType(aaclib.AOTAACLC))
enc, err := aaccodec.NewEncoder(w, 44100, 2)  // framed AUs → playable .aac
```

- Writer options: `WithObjectType` (default AAC-LC), `WithMPEGVersion(int)` (0 = MPEG-4 default, 1 = MPEG-2), `WithCRC(bool)` (default off). `WritePacket(au)` frames one raw access unit — pure re-mux (e.g. MP4 → ADTS) needs no AAC engine.
- Low-level: `adts.ParseHeader` / `adts.EncodeHeader` / `FrameHeader`; constants `HeaderLen` (7), `HeaderLenCRC` (9), `SyncWord` (0xFFF), `MaxFrameLen`.
- ADTS restates config in every frame header but carries **no SBR/PS signalling** — HE-AAC belongs in MP4 (explicit esds ASC); ADTS round-trips AAC-LC cleanly.

## Choosing

| You have / want | Use |
|---|---|
| `.wav` | `containers/wav` + `codec/pcm` |
| `.opus` | `containers/ogg` (`OpusReader`/`OpusWriter`) + `codec/opus` |
| `.flac` | `containers/flac` + `codec/flac` (or `containers/ogg` FLAC mapping) |
| `.mp3` with tags | `containers/mp3` + `codec/mp3` |
| `.m4a` / `.mp4` audio | `containers/mp4` + `codec/aac` (decode needs `-tags aacfdk`) |
| raw `.aac` / broadcast AAC | `containers/adts` + `codec/aac` |
| bare codec stream, no tags needed | the codec package alone (FLAC/MP3 are self-framed) |
