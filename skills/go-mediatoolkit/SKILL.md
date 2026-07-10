---
name: go-mediatoolkit
description: "Use this skill when writing Go code that decodes, encodes, processes, plays, or analyses audio or video with github.com/daniel-sullivan/go-mediatoolkit. Covers the streaming codec.Decoder/codec.Encoder model over interleaved float64 samples, codec packages (codec/aac, codec/mp3, codec/flac, codec/opus, codec/pcm), containers (containers/wav, containers/ogg, containers/flac, containers/mp3, containers/mp4, containers/adts), license-fence build tags (mp3lame, aacfdk, CGO_ENABLED=0 pure-Go default), the sample pipeline (resample, mutations, loudness EBU R128 metering/normalisation, vad voice-activity detection with Gate/Ducker, aec echo cancellation, generators, timeline, mixer, buffers), OS audio devices (CoreAudio, WASAPI, PulseAudio), and hardware-accelerated video (codec/hwaccel, video.Frame/Packet, VideoToolbox, VAAPI, V4L2, NVENC). Triggers on: go-mediatoolkit, mediatoolkit, decode audio in Go, encode audio in Go, Go audio library, mutations.Audio, codec.Decoder, codec.Encoder, NewDecoder, NewEncoder, PacketReader, PacketWriter, FLAC, Opus, MP3, AAC, HE-AAC, PCM, WAV, Ogg, M4A, MP4 audio, ADTS, ID3, resample, sample rate conversion, libsamplerate, LUFS, EBU R128, loudness normalization, true peak, limiter, leveller, VAD, voice activity detection, Silero, libfvad, speech gate, ducking, AEC, acoustic echo cancellation, AEC3, sine generator, audio timeline, audio mixer, ring buffer, audio devices Go, microphone capture Go, audio playback Go, hwaccel, hardware video encode, hardware video decode, transcode H.264 H.265, NV12, Annex-B."
license: MIT
---

Go audio + video toolkit: module `github.com/daniel-sullivan/go-mediatoolkit`, Go 1.26, MIT-licensed. Pin the latest released tag:

```sh
go get github.com/daniel-sullivan/go-mediatoolkit@v1.2.0
```

The default build is **pure Go** — `CGO_ENABLED=0 go build` works for everything documented here; no C toolchain, no system libraries. Audio codecs are 1:1 ports of their canonical C references (bit-exact / byte-identical), so pure-Go output matches the C libraries exactly.

## The universal streaming model

Everything audio flows through **interleaved `float64` samples in `[-1.0, 1.0]`** (stereo: `[L0, R0, L1, R1, ...]`). A "frame" is one sample per channel; buffer lengths must be multiples of the channel count.

- **`codec.Decoder`** — `Read(buf []float64) (mutations.Audio, error)`: fills the caller's buffer, returns an `Audio` aliasing the filled portion; loop until `io.EOF`. A partial read alongside `io.EOF` is valid and must be consumed.
- **`codec.Encoder`** — `Write(audio mutations.Audio) (int, error)` then `Close()`: `audio.SampleRate`/`Channels` must match the encoder (else `ErrFormatMismatch`). `Close` flushes but **never closes the underlying writer**.
- **`mutations.Audio`** — `{Data []float64; SampleRate, Channels int}`. Copying shares `Data`; use `Clone()` to duplicate. Helpers: `Frames()`, `Duration()`; chainable in-place methods (`ApplyGain`, `ApplyFadeIn`, `ApplyEffect`, ...).

Every codec has the same face, so decode → process → encode pipelines compose by swapping constructors:

```go
import (
    "bytes"
    "io"
    "time"

    flaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/flac"
    "github.com/daniel-sullivan/go-mediatoolkit/generators"
)

tone := generators.Sine(440, time.Second, 44100) // mutations.Audio, mono

var buf bytes.Buffer
enc, err := flaccodec.NewEncoder(&buf, tone.SampleRate, tone.Channels,
    flaccodec.WithCompressionLevel(8))
if err != nil { /* handle */ }
if _, err := enc.Write(tone); err != nil { /* handle */ }
if err := enc.Close(); err != nil { /* handle */ }

dec, err := flaccodec.NewDecoder(&buf)
if err != nil { /* handle */ }
out := make([]float64, 8192)
for {
    audio, err := dec.Read(out) // audio.Data aliases out[:n]
    _ = audio
    if err == io.EOF {
        break
    }
    if err != nil { /* handle */ }
}
```

