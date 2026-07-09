# aec

Pure Go implementation of acoustic echo cancellation (AEC): a 1:1 port of WebRTC's **AEC3** (freedesktop's `webrtc-audio-processing` v2.1, tracking WebRTC M131), with an optional Cgo parity oracle (the fetched, not vendored, C++ reference) used only to verify the port — there is no Cgo runtime backend to opt into.

An echo canceller removes a known far-end (loudspeaker) signal from a near-end (microphone) capture: it reflects back acoustically, delayed and distorted by the room and the device's audio path, mixed in with whatever the near end actually wants to say. AEC3 was chosen over simpler alternatives (e.g. Speex MDF) specifically for its built-in continuously-adapting delay estimation and nonlinear residual-echo suppression, which together tolerate the wide and drifting far-end↔mic latency real-world audio rigs exhibit — anywhere from ~20ms to over a second, across device buffers, Bluetooth, and OS audio stacks. It's a natural complement to this toolkit's VAD-based ducking.

## Usage

```go
import "github.com/daniel-sullivan/go-mediatoolkit/aec"
```

`aec` operates on interleaved **`float64`** samples in `[-1, 1]` — the same convention as [`mutations`](../mutations), which it builds on (`mutations.Processor`, `mutations.FramesToDuration`).

### A two-stream processor

Unlike every other `Processor` in this toolkit (which transforms one signal in place), an echo canceller fundamentally needs *two* signals, so the API has two entry points instead of one:

- **`FeedFarEnd(samples)`** — the render path: whatever is being (or about to be) played out of the loudspeaker, e.g. tapped from a `mixer`'s master output or a `devices` render buffer.
- **`Process(samples)`** — the capture path: removes the estimated echo from `samples` in place, implementing `mutations.Processor` so it composes with the rest of the toolkit's streaming effect chain (`timeline.EffectSource`, `mixer.Config.Processors`) exactly like any other `Processor` once wired up.

```go
c, err := aec.NewCanceller(aec.CancellerConfig{
    SampleRate:      48000,
    CaptureChannels: 1,
    RenderChannels:  1,
})
// ...
err = c.FeedFarEnd(renderChunk)  // whatever went to the speaker
c.Process(captureChunk)         // mic input, echo removed in place
```

**Single-goroutine contract.** `FeedFarEnd` and `Process` are NOT independent: the underlying AEC3 port requires them to be externally serialized onto a single goroutine, never called concurrently with each other or with themselves. This is deliberately stricter than upstream's own `EchoCanceller3` (whose class comment permits `AnalyzeRender` to run concurrently via a lock-free queue) — this port replaces that queue with a plain unsynchronized one, so both calls must come from the same one goroutine, one at a time. The one exception is `SetAudioBufferDelay` (see below) and `Metrics()`, both safe to call from any goroutine at any time via `atomic.Uint64`/`atomic.Int64`/`atomic.Pointer[Metrics]` hand-offs — see the `Canceller` type doc comment for the full contract.

### Rates, channels, latency

`CancellerConfig.SampleRate` must be 16000, 32000, or 48000 Hz — the three rates AEC3's frequency-band splitting supports (16kHz is a single-band passthrough; 32/48kHz split into multiple bands). `CaptureChannels`/`RenderChannels` are independent and each must be in 1..64; they need not be equal (e.g. mono render, stereo capture). Per upstream, a `Canceller`'s configuration is fixed at construction time — there is no live rate/channel change, only `Reset()` to clear state and start over at the same config.

`Process`'s round-trip latency is **fixed at exactly one 10ms AEC3 frame**, regardless of sample rate — not the smaller 4ms internal block-processing granularity, because splitting a frame into frequency bands isn't causal at sub-frame granularity (the band-splitting filters need a whole 10ms frame before producing output for any of it). This holds for any call chunking pattern, including calls smaller than one frame; see `Latency`'s doc comment in `canceller.go` for the full derivation and proof that the internal output delay line never underruns.

### Delay handling

Real-world render→capture delay is rarely fixed and rarely known exactly, so AEC3 attacks it from three angles simultaneously:

