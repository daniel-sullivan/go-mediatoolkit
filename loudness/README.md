# loudness

Pure Go implementation of EBU R128 / ITU-R BS.1770-4 loudness metering and normalisation, with an optional Cgo parity oracle (the vendored reference C [libebur128](https://github.com/jiixyj/libebur128)) used only to verify the port — there is no Cgo runtime backend to opt into.

BS.1770-4 defines how to measure perceived loudness the way broadcasters and streaming platforms regulate it: K-weighted, gated, integrated across a whole programme, with a companion oversampled "true peak" meter that catches inter-sample overshoots a plain sample-peak meter misses. EBU R128 (Tech 3341/3342/3343) builds the broadcast recommendation on top of that measurement. This package provides:

- **Metering** — momentary/short-term/integrated LUFS, loudness range (LRA), sample peak, true peak, and plain RMS, both streaming (`Meter`) and one-shot (`Measure`).
- **Normalisation** — offline gain-to-target with a peak ceiling (`Normalize`), plus three streaming `mutations.Processor`s: a fixed-gain `Normalizer`, a lookahead true-peak `Limiter`, and an adaptive AGC `Leveller`.
- **Live monitoring** — a mutex-guarded `Monitor` for reading loudness off a bus (e.g. a `mixer.Config.Processors` master chain) while it runs.

## Usage

```go
import "github.com/daniel-sullivan/go-mediatoolkit/loudness"
```

`loudness` operates on interleaved **`float64`** samples in `[-1, 1]` — the same convention as [`mutations`](../mutations), which it builds on (`mutations.Audio`, `mutations.Decibels`/`AmplitudeToDecibels`, and the `mutations.Processor` interface for its streaming effects).

### Streaming metering (`Meter`)

```go
m, err := loudness.NewMeter(48000, 2, loudness.ModeAll) // integrated + LRA + true peak
// ...
err = m.AddFrames(chunk) // interleaved stereo, any chunking

momentary, err := m.Momentary()  // LUFS, trailing 400ms
integrated, err := m.Integrated() // LUFS, gated, whole stream so far
peak, err := m.TruePeak(0)       // linear amplitude, channel 0; convert with mutations.AmplitudeToDecibels
```

A reader whose mode bit wasn't requested returns `ErrInvalidMode`; before enough audio has been seen, loudness readers return `(-Inf, nil)` — a valid "nothing measured yet" state, not an error.

### One-shot measurement (`Measure`)

```go
meas, err := loudness.Measure(clip) // clip is a mutations.Audio
fmt.Printf("I=%.1f LUFS, LRA=%.1f LU, TP=%.1f dBTP\n",
    meas.Integrated, meas.Range, meas.TruePeak)
```

`Measure` allocates a fresh `Meter`, feeds the whole buffer in 100ms blocks (the same cadence the momentary/short-term maxima are tracked at), and returns a full `Measurement` report. See [`examples/measure`](examples/measure/main.go) for a runnable scanner over a synthesised multi-level programme.

### Offline normalisation (`Normalize`)

```go
res, err := loudness.Normalize(clip, loudness.NormalizeOptions{
    Target: loudness.TargetPodcast, // -16 LUFS
    Mode:   loudness.NormalizeLimit,
})
fmt.Printf("applied %.1f dB, delivered %.1f LUFS\n", res.GainDB, res.Output.Integrated)
```

`Normalize` mutates `clip.Data` in place, length-preserving, and reconciles the loudness target against the true-peak `Ceiling` per `Mode`:

- **`NormalizeClamp`** (default) — one constant gain, reduced if needed so the ceiling is never exceeded. Transparent (nothing but level changes), but a peaky programme may land under `Target`.
- **`NormalizeLimit`** — the full gain to reach `Target`, then a `Limiter` tames whatever peaks that gain raised above the ceiling. Hits the number; trades a little peak dynamics to get there.

Silent/unmeasurable input returns `ErrSilentInput`. See [`examples/normalize`](examples/normalize/main.go), which runs both modes over the same clip and prints gain + before/after peaks.

### Streaming processors

All three are `mutations.Processor`s — drop them into a `timeline.EffectSource` chain, a `mixer.Config.Processors` master bus, or drive `Process` directly.

**`Normalizer`** — fixed gain from a prior measurement:

```go
norm, err := loudness.NewNormalizer(measuredLUFS, loudness.TargetStreaming)
norm.Process(samples) // constant gain, no state beyond that
```

**`Limiter`** — lookahead true-peak limiter, provably non-overshooting in the sample domain (see the type doc for the sliding-min/boxcar cascade):

```go
lim, err := loudness.NewLimiter(loudness.LimiterConfig{SampleRate: 48000, Channels: 2})
lim.Process(samples) // length-preserving; output delayed by lim.LatencyFrames()
```

Because the output is delayed by `LatencyFrames()`, a finite render needs its tail flushed. Offline, use `mutations.Audio.RenderWithEffects(chain, lim.Latency())`; for a streaming `timeline.Source`, wrap it with `timeline.NewEffectSource(src, lim).WithTail(lim.Latency())` so the tail of zero-input frames drains through the delay line before the source reports `io.EOF`.

**`Leveller`** — adaptive AGC that steers a stream toward a loudness `Target` over time:

```go
lev, err := loudness.NewLeveller(loudness.LevellerConfig{SampleRate: 48000, Channels: 2})
lev.Process(samples)
currentGainDB := lev.GainDB()
```

Unlike `Meter`/`Limiter` (bit-exact ports of, or built on, libebur128), **the `Leveller` is an original design with no reference implementation and no parity oracle** — its behaviour is defined by its doc comment and unit tests (convergence, gate-freeze, bounds, emergency response, zipper-free ramping), not by matching an external tool. It embeds a `Limiter` at its output (disable with `LevellerConfig.DisableLimiter`), so its `Latency()` is exactly the embedded limiter's — flush the tail the same way.

**`Monitor`** — a mutex-wrapped `Meter` for concurrent readout, built for live buses:

```go
mon, err := loudness.NewMonitor(48000, 2, loudness.ModeShortTerm)
mx, err := mixer.New(mixer.Config{
    SampleRate: 48000, Channels: 2,
    Processors: []mutations.Processor{mon}, // metering-only: never alters the audio
})
// from another goroutine, safely, while the mixer runs:
st, err := mon.ShortTerm()
```

`Monitor.Process` only feeds the wrapped meter — no gain change, no added latency — so it's the one processor in this package safe to read from outside the mix goroutine while a `mixer.Mixer` (or any other live producer) keeps calling `Process`. See [`examples/live_leveller`](examples/live_leveller/main.go), which drives a mixer with a `Leveller` + `Monitor` on the master bus and prints the AGC converging.

## Units

| Convention | Meaning | Used by |
|---|---|---|
| **LUFS** ("Loudness Units, relative to Full Scale") | Absolute integrated/momentary/short-term loudness. | `Integrated`, `Momentary`, `ShortTerm`, `Target*` presets |
| **LU** ("Loudness Units") | A *relative* loudness difference — 1 LU == 1 dB in magnitude, but names a delta, not an absolute level. | `Range` (LRA), `RelativeThreshold` gap |
| **dBTP** ("dB True Peak") | Inter-sample reconstructed peak level. Always ≤ 0 for a signal that never clips. | `Measurement.TruePeak`, `Ceiling*` presets |
| **dBFS** ("dB Full Scale") | Plain sample-domain peak or RMS level; 0 dBFS == full-scale amplitude 1.0. | `Measurement.SamplePeak`/`RMS` |
| **linear amplitude** | 1.0 == full scale, no dB conversion applied. | `Meter.SamplePeak`/`TruePeak`, `Measurement.Channel*Peaks`, `loudness.RMS` |

Convert between linear amplitude and dB with `mutations.Decibels` (dB → linear) and `mutations.AmplitudeToDecibels` (linear → dB) — the same `20·log10` convention used everywhere else in the toolkit.

## Verification

Three independent tiers back this package, from strongest to weakest:

### 1. Cgo parity oracle (bit-exact)

The Go port in `loudness/internal/r128` is checked against the vendored **libebur128 v1.2.6** C reference (`loudness/libebur128/`, compiled inline via Cgo) across 8 parity packages under `internal/parity_tests/`:

| Package | Checks |
|---|---|
| `smoke` | End-to-end sanity: a plausible LUFS reading from the oracle build, no bit-exact assertions. |
| `filter` | K-weighting filter coefficients (all rate classes 8000–192000 Hz, 1/2/5/6 channels) + filtered output; includes the 48 kHz BS.1770 Table 1 self-check. |
| `gatingblock` | Per-block channel weighting (L/R/C 1.0, surround 1.41, dual-mono 2.0, UNUSED skip). |
| `integrated` | Gating (absolute/relative thresholds), trailing partial-block discard, edge-case streams. |
| `windows` | Momentary/short-term/`LoudnessWindow` readings and prime-sized chunking. |
| `lra` | Percentile edge cases (n=1,2,3,31), history trimming, histogram-vs-list agreement. |
| `truepeak` | All oversampling-rate classes, tap-drop, sample-peak fold-in. |
| `e2e` | ~60 sampled cases across rate × channel (incl. channel-map overrides) × mode × chunking × window/history settings. |
| `benchcmp` | Not a correctness gate — the Go-vs-C benchmark harness (see Benchmarks below). |

**`loudness_strict` build tag semantics** — unlike `libraries/flac`'s `flac_strict`, this tag does **not** change the Go port's own arithmetic: `internal/r128` is **unconditionally FMA-free** (explicit `float64()` conversion barriers at every multiply-accumulate site), in both the default and `loudness_strict` builds. The tag instead selects which bound each parity assertion is checked against:

- **With `-tags=loudness_strict`** (set automatically by the `mise` tasks below, paired with `CGO_CFLAGS=-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops`) — the C oracle is forced to scalar, unfused arithmetic matching the Go port exactly, so assertions run **bit-exact** (or bounded to a handful of documented libm-only ULP exceptions — e.g. a 1-ULP `tan`/`pow` transcendental gap at specific sample rates, or a ~18-ULP `math.Pow`-vs-libm gap at a gating boundary — each with an in-test rationale, never a blanket epsilon).
- **Without the tag** (a bare `go test ./...`, e.g. plain CI per `.github/workflows/tests.yml`) — the C oracle may be built with whatever FMA/vectorization the default toolchain chooses, so the same assertions widen to a relative+absolute tolerance loose enough to absorb that drift.

Run the bit-exact gate (requires a C toolchain — the oracle compiles vendored libebur128, no system install needed):

```sh
# from loudness/
mise run parity   # the 8 parity packages, -tags=loudness_strict
mise run test     # full loudness package suite, strict

# from the repo root (monorepo task form)
MISE_EXPERIMENTAL=1 mise run //loudness:parity
MISE_EXPERIMENTAL=1 mise run //loudness:test
```

### 2. EBU compliance vectors (pure Go, default CI)

`tech3341_test.go` / `tech3342_test.go` synthesize the EBU Tech 3341 v4 and Tech 3342 v4 (Geneva, Nov 2023) reference waveforms in-code and check the meter's readings against the documents' published tolerances (±0.1 LU for M/S/I, +0.2/−0.4 dB for true peak, ±1 LU for LRA):

- **Tech 3341**: cases 1–6 and 9–19 implemented (steady/varying tones, 5.0 multichannel, momentary/short-term settling, true-peak at all 5 rate classes plus phase/level variants, the −18 LUFS calibration check).
- **Tech 3341 cases 7/8** (authentic NLR/WLR programme audio) — `t.Skip`ped: the document gives no synthesizable waveform parameters, only a qualitative genre description; would need the official EBU reference WAVs.
- **Tech 3341 cases 20–23** (4×-oversampled, downsample-phase-offset true-peak splices) — **deferred**, not implemented: the document underspecifies the splice construction (phase-only continuity between an fs/4 and an fs/6 tone, unstated anti-alias filter and fade duration) closely enough that reproducing it exactly needs the official reference WAVs. The true-peak interpolator itself is already parity-pinned bit-exact against libebur128 (the `truepeak` slice above) and exercised by cases 15–19, so 20–23 would add interpolator coverage this suite already has, not new metering behaviour.
- **Tech 3342**: LRA cases 1–4 implemented; cases 5/6 (authentic programme) skipped for the same reason as 3341 cases 7/8.

These run in plain CI with no build tag or C toolchain — `go test ./loudness/...`.

### 3. Unit tests

Per-concern tests cover what the compliance vectors don't: `Meter` construction/mode-gating/reset/`-Inf` semantics, K-weighting against BS.1770 Table 1 (`internal/r128/kweight_test.go`), the true-peak interpolator in isolation (`internal/r128/truepeak_test.go`), `Normalize` clamp-vs-limit math and `ErrSilentInput`, `Limiter` ceiling/latency/attack/release/chunking-invariance, `Leveller` convergence/gate-freeze/bounds/emergency-path/zipper-free-ramping/chunking-invariance, `Monitor` concurrent-reader race safety, and the mixer's `Processors` chain (ordering vs saturator, chunk continuity, nil-safe, race).

```sh
go test ./loudness/...                                    # tiers 2+3, no cgo needed
go test -race -count=1 ./loudness/... ./mixer/...          # + concurrency
CGO_ENABLED=0 go build ./loudness/...                      # confirms no cgo leaks into the default build
```

## Benchmarks

Go-vs-C comparison from `internal/parity_tests/benchcmp/bench_test.go`, run via `mise run //loudness:bench`: 60 s synthetic 48 kHz signals (sines plus low-level noise; stereo unless marked mono), a single `-benchtime=2s` run on Apple M3 Pro (arm64). Both columns build the C oracle scalar and unfused (`-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops`, no intrinsics) — this is an apples-to-apples scalar comparison, **not** native-vs-production-libebur128 (a production build with SIMD would be faster than the C column here).

| Benchmark | Go | C (scalar oracle) | Go/C | Go allocs/op |
|---|---|---|---|---|
| Integrated loudness, 60 s stereo | 49.8 ms | 52.9 ms | **0.94×** (Go faster) | 32 (344 KB) |
| True peak, 60 s stereo | 156.7 ms | 195.6 ms | **0.80×** (Go faster) | 34 (1.1 MB) |
| True peak, 60 s mono | 94.6 ms | 90.3 ms | **1.05×** (Go slower) | 34 (582 KB) |

Notes:
- **Integrated loudness** is dominated by the K-weighting filter and 100ms gating-block bookkeeping — both simple scalar loops — so the FMA-free Go port keeps pace with the scalar C oracle.
- **True peak** is dominated by the 49-tap polyphase interpolator, run at up to 4× oversampling per channel. The Go port restructures the interpolator's delay line (a mirrored, channel-interleaved ring that makes every polyphase read window contiguous and wrap-free) and processes channel pairs as two SIMD lanes — NEON on arm64, SSE2 on amd64, with an always-compiled pure-Go kernel elsewhere and as the testable reference. The kernels use separate multiply and add instructions (never FMA), so every output remains **bit-identical** to scalar C libebur128 — the same `truepeak`/`e2e` parity slices assert bit-exactness over the optimised paths.
- **Scope of the SIMD claim**: the pair kernel covers channels two at a time, so stereo and even channel counts run entirely on it; the stereo row above is what the SIMD kernels deliver (below the scalar C oracle). Mono — and the odd trailing channel of odd channel counts — runs the pure-Go scalar column kernel instead, so the mono row measures the unaccelerated path: roughly on par with the scalar C, as the table shows.

Reproduce:

```sh
MISE_EXPERIMENTAL=1 mise run //loudness:bench
```

## License

This package's own code is MIT, like the rest of go-mediatoolkit. The vendored C libebur128 source (`libebur128/`) is copyright Jan Kokemüller under the MIT license — see [`libebur128/COPYING`](libebur128/COPYING); the exact vendored revision is recorded in [`libebur128/VERSION`](libebur128/VERSION). It is compiled **only** by the opt-in parity-test packages under `internal/parity_tests/` (each of which compiles its own self-contained copy as its cgo oracle). Importing the `loudness` package itself **never** builds any C, cgo-enabled or not: the package delegates entirely to the pure-Go `internal/r128` port and carries no cgo of its own, so there is no public API that requires the C at runtime and nothing to strip from a `CGO_ENABLED=0` build. See the root [`LICENSING.md`](../LICENSING.md) for the project-wide map.

## Known limitations

- **Unbounded history by default.** `Meter`/`Monitor`'s integrated-loudness and LRA history is unbounded by default (mirrors libebur128), which grows without limit on a 24/7 stream. For continuous metering, call `SetMaxHistory` or construct with `ModeHistogram` (bounded, fixed-size histogram accounting).
- **Channel model.** The `Channel` enum implements BS.1770-4's channel weighting (mono/stereo, 5.1, plus every ITU speaker position up to 64 channels); it does not implement the immersive-audio channel model added in BS.1770-5 Annex 3/4 (e.g. height/object-based layouts beyond the plain ITU speaker positions above).
- **Tech 3341 cases 20–23** are deferred (see Verification, tier 2) — they add true-peak interpolator coverage the parity suite and cases 15–19 already provide, not new metering behaviour.
- **Limiter inter-sample regrowth.** A limited waveform can, after gain reduction, reconstruct an inter-sample peak fractionally (typically 0.1–0.3 dB) above the configured ceiling — inherent to any low-distortion true-peak limiter, and why the default −1 dBTP ceiling leaves margin below 0 dBFS. Treat the ceiling as "within a few tenths of a dB", not exact.
- **Leveller has no oracle.** It's a spec-driven original design (see its type doc), verified by its own unit tests only — there is no external "adaptive leveller" reference implementation to hold it against, unlike `Meter`/`Limiter`.