## Three layers — import the right one

| Layer | Example | Use for |
|---|---|---|
| `containers/*` | `containers/ogg`, `containers/mp4` | file framing + metadata/tags; supplies the byte stream or `PacketReader`/`PacketWriter` the codec needs |
| `codec/*` | `codec/opus`, `codec/flac` | the streaming float64 seam (`codec.Decoder`/`codec.Encoder`) |
| `libraries/*` | `libraries/opus`, `libraries/aac` | the codec **engines** (1:1 C ports + cgo backends + parity gates). **Not for external import** — except the small enums codec options reference (`libraries/opus.Application`, `libraries/aac.AudioObjectType`, `libraries/aac.AudioSpecificConfig`) |

Three codec shapes decide how you wire a container:

- **Byte-stream** (`codec/mp3`, `codec/flac`): self-framed — `NewDecoder(io.Reader)` / `NewEncoder(io.Writer, rate, ch, opts...)`. Containers wrap the same bytes (`Reader.Data()` replays the stream).
- **Packet** (`codec/opus`, `codec/aac`): no canonical framing — constructors take `PacketReader`/`PacketWriter`; the container is the framing (`ogg.OpusReader`, `mp4.Reader.Packets()`, `adts.Reader` all implement `PacketReader`).
- **Headerless** (`codec/pcm`): raw bytes; you must supply rate/channels/`mutations.SampleFormat` at construction (a WAV `Header` provides them).

## Choosing a codec + build tags / license fences

| Codec | Decode | Encode | Build tag needed | Without the tag |
|---|---|---|---|---|
| `codec/flac` | always | always | none (BSD) | — |
| `codec/opus` | always | always | none (BSD) | — |
| `codec/pcm` | always | always | none (pure Go, no engine) | — |
| `codec/mp3` | always (minimp3, CC0) | **`-tags mp3lame`** (LAME, LGPL-2.0-or-later) | encode only | `NewEncoder` → `ErrEncoderRequiresLAME` |
| `codec/aac` | **`-tags aacfdk`** | **`-tags aacfdk`** (FDK-AAC license) | whole engine | `NewDecoder`/`NewEncoder` → `ErrEngineRequiresFDK` |

- A default build links **zero LGPL and zero FDK-AAC code** — 100% MIT + permissive. Shipping an `mp3lame` binary triggers LGPL relink/source obligations; see the repo's `LICENSING.md`.
- With cgo **on** (default `go build`), FLAC/Opus route through the vendored C reference; with `CGO_ENABLED=0` they use the bit-exact pure-Go ports. Same API, same output. `-tags aacfdk` with cgo off selects the pure-Go FDK port.
- The `*_strict` tags (`opus_strict`, `flac_strict`, `mp3_strict`, `aac_strict`) are parity-gate correctness modes, **not** shipping configurations — never suggest them for production.
- `go test ./...` builds with cgo by default and therefore **needs a C compiler**; `CGO_ENABLED=0 go build ./...` does not.

Key constraints: Opus supports only sample rates 8000/12000/16000/24000/48000 Hz and 1–2 channels (`ErrBadSampleRate`/`ErrBadChannels`); FLAC encode takes `WithBitsPerSample` (default 16) + `WithCompressionLevel(0–8)` (default 5); MP3 `SampleRate()`/`Channels()` read 0 until the first frame parses. Full options, HE-AAC profiles, and per-codec detail: **read `references/codecs.md`**.

## Containers (framing + tags)

| Container | Pairs with | Reader → codec wiring |
|---|---|---|
| `containers/wav` | `codec/pcm` | `pcm.NewDecoder(rd.Data(), h.SampleRate, h.Channels, h.SampleFormat)` |
| `containers/ogg` | `codec/opus`, `codec/flac` | `opus.NewDecoder(rd, h.SampleRate, h.Channels)` — `OpusReader` is a `PacketReader` |
| `containers/flac` | `codec/flac` | `flac.NewDecoder(rd.Data())` |
| `containers/mp3` | `codec/mp3` | `mp3.NewDecoder(rd.Data())` (ID3 parsed; bytes replayed) |
| `containers/mp4` | `codec/aac` | `aac.NewDecoder(rd.Packets(), rd.Header().Extra.Config)` |
| `containers/adts` | `codec/aac` | `aac.NewDecoder(rd, rd.ASC())` — `adts.Reader` is a `PacketReader` |

