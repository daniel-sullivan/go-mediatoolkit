# aac

Pure-Go 1:1 port of the **Advanced Audio Coding (AAC)** packet codec, derived
from the vendored **Fraunhofer FDK-AAC** reference (mstorsjo fork, v2.0.3),
with an optional Cgo path that links the same FDK-AAC C source directly.

AAC is a **lossy, packet-based** codec: an AAC stream is a sequence of
independent access units (raw data blocks / packets), each decoding to a fixed
number of samples per channel — 1024 for AAC-LC, or 2048 for the long-frame
(SBR) variants. Each frame is coded with:

- **Modified Discrete Cosine Transform (MDCT)** — the time/frequency
  transform, with short/long block switching.
- **Scalefactor-band quantization + Huffman coding** — the spectral
  coefficients are quantized per band and entropy-coded.
- **Coding tools** — TNS (temporal noise shaping), PNS (perceptual noise
  substitution), intensity stereo, M/S stereo, and optionally SBR (HE-AAC) / PS.

The full family is **complete** here — decode is bit-exact and encode is
byte-identical vs the FDK reference across all three:

| Profile | Object type | Decode | Encode | Output transform |
|---|---|---|---|---|
| **AAC-LC** | `AOTAACLC` (2) | bit-exact | byte-identical (+ VBR) | none (rate/channels as coded) |
| **HE-AAC v1** | `AOTSBR` (5) | bit-exact | byte-identical | **SBR** doubles the sample rate (AAC-LC core + regenerated high band) |
| **HE-AAC v2** | `AOTPS` (29) | bit-exact stereo (native + cgo) | byte-identical | SBR + **parametric stereo**: a mono core is upmixed to a 2-channel output |

