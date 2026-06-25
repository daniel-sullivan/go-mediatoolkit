# mp3

Go implementation of the MP3 (MPEG-1/2/2.5 Audio Layer III) lossy audio codec, with optional Cgo paths wrapping the reference C [minimp3](https://github.com/lieff/minimp3) **decoder** and [LAME 3.100](https://lame.sourceforge.io/) **encoder**.

MP3 is a **self-framed** codec: the byte stream itself carries frame-sync headers, per-granule side information, scale factors, and Huffman-coded spectral data in a continuous sequence — there is no outer container. Each frame decodes through: frame sync + header parse, side-info parse, a cross-frame **bit reservoir** that reassembles main-data, Huffman decode + requantization of the spectral lines, then the IMDCT and polyphase synthesis filterbank back to 16-bit PCM. Encoding runs the inverse: an MDCT analysis filterbank + FFT, a psychoacoustic model that allocates bits by masking threshold, quantization, and Huffman coding.

> **Two engines, two licenses.** Decode is **minimp3** (CC0 / public domain) and is in the default MIT build. Encode is a 1:1 port of **LAME** (LGPL-2.0-or-later) and is fenced behind the **`mp3lame`** build tag; a default build links no LGPL code and `NewEncoder` returns `ErrEncoderRequiresLAME`. See [License](#license) and [`LICENSING.md`](../../LICENSING.md).

## Usage

```go
import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3"
```

`libraries/mp3` operates on interleaved **`int16`** samples (signed 16-bit PCM — MP3's natural decode type) and reads/writes a continuous MP3 byte stream of self-framed frames. For stereo: `[L0, R0, L1, R1, ...]`.

Three layers sit on top of this package, mirroring the rest of go-mediatoolkit:

| Package | Works with | Use for |
|---|---|---|
| `libraries/mp3` | `int16` samples ↔ MP3 byte stream | direct frame-by-frame encode/decode, lowest overhead |
| `codec/mp3` | `float64` samples (`mutations.Audio`) ↔ MP3 byte stream | streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention) |
| `containers/mp3` | ID3v2 / ID3v1 metadata + tag projection | inspecting/writing artist/title/album tags, album art |

### Decoding

```go
dec, err := mp3.NewDecoder(r)

// buf must hold at least one full frame: MaxSamplesPerFrame × Channels() int16 values.
buf := make([]int16, mp3.MaxSamplesPerFrame*mp3.MaxChannels)
for {
    n, err := dec.DecodeFrame(buf) // n = samples-per-channel produced
    if err == io.EOF {
        break
    }
    // buf[:n*dec.Channels()] holds interleaved samples;
    // (0, nil) means a non-audio frame (e.g. an ID3 or skipped frame) was consumed.
}
```

### Encoding (requires `-tags mp3lame`, LGPL)

```go
info := mp3.StreamInfo{SampleRate: 44100, Channels: 2}
enc, err := mp3.NewEncoder(w, info,   // returns ErrEncoderRequiresLAME unless built -tags mp3lame
    mp3.WithBitRate(192000),
    mp3.WithQuality(2),
)

// Submit interleaved int16 samples (length a multiple of Channels).
err = enc.EncodeFrame(samples)

// Close flushes the encoder's final frames. The underlying writer is not closed.
err = enc.Close()
```

The encoder is a derivative of LAME and is therefore LGPL-2.0-or-later; it is compiled in only when the `mp3lame` build tag is set. Without the tag, both `NewEncoder` and `NewNativeEncoder` return `ErrEncoderRequiresLAME` and the binary links no LGPL code (see [Build tags](#build-tags) and [License](#license)).

### Streaming float64 (`codec/mp3`)

```go
import codecmp3 "github.com/daniel-sullivan/go-mediatoolkit/codec/mp3"

dec, err := codecmp3.NewDecoder(r)                    // io.Reader → mutations.Audio
enc, err := codecmp3.NewEncoder(w, 44100, 2,          // float64 in, MP3 out (needs -tags mp3lame)
    codecmp3.WithBitRate(192000), codecmp3.WithQuality(2))
```

### Metadata & tags (`containers/mp3`)

```go
import ctrmp3 "github.com/daniel-sullivan/go-mediatoolkit/containers/mp3"

rd, err := ctrmp3.NewReader(r)   // parses the leading ID3v2 (and trailing ID3v1 if seekable)
hdr := rd.Header()               // SampleRate/Channels + standard tags + ID3 extras
dec, _ := mp3.NewDecoder(rd.Data())  // rd.Data() replays ID3 prefix + audio frames
```

## Implementation

The package self-frames (no outer container): the codec is the framing. Two reference engines back it — **minimp3** for decode, **LAME** for encode — each with a vendored C cgo path and a pure-Go 1:1 port under `internal/nativemp3/`.

When built with `CGO_ENABLED=1` (default on most platforms), `NewDecoder` uses the vendored C minimp3; `NewEncoder` uses the vendored C libmp3lame **when also built with `-tags mp3lame`**. The pure-Go port is used when cgo is disabled or when explicitly requested via `NewNativeDecoder`/`NewNativeEncoder`.

| Constructor | Cgo enabled | Cgo disabled | `mp3lame` required? |
|-------------|-------------|--------------|---------------------|
| `NewDecoder` / `NewNativeDecoder` | C minimp3 (cgo) / Native Go | Native Go | no — decode is always available |
| `NewEncoder` | C libmp3lame (cgo) | Native Go (LAME port) | **yes** — else `ErrEncoderRequiresLAME` |
| `NewNativeEncoder` | Native Go (LAME port) | Native Go (LAME port) | **yes** — else `ErrEncoderRequiresLAME` |

To force the pure-Go decode path:

```sh
CGO_ENABLED=0 go build ./libraries/mp3/
```

**Tags on decode.** ID3 tag/metadata parsing is a **container** concern — neither backend surfaces tags from `libraries/mp3` itself (matching the [opus](../opus) and [flac](../flac) native paths). Read tags via `containers/mp3`, which parses the ID3v2/ID3v1 chain on either path.

**Streaming-only encode.** Both encoder backends are append-only (no `io.WriteSeeker`); any trailing/header frames are finalized at `Close`. (Same model as the opus and flac encoders.)

### Build tags

Two independent build tags apply. They compose: a parity build sets both (`-tags 'mp3lame mp3_strict'`).

| Tag | Gates | Effect |
|-----|-------|--------|
| `mp3lame` | the **LGPL** encoder (the LAME-derived cgo wrappers + the pure-Go LAME port). | Compiles in the MP3 encoder. **Without it, no LGPL code is linked** and the encoder constructors return `ErrEncoderRequiresLAME`. The default MIT build (with or without cgo) is decode-only. |
| `mp3_strict` | **floating-point fusion** on the bit-exact paths (decode *and* encode). | Routes every float mul/add through `//go:noinline` helpers so the compiler cannot fuse `a*b+c` into a single-rounded FMA, and computes transcendentals via the double kernel narrowed to `float32` (`float32(math.Cos(float64(x)))`). Matches the C reference compiled with `-ffp-contract=off` bit-for-bit. Slower (no FMA / SIMD); used for parity testing and toolchain-independent output. |

```sh
go build ./libraries/mp3/                              # default: decode-only, FMA-fused
go build -tags mp3lame ./libraries/mp3/                # + LGPL encoder, FMA-fused
go build -tags 'mp3lame mp3_strict' ./libraries/mp3/   # + LGPL encoder, bit-exact parity mode
```

> The integer slices (bit reader, frame sync, side-info, reservoir, Huffman-tree traversal, bit allocation) are bit-identical regardless of the `mp3_strict` tag; the tag only affects the floating-point requantization / IMDCT / synthesis (decode) and the MDCT analysis / FFT / psychoacoustic model (encode). The default build is within PSNR noise of the reference but not bit-identical in every corner. `nativemp3.StrictMode` reflects the active tag for code that must branch at runtime, but file-level `*_fp_strict.go` / `*_fp_default.go` splits are preferred.

> **The two tags fence different licenses.** The decode-side FP helpers (`huffman_fp_strict.go` / `huffman_fp_default.go`) are MIT and gate only on `mp3_strict`. The encode-side FP helpers (`mdct_analysis_fp_*.go`, `psymodel_fp_*.go`, `frame_encode_fp_*.go`) are **LGPL** and gate on `mp3lame && mp3_strict`. Do not move a file across that line — see [`LICENSING.md`](../../LICENSING.md).

### Bit-exactness & parity

MP3 decode and encode are **lossy** and floating-point heavy, so the bit-exact bar applies only under `mp3_strict`. The native Go port is validated against C minimp3 (decode) and C libmp3lame (encode), called via Cgo, as the oracle. The FP-parity convention is shared with the [opus](../opus) and [flac](../flac) ports:

- The cgo oracle is compiled with `-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops` (plus `MINIMP3_NO_SIMD` in-source for the decoder), so every C multiply/add is separately rounded.
- The Go `mp3_strict` build is FMA-free (`//go:noinline` float helper families) for the FP slices.
- Reference `static` helpers are surfaced to each oracle TU via thin trampolines; each parity package compiles its **own** private copy of minimp3 / LAME (no import of `libraries/mp3`, which would link the engine twice and clash on its statics / `Min`/`Max` macros).

| Package | Engine | Checks |
|---|---|---|
| `bitreader` | minimp3 | `bs_init` / `get_bits` — MSB-first bit reader value + (pos, limit) state, overrun, zero-width |
| `bitstream-format` | minimp3 | frame-header field accessors (`hdr_*`) + format validators |
| `main-bits` | minimp3 | frame sync (`mp3d_find_frame` / `mp3d_match_frame`), header accessors, and the bit reservoir (`L3_save_reservoir` / `L3_restore_reservoir`) |
| `frame-decode-dispatch` | minimp3 | the `mp3dec_decode_frame` driver: frame detect, info fill, granule/synthesis dispatch, sample count |
| `huffman-decode` | minimp3 | L3 Huffman spectral decode against minimp3's `L3_huffman` |
| `dequantize` | minimp3 | scalefactor decode + requantization of the spectral lines |
| `stereo-decoding` | minimp3 | MS / intensity stereo reconstruction |
| `imdct-synthesis-filterbank` | minimp3 | IMDCT + polyphase synthesis filterbank back to PCM |
| `frame-encode-dispatch` | LAME | `lame_encode_mp3_frame` dispatcher (LGPL) |
| `mdct-analysis` | LAME | MDCT analysis filterbank + FFT (LGPL) |
| `psychoacoustic-model` | LAME | the masking-threshold psy model (LGPL) |
| `huffman-encode` | LAME | Huffman code-book selection + emission (LGPL) |
| `quantize-pvt` | LAME | ATH shaping (`athAdjust` / `ATHmdct` / `compute_ath`), `calc_xmin` allowed-distortion budget, `calc_noise` (LGPL) |
| `takehiro` | LAME | the `takehiro` count-bits / best-scalefactor-storage quantizer (LGPL) |
| `bit-allocation` | LAME | quantization / bit allocation under the reservoir (LGPL) |

Per-slice porting status (which TUs are bit-exact vs in progress) lives in the top comment of `internal/nativemp3/nativemp3.go`.

**Accepted pow/log10 ATH residual.** Every slice above is asserted **bit-for-bit** except the three ATH-shaping helpers in `quantize-pvt` (`athAdjust` / `ATHmdct` / `compute_ath`), which are asserted within a **≤ 2 ULP (float32)** bound rather than bit-exact. They rest on the C *double* transcendentals `pow()` / `log10()`, and Go's `math.Pow` / `math.Log10` are not bit-identical to the platform libm the cgo oracle links: `math.Pow` diverges by up to 1 ULP in double, and `math.Log10` is computed as `log(x)·Log10E` so it differs from libm's dedicated `log10` (e.g. `log10(1e-30)` is `-29.999999999999996` in Go vs exactly `-30` in libm). `math.Cos`/`Sin`/`Exp`/`Log` *do* match libm on the arm64 parity target — which is why the `cosf` shim works and the FMA-decomposed float32 kernels stay bit-exact — but `pow`/`log10` do not, so the opus/flac ports (and this one) route around bit-pinning them. The load-bearing budgets the residual feeds (`calc_xmin`, `calc_noise`) are themselves bit-exact; only the input-side ATH shaping carries the ULP bound. See `internal/parity_tests/quantize-pvt/parity_test.go` (`athMaxULP`) for the full rationale.

Run the suite (requires a C toolchain — the oracle compiles vendored minimp3 / LAME):

```sh
# from libraries/mp3/
mise run parity                       # the parity packages, -tags=mp3_strict
mise run test                         # all MP3 package tests, strict
mise run decode-parity                # end-to-end: encode (cgo LAME) → assert native decode == cgo decode, bit-exact

# from the repo root (monorepo task form)
MISE_EXPERIMENTAL=1 mise run //libraries/mp3:parity
```

The `mise` tasks set `CGO_CFLAGS=-O2 -ffp-contract=off …` and `CGO_CFLAGS_ALLOW=.*` (Go's cgo flag allowlist rejects `-ffp-contract=off` in-source) plus `-tags=mp3_strict` (and `mp3lame` where the encoder oracle is involved). A bare `go test ./internal/parity_tests/...` without that env and tag will diverge on the FP-heavy packages by design; the integer slices match either way.

### Benchmarks (`internal/parity_tests/benchcmp/`)

`mise run //libraries/mp3:bench` (and `bench:strict`) compares the pure-Go port against the vendored C engines over the ported slices. The C column is the same **scalar parity oracle** the parity tests use (`MINIMP3_NO_SIMD`, `-ffp-contract=off`, no auto-vectorization/unrolling), so it is an apples-to-apples scalar comparison, **not** native-vs-production-C. `bench:production` rebuilds the C oracle at default `-O2` for a more realistic C number.

```sh
mise run //libraries/mp3:bench             # default Go build vs scalar C oracle
mise run //libraries/mp3:bench:strict      # mp3_strict (FMA-free) Go vs same oracle
mise run //libraries/mp3:bench:production  # vs a default-O2 C minimp3 (more realistic C)
```

### Internal structure

The native Go implementation lives under `internal/nativemp3/` as a 1:1 port of the vendored single-header minimp3 (`libminimp3/minimp3.h`, decode) and LAME (`liblame/`, encode); each ported function carries a `file:line` reference comment to its C counterpart. Key files:

- `nativemp3.go` — package doc: per-slice porting status + the FP-parity convention
- **Decode (MIT, minimp3-derived):** `bitstream.go` (`bs_t` / `get_bits`), `header.go` + `framesync.go` (`hdr_*`, `mp3d_find_frame`), `side_info.go` + `grinfo.go` (`L3_read_side_info`), `reservoir.go` (bit reservoir), `scalefactor_bands.go`, `framedecode.go` (the `mp3dec_decode_frame` driver), `l3decode.go`, `huffman.go`, `dequantize.go`, `stereo.go`, `imdct.go`, `mdct_analysis_filterbank.go`, `synthesis_state.go`, and the decode FP split `huffman_fp_strict.go` / `huffman_fp_default.go`
- **Encode (LGPL, LAME-derived — `mp3lame` tag):** `fft.go`, `mdct_analysis.go` + `mdct_analysis_fp_strict.go` / `mdct_analysis_fp_default.go`, `psymodel*.go` + `psymodel_fp_strict.go` / `psymodel_fp_default.go`, `huffman_encode.go`, `bitstream_encode.go`, `frame_encode.go` + `frame_encode_fp_strict.go` / `frame_encode_fp_default.go`
- `strict.go` / `strict_default.go` — the `StrictMode` const selected by the `mp3_strict` tag
- `parityhooks*.go` — exported thin wrappers over the unexported ported functions so the cgo parity oracles can drive them (the encoder hooks `parityhooks_mdct.go` / `parityhooks_psymodel.go` / `parityhooks_huffman_encode.go` carry the LGPL header and the `mp3lame` tag)

The `*_fp_strict.go` / `*_fp_default.go` pairs are the FMA-free vs FMA-fused float helpers selected by `mp3_strict`. Decode-side pairs are MIT; encode-side pairs are LGPL and additionally gate on `mp3lame`.

## API

### Types

- `MPEGVersion` — `MPEGVersion1` / `MPEGVersion2` / `MPEGVersion25` (or `MPEGVersionUnknown`)
- `StreamInfo` — `Version`, `SampleRate`, `Channels`, `BitRate`, `SamplesPerFrame`

### Interfaces

**Decoder:**
- `DecodeFrame(buf []int16) (samplesPerChannel int, err error)` — fill `buf` (≥ `MaxSamplesPerFrame × Channels()`); loop until `io.EOF`. `(0, nil)` means a non-audio frame (e.g. an ID3 or skipped frame) was consumed.
- `StreamInfo() StreamInfo`, `SampleRate() int`, `Channels() int`
- `Close() error`

**Encoder** (requires `-tags mp3lame`):
- `EncodeFrame(buf []int16) error` — interleaved samples, length a multiple of `StreamInfo.Channels`
- `Close() error` — flush + finalize

### Options

- Encoder: `WithBitRate(bps int)` (CBR target, default 128000), `WithQuality(q int)` (0–9, 0 = best/slowest, default 3), `WithVBR(bool)`

### Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `MaxChannels` | 2 | Maximum channel count (stereo / joint / dual / mono) |
| `BitsPerSample` | 16 | Decoded PCM bit depth |
| `MaxSamplesPerFrame` | 1152 | Largest samples/channel per frame (MPEG-1 L3; MPEG-2/2.5 carry 576) |
| `MinSampleRate` / `MaxSampleRate` | 8000 / 48000 | Sample-rate bounds (Hz) |
| `MinBitRate` / `MaxBitRate` | 8000 / 320000 | Nominal bit-rate bounds (bits/s) |

### Errors

- `ErrBadArg` — invalid argument (nil reader/writer, or a `buf` length not a multiple of channels)
- `ErrBadSampleRate`, `ErrBadChannels`, `ErrBadBitRate` — specific out-of-range parameters
- `ErrInvalidStream` — corrupted/malformed MP3 stream (bad sync, truncated frame)
- `ErrUnsupportedStream` — a stream feature this path does not handle (e.g. free-format or an unsupported layer)
- `ErrEncoderRequiresLAME` — `NewEncoder` / `NewNativeEncoder` called from a build without `-tags mp3lame`; the LAME-derived encoder is LGPL and fenced behind that tag (decode, minimp3/CC0, is always available)
- `ErrNotImplemented` — a scaffolded-but-not-yet-ported code path
- `ErrInternal`, `ErrClosed`

## License

go-mediatoolkit is **MIT** and the default build links only MIT + permissive code:

- **Decode — MIT build.** The vendored C minimp3 (`libminimp3/minimp3.h`, by Martin Fiedler / lieff) is released into the public domain (CC0). MP3 patents have expired worldwide; the format is royalty-free. The pure-Go decoder port is MIT.
- **Encode — LGPL island, opt-in only.** The encoder is a 1:1 port of LAME, so both the vendored C (`liblame/**`, LAME 3.100, see [`liblame/COPYING.LAME`](liblame/COPYING.LAME)) and its Go translation are **LGPL-2.0-or-later**, carrying per-file `SPDX-License-Identifier: LGPL-2.0-or-later` headers. They are fenced behind the `mp3lame` build tag; a default build excludes every one of them and links **no LGPL code**. Distributing an `-tags mp3lame` binary carries the usual LGPL weak-copyleft obligations (relink / source availability).

The full map of which file is which license and which build tag gates it lives in [`LICENSING.md`](../../LICENSING.md) at the repo root.