All expose a uniform `containers.Header[Extras]` (rate/channels/duration + `containers.StandardTags`, whose fields are `*string` — nil means unset). Details, write paths, and per-format gotchas: **read `references/containers.md`**.

## Sample pipeline (process the float64 stream)

- **`resample`** — libsamplerate port: `resample.Simple(in, resample.SincFastest, channels, resample.Ratio{InputRate: 44100, OutputRate: 48000})` one-shot, or `resample.New` + `Process` streaming. Converters: `SincBestQuality`/`SincMediumQuality`/`SincFastest`/`Linear`/`ZeroOrderHold`.
- **`mutations`** — format conversion (`FormatInt16`, ... ↔ float64), interleave, trim, chunk, gain/fades, saturation, and stateful `Processor` effects (`NewLowpass`, `NewEcho`, `NewReverb`, ...). The `Processor` interface (`Process([]float64)` + `Reset()`) is the toolkit-wide effect seam.
- **`loudness`** — EBU R128 / BS.1770-4: `Measure`, `Normalize`, streaming `Meter`, `Limiter`, `Leveller`, goroutine-safe `Monitor`. Targets like `TargetPodcast` (−16 LUFS), `TargetEBUR128` (−23 LUFS).
- **`vad`** — three `Detector` engines (`NewEnergyDetector`, `NewWebRTCDetector`, `NewSileroDetector`) + `Gate` and sidechain `Ducker`.
- **`aec`** — WebRTC AEC3 port: two-stream `Canceller` (`FeedFarEnd` render + `Process` capture). Rates 16000/32000/48000 only.
- **`generators`** — `Sine`, `Chord`, `WhiteNoise`, `PinkNoise`, `SineSweep`, `Note`, `Melody`, `Beep`, `Pluck` — all return mono `mutations.Audio`.
- **`timeline` / `mixer` / `buffers`** — schedule `Cue`s of `Source`s on a `Timeline`, sum sources in a `mixer.Mixer` whose `Fill` binds to a device callback; `buffers.Ring` is the lock-free SPSC bridge.

For any of these, **read `references/pipeline.md`** (resample/mutations/generators/timeline/mixer/buffers) or **`references/dsp.md`** (loudness/vad/aec) before writing code.

## Devices, consts, video

- **`devices`** — OS audio (CoreAudio/WASAPI/PulseAudio, all cgo-free): `devices.GetSystem()`, `List()`, `DefaultOutput()`, `OpenOutput(dev, devices.StreamFormat{...}, callback)`. The callback runs on a **realtime thread: no allocation, no locks, no I/O; zero samples you don't fill**; read `stream.Format()` after opening — the backend may negotiate a different format.
- **`consts`** — `consts.SampleRate48000`, `consts.FreqNoteA4`, `consts.CommonSampleRates` (no channel-count constants — pass plain ints).
- **`codec/hwaccel` + `video`** — hardware video encode/decode (VideoToolbox, VAAPI, V4L2, NVENC), cgo-free via purego/syscall. `hwaccel.OpenEncoder(hwaccel.Policy{Mode: hwaccel.PreferHardware}, hwaccel.NewConfig(hwaccel.WithCodec(video.H265), ...))`; `Encode`/`Decode` return **slices** (0..n results, pipelined) and `Flush()` drains. `video.Frame` (NV12/I420 planes — honour `Strides`, never assume stride == width) and `video.Packet` (H.264/H.265 = Annex-B; VP9/AV1 = raw IVF-style payloads).

**Read `references/devices-video.md`** for streams, hotplug, the backend matrix, policy modes, and packet framing detail.

## Pitfalls (repo-documented; check before writing code)

