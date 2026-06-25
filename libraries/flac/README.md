# flac

Pure Go implementation of the FLAC lossless audio codec ([RFC 9639](https://www.rfc-editor.org/rfc/rfc9639)), with an optional Cgo path wrapping the reference C libFLAC.

FLAC (Free Lossless Audio Codec) compresses PCM audio with **no loss** — the decoded samples are bit-for-bit identical to the input. Each frame is encoded with:

- **Linear prediction** — a fixed polynomial predictor (orders 0–4) or a quantized LPC predictor (orders 1–32), chosen per subframe.
- **Rice-coded residuals** — the prediction error is entropy-coded with partitioned Rice/Golomb coding.
- **Inter-channel decorrelation** — stereo is optionally stored as left/side, right/side, or mid/side.

A STREAMINFO MD5 of the original samples lets a decoder verify losslessness.

## Usage

```go
import "github.com/daniel-sullivan/go-mediatoolkit/libraries/flac"
```

`libraries/flac` operates on interleaved **`int32`** samples (sign-extended from the stream bit depth) and reads/writes a complete native FLAC byte stream — the `fLaC` magic, metadata blocks, and audio frames. For stereo: `[L0, R0, L1, R1, ...]`.

Three layers sit on top of this package, mirroring the rest of go-mediatoolkit:

| Package | Works with | Use for |
|---|---|---|
| `libraries/flac` | `int32` samples ↔ FLAC byte stream | direct encode/decode, lowest overhead |
| `codec/flac` | `float64` samples (`mutations.Audio`) ↔ FLAC byte stream | streaming pipelines (the `codec.Decoder`/`codec.Encoder` convention) |
| `containers/flac` | metadata blocks + tag projection | inspecting/writing STREAMINFO, VORBIS_COMMENT, SEEKTABLE, … |
| `containers/ogg` | Ogg-FLAC encapsulation | FLAC carried in an Ogg container |

### Encoding

```go
info := flac.StreamInfo{SampleRate: 44100, Channels: 2, BitsPerSample: 16}
enc, err := flac.NewEncoder(w, info,
    flac.WithCompressionLevel(5),
    flac.WithTag("ARTIST", "…"),
)

// Encode interleaved int32 samples (length a multiple of Channels).
err = enc.Encode(samples)

// Close flushes the final frame, backfills STREAMINFO, and writes trailing
// metadata. The underlying writer is not closed.
err = enc.Close()
```

### Decoding

```go
dec, err := flac.NewDecoder(r)

// buf must hold at least one full block: MaxBlockSize × Channels() int32 values.
buf := make([]int32, flac.MaxBlockSize*2)
for {
    n, err := dec.Decode(buf) // n = samples-per-channel produced
    if err == io.EOF {
        break
    }
    // buf[:n*dec.Channels()] holds interleaved samples
}
```

### Streaming float64 (`codec/flac`)

```go
import codecflac "github.com/daniel-sullivan/go-mediatoolkit/codec/flac"

dec, err := codecflac.NewDecoder(r)                       // io.Reader → mutations.Audio
enc, err := codecflac.NewEncoder(w, 44100, 2,             // float64 in, FLAC out
    codecflac.WithBitsPerSample(16), codecflac.WithCompressionLevel(5))
```

### Metadata & tags (`containers/flac`)

```go
import ctrflac "github.com/daniel-sullivan/go-mediatoolkit/containers/flac"

rd, err := ctrflac.NewReader(r)   // parses the metadata chain
hdr := rd.Header()                // SampleRate/Channels/BitsPerSample + tags + seektable
dec, _ := flac.NewDecoder(rd.Data())  // rd.Data() replays magic+metadata+frames
```

## Implementation

When built with `CGO_ENABLED=1` (default on most platforms), `NewEncoder` and `NewDecoder` use the vendored C libFLAC. The pure Go port is used when cgo is disabled or when explicitly requested via `NewNativeEncoder`/`NewNativeDecoder`.

| Constructor | Cgo enabled | Cgo disabled |
|-------------|-------------|--------------|
| `NewEncoder` / `NewDecoder` | C libFLAC (via Cgo) | Native Go |
| `NewNativeEncoder` / `NewNativeDecoder` | Native Go | Native Go |

To force the pure Go path:

```sh
CGO_ENABLED=0 go build ./libraries/flac/
```

**Tags on decode.** Tag/vendor parsing (`Decoder.Vendor()` / `Decoder.Tags()`) is provided by the **cgo** backend. The native decode path intentionally length-skips VORBIS_COMMENT and returns `""`/`nil` (matching the [opus](../opus) native path); read tags via the cgo backend or via `containers/flac`, which parses the metadata chain on either path.

**Streaming-only encode.** Both backends are append-only (no `io.WriteSeeker`); STREAMINFO frame-size/MD5 fields are finalized at `Close`. (Same model as the opus encoder.)

### Build tags

The native Go implementation is **scalar** (there is no SIMD in the port). A single build tag controls floating-point fusion:

| Tag | Effect | When to use |
|-----|--------|-------------|
| *(none — default)* | Plain float operators; the Go backend is free to fuse `a*b+c` into a single-rounded FMA (`FMADD`/`FMADDS`). Lossless decode is unaffected; encoder analysis (window/autocorrelation/LPC) is within rounding noise of the reference but not bit-identical in every corner. | **Production / library consumers.** |
| `flac_strict` | Routes every float mul/add through `//go:noinline` helpers so the compiler cannot fuse `a*b+c`, and computes transcendentals via the double kernel narrowed to `float32` (`float32(math.Cos(float64(x)))`). Matches the reference C build compiled with `-ffp-contract=off` bit-for-bit. | Parity testing and toolchain-independent encoder output. Slower (no FMA). The `mise` tasks set this tag automatically. |

```sh
go build ./libraries/flac/                       # default: FMA-fused scalar
go build -tags=flac_strict ./libraries/flac/     # bit-exact parity mode
```

> Lossless **decode** output is bit-exact in both builds — the build tag only affects the floating-point *encoder analysis* (and the parity oracle it is compared against). FMA fusion can change which predictor/partition the encoder *chooses*, never the losslessness of the result.

### Performance (Apple M3 Pro, arm64)

All benchmarks use a 1-second 44.1 kHz stereo 16-bit signal (a mix of sine tones plus a low-amplitude deterministic noise floor — compressible but realistic). Source: `internal/parity_tests/benchcmp/bench_test.go` (run via `mise run //libraries/flac:bench` and `bench:strict`). The C column is the vendored libFLAC compiled as the **scalar parity oracle**: no x86/NEON intrinsics (`config.h` leaves them undefined), `-ffp-contract=off` (no FMA fusion), `-fno-vectorize -fno-slp-vectorize -fno-unroll-loops` (no auto-vectorization or unrolling). A production libFLAC build with intrinsics and default optimization would be substantially faster than the numbers below — this is an apples-to-apples scalar comparison, **not** native-vs-production-C.

The figures below are the **median of 6 runs at `-benchtime=2s`** (`-count=6`), default build for the Go and cgo columns and `-tags=flac_strict` for the Go-strict column. The Go-strict encode path is meaningfully slower than default because it forgoes FMA fusion and the NEON FP kernels (every multiply/add in the autocorrelation/LPC analysis is separately rounded through a `//go:noinline` boundary). Decode is integer-only, so it is effectively build-independent.

**Encode** — full 1s buffer, median ns/op (lower is better):

| Level | C libFLAC (cgo oracle) | Go default | Go strict | Go/C (default) |
|-------|------------------------|------------|-----------|----------------|
| 0 |  1.16 ms |  2.01 ms |  1.62 ms | ~1.7× |
| 5 |  3.55 ms |  2.83 ms |  8.35 ms | ~0.8× |
| 8 | 13.65 ms |  6.48 ms | 31.18 ms | ~0.47× |

**Decode** — full 1s level-5 stream, median ns/op (build-independent; integer-only):

| Operation | C libFLAC (cgo oracle) | Go default | Go strict | Go/C |
|-----------|------------------------|------------|-----------|------|
| Decode | 0.89 ms | 1.06 ms | 1.09 ms | ~1.2× |

Numbers above are post-optimization (see **Optimizations** below). For reference, the pre-optimization baseline was: encode default L0 1.76 / L5 6.02 / L8 24.69 ms; strict L8 36.70 ms; decode ~3.3 ms.

Notes:
- **The C-oracle encode columns are noisy.** The scalar libFLAC encode benchmarks swing ±15–40% run-to-run on this machine (the L0/L5 cases are the worst, ~30–40% spread); the medians above pool 12 cgo samples across both build runs, but the Go/C ratios are reported only to the precision the noise supports — don't read more than ~±0.2× into them. The Go (in-process) columns are tighter (≤25% spread).
- **The encode hierarchy is real even through the noise.** L0 (fixed predictors, almost no float analysis) stays slower than the scalar C oracle. At L5/L8 the default Go build — with the integer NEON kernels (fixed-predictor, partition-sum, LPC compute-residual MAC) plus the default-only FP NEON kernels (autocorrelation, window) and FMA fusion — runs **at or below** the scalar C oracle (L5 ≈ 0.8×, L8 ≈ 0.5×). `flac_strict` drops the FP NEON kernels and FMA, so its L5/L8 encode path is ~3–5× slower than default. (At L0, where there is no LPC/window/autocorrelation analysis, the no-FMA strict build is actually a touch *faster* than default — its only difference there is benchmark-run variance and the absence of the FP-helper indirection, which L0 barely exercises.)
- **Decode is integer-only** (Rice decoding + integer prediction), so the strict vs default tag does not change it; the word-accumulator bitreader and slice-by-8 CRC cut it to ~1.06 ms from the ~3.3 ms baseline (≈3×). The ~1.06 ms figure carries ~5–10% run-to-run noise; the cgo-oracle decode column ~15%.
- **Allocations**: native encode ~82–84 allocs/op (~0.68 MB); native decode ~47 allocs/op (~0.27 MB, per-block channel buffers). The cgo paths show 1 alloc/op (the Go-side output buffer); their C-side allocations are invisible to the Go allocator.

#### Optimizations

The following landed on top of the scalar baseline:

- **Word-accumulator bitreader** — reads decode bitstreams a machine word at a time instead of bit-by-bit, with CRC-16 accumulated over whole words rather than per-byte table lookups (integer; both builds).
- **Buffer sizing** — `ensureFrameBuffers` pre-sizes the per-frame working buffers so the decode hot path stops reallocating (both builds).
- **Unrolled-scalar LPC residual restore** — the *decode-side* `LPCRestoreSignal` is a serial recursive filter (each output sample feeds the next), which cannot be SIMD-vectorized; it ships as a pure-Go unrolled scalar kernel (orders 1..12 specialized, 13..32 generic). Both builds; integer-exact.
- **arm64 NEON kernels** — hand-written Plan9 asm with pure-Go scalar fallbacks on other architectures:
  - *Integer, exact in **both** builds (verified bit-identical by the strict parity gate):* fixed-predictor residual, partition absolute-sum, and the **encoder LPC compute-residual MAC** (`lpcResidualMACNEON`, `lpc_mac_arm64.s` — four output samples per iteration via int32 `MUL`/`MLA`/`SSHL`/`SUB`; int32 wrap is associative so it matches the scalar tap order exactly). The bulk is done in NEON; the `dataLen % 4` tail falls through to the unrolled scalar path.
  - *FP, **default build only** (`arm64 && !flac_strict`):* autocorrelation (float64x2) and window-coefficient multiply. The `flac_strict` build keeps the `//go:noinline` scalar FP path so it stays bit-exact against the `-ffp-contract=off` oracle.

The integer kernels (bitreader/CRC, fixed-predictor, partition-sum, LPC compute-residual MAC, LPC restore) are all exercised by the strict parity suite, which asserts byte-identical output against the C oracle; the FP (default-only) kernels are within rounding noise but not bit-identical, by design.

### Bit-exactness & parity

FLAC is lossless, so the parity bar is the strongest possible: **bit-exact**, not PSNR. The native Go port is validated against C libFLAC (called via Cgo) as the oracle, across 15 parity packages under `internal/parity_tests/`. The FP-parity convention is shared with the [opus](../opus) port:

- The cgo oracle is compiled with `-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops`, so every C multiply/add is separately rounded.
- The Go `flac_strict` build is FMA-free (`//go:noinline` `f32`/`f64` helper families).
- `window.c`'s single-precision transcendentals (`cosf`) are shimmed in the oracle to `(float)cos((double)x)` so both sides use the same correctly-rounded, portable math.

| Package | Checks |
|---|---|
| `foundation` | low-level primitives (CRC, bitmath, MD5, format validators) |
| `bitreader`, `bitwriter` | every reader/writer entry point + CRC |
| `subframe`, `predictors`, `frameheader` | subframe parse, fixed/LPC restore, frame headers |
| `channel`, `frame` | stereo decorrelation, full `read_frame_` (incl. out-of-bounds rejection) |
| `metadata` | STREAMINFO parse + metadata skip |
| `window`, `fixed_encode`, `lpc_encode`, `framing` | encoder analysis + frame framing |
| `decode_e2e` | full stream decode: native ≡ cgo libFLAC (identical samples + STREAMINFO + MD5) |
| `encode_e2e` | full stream encode: native output **byte-identical** to libFLAC |

Run the suite (requires a C toolchain — the oracle compiles vendored libFLAC):

```sh
# from libraries/flac/
mise run parity                       # the 15 parity packages, -tags=flac_strict
mise run test                         # all FLAC package tests, strict

# from the repo root (monorepo task form)
MISE_EXPERIMENTAL=1 mise run //libraries/flac:parity
```

The `mise` tasks set `CGO_CFLAGS=-O2 -ffp-contract=off …` and `CGO_CFLAGS_ALLOW=.*` (Go's cgo flag allowlist rejects `-ffp-contract=off` in-source) plus `-tags=flac_strict`. A bare `go test ./internal/parity_tests/...` without that env and tag may diverge on the FP-heavy packages depending on the toolchain/FMA behaviour — run it via the `mise` tasks for a deterministic bit-exact result.

### Internal structure

The native Go implementation lives under `internal/nativeflac/` as a 1:1 port of the vendored C libFLAC source; each function carries a `file:line` reference comment to its C counterpart. Key files:

- `nativeflac.go` — package doc: per-TU porting status + the FP-parity convention
- **Decode:** `bitreader.go`, `crc.go`, `md5.go`, `bitmath.go`, `format.go`; `frame.go`, `subframe.go`, `fixed.go`, `lpc.go` (restore predictors); `decode_frame.go` (`read_frame_`/`read_subframe_`), `channel.go` (decorrelation + footer CRC), `metadata_decode.go`, `decoder_state.go` + `decoder_stream.go` (state machine + driver loop)
- **Encode:** `bitwriter.go`; `window_fn.go` + `window_fp_strict.go`/`window_fp_default.go`; `fixed_encode.go`; `lpc_encode.go` + `lpc_fp_strict.go`/`lpc_fp_default.go` (autocorrelation/Levinson/quantize); `encode_framing.go`; `encoder_state.go`, `encoder_subframe.go`, `encoder_frame.go`, `encoder_stream.go`
- The `*_fp_strict.go` / `*_fp_default.go` pairs are the FMA-free vs FMA-fused float helpers selected by the `flac_strict` tag.

## API

### Types

- `StreamInfo` — `SampleRate`, `Channels`, `BitsPerSample`, `Min/MaxBlockSize`, `Min/MaxFrameSize`, `TotalSamples`, `MD5Signature [16]byte`

### Interfaces

**Decoder:**
- `Decode(buf []int32) (samplesPerChannel int, err error)` — fill `buf` (≥ `MaxBlockSize × Channels()`); loop until `io.EOF`
- `StreamInfo() StreamInfo`, `SampleRate() int`, `Channels() int`, `BitsPerSample() int`
- `Vendor() string`, `Tags() map[string][]string` — cgo backend only; native returns `""`/`nil`
- `Reset() error` — rewind (requires `io.Seeker`), `Close() error`

**Encoder:**
- `Encode(buf []int32) error` — interleaved samples, length a multiple of `Channels`
- `Close() error` — flush + finalize STREAMINFO

### Options

- Decoder: `WithMD5Check(bool)` — verify the STREAMINFO MD5 against decoded samples (reports `ErrMD5Mismatch`)
- Encoder: `WithCompressionLevel(int)` (0–8), `WithVerify(bool)`, `WithBlockSize(int)`, `WithTotalSamples(uint64)`, `WithVendor(string)`, `WithTag(key, value string)`, `WithTags(map[string][]string)`

### Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `MinBlockSize` / `MaxBlockSize` | 16 / 65535 | Block size bounds (samples/channel) |
| `MaxChannels` | 8 | Maximum channel count |
| `MinBitsPerSample` / `MaxBitsPerSample` | 4 / 32 | Sample resolution bounds |
| `MaxSampleRate` | 1048575 | Maximum sample rate (Hz) |

### Errors

- `ErrBadArg` — invalid argument (sample rate, channels, bit depth, nil reader/writer, or a `buf` length not a multiple of channels)
- `ErrBadSampleRate`, `ErrBadChannels`, `ErrBadBitDepth` — specific out-of-range parameters
- `ErrInvalidStream` — corrupted/malformed FLAC stream
- `ErrUnsupportedStream` — a stream feature this path does not handle (e.g. Ogg-FLAC passed to the native FLAC decoder)
- `ErrMD5Mismatch` — STREAMINFO MD5 ≠ decoded samples (only with `WithMD5Check`)
- `ErrEncoderVerify` — encoder self-verification disagreed with the input (only with `WithVerify`)
- `ErrAllocFail`, `ErrInternal`, `ErrClosed`

## License

The vendored C libFLAC source (`libflac/`) is copyright the Xiph.Org Foundation under the BSD 3-clause license; FLAC is royalty-free. See [`libflac/COPYING.Xiph`](libflac/COPYING.Xiph).
