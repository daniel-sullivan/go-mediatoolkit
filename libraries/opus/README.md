# opus

Pure Go implementation of the Opus audio codec ([RFC 6716](https://www.rfc-editor.org/rfc/rfc6716)), with an optional Cgo path wrapping C libopus.

Opus is a lossy audio codec supporting bitrates from 6 to 510 kbit/s, designed for both speech and music. It combines two codecs:

- **SILK** -- LPC-based, optimized for speech
- **CELT** -- MDCT-based, optimized for music and general audio

The codec automatically selects SILK-only, CELT-only, or Hybrid mode based on the content and configuration.

## Usage

```go
import "github.com/daniel-sullivan/go-mediatoolkit/libraries/opus"
```

All audio data uses interleaved `float64` samples (the go-mediatoolkit convention). For stereo: `[L0, R0, L1, R1, ...]`.

### Encoding

```go
enc, err := opus.NewEncoder(opus.Rate48000, 1, // 48 kHz, mono
    opus.WithBitrate(64000),
    opus.WithComplexity(10),
    opus.WithApplication(opus.AppAudio),
)

// Encode one 20ms frame (960 samples at 48 kHz).
pcm := make([]float64, 960)
// ... fill pcm with audio samples ...

packet, err := enc.Encode(pcm, opus.MaxFrameBytes)
// packet is a self-contained Opus packet
```

### Decoding

```go
dec, err := opus.NewDecoder(opus.Rate48000, 1) // 48 kHz, mono

pcm := make([]float64, opus.MaxFrameSize(opus.Rate48000))
samples, err := dec.Decode(packet, pcm)
// pcm[:samples] contains the decoded audio

// Packet loss concealment: pass nil to generate a fill-in frame.
samples, err = dec.Decode(nil, pcm)
```

### Packet inspection

```go
info, err := opus.ParsePacket(packet)
fmt.Printf("mode=%s bandwidth=%s duration=%.1fms frames=%d stereo=%v\n",
    info.Mode, info.Bandwidth, info.FrameDuration, info.FrameCount, info.Stereo)
```

## Implementation

When built with `CGO_ENABLED=1` (default on most platforms), `NewEncoder` and `NewDecoder` use the C libopus implementation with NEON/SSE intrinsics where available. The pure Go path is used when cgo is disabled or when explicitly requested via `NewNativeEncoder`/`NewNativeDecoder`.

| Constructor | Cgo enabled | Cgo disabled |
|-------------|-------------|--------------|
| `NewEncoder` / `NewDecoder` | C libopus (via Cgo) | Native Go |
| `NewNativeEncoder` / `NewNativeDecoder` | Native Go | Native Go |

To force the pure Go path:

```sh
CGO_ENABLED=0 go build ./libraries/opus/
```

### Build tags

The native Go implementation ships with FMA-fused floating-point math and arm64/amd64 SIMD enabled by default. Two build tags opt out of these optimizations when you need them:

| Tag | Effect | When to use |
|-----|--------|-------------|
| *(none — default)* | FMA helpers inlined, Go compiler free to emit `FMADDS`/`VFMADD`; NEON/SSE kernels enabled on arm64 and amd64 (inner_prod, xcorr, NSQ short_prediction+allpass, float2int16, limit2) | **Production / library consumers.** Fastest. Output is perceptually equivalent to upstream libopus (kernel-level SIMD-vs-scalar drift is <1e-5 relative, well below the 16-bit quantization floor). |
| `opus_strict` | Routes every float32 mul/add through `//go:noinline` wrappers so the compiler cannot fuse `a + b*c` into a single-rounded FMA, and disables all SIMD kernels. Matches the reference C build compiled with `-ffp-contract=off` bit-for-bit. | Parity testing, regression bisection, or strict toolchain-independent output. ~2–3× slower on encode paths. Run the `mise test` / `parity:*` tasks — they set this tag automatically. |
| `opus_nosimd` | Disables all NEON/SSE asm kernels; forces the pure-Go scalar implementations throughout. | Compatibility fallback (non-arm64/amd64 platforms get this automatically), perf isolation for benchmarking. Can be combined with `opus_strict`. |

Build examples:
```sh
go build ./libraries/opus/                         # default: FMA + SIMD
go build -tags=opus_strict ./libraries/opus/       # bit-exact parity mode
go build -tags=opus_nosimd ./libraries/opus/       # scalar fallback, still FMA
go build -tags=opus_strict,opus_nosimd ./libraries/opus/   # pure scalar + no FMA
```

### Benchmarks (Apple M3 Pro, arm64)

All benchmarks use a mono 20ms frame at 48 kHz. Source: `internal/parity_tests/benchcmp/bench_test.go`. The C column is the vendored libopus compiled as the bit-exact parity oracle: **scalar only** (no NEON/SSE intrinsics — see `libopus/config.h`), `-ffp-contract=off` (no FMA fusion), `-fno-vectorize -fno-slp-vectorize -fno-unroll-loops` (no auto-vectorization or unrolling). A production libopus build with intrinsics and default optimization would be substantially faster than the numbers below.

**Summary — Native Go wall time by tag combination (ns/op):**

| Operation | C strict | Default (FMA+SIMD) | `opus_strict` | `opus_nosimd` | `opus_strict,opus_nosimd` |
|-----------|----------|--------------------|---------------|---------------|---------------------------|
| CELT Encode |  75,800 |  **78,500** | 224,355 | 122,320 | 258,861 |
| CELT Decode |  15,200 |  **19,800** |  35,450 |  23,885 |  34,811 |
| SILK Encode | 176,500 | **185,900** | 494,734 | 250,923 | 499,832 |
| SILK Decode |   8,300 |  **14,100** |  13,931 |  14,323 |  13,594 |

(Bold = recommended default for that column. Strict combos include bit-exact parity; non-strict combos include ~1-2 ULP FMA drift and 4-lane reduction-order drift — inaudible. Default-build numbers include the arm64 NEON `xcorr_kernel`, `celt_inner_prod`, `dual_inner_prod`, NSQ `short_prediction`, and NSQ `noise_shape_allpass` kernels.)

**Default build** — FMA + NEON SIMD (what `go get` ships):

| Operation | C libopus (via Cgo) | Native Go | Go/C | Go allocs/op |
|-----------|-----------|-----------|------|--------------|
| CELT Encode |  75.8 μs |  78.5 μs | 1.04× | 46 / 26 KB |
| CELT Decode |  15.2 μs |  19.8 μs | 1.30× |  4 / 704 B |
| SILK Encode | 176.5 μs | 185.9 μs | 1.05× | 14 / 17 KB |
| SILK Decode |   8.3 μs |  14.1 μs | 1.70× |  1 / 2 B   |

**Strict build** — `-tags=opus_strict` (parity-validation mode; no SIMD, no FMA):

| Operation | C libopus (via Cgo) | Native Go | Go/C | Δ vs default |
|-----------|-----------|-----------|------|--------------|
| CELT Encode | 74.6 μs | 224.4 μs | 3.01× | +186% |
| CELT Decode | 14.9 μs |  35.5 μs | 2.37× |  +79% |
| SILK Encode | 173.3 μs | 494.7 μs | 2.86× | +166% |
| SILK Decode |  9.6 μs |  13.9 μs | 1.45× |   -1% |

All operations in both builds are well under the 20ms real-time budget (20ms @ 48 kHz = 960 samples).

Notes:
- **FMA vs strict** contributes a large portion of the delta on encode paths (~2× on CELT/SILK encode, ~1.5× on CELT decode). SILK decode time is dominated by the resampler and barely moves with FMA.
- **NEON SIMD on the pitch-analysis hot path** (`xcorr_kernel`, `celt_inner_prod`, `dual_inner_prod`) cuts CELT encode from 113.8 → 78.5 μs (-31%). `opus_nosimd` falls back to scalar Go.
- **NSQ SoA multi-kernel fusion** on arm64 (`short_prediction` + `noise_shape_allpass` in the delay-decision inner loop, at complexity 10 where nStatesDelayedDecision=4) cuts SILK encode from 264.3 → 185.9 μs (-30%). Only active when the encoder's delay-decision state count is 4; lower counts fall through to scalar NSQ unchanged.
- **Allocations** are identical across all 4 tag combinations: 46 / 26 KB for CELT encode, 4 / 704 B for CELT decode, 14 / 17 KB for SILK encode (down from 22/27 KB after the stack-allocated scratch buffers in the NSQ path), 1 / 2 B for SILK decode. The library pre-allocates per-frame scratch buffers on the encoder/decoder state at construction.

### Quality — 4-way PSNR matrix

Pairwise output comparison on a worst-case signal (`pink_stereo_128k_AUDIO`, 5 seconds of pink noise stereo encoded at 128 kbps). PSNR (dB) is reported on the final 16-bit audio-facing output (soft-clipped `opus_decode` path). Higher is better; 16-bit audio has a theoretical SNR ceiling of 96.33 dB, so any value above ~100 dB is below the quantization floor and inaudible.

> **Note:** the numbers below were measured with an earlier default build and have not been re-run since the arm64 NEON kernels (`celt_inner_prod`/`dual_inner_prod`, NSQ `short_prediction`/`noise_shape_allpass`, etc.) landed. The new SIMD kernels introduce 4-lane tree-reduction rounding-order drift on the float side and accept 1-ULP saturation-domain drift on fixed-point kernels — all inaudible. Individual SIMD-vs-scalar-Go drift is bounded at <1e-5 relative, well below the 16-bit quantization floor. A refreshed matrix would most likely widen the Go-default-vs-C-strict column by a handful of dB but still sit above 110 dB.

|             | C strict | C FMA     | Go strict | Go default |
|-------------|----------|-----------|-----------|------------|
| **C strict**   | —        | 116.74 dB | ∞ (bit-exact) | 117.01 dB |
| **C FMA**      | 116.74   | —         | 116.74    | 117.02     |
| **Go strict**  | ∞        | 116.74    | —         | 121.66     |
| **Go default** | 117.01   | 117.02    | 121.66    | —          |

Where:
- *C strict* = vendored libopus compiled with `-ffp-contract=off` (the parity reference).
- *C FMA* = upstream libopus built with default clang `-O2` (typical production C build).
- *Go strict* = this library with `-tags=opus_strict`.
- *Go default* = this library with no tags.

Takeaways:
- The default Go build was within 117 dB PSNR of both C reference builds in the measured snapshot — on par with the drift between different C toolchain builds of the same source. The newer SIMD kernels add a few more drift sources (detailed in the note above), but each kernel's SIMD-vs-scalar parity is bounded at <1e-5 relative, keeping the result well above the 16-bit quantization floor.
- The bit-exact `opus_strict` build matches C strict sample-for-sample.
- At 24-bit internal resolution (no soft-clip) the measured matrix recorded all pairwise PSNR at 151 dB or higher.
- All variants are perceptually equivalent; the build-tag choice is a performance-vs-binary-stability decision, not a quality decision.

### Native ↔ C parity

C libopus (called via Cgo) is the oracle for the native Go port. Tests live in `internal/parity_tests/`:

| Layer | Path | What it checks |
|---|---|---|
| Unit-level kernels | `internal/parity_tests/benchcmp/parity_*_test.go` | Each ported sub-routine (`celt_pitch_xcorr`, NSQ inner loops, MDCT, range coder, …) byte-exact against C |
| Whole-pipeline encode matrix | `internal/parity_tests/benchcmp/parity_opus_encode_matrix_test.go` | `opus_encode` × bandwidth × bitrate × complexity; bit-exact packets out |
| Three-way decode | `internal/parity_tests/benchcmp/parity_threeway_test.go` | Native decode ≡ C decode (via Cgo) ≡ upstream CLI on real bitstreams |
| Black-box | `internal/parity_tests/blackbox/` | Cold checkout: encode + decode vs the upstream `opusenc`/`opusdec` binaries |

> **Heads-up on `go test ./libraries/opus/...`:** a bare recursive `./...`
> descends into `internal/parity_tests/`, which builds the cgo oracle and (for
> blackbox) shells out to a cloned `opus_demo` — slow, network-dependent, and it
> diverges from the FP oracle without the `opus_strict` tag + `-ffp-contract=off`
> env. To test only the package, scope it: `go test ./libraries/opus/` (no
> `/...`). For the full strict suite use `mise run //libraries/opus:test`, which
> sets the tag and CGO flags for you.

Run via mise (first time only: `mise trust` to authorize this repo's task
config):

```sh
mise trust                                # one-time, authorizes the mise.toml tasks
mise run //libraries/opus:parity:benchcmp # ~8 s — unit-level parity vs cgo oracle
mise run //libraries/opus:parity:blackbox # several min first run — see below
mise run //libraries/opus:parity:threeway # ~2 s — 3-way decode diff (needs blackbox first)
```

`parity:benchcmp` is the fast gate (cgo amalgamation, no network).

`parity:blackbox` is **not** a ~30 s task on a cold machine: its `run.sh`
git-clones `xiph/opus` (from `gitlab.xiph.org`, pinned ref) into
`/tmp/opus-upstream/`, then runs `autogen.sh` → `configure` → `make` to build
`opus_demo` from source. `autogen.sh` also pulls upstream's neural model assets
from `media.xiph.org` into `dnn/models/` (**~200 MB** — measured 203 MB; this
build disables DRED/OSCE/deep-PLC at `configure` time, but the fetch happens in
`autogen.sh` first regardless). So it needs **network access plus a C toolchain
(autotools + clang/make)**, and the first run takes **several minutes**.
Subsequent runs reuse the cached `/tmp` checkout and built binary; the test
itself then runs in ~3 s.

`parity:threeway` runs `TestParity_ThreeWayDecode` and depends on
`OPUS_DEMO_BIN` pointing at a built `opus_demo` — run `parity:blackbox` first
(it produces and exports that binary) or set `OPUS_DEMO_BIN` yourself. The task
fails fast with a message if it's unset.

`parity:all` chains all three (`benchcmp` → `blackbox` → `threeway`). All pass
with 0 diffs in `opus_strict` builds (what `mise run` uses).

### Internal structure

The native Go implementation lives under `internal/nativeopus/` as a 1:1 port of the vendored C libopus source. Key files:

- `fma_default.go` / `fma_strict.go` — FMA helpers (tag-selected variants above)
- `fma_batch_default.go` / `fma_batch_strict.go` — batched inner-product helpers
- `xcorr_kernel_arm64.s` / `xcorr_kernel_amd64.s` — SIMD cross-correlation kernels
- `inner_prod_arm64.s` / `inner_prod_amd64.s` — NEON/SSE `celt_inner_prod` + `dual_inner_prod`
- `float2int16_arm64.s` — NEON `celt_float2int16` (FMUL .4S, FCVTAS, SQXTN) for int16 output path
- `limit2_arm64.s` — NEON `opus_limit2_checkwithin1` for soft-clip path
- `silk_NSQ_soa.go` — struct-of-arrays layout + AoS↔SoA helpers for delay-decision NSQ
- `silk_NSQ_simd_arm64.s` — NEON 4-lane `short_prediction` kernel
- `silk_NSQ_allpass_simd_arm64.s` — NEON 4-lane `noise_shape_allpass` kernel
- `silk_NSQ_del_dec_soa.go` — SoA-fused NSQ inner loop consuming both NSQ SIMD kernels; dispatched when `nStatesDelayedDecision == 4`
- `silk_LPC_inv_pred_gain_simd.go` — pure-Go 4-lane reference for `silk_LPC_inverse_pred_gain` (Phase A; asm port is future work)
- `silk_biquad_alt_simd_ref.go` — pure-Go 4-lane reference for `silk_biquad_alt_stride2` (Phase A; test-only consumer today, production uses float-path `silk_biquad_res`)
- `celt_encoder.go` / `celt_decoder.go` — CELT (MDCT, bands, PVQ)
- `silk_*.go` — SILK (LPC, LSF, resampler, NSQ)
- `entcode.go`, `entenc.go`, `entdec.go` — range coder

## API

### Types

- `Mode` -- `ModeSILKOnly`, `ModeHybrid`, `ModeCELTOnly`
- `Bandwidth` -- `BandwidthNarrowband` (4 kHz) through `BandwidthFullband` (20 kHz)
- `Application` -- `AppVoIP`, `AppAudio`, `AppLowDelay`
- `PacketInfo` -- mode, bandwidth, frame duration, frame count, stereo flag
- `Frame` -- raw frame payload sub-sliced from a packet

### Interfaces

**Decoder:**
- `Decode(data []byte, pcm []float64) (samplesPerChannel int, err error)` -- pass `nil` data for PLC
- `SampleRate() int`, `Channels() int`
- `LastPacketDuration() int`
- `Reset()`

**Encoder:**
- `Encode(pcm []float64, maxPacketSize int) ([]byte, error)`
- `SampleRate() int`, `Channels() int`
- `SetBitrate(bps int) error`
- `Reset()`

### Options

- `WithBitrate(bps int)` -- target bitrate (default: 64000)
- `WithComplexity(n int)` -- encoder complexity 0-10 (default: 10)
- `WithApplication(app Application)` -- application hint (default: `AppAudio`)
- `WithGain(dB float64)` -- decoder output gain

### Constants

| Constant | Value | Description |
|----------|-------|-------------|
| `Rate8000`..`Rate48000` | 8000..48000 | Supported sample rates |
| `FrameSamples20ms` | 960 | Samples per channel for 20ms at 48 kHz |
| `MaxPacketDuration` | 5760 | Max samples per channel per packet (120ms at 48 kHz) |
| `MaxFrameBytes` | 1275 | Max bytes per Opus frame |

### Functions

- `MaxFrameSize(sampleRate int) int` -- max samples per channel for any valid packet at the given rate
- `SamplesPerFrame(durationMs float64, sampleRate int) int` -- samples per channel for a given frame duration
- `ParsePacket(data []byte) (PacketInfo, error)` -- parse an Opus packet header without decoding

### Errors

- `ErrInvalidPacket` -- corrupted or malformed packet
- `ErrBadArg` -- unsupported sample rate, channel count, or parameter
- `ErrBufferTooSmall` -- output buffer too small for decoded frame
- `ErrInternalError` -- internal codec error
- `ErrUnimplemented` -- codec mode or feature not yet implemented

## License

The vendored C libopus source (`libopus/`) is copyright Xiph.Org, Skype Limited, and others under the BSD 3-clause license. Opus is royalty-free. See [`libopus/COPYING`](libopus/COPYING).