- **Built-in delay estimation.** The engine continuously estimates the render-to-capture delay from the signals themselves — no external hint is required for basic operation. `Metrics().DelayMs` reports the current estimate.
- **Clockdrift detection.** When render and capture are sourced from independent audio devices with slightly different crystal oscillators, the delay slowly drifts rather than staying fixed. `Metrics().Clockdrift` reports `ClockdriftLevelNone`/`Probable`/`Verified` as the detector's confidence grows.
- **`SetAudioBufferDelay` hint.** If the caller has an external estimate of the audio buffer delay (e.g. a round-trip latency figure reported by the audio backend), passing it via `SetAudioBufferDelay(delay)` helps the estimator converge faster and more robustly. It's the one setter safe to call from any goroutine at any time — the value is only actually applied to the engine from inside the next `Process` call, via a lock-free generation-counter hand-off.

### Metrics

`Metrics()` returns a snapshot updated once per processed capture frame, safe to read from any goroutine concurrently with `FeedFarEnd`/`Process`:

```go
m := c.Metrics()
// m.EchoReturnLoss             — dB, linear-stage echo reduction
// m.EchoReturnLossEnhancement  — dB, suppressor-stage echo reduction on top of that
// m.DelayMs                    — current estimated render→capture delay
// m.Clockdrift                 — ClockdriftLevelNone/Probable/Verified
```

## Tuning

`CancellerConfig.Tuning *config.Config` (package [`aec/config`](config)) exposes the *full* AEC3 tuning surface — every field of upstream's `EchoCanceller3Config`, tracking WebRTC M131 field-for-field — rather than a curated subset. `nil` (the zero value, so every existing caller is unaffected) selects `config.DefaultConfig()`, exactly upstream's own defaults and this package's only behaviour before `Tuning` existed. A non-nil value is validated against `config.Validate`'s range rules at construction time: any field `Validate` would have had to clamp is rejected with an error wrapping `aec.ErrBadArg` naming the offending field, rather than silently substituting the clamped value.

```go
tuning := config.DefaultConfig()
// Loosen the suppressor's normal-tuning masking thresholds, trading
// echo suppression for more transparency (less near-end distortion).
tuning.Suppressor.NormalTuning.MaskLF.EnrTransparent = 8
tuning.Suppressor.NormalTuning.MaskLF.EnrSuppress = 12

c, err := aec.NewCanceller(aec.CancellerConfig{
    SampleRate:      48000,
    CaptureChannels: 1,
    RenderChannels:  1,
    Tuning:          &tuning,
})
```

`SampleRate`/`CaptureChannels`/`RenderChannels` stay separate `CancellerConfig` fields, not part of `Tuning`: upstream's own `Config` has no rate or channel-count fields (its `MultiChannel` sub-config is a stereo-detection knob this port doesn't expose — see `aec/config`'s package doc), so there is no overlap or precedence to reason about between the two.