HE-AAC v2 encode takes a **stereo** input and produces a mono AAC-LC core +
SBR + PS side-info; a non-stereo input returns [`ErrPSRequiresStereo`]. The
output rate/channels of an HE-AAC stream differ from the core's, and
[`AudioSpecificConfig.Output`](#types) projects the true decoded format from the
ASC's explicit-hierarchical signalling.

Unlike FLAC, AAC has **no single canonical byte-stream framing** — access
units may be carried in ADTS, LATM/LOAS, or an ISOBMFF/MP4 sample table — so,
like Opus, this library works on **individual packets**. Framing is a container
concern: AAC rides in **MP4** ([`containers/mp4`](../../containers/mp4), `esds`
ASC + sample tables) and in raw **ADTS** `.aac`
([`containers/adts`](../../containers/adts), self-framed 7-byte frame headers).

## The FDK-AAC fence (read this first)

**FDK-AAC is the only AAC engine in this repo, for both decode and encode.**
Every AAC code path — the vendored C++ source, the cgo translation-unit
wrappers, the cgo backends, and the whole pure-Go port under
`internal/nativeaac` — is derived from FDK-AAC and is therefore fenced behind
the opt-in **`aacfdk` build tag**:

- A default `go build ./...` (no `aacfdk`) links **zero** FDK-AAC code. The
  public seams (`decoder.go` / `encoder.go`, built `//go:build !aacfdk`) then
  return [`ErrEngineRequiresFDK`] from `NewDecoder`/`NewEncoder` **and**
  `NewNativeDecoder`/`NewNativeEncoder`. There is no FDK-free AAC fallback —
  the engine is the only engine.
- Building with `-tags aacfdk` (and `CGO_ENABLED=1`) compiles the FDK-AAC C
  reference and routes the public constructors to it.
- The pure-Go port (`internal/nativeaac`) is also FDK-derived, so it too is
  `//go:build aacfdk`; `NewNativeDecoder`/`NewNativeEncoder` reach it only
  under the tag.

Every engine-derived file carries a leading `// SPDX-License-Identifier:
FDK-AAC` header and the `aacfdk` (and, for the C path, `cgo`) build tag. The
sibling `containers/mp4` ISOBMFF parser and the `codec/aac` streaming adapter
are **MIT/untagged** — they compile in the default build and only surface
`ErrEngineRequiresFDK` at use. See [`LICENSING.md`](../../LICENSING.md) for the
full file-by-file / build-tag license map and the FDK obligations.

```sh
go build ./libraries/aac/                 # default: no FDK linked; ErrEngineRequiresFDK at use
go build -tags aacfdk ./libraries/aac/    # FDK-AAC engine (requires CGO_ENABLED=1 for the C path)
```

> **FDK-AAC license** — SPDX `FDK-AAC`: permissive (attribution + carried
> license/patent notices) but **non-FOSS** and **not** copyleft, so it imposes
> no relink/source-availability obligation on the rest of your app. It grants
> no patent licenses; AAC-LC patents **expired in 2017**, so the AAC-LC target
> here carries no live patent concern. Read `libfdk/COPYING` in full before
> shipping an `aacfdk` binary.

## Usage

```go
import "go-mediatoolkit/libraries/aac"
```

`libraries/aac` operates on interleaved **`float64`** samples normalized to
`[-1.0, 1.0]` and encodes/decodes one **AAC access unit** at a time. For
stereo: `[L0, R0, L1, R1, ...]`. The out-of-band [`AudioSpecificConfig`](#types)
(the MPEG-4 ASC, normally carried in the MP4 `esds` box) tells the decoder the
profile, sample rate, and channel layout.

Three layers sit on top of this package, mirroring the rest of go-mediatoolkit:

| Package | Works with | Use for |
|---|---|---|
| `libraries/aac` | `float64` samples ↔ AAC access units (packets) | direct per-packet encode/decode, lowest overhead |
| `codec/aac` | `float64` samples (`mutations.Audio`) ↔ packets via `PacketReader`/`PacketWriter` | streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention) |
| `containers/mp4` | ISOBMFF box tree + iTunes tag projection | reading/writing `.m4a`/`.mp4`: `esds` ASC, `stsz`/`stsc`/`stco` sample tables, `ilst` metadata |

The codec-vs-container split is identical to `codec/opus` + `containers/ogg/opus.go`:
the AAC library is packet-oriented; framing and tags belong to `containers/mp4`,
not the codec.

### Decoding (requires `-tags aacfdk`)

```go
dec, err := aac.NewDecoder(asc) // asc parsed from the MP4 esds box
// NewDecoder returns ErrEngineRequiresFDK unless built -tags aacfdk.

// pcm must hold at least one full frame: FrameSamples × Channels() float64s.
pcm := make([]float64, aac.FrameSamplesLong*dec.Channels())
n, err := dec.Decode(pkt, pcm) // n = samples-per-channel produced
// pcm[:n*dec.Channels()] holds interleaved samples
```

### Encoding (requires `-tags aacfdk`)

```go
enc, err := aac.NewEncoder(44100, 2,
    aac.WithObjectType(aac.AOTAACLC),
    aac.WithBitrate(128000),
)
// NewEncoder returns ErrEngineRequiresFDK unless built -tags aacfdk.

// Encode exactly one frame of interleaved PCM (FrameSamples × Channels).
pkt, err := enc.Encode(frame)

// enc.Config() returns the AudioSpecificConfig the container layer must emit
// (e.g. in the MP4 esds box).
asc := enc.Config()
```

### Streaming float64 (`codec/aac`)

```go
import aaccodec "go-mediatoolkit/codec/aac"

dec, err := aaccodec.NewDecoder(packetReader, asc)            // packets → mutations.Audio
enc, err := aaccodec.NewEncoder(packetWriter, 44100, 2,       // float64 in, packets out
    aaccodec.WithBitrate(128000))
```

### M4A files (`containers/mp4`)

```go
import "go-mediatoolkit/containers/mp4"

rd, err := mp4.NewReader(r)            // parses the ISOBMFF box tree (MIT; no FDK)
hdr := rd.Header()                     // SampleRate/Channels + tags + Extra.Config (ASC)
dec, _ := aaccodec.NewDecoder(rd.Packets(), hdr.Extra.Config) // decode needs -tags aacfdk
```

## Implementation

The engine has two interchangeable backends, both `//go:build aacfdk`:

| Constructor | `-tags aacfdk` + cgo | `-tags aacfdk`, cgo disabled | default (no `aacfdk`) |
|-------------|----------------------|------------------------------|------------------------|
| `NewEncoder` / `NewDecoder` | FDK-AAC C reference (via cgo) | pure-Go FDK port | `ErrEngineRequiresFDK` |
| `NewNativeEncoder` / `NewNativeDecoder` | pure-Go FDK port | pure-Go FDK port | `ErrEngineRequiresFDK` |

Both the cgo C reference and the pure-Go port are FDK-derived and require the
`aacfdk` tag — there is **no** cgo-disabled, FDK-free path. (This differs from
`libraries/flac`, whose native port is original-licensed and ships in the
default build; AAC's whole engine is FDK, so it is fenced.)

> **Status: complete.** The public API, the `codec/aac` + `containers/mp4` +
> `containers/adts` layers, and the parity/bench wiring are all in place, and
> the whole AAC family is ported and verified:
>
> - **AAC-LC** — decode is **bit-exact** (`NewNativeDecoder().Decode`, validated
>   by the per-stage + `decode-e2e` parity slices) and encode is
>   **byte-identical** (`NewNativeEncoder().Encode` drives the ported
>   `EncodeFrame` → `psyMain` → `QCMain` → bitstream chain; CBR and VBR).
> - **HE-AAC v1 (SBR)** — `internal/nativeaac/heaac` SBR-doubles the 1024-sample
>   AAC-LC core to a 2048-sample output at the doubled rate; decode bit-exact,
>   encode byte-identical.
> - **HE-AAC v2 (PS)** — parametric stereo upmixes a mono core to a bit-exact
>   stereo output on decode (native + cgo); encode takes stereo input → mono
>   core + SBR + PS and is byte-identical (`ErrPSRequiresStereo` for non-stereo).
>
> Each stage carries its own exact-integer parity slice vs the FDK oracle (see
> the slice list below). The cgo backend and the vendored C reference are wired
> by the same build-tag split as `libraries/flac`
> (`decoder.go`/`decoder_cgo.go`, `encoder.go`/`encoder_cgo.go`), here gated by
> `aacfdk`.

**Tags on decode.** AAC carries no tags in the codec stream; artist/title/
cover-art are an MP4 (`ilst`) concern — read them via `containers/mp4`, which
projects the iTunes atoms onto `containers.StandardTags`.

### Build tags

| Tag | Effect | When to use |
|-----|--------|-------------|
| **`aacfdk`** | **Required** for any working AAC path. Fences the FDK-AAC engine (the only AAC engine) — the vendored C, the cgo backends, and the pure-Go port. The C backend additionally needs `CGO_ENABLED=1`; with cgo off, `aacfdk` selects the pure-Go FDK port. | **Any AAC use.** Default builds surface `ErrEngineRequiresFDK`. |
| `aac_strict` | Un-skips the in-package **integer-parity** assertions and strict-gated tests (flips `nativeaac.StrictMode`). It does **not** select a different arithmetic build — see below. | Parity testing. Composed with `aacfdk` by the `mise` tasks. |

```sh
go build -tags aacfdk ./libraries/aac/                  # FDK-AAC engine
go build -tags 'aac_strict aacfdk' ./libraries/aac/     # + integer-parity assertions on
```

### Bit-exactness & parity — fixed-point, exact integer

**FDK-AAC is a FIXED-POINT codec.** Both AAC-LC decode and the ported encoder
kernels operate on `int32` Q-format fixed-point values (`FIXP_SGL` /
`FIXP_DBL`): the bit-unpacking, Huffman/spectrum decode, inverse quantization,
the TNS/stereo tools, the fixed-point FFT/DCT/MDCT filterbank with its
per-stage scale headroom, and the encoder quantizer/rate-control and bitstream
syntax are all integer arithmetic. The reproducibility contract is therefore
**EXACT integer equality** on decode and a **byte-identical bitstream** on
encode — there is **no floating-point, no FMA fusion, and no ULP tolerance**.

This is fundamentally different from the FP-parity discipline used by
[`opus`](../opus) and [`flac`](../flac):

- **No FP-strict build split.** There are no `*_fp_strict.go` / `*_fp_default.go`
  files, no FMA-free float helpers, and no `cosf`/`sinf` double-shim, because
  there is no float path to make deterministic. Any such FP framing in AAC
  docs/code is a bug.
- **`aac_strict` is not an arithmetic switch.** It only flips
  `nativeaac.StrictMode` so the strict-gated integer-parity assertions run
  instead of `t.Skip()`; a bare `go test` stays clean while the strict run
  asserts exact equality against the oracle.
- **The oracle's `-ffp-contract=off …` flags are belt-and-suspenders.** The
  `mise` tasks compile the cgo oracle with `-O2 -ffp-contract=off
  -fno-vectorize -fno-slp-vectorize -fno-unroll-loops` only to mirror the
  opus/flac oracle convention; for these integer kernels the fixed-point
  arithmetic is bit-identical regardless.

The native Go port is validated against the vendored FDK-AAC C reference
(called via cgo) as the oracle, across the per-stage parity packages under
`internal/parity_tests/`:

- **AAC-LC decode:** `frame-sync-parse`, `ics-parse`, `huffman-spectral-decode`,
  `dequant`/`inverse-quant`, `tns-decode`, `ms-stereo-decode`,
  `fft`/`dct`/`filterbank`, `decode-e2e`.
- **AAC-LC encode:** `enc-analysis-filterbank`, `enc-block-switch`, `enc-bandwidth`,
  `enc-psy-config`, `enc-psy-model`, `enc-psy-main`/`psymain-multiframe`,
  `psychoacoustics-encoder`, `enc-sf-estim`, `enc-quantize`, `enc-adj-thr`,
  `enc-qc-main`/`enc-qc-main-loop`, `enc-stereo-tns`,
  `enc-tns-full`/`enc-tns-finish`/`enc-tns-gauss`, `enc-intensity`, `enc-pns`,
  `channel-map`, `enc-init`, `bitstream-encode`/`enc-bitstream`, `enc-frame`,
  the assembled `encode-e2e`, and the VBR end-to-end `enc-vbr-e2e`.
- **HE-AAC v1 (SBR):** the QMF foundation `sbr-qmf`; decode `sbr-env-extr`,
  `sbr-env-calc`, `sbr-dec-env`, `sbr-dec-hfgen`, `sbr-dec-e2e`,
  `sbr-dec-codec-e2e`; encode `sbr-enc-analysis`, `sbr-enc-est`,
  `sbr-enc-toncorr`, `sbr-enc-freqsca`, `sbr-enc-code-env`, `sbr-enc-resampler`,
  `sbr-enc-bitwrite`, `sbr-enc-e2e`.
- **HE-AAC v2 (PS):** decode `ps-dec-parse`, `ps-dec-hybrid`, `ps-dec-apply`,
  `ps-dec-e2e`; encode `ps-enc-downmix`, `ps-enc-extract`, `ps-enc-sbrext`,
  `ps-enc-bitwrite`, `ps-enc-e2e`.
- **shared:** `rom-tables` (the fixed-point ROM/constant tables) and
  `benchcmp` (the Go-vs-scalar-C benchmark suite).

Run the real gate (requires a C toolchain — the oracle compiles the vendored
FDK-AAC):

```sh
# from the repo root (monorepo task form) — this is the gate
MISE_EXPERIMENTAL=1 mise run //libraries/aac:parity   # -tags 'aac_strict aacfdk'
MISE_EXPERIMENTAL=1 mise run //libraries/aac:test     # full AAC suite, strict + aacfdk

# from libraries/aac/
mise run parity
mise run test
```

Each task sets `CGO_CFLAGS='-O2 -ffp-contract=off …'` and
`CGO_CFLAGS_ALLOW='.*'` (Go's cgo flag allowlist rejects `-ffp-contract=off`
in-source) plus `-tags 'aac_strict aacfdk'`. The `aacfdk` tag is **load-bearing
for the gate**: it is what makes the cgo parity slices (each `//go:build cgo &&
aacfdk`) actually compile and run, rather than vacuously skip — `aac_strict`
alone would leave them unbuilt. A bare `go test ./internal/parity_tests/...`
without the tags and env builds none of the FDK-gated slices.

### Testing without the mise gate

To run the functional suites (encode/decode round-trips, the public API
contract) without the strict integer-parity oracle, build with just the
`aacfdk` tag — no `aac_strict`, no `-ffp-contract=off` env, no `mise`:

```sh
go test -tags aacfdk ./...     # functional AAC suites (engine on, parity oracle off)
```

This exercises the real FDK engine and the native port end-to-end; it does
**not** assert exact-integer equality against the cgo oracle (that is the
`aac_strict` gate above). Caveat: some synthetic HE-AAC v2 (PS) ASCs are
validated more strictly by the FDK C engine than by the native port — a
hand-constructed config the C engine rejects may decode on the port (and vice
versa), so config-rejection differences there are expected, not parity misses.

### Benchmarks

`internal/parity_tests/benchcmp/` benchmarks the pure-Go `nativeaac` path
against the vendored FDK-AAC reference (cgo), mirroring the [flac](../flac)
benchcmp suite. The C column is the same scalar oracle the parity tests use, so
it is an apples-to-apples scalar comparison — **not** native-vs-production-C.

```sh
MISE_EXPERIMENTAL=1 mise run //libraries/aac:bench            # Go default vs scalar C oracle
MISE_EXPERIMENTAL=1 mise run //libraries/aac:bench:strict     # Go aac_strict (assertions on) vs oracle
MISE_EXPERIMENTAL=1 mise run //libraries/aac:bench:production  # Go vs production-style C (-O2)
```

The fixed-point kernels are exact-integer in both the default and `aac_strict`
builds, so those bench tasks differ only in whether the integer-parity
assertions are active, not in arithmetic. The repo-root
[`BENCHMARKS.md`](../../BENCHMARKS.md) tabulates the native-vs-cgo results;
notably AAC **encode** is the standout where the pure-Go port runs *faster*
(~0.75×) than the production C reference (the FDK afterburner trades CPU for
quality; the port skips that mode), while decode runs comfortably faster than
real time.

## API

### Types

- `AudioSpecificConfig` — the MPEG-4 ASC: `ObjectType` (`AudioObjectType`),
  `SampleRate`, `Channels`, `FrameSamples`, and `Raw` (verbatim ASC bytes for
  byte-for-byte re-mux). Its `Output() (sampleRate, channels int)` method
  resolves the **true decoded** rate/channels from the explicit-hierarchical
  HE-AAC signalling: AOT-5/SBR reports the SBR-doubled rate, AOT-29/PS also
  reports a stereo (2-channel) output for its mono core. Plain AAC passes
  through unchanged.
- `AudioObjectType` — the AAC profile / coding tool set: `AOTNull`,
  `AOTAACMain`, `AOTAACLC`, `AOTAACSSR`, `AOTAACLTP`, `AOTSBR` (HE-AAC v1), and
  `AOTPS` (= 29, HE-AAC v2 / parametric stereo) (all with `String()`)
- `StreamInfo` — `Config`, `SampleRate`, `Channels`, `FrameSamples`

### Interfaces

**Decoder:**
- `Decode(pkt []byte, pcm []float64) (samplesPerChannel int, err error)` —
  decode one access unit; `pcm` must hold ≥ `FrameSamples × Channels()`
- `SampleRate() int`, `Channels() int`, `Config() AudioSpecificConfig`, `Reset()`

**Encoder:**
- `Encode(pcm []float64) ([]byte, error)` — encode exactly one frame
  (`FrameSamples × Channels()`) into one access unit
- `Config() AudioSpecificConfig` — the ASC bytes the container must emit (e.g.
  in `esds`)
- `SampleRate() int`, `Channels() int`, `Reset()`

Constructors: `NewDecoder` / `NewEncoder` (tag-routed: FDK engine under
`aacfdk`, else `ErrEngineRequiresFDK`); `NewNativeDecoder` / `NewNativeEncoder`
(always the pure-Go FDK port — still `aacfdk`-gated).

### Options

- Decoder: `DecoderOption` (reserved for future decode-time options, e.g. SBR
  downsample mode)
- Encoder: `WithObjectType(AudioObjectType)` (default `AOTAACLC`; `AOTSBR` for
  HE-AAC v1, `AOTPS` for HE-AAC v2), `WithBitrate(bps int)` (default 128000,
  CBR), `WithVBR(quality int)` (1..5 low→high, selects variable bitrate and
  makes `WithBitrate` ignored)

### Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `FrameSamplesShort` | 1024 | Samples/channel per AAC-LC access unit |
| `FrameSamplesLong` | 2048 | Samples/channel per long-frame (SBR) access unit |
| `MaxChannels` | 8 | Maximum channel count |
| `MaxFrameBytes` | 8191 | Maximum on-wire access-unit size (ADTS 13-bit length) |
| `MaxSampleRate` | 96000 | Maximum standard sample rate (Hz) |

### Errors

- `ErrBadArg` — invalid argument (unsupported sample rate, channel count, or ASC)
- `ErrEngineRequiresFDK` — `New*` called in a build without the `aacfdk` tag
  (FDK-AAC is the only engine; rebuild `-tags aacfdk`)
- `ErrInvalidPacket` — corrupted/malformed AAC access unit
- `ErrInvalidConfig` — malformed/unsupported `AudioSpecificConfig`
- `ErrBufferTooSmall` — output buffer too small for the decoded frame
- `ErrUnimplemented` — a profile/coding tool not yet implemented by the pure-Go port
- `ErrEncodeFailed` — the encoder rejected the input frame (a non-OK
  `AAC_ENCODER_ERROR` from the 1:1 FDK encode path)
- `ErrPSRequiresStereo` — an HE-AAC v2 (`AOTPS`) encoder was constructed with a
  channel count other than 2 (parametric stereo encodes a stereo input to a
  mono core, so it needs exactly two input channels)
- `ErrInternal` — internal codec error

## License

This package is the **FDK-AAC island**. The vendored C++ source under
`libfdk/` and every engine-derived file (the cgo wrappers, the cgo backends,
and the whole `internal/nativeaac` port — decode **and** encode) are
**Fraunhofer FDK-AAC** licensed (SPDX `FDK-AAC`) and fenced behind the
`aacfdk` build tag; a default build links none of them. The sibling
`containers/mp4` and `codec/aac` adapters are MIT. See
[`libfdk/COPYING`](libfdk/COPYING) and the repo-root
[`LICENSING.md`](../../LICENSING.md) for the full map and obligations.
