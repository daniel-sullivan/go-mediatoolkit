# go-mediatoolkit

[![tests](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/tests.yml/badge.svg)](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/tests.yml)
[![lint](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/lint.yml/badge.svg)](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/lint.yml)
[![sanitizers](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/sanitizers.yml/badge.svg)](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/sanitizers.yml)
[![opus blackbox](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/blackbox-opus.yml/badge.svg)](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/blackbox-opus.yml)
[![flac blackbox](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/blackbox-flac.yml/badge.svg)](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/blackbox-flac.yml)
[![mp3 blackbox](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/blackbox-mp3.yml/badge.svg)](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/blackbox-mp3.yml)
[![aac blackbox](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/blackbox-aac.yml/badge.svg)](https://github.com/daniel-sullivan/go-mediatoolkit/actions/workflows/blackbox-aac.yml)

A pure-Go audio **and** video toolkit: streaming codecs, containers, a sample
pipeline (resample / mutate / mix / time-line), OS audio devices, and a
hardware-accelerated video offload framework — module `go-mediatoolkit`, Go
**1.26**.

The defining trait of this repo is **fidelity**. Every compressed audio codec is
a **1:1 port of its canonical C reference** held to a **bit-exact / byte-identical
parity gate** against that reference, and the default build is **`CGO_ENABLED=0`**
pure Go, with the original C libraries available as optional cgo backends. You get
the C codecs' exact output without linking any C — or link the C reference when
you want it.

```
audio:  bytes ──decode──► float64 samples ──mutate/resample/mix──► float64 ──encode──► bytes
video:  Packet ──hwaccel.Decode──► Frame ──(transform)──► Frame ──hwaccel.Encode──► Packet
```

## What's in the box

| Area | Packages |
|---|---|
| **Audio codecs** | [`codec/aac`](codec/aac) · [`codec/mp3`](codec/mp3) · [`codec/flac`](codec/flac) · [`codec/opus`](codec/opus) · [`codec/pcm`](codec/pcm) (streaming `Encoder`/`Decoder` over interleaved `float64`) |
| **Codec engines** | [`libraries/aac`](libraries/aac) · [`libraries/mp3`](libraries/mp3) · [`libraries/flac`](libraries/flac) · [`libraries/opus`](libraries/opus) · [`libraries/ogg`](libraries/ogg) (the 1:1 C ports + cgo backends + parity gates; not for external import) |
| **Containers** | [`containers/wav`](containers/wav) · [`containers/ogg`](containers/ogg) · [`containers/flac`](containers/flac) · [`containers/mp3`](containers/mp3) · [`containers/mp4`](containers/mp4) · [`containers/adts`](containers/adts) |
| **Video** | [`codec/hwaccel`](codec/hwaccel) (hardware encode/decode framework) · [`video`](video) (shared `Frame`/`Packet` types) |
| **Sample pipeline** | [`resample`](resample) · [`mutations`](mutations) · [`generators`](generators) · [`timeline`](timeline) · [`mixer`](mixer) · [`buffers`](buffers) |
| **Runtime / IO** | [`devices`](devices) · [`events`](events) · [`consts`](consts) · [`inspection`](inspection) · [`tools`](tools) |

The only required runtime dependency is `github.com/stretchr/testify` (tests). The
video/device subsystems pull in a few more — `ebitengine/purego` (dlopen video and
PulseAudio libraries with no cgo), `golang.org/x/sys` (V4L2 ioctls), `jfreymuth/pulse`
(PulseAudio), `bubbletea`/`lipgloss` (the `tools` TUI), and `go-hpt` (mixer timing).
The audio codecs themselves need none of these.

## Audio codecs

Every codec exposes the same streaming face — `codec.Decoder` / `codec.Encoder`
over interleaved `float64` in `[-1.0, 1.0]` — and routes to a vendored C reference
(cgo) or a pure-Go 1:1 port of it. Two codecs are fenced behind opt-in build tags
because their reference library is not permissively licensed.

| Codec | Decode | Encode | Native (pure-Go) port | C reference (cgo) | License fence | Parity gate |
|---|---|---|---|---|---|---|
| **[AAC](codec/aac)** | ✅ AAC-LC, HE-AAC v1 (SBR), HE-AAC v2 (PS) | ✅ AAC-LC + VBR, HE-AAC v1, HE-AAC v2 | bit-exact decode / byte-identical encode | Fraunhofer FDK-AAC v2.0.3 | **`aacfdk`** (FDK-AAC license) | `mise run //libraries/aac:parity` |
| **[MP3](codec/mp3)** | ✅ (always available) | ✅ CBR/ABR + VBR | minimp3 decode / LAME encode | minimp3 (CC0) + LAME 3.100 | encode-only **`mp3lame`** (LGPL-2.0-or-later) | `mise run //libraries/mp3:parity` |
| **[FLAC](codec/flac)** | ✅ lossless | ✅ levels 0–8 | bit-exact (decode lossless either build) | libFLAC (BSD-3) | none | `mise run //libraries/flac:parity` |
| **[Opus](codec/opus)** | ✅ SILK + CELT | ✅ SILK + CELT | RFC 6716 port, ~117 dB PSNR; arm64 NEON fast paths | libopus (BSD) | none | `mise run //libraries/opus:parity:benchcmp` |
| **[PCM](codec/pcm)** | ✅ | ✅ | pure Go (no engine) | — | none | — |

Notes:

- **AAC** has a single engine (FDK-AAC) used for both decode and encode; the
  whole island — vendored C, cgo backends, and the Go port — is fenced behind
  **`aacfdk`**. A default build links **zero** FDK-AAC code and `NewDecoder`/`NewEncoder`
  return `ErrEngineRequiresFDK`. FDK-AAC is fixed-point, so parity is **exact integer**
  (no FP/ULP), and the port's encode actually runs *faster* than the C reference
  (see [BENCHMARKS.md](BENCHMARKS.md)).
- **MP3** decode (minimp3, CC0) is always available; the **LAME**-derived encoder
  is **LGPL** and fenced behind **`mp3lame`** — a default build is decode-only and
  `NewEncoder` returns `ErrEncoderRequiresLAME`.
- **FLAC** and **Opus** are BSD-licensed with **no fence** — encode and decode work
  in every build, including `CGO_ENABLED=0`.

Each codec's README documents the float64 streaming seam; the matching
`libraries/*` README documents the engine, the cgo-vs-native routing, the build
tags, and the parity discipline.

## Containers

Containers handle **framing and metadata only** — they never touch the audio
bitstream (that's the codec's job). Each pairs with a codec above.

| Container | Frames | Carries | Pairs with |
|---|---|---|---|
| **[wav](containers/wav)** | RIFF/WAVE chunks over raw PCM | `fmt` layout, `data`, LIST/INFO + `bext` tags | `codec/pcm` |
| **[ogg](containers/ogg)** | Ogg pages → packets | Opus/FLAC header packets, vendor/tags | `codec/opus`, `codec/flac` |
| **[flac](containers/flac)** | `fLaC` magic + metadata-block chain | STREAMINFO, VORBIS_COMMENT | `codec/flac` |
| **[mp3](containers/mp3)** | ID3 around the self-framed MP3 stream | ID3v2 / ID3v1 (artist/title/album/art) | `codec/mp3` |
| **[mp4](containers/mp4)** | ISOBMFF box tree (`.m4a`/`.mp4`) | `esds` ASC, sample tables, iTunes `ilst` tags | `codec/aac` |
| **[adts](containers/adts)** | per-frame ADTS headers over AAC AUs (`.aac`) | inline per-frame config | `codec/aac` |

`containers/mp4` and `containers/adts` are MIT/untagged and compile in the default
build; decoding the AAC access units they yield still requires `-tags aacfdk`.

## Video — hardware codec offload

[`codec/hwaccel`](codec/hwaccel) is the **ffmpeg model in pure Go**: take raw frames
(or an incoming stream) and encode/decode on whatever fixed-function silicon the
host exposes, **preferring hardware and falling back to software loudly — never
silently**. The headline use case is the NVR / transcode path: hardware-decode
H.264 and re-encode it as H.265 in one pipeline. The whole subsystem is
**`CGO_ENABLED=0`** — raw syscall where the kernel ABI is stable (V4L2 ioctls),
`purego` dlopen for the vendor userspace libraries (VideoToolbox, VAAPI, NVENC).
There is **no pure-Go video codec** and the software tier is a defined-but-unwired
seam; the framework's reason to exist is the hardware tier.

Status reflects committed code **and committed test results** — it does not
overclaim.

| Backend | Platform | H.264 | H.265 | VP9 | AV1 | Status |
|---|---|---|---|---|---|---|
| **videotoolbox** | macOS (Apple silicon) | enc + dec ✅ | enc + dec ✅ | — | **dec** ✅ | H.264/H.265 enc+dec verified; AV1 **decode** verified on M3; no VP9 path, no VP9/AV1 encoder on Apple silicon |
| **vaapi** | Linux Intel Arc / AMD GPU | enc + dec ✅ | enc + dec ✅ | dec ✅ / enc 🚫 | dec ✅ / enc 🚫 | H.264/H.265 enc+dec + H.264→H.265 transcode verified; VP9/AV1 **decode** verified; VP9/AV1 **encode gated** (`ErrEncodeUnsupportedOnDriver` on Intel iHD) |
| **v4l2** | Linux SoC (Pi, Rockchip, Amlogic) | dec (stateful) | **dec** ✅ stateless + stateful | — | — | stateless HEVC decode verified bit-exact on **Pi 5**; stateful M2M decode/encode **spec-correct but unverified** |
| **nvenc / nvdec** | NVIDIA GPU (Linux) | enc + dec | enc + dec | dec | enc + dec | written to the Video Codec SDK 13.0 ABI (struct layouts pinned in tests); **UNVERIFIED on hardware** — no NVIDIA device was available |

Legend: ✅ verified on hardware · 🚫 gated with a sentinel error · — no hardware path.
NVENC/NVDEC cells are implemented-but-unverified.

Policy modes: **`PreferHardware`** (fall back loudly — fire a `HardwareFallbackEvent`
and log a `WARNING`), **`RequireHardware`** (return `ErrHardwareUnavailable`, never
degrade), **`SoftwareOnly`**. See the framework overview in
[`codec/hwaccel/README.md`](codec/hwaccel/README.md), the design rationale and
per-platform probe mapping in [`codec/hwaccel/DESIGN.md`](codec/hwaccel/DESIGN.md),
and the shared `Frame`/`Packet` carriers (NV12/I420 planes; Annex-B / OBU / VP9
framing) in [`video/README.md`](video/README.md). Measured hardware throughput is in
the [hardware-video section of BENCHMARKS.md](BENCHMARKS.md#hardware-video-hwaccel).

## The rest of the toolkit

- **[resample](resample)** — pure-Go port of libsamplerate; sinc / linear / ZOH converters with a parity oracle.
- **[mutations](mutations)** — sample transforms: format conversion (int16/int24/float32/…), interleave/deinterleave, gain, fades, trim, chunk; the `Audio` value type.
- **[generators](generators)** — test-signal helpers (sine, chord, sweep, noise).
- **[timeline](timeline)** — Cue/Source playback engine: clips, fades, transforms, and nested timelines.
- **[mixer](mixer)** — sums multiple `timeline.Source` streams onto an SPSC ring for a device callback.
- **[buffers](buffers)** — lock-free SPSC ring of `float64` samples for bridging audio callbacks across goroutines.
- **[devices](devices)** — OS audio device enumeration, hotplug, and capture/render streams (CoreAudio, WASAPI, PulseAudio).
- **[events](events)** — typed pub-sub bus (generic `Bus[T]`, synchronous delivery).
- **[consts](consts)** — shared numeric constants (sample rates, channel counts, equal-temperament note frequencies).
- **[inspection](inspection)** — ad-hoc analysis utilities.
- **[tools](tools)** — example-only cross-package helpers (e.g. `audioio` bridging devices+timeline, `devicepicker` TUI). Not for production import.

## Build tags & license fences

A default **`CGO_ENABLED=0 go build ./...`** links **only MIT + permissively-licensed**
code (the project's own MIT code plus the CC0 minimp3 and BSD libFLAC references)
and links **zero LGPL and zero FDK-AAC code**. The non-permissive engines are
opt-in:

| Tag | Opts into | License | Effect when absent |
|---|---|---|---|
| **`mp3lame`** | the LAME-derived MP3 **encoder** | LGPL-2.0-or-later | `NewEncoder` → `ErrEncoderRequiresLAME`; decode still works |
| **`aacfdk`** | the FDK-AAC AAC **engine** (decode + encode) | FDK-AAC (permissive, non-FOSS) | `NewDecoder`/`NewEncoder` → `ErrEngineRequiresFDK` |
| **`cgo`** (built-in) | the vendored C reference backends | per-engine | pure-Go ports run instead |
| **`*_strict`** (`opus_strict`, `flac_strict`, `mp3_strict`, `aac_strict`) | the bit-exact parity build (FMA-free / SIMD-off / assertions on) | — | default build is the fast FMA/NEON build, not the parity gate |

The strict tags are a **correctness gate, not a shipping configuration** — the
shippable build is the default fast path. Full file-by-file map and the LGPL relink
obligations are in **[LICENSING.md](LICENSING.md)**.

## Quick start

Generate a tone, FLAC-encode it, and decode it back — FLAC and Opus need **no**
license fence and work in `CGO_ENABLED=0`:

```go
package main

import (
	"bytes"
	"io"
	"log"
	"time"

	flaccodec "go-mediatoolkit/codec/flac"
	"go-mediatoolkit/generators"
)

func main() {
	// One second of a 440 Hz mono tone.
	tone := generators.Sine(440, time.Second, 44100)

	// Encode to a FLAC byte stream.
	var buf bytes.Buffer
	enc, err := flaccodec.NewEncoder(&buf, tone.SampleRate, tone.Channels,
		flaccodec.WithCompressionLevel(8),
		flaccodec.WithTotalSamples(uint64(tone.Frames())),
	)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := enc.Write(tone); err != nil {
		log.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		log.Fatal(err)
	}

	// Decode it back to interleaved float64 samples.
	dec, err := flaccodec.NewDecoder(&buf)
	if err != nil {
		log.Fatal(err)
	}
	out := make([]float64, 8192)
	var total int
	for {
		audio, err := dec.Read(out)
		total += len(audio.Data)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
	}
	log.Printf("decoded %d samples at %d Hz", total, dec.SampleRate())
}
```

A ready-to-run version of the above is `go run ./codec/flac/examples/roundtrip`.

Every codec follows the same `Encoder.Write` / `Decoder.Read` shape, so swapping
`codec/flac` for `codec/opus`, `codec/mp3`, `codec/aac`, or `codec/pcm` changes
only the constructor (and, for AAC/MP3 encode, the build tag).

### Running things

```sh
# Whole suite — green out of the box. Uses cgo by default, so it links the
# vendored C reference backends and needs a C compiler (clang/gcc) on PATH.
go test ./...

# Runnable examples (each codec/container has standalone binaries under examples/)
go run ./codec/flac/examples/roundtrip
go run ./codec/opus/examples/pipeline
go run ./codec/hwaccel/examples/probe        # enumerate hardware backends; runs everywhere
go run ./devices/examples/list               # list audio devices; safe, no hardware needed

# Bit-exact parity gates (need the strict tag + a -ffp-contract=off cgo oracle via mise)
MISE_EXPERIMENTAL=1 mise run //libraries/flac:parity
MISE_EXPERIMENTAL=1 mise run //libraries/opus:parity:benchcmp
MISE_EXPERIMENTAL=1 mise run //libraries/mp3:parity         # decode; encode-parity needs mp3lame
MISE_EXPERIMENTAL=1 mise run //libraries/aac:parity         # exact-integer, -tags 'aac_strict aacfdk'
```

The **build** claims in this README are about the pure-Go default — `CGO_ENABLED=0
go build ./...` links zero C. The **test** command above is different: `go test
./...` builds with cgo enabled by default, so it compiles and links the C reference
backends and therefore needs a C compiler (clang/gcc). A bare `go test ./...` is
genuinely green and exercises the default (fast, non-strict) build alongside the
cgo reference paths — but it does **not** run the bit-exact oracles. Those parity
gates additionally need clang with `-ffp-contract=off`, supplied via the mise env
together with the `*_strict` tags (see [CONTRIBUTING.md](CONTRIBUTING.md)); they do not run
under a bare `go test`.

The other `devices/` examples (`echo`, `play`, `record`, `watch`) and the
`timeline`/`tools` playback examples open real audio hardware and run until
`Ctrl-C`; only `devices/examples/list` is hardware-free.

## Pointers

- **[BENCHMARKS.md](BENCHMARKS.md)** — native (pure-Go) vs C (cgo) throughput per codec, and the measured hardware-video numbers. Headlines: every native decoder runs **600–1400× realtime**; Opus CELT encode is within **12%** of libopus; AAC **encode is faster** than the C reference (0.75×); Arc A380 VAAPI sustains ~470 fps H.264→H.265 transcode at 480p.
- **[LICENSING.md](LICENSING.md)** — the file-by-file license-fence map (`mp3lame`/LGPL, `aacfdk`/FDK) and what each build links.
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — contributor guide: package list, style rules, build-tag conventions, and the parity-gate commands.
- **Per-package READMEs** — every package has its own README with the full API, options, and examples: each codec, each container, the hwaccel framework, and the rest of the toolkit including [timeline](./timeline/README.md), [mutations](./mutations/README.md), [generators](./generators/README.md), [consts](./consts/README.md), and [devices](./devices/README.md).

## License

go-mediatoolkit is **MIT** (see [LICENSE](LICENSE)). A few vendored engines and the
Go code derived from them carry their own licenses, all opt-in behind build tags —
see [LICENSING.md](LICENSING.md). The default `CGO_ENABLED=0` build is 100% MIT +
permissive.