Tuning AEC3 well requires understanding its internals — delay estimation, adaptive filtering, nonlinear suppression — well enough to reason about the upstream field documentation these fields are lifted from; see the [`aec/config`](config) package doc for field-by-field detail and the stability caveat (the field set tracks upstream, and isn't expected to change independently of a WebRTC revision bump).

## Examples

- **[`examples/cancel`](examples/cancel/main.go)** — a minimal, fully offline demonstration: synthesize a broadband far-end signal and a near-end capture containing a delayed, attenuated echo of it, run the pair through a `Canceller`, and print the before/after residual-echo level plus converged metrics. No mixer or device involved.
- **[`examples/duplex`](examples/duplex/main.go)** — a `Canceller` wired against a live `mixer.Mixer`'s output as the far-end reference, with a synthetic "microphone" built by re-injecting that same output (delayed, attenuated) plus added near-end speech starting partway through. Prints two separate windows — echo-only (before any near-end speech) and double-talk (near-end speech overlapping the echo) — to show both "echo removed" and "near-end preserved" as distinct, non-conflated measurements.

Both are headless (no audio device) and safe to run as-is:

```sh
go run ./aec/examples/cancel
go run ./aec/examples/duplex
```

`cancel` prints:

```
residual echo: before=-21.2 dBFS after=-47.3 dBFS (reduction=26.1 dB)
metrics: delay=16ms ERL=9.6dB ERLE=42.8dB clockdrift=0
```

`duplex` prints:

```
echo-only window:  before=-18.3 dBFS after=-50.4 dBFS (echo removed: 32.1 dB)
double-talk window: before=-10.1 dBFS after=-11.1 dBFS near-end-alone=-10.8 dBFS (near-end preserved)
metrics: delay=12ms ERLE=124.8dB clockdrift=0
```

Both scenarios use broadband (white/pink noise) stimulus rather than a pure tone — AEC3's delay estimator, like most cross-correlation-based estimators, needs spectral content to converge; see `canceller_test.go`'s `TestCanceller_EchoReduction` doc comment for the same rationale applied to the package's own unit test.

## Verification

The Go port in `internal/aec3` is checked bit-exact against the fetched (not vendored) **webrtc-audio-processing v2.1** C++ oracle across **15 parity slices** under `internal/parity_tests/`:

| Package | Checks |
|---|---|
| `smoke` | Coarse end-to-end sanity: the oracle links, runs, and substantially reduces echo — no bit-exact assertions. |
| `fft` | Ooura 128-point real FFT + the `Aec3Fft` windowing/zero-padding wrapper. |
| `blocking` | Frame↔block reframing (`BlockFramer`/`FrameBlocker`). |
| `bandsplit` | Frequency-band splitting/merging (`SplitIntoFrequencyBands`, `MergeFrequencyBands`) at every supported rate. |
| `adaptivefir` | The core NLMS-family adaptive FIR filter bank. |
| `subtractor` | Linear echo subtraction built on the adaptive filter. |
| `decimator` | The signal decimator used ahead of delay estimation. |
| `matchedfilter` | Cross-correlation-based coarse delay estimation. |
| `delaypipe` | The delay estimator's internal pipeline/state machine. |
| `aecstate` | `AecState` — the shared state tracked across a stream (usability, ERL/ERLE, saturation). |
| `erle` | Echo Return Loss Enhancement estimation. |
| `reverb` | Reverb/late-reflection modeling. |
| `echoremover` | The top-level `EchoRemover` orchestrating subtraction + suppression per frame. |
| `suppressor` | The nonlinear residual-echo suppressor. |
| `ec3` | Top-level integration: the full `EchoCanceller3` (`AnalyzeRender`/`AnalyzeCapture`/`ProcessCapture`), matching this package's own `Canceller` call pattern end to end. |

### Oracle: fetched, not vendored

Unlike this repo's other cgo parity oracles (libopus, libFLAC, libebur128 — all a few thousand lines of C vendored directly into their package trees), the AEC3 oracle is ~20-25 kLOC of C++17 plus an abseil-cpp dependency — large enough that committing it would dwarf every other vendored engine in this repo combined. Instead it's fetched into a per-user cache directory on demand and built there with meson+ninja, never checked in:

```sh
MISE_EXPERIMENTAL=1 mise run //aec:oracle:fetch  # one-time, network + meson/ninja required
MISE_EXPERIMENTAL=1 mise run //aec:parity        # 15 bit-exact slices, -tags='aec_oracle aec_strict'
MISE_EXPERIMENTAL=1 mise run //aec:vet           # go vet, CGO_ENABLED=0
```

See `oracle/VERSION` for the exact pinned tarball URL, SHA-256, abseil-cpp pin, and build recipe (`-Dneon=disabled`, `--default-library=static`).

Without `aec_oracle` (or without cgo, or before the oracle has been fetched), each parity slice's real cgo test files are excluded from the build at the **build-constraint level**, not a runtime `t.Skip` — the fetched oracle's headers/libraries may not exist on disk at all, and a runtime skip can't recover from a missing-header compile failure. See `internal/parity_tests/smoke/cgo.go`'s doc comment for the full rationale. A bare `go test ./aec/...` still runs the full non-parity unit suite (including `TestCanceller_EchoReduction`) with no network access and no C++ toolchain required.

### `aec_strict`: two FP trap classes

The oracle is built with `-ffp-contract=off -Dneon=disabled`; the `aec_strict` build tag makes the Go port match it bit-exactly by guarding against the two ways floating-point arithmetic can silently diverge from a literal port:

1. **FMA contraction.** The Go spec permits — and the arm64 backend performs — fusing `a + b*c` into a single-rounding FMA, which diverges from the oracle's separately-rounded multiply-then-add by up to 1 ULP. Every `a*b+c`-shaped expression in `internal/aec3` routes through the `mla`/`muladd`/`mulsub` composite helpers (`fp_strict.go`/`fp_default.go`), which force a rounding boundary between the multiply and the add via `//go:noinline` primitives under `aec_strict`, and are plain fused-eligible expressions otherwise. Same convention as the opus port's `fma_strict.go`/`fma_default.go` and the flac port's `f64` helpers.
2. **SIMD/NEON auto-vectorization divergence.** Auto-vectorized C++ can reorder or regroup floating-point reductions in ways that change rounding versus the scalar reference algorithm. Rather than a Go-side guard, this is addressed by building the oracle itself with `-Dneon=disabled` (mirroring the opus black-box oracle's `--disable-intrinsics` choice), so the C++ side stays scalar-equivalent and the comparison is apples-to-apples.

The default (non-`aec_strict`) build lets the compiler fuse for speed and is not a bit-exact parity target — this is the same shape as `flac_strict`/`opus_strict`.

```sh
go test ./aec/...                             # full non-parity unit suite, no cgo needed
CGO_ENABLED=0 go build ./...                  # confirms no cgo leaks into the default build
```

## Benchmarks

`Canceller.Process` + `FeedFarEnd` throughput, one 10ms frame per iteration, 48kHz stereo (`canceller_bench_test.go`, `BenchmarkProcess_48kStereo`):

| Frame | Throughput | Realtime factor | Allocs/op | Bytes/op |
|---|---|---|---|---|
| 10ms @ 48kHz stereo | ~306 µs/op (~25.1 MB/s) | **~32.6×** realtime | ~69 | ~36.8 KB |

Reproduce:

```sh
go test -run '^$' -bench BenchmarkProcess_48kStereo -benchmem ./aec/...
```

This is a standalone throughput report, not a Go-vs-C comparison — there is no cgo runtime backend to compare against (the C++ side exists only as a parity oracle, never a production path). See BENCHMARKS.md's "Hardware video (hwaccel)" section for the same standalone-report convention.

## License

This package's own Go code (`aec/*.go`, `aec/internal/aec3/**`) is a 1:1 port of AEC3, which is BSD-3-Clause plus a WebRTC PATENTS grant (a license to Google's related patents, conditioned on not asserting patent claims against implementations of the covered specification) — the same shape as libfvad's grant already accepted for this toolkit's VAD package. Like the FLAC and Opus ports, this needs **no license fence**: it compiles into a plain `go build ./...` / `CGO_ENABLED=0` build with no opt-in build tag. Only the C++ parity oracle (fetched into a local cache, never vendored, `aec_oracle`-gated) remains opt-in. See the root [`LICENSING.md`](../LICENSING.md) for the full statement and file-by-file map.

## Known limitations

- **Native rates only.** `SampleRate` must be 16000, 32000, or 48000 Hz, matching AEC3's own frequency-band splitting — no internal resampling for other rates. Use [`resample`](../resample) ahead of the canceller if your source runs at something else (e.g. 44100 Hz).
- **No SIMD yet.** The Go port is scalar-only end to end (no NEON/AVX kernels), unlike e.g. `libraries/flac`'s NEON pass. The adaptive FIR filter bank (`internal/aec3/adaptivefir.go`) is the most likely candidate for a future NEON optimisation pass if throughput becomes a bottleneck — see the Benchmarks section above for the current baseline to compare against.
- **Construction-time configuration.** Per upstream, `SampleRate`/`CaptureChannels`/`RenderChannels` are fixed for a `Canceller`'s lifetime; there is no live reconfiguration, only `Reset()` (same config, cleared state).
- **No AGC level-step hint.** Upstream's `ProcessCapture` takes a `levelChange` hint (signalling an AGC gain step just occurred); the public API has no surface for a caller to signal this, so `Canceller.Process` always passes `false` — the conservative, no-hint path, as if no AGC ran ahead of the canceller.