1. **Never import `libraries/*` for encode/decode** — use `codec/*`; the enums (`AOTAACLC`, `AppAudio`, `AudioSpecificConfig`) are the only sanctioned imports.
2. **Encoder format must match**: build `mutations.Audio` with the encoder's exact `SampleRate`/`Channels` or `Write` returns `ErrFormatMismatch` — resample/remix first.
3. **`Close` flushes, never closes** the underlying `io.Writer`/`PacketWriter`; close container writers too (e.g. `wav.Writer.Close` backpatches RIFF sizes and requires an `io.WriteSeeker`).
4. **Decoders and encoders are not goroutine-safe**; `resample.Converter` is not either — use `Clone()` per goroutine.
5. **AAC/MP3 fences**: default builds return `ErrEngineRequiresFDK` / `ErrEncoderRequiresLAME` at construction or first use — this is by design, not a bug; tell users to rebuild with the tag.
6. **HE-AAC output differs from the core**: SBR doubles the rate, PS widens mono→stereo — trust `dec.SampleRate()`/`Channels()` (from `AudioSpecificConfig.Output()`), not the file's nominal values. HE-AAC decode wants the MP4 `esds` ASC; ADTS carries no SBR/PS signalling.
7. **`mp4.NewReader` buffers the whole stream in memory** (ISOBMFF is random-access); every other reader streams.
8. **AEC** needs 16/32/48 kHz (resample 44.1 kHz first) and `FeedFarEnd`+`Process` on **one goroutine**; **VAD `Gate`** owns its detector's feed — never `Process` the same detector from two places (readers are fine).
9. **Latency-carrying processors need tail flushes**: `loudness.Limiter`/`Leveller`, `vad.Gate`, and resampling detectors — use `timeline.NewEffectSource(src, p).WithTail(p.Latency())` or `mutations.Audio.RenderWithEffects(chain, tail)`.
10. **24/7 metering**: `loudness.Meter` history is unbounded by default — `SetMaxHistory` or `ModeHistogram`.
11. **Generators are mono** — `mutations.UpmixMonoToStereo` for stereo pipelines.
12. **`timeline` rejects format-mismatched cues; `mixer` adapts** (resamples + mono↔stereo). Put adaptation at the mixer, not the timeline.

## Recipe: transcode with resampling (WAV 44.1 kHz → Ogg/Opus 48 kHz)

The canonical pipeline shape — container reader → codec decoder → resample → codec encoder → container writer:

```go
rd, err := wav.NewReader(in)
h := rd.Header()
dec, err := pcm.NewDecoder(rd.Data(), h.SampleRate, h.Channels, h.SampleFormat)

ow, err := ogg.NewOpusWriter(out, h.Channels)             // Opus must run at a supported rate
enc, err := opuscodec.NewEncoder(ow, 48000, h.Channels)   // 48000 — 44100 is NOT valid for Opus

conv, err := resample.New(resample.SincMediumQuality, h.Channels)
defer conv.Close()
ratio := resample.Ratio{InputRate: h.SampleRate, OutputRate: 48000}

buf, res := make([]float64, 8192), make([]float64, 16384)
for {
    audio, readErr := dec.Read(buf)
    d := &resample.Data{DataIn: audio.Data, DataOut: res, Ratio: ratio, EndOfInput: readErr == io.EOF}
    if err := conv.Process(d); err != nil { /* handle */ }
    resampled := mutations.Audio{Data: res[:d.OutputFramesGen*h.Channels], SampleRate: 48000, Channels: h.Channels}
    if _, err := enc.Write(resampled); err != nil { /* handle */ }
    if readErr == io.EOF {
        break
    }
    if readErr != nil { /* handle */ }
}
// Close order: codec encoder first (flushes frames), then the container writer.
if err := enc.Close(); err != nil { /* handle */ }
if err := ow.Close(); err != nil { /* handle */ }
```

## Reference files

| Read when... | File |
|---|---|
| Encoding/decoding any audio codec: constructor options, packet I/O, HE-AAC, engine routing, build tags | [references/codecs.md](references/codecs.md) |
| Reading/writing files or tags: WAV, Ogg/Opus, native FLAC, ID3, MP4/M4A, ADTS | [references/containers.md](references/containers.md) |
| Resampling, sample transforms, effects, signal generation, playback scheduling, mixing, ring buffers | [references/pipeline.md](references/pipeline.md) |
| Loudness (LUFS/normalisation/limiting), voice-activity detection, echo cancellation | [references/dsp.md](references/dsp.md) |
| Audio device I/O, events bus, consts, hardware video encode/decode/transcode | [references/devices-video.md](references/devices-video.md) |

Every package also ships a README (`codec/flac/README.md`, `loudness/README.md`, ...) and runnable `examples/` directories — consult them for anything not covered here.
