# vad

Streaming voice-activity detection (VAD) for interleaved `float64` audio, plus two VAD-driven dynamics processors: a speech **Gate** and a sidechain **Ducker**.

A detector is a pass-through [`mutations.Processor`](../mutations): `Process` feeds audio into the detection engine and never modifies it, so a detector drops into any effects chain — a `timeline.EffectSource` on a track, or a `mixer.Config.Processors` master bus — purely to observe the stream. Detection state comes out four ways:

- **Pass-through chain insertion** — the `Detector` itself, via `Process`.
- **Polled state** — `Active()`, `Probability()`, `LastTransition()`; lock-free atomics, safe to read from any goroutine while another drives `Process`.
- **Events** — an `events.Bus[SpeechEvent]` publishing `SpeechStart`/`SpeechEnd` transitions with back-timestamped stream positions.
- **Dynamics** — `Gate` (mutes/attenuates non-speech in the detector's own stream) and `Ducker` (attenuates a *different* stream while the sidechain hears speech).

## Engines

The engine is fixed at construction; every tuning knob is live-settable mid-stream from any goroutine (see Concurrency). All engines implement the same `Detector` interface, so `Gate`/`Ducker`/user code don't care which one they're given.

| Engine | Constructor | Kind | Decision latency (defaults) | Status |
|---|---|---|---|---|
| **Energy** | `NewEnergyDetector` | Band-limited adaptive-threshold energy (original design) | ~40 ms (10 ms frame + 30 ms onset) | **Available** |
| **WebRTC** | `NewWebRTCDetector` | GMM speech model — bit-exact pure-Go port of [libfvad](https://github.com/dpirch/libfvad) | ~60 ms (20 ms frame + 2-frame onset) | **Available** |
| **Silero** | `NewSileroDetector` | Neural (Silero VAD v6.2, hand-ported fixed graph, vendored MIT weights) | ~288 ms (32 ms window + 250 ms MinSpeech gate) | **Available** |

Rule of thumb: **Energy** for lowest latency/cost on clean-ish signals, **WebRTC** for a real speech-vs-noise model at low latency, **Silero** for the best discrimination when its latency is acceptable. Low-latency gating should steer to Energy/WebRTC.

## Usage

```go
import "github.com/daniel-sullivan/go-mediatoolkit/vad"
```

`vad` operates on interleaved **`float64`** samples in `[-1, 1]` — the same convention as the rest of the toolkit. Multi-channel input is downmixed internally (stereo: L/R average; >2 channels: equal-weight average); detection is mono.

### Detecting speech

```go
det, err := vad.NewEnergyDetector(vad.EnergyConfig{SampleRate: 48000, Channels: 1})

det.Events().Subscribe(func(ev vad.SpeechEvent) {
    fmt.Printf("%s at %v (frame %d)\n", ev.Kind, ev.Pos, ev.Frame)
})

det.Process(chunk)   // pass-through: chunk is never modified
if det.Active() { …} // polled state, safe from any goroutine
```

Every `EnergyConfig` field except `SampleRate`/`Channels` has a zero-value default (`HighpassHz` 200, `LowpassHz` 4000, on/off thresholds +6/+3 dB over the adaptive floor, absolute floor −55 dBFS, onset 30 ms, hangover 200 ms). Wherever a literal zero would be meaningful the field doc names the epsilon escape hatch (e.g. `Onset: time.Nanosecond` for no debounce, `AbsoluteFloorDB: math.Inf(-1)` to disable the absolute gate) — the [`loudness`](../loudness) convention. See [`examples/detect`](examples/detect/main.go).

### The WebRTC engine

```go
det, err := vad.NewWebRTCDetector(vad.WebRTCConfig{
    SampleRate: 48000, Channels: 2,
    Mode: 2, // 0 "quality" (default) … 3 "very aggressive"
})
```

`NewWebRTCDetector` is a **bit-exact pure-Go port of [libfvad](https://github.com/dpirch/libfvad)** (the WebRTC project's legacy GMM voice-activity detector) behind the same `Detector` interface — a trained speech model (Gaussian mixtures over six sub-band energies, 80 Hz–4 kHz, adapted online) that discriminates speech from steady tones, hum, and stationary noise far better than an energy threshold, at nearly the same latency and cost. It is not immune to tonal false positives, though — a pure tone can still fool a Gaussian-mixture-over-energies model; Silero is the discriminating engine when that matters more than latency/cost. Because libfvad is pure integer arithmetic, the port is bit-identical to the C on every platform, pinned frame-by-frame by the cgo parity suite (`mise run //vad:parity`); the vendored reference lives in [`libfvad/`](libfvad/) and is compiled **only** by those parity tests — the shipped package stays cgo-free. libfvad is BSD-3-Clause plus an explicit Google WebRTC patent grant covering this implementation (see [`libfvad/PATENTS`](libfvad/PATENTS)); [`LICENSING.md`](../LICENSING.md) has the full fence map.

Config: `Mode` 0–3 (higher = more restrictive in reporting speech; live-settable via `SetMode`); `FrameDuration` 10/20/30 ms (zero → 20 ms, fixed at construction); `Onset` (zero → 2 engine frames) and `Hangover` (zero → 200 ms) debounce exactly as in the Energy engine, live via `SetOnset`/`SetHangover`. The native libfvad rates 8/16/32/48 kHz feed the engine unresampled; **any other rate is resampled to 16 kHz** internally (SincFastest), making event positions accurate to within one engine frame instead of sample-exact. `Probability()` is a binary 0/1 like every non-neural engine.

### The Silero engine

```go
det, err := vad.NewSileroDetector(vad.SileroConfig{
    SampleRate: 48000, Channels: 2,
    Threshold: 0.5, // zero → 0.5; NegThreshold derives as max(T−0.15, 0.01)
})
```

`NewSileroDetector` is a **pure-Go, dependency-free port of the [Silero VAD](https://github.com/snakers4/silero-vad) v6.2 neural model** (the fixed 16 kHz graph, hand-ported operator by operator; MIT-licensed weights vendored and embedded — no onnxruntime, no cgo, nothing to install). It is the package's best discriminator against music, tones, and noise. `Probability()` reports the real model posterior per 512-sample window.

The decision layer mirrors upstream `utils_vad.py`: probabilities at or above `Threshold` are speech; once speaking, only a drop below `NegThreshold` (default `Threshold−0.15`) starts the silence countdown, and `MinSilence` (100 ms) of it confirms the end — windows in between extend the current state. `MinSpeech` (250 ms) discards shorter bursts and acts as the onset gate (it dominates `DecisionLatency()`; set it small for VADIterator-style instant starts). `SpeechPad` (30 ms) pads both utterance edges, clamped so consecutive utterances never overlap. All five knobs are live: `SetThreshold` (re-derives `NegThreshold` unless it was set explicitly), `SetNegThreshold`, `SetMinSpeech`, `SetMinSilence`, `SetSpeechPad`. Input at any rate ≥ 8 kHz is resampled to the model's fixed 16 kHz (positions then accurate to within one 32 ms window).

Latency honesty: with defaults a `SpeechStart` is announced ~288 ms after the speech it back-timestamps began. That is inherent to the model's decision quality — gate with `Lookahead: det.DecisionLatency()`, or steer low-latency gating to Energy/WebRTC.

### Positions, back-timestamping, and latency honesty

`SpeechEvent` positions are **stream positions** (input frames since construction/`Reset`), not wall-clock times. A detector cannot announce speech the instant it begins — a decision frame must fill and an onset debounce must pass — so events are *back-timestamped*: `SpeechStart.Frame` points at where the detected run actually began, and `SpeechEnd.Frame` at where speech actually stopped (the hangover then confirms it). The announcement delay is reported honestly by `DecisionLatency()`, which tracks live `SetOnset` changes. Positions are sample-accurate for engines that don't resample internally (Energy never does) and within one engine decision frame otherwise.

**Flushing a finite stream:** a detector that resamples internally (Silero always; Energy/WebRTC only off their native rate) can still be holding up to `DecisionLatency()` worth of trailing audio in the resampler's FIR history and the not-yet-full decision frame when a finite stream's last `Process` call returns — so a speech run right at end-of-stream may never announce its `SpeechEnd` (or, if it's shorter than the tail, neither event at all). Flush it the same way `Gate` recommends flushing its own delay line: feed `DecisionLatency()` worth of trailing silence at end-of-stream — `timeline.NewEffectSource(src, detector).WithTail(detector.DecisionLatency())`, or an offline append of that much silence.

### Gating a voice track (`Gate`)

```go
gate, err := vad.NewGate(vad.GateConfig{
    SampleRate: 48000, Channels: 1,
    Detector:  det,                   // the Gate FEEDS this detector
    Lookahead: det.DecisionLatency(), // open before the onset emerges
    FloorDB:   -60,                   // 0 → full mute; −60 leaves faint room tone
})
gate.Process(samples) // length-preserving; output delayed by gate.LatencyFrames()
```

The Gate **feeds** its injected detector with the dry, undelayed signal, then applies a linearly-ramped gain (attack 5 ms / release 100 ms defaults) to the audio delayed by `Lookahead`. With `Lookahead = det.DecisionLatency()` the gate is already open when the audio that triggered the detection emerges — onsets survive. Because the Gate owns the detector's stream, don't insert that detector anywhere else — a `Detector` is a `mutations.Processor`, so (like any stateful `Processor`) a second feeder silently corrupts its stream position rather than erroring; `gate.Reset` resets it too. A `Ducker` is fine reading that same detector alongside the Gate (readers are goroutine-safe atomics) — only a second *feeder* is the problem.

`Latency()`/`LatencyFrames()` report exactly the configured lookahead (the `loudness.Limiter` honesty pattern). Flush the tail of a finite stream with `timeline.NewEffectSource(src, gate).WithTail(gate.Latency())`. See [`examples/gate`](examples/gate/main.go).

### Ducking a music bed (`Ducker`)

```go
duck, err := vad.NewDucker(vad.DuckerConfig{
    SampleRate: 48000, Channels: 2,
    Detector: det, // read-only sidechain — fed by the VOICE track's chain
})
```

The Ducker sits in the **bed** track's chain and only **reads** `det.Active()`; the detector is fed elsewhere (typically inserted in the voice track's `timeline.EffectSource`). The goroutine-safe atomic readers are what make that cross-track read legal; the ≤ 1-chunk staleness between the two chains is absorbed by the 50/400 ms attack/release ramps. The ducker is deliberately not coupled to the mixer package — it works between any two chains. Defaults: depth −12 dB, attack 50 ms, release 400 ms. See [`examples/ducking`](examples/ducking/main.go), which wires a real `mixer` with the detector on the voice track and the ducker on the bed.

### Pre-rolling an ASR feed (`PreRoll`)

```go
pre, err := vad.NewPreRoll(vad.PreRollConfig{
    SampleRate: 16000, Channels: 1,
    Duration: 300 * time.Millisecond, // 0 disables (Push no-op, Replay empty)
})

pre.Push(frame, captureTimestamp) // out-of-speech frames, tagged
// … detector announces speech:
start, _ := pre.OldestTag()       // back-date the utterance start here
pre.Replay(func(frame []float64, tag int64) { asr.Feed(frame, tag) })
```

A detector cannot announce speech the instant it begins (see back-timestamping above), so by announcement time the first syllables have already flowed past. `PreRoll` keeps the most recent `Duration` of caller-framed audio — each frame paired with an opaque `int64` tag, typically a capture timestamp — while no speech is in progress; on `SpeechStart`, `Replay` hands the buffered frames back oldest-first (then clears) so the utterance reaches the consumer whole, and `OldestTag` back-dates the utterance's start to where the replayed audio actually begins. Frame lengths may vary per call; frames replay with the exact boundaries and tags they were pushed with. `SetDuration` live-resizes the window (keeping the newest audio; zero disables). Like a `mutations.Processor`, a `PreRoll` is single-goroutine — drive it from the audio goroutine that feeds the detector.

Pair it with a detector tuned for immediate onset: on `SileroConfig`, `MinSpeech: time.Nanosecond` + `SpeechPad: time.Nanosecond` reproduce the upstream silero-vad `VADIterator` streaming semantics exactly — `SpeechStart` on the first window at or above `Threshold`, no onset gate, no back-padding — with the pre-roll supplying the lead-in audio that `SpeechPad`'s back-padding would otherwise cover.

## Concurrency

`Process`/`Reset` on any type in this package must be driven from a single goroutine (the audio goroutine), like every `mutations.Processor`. Everything else is deliberately goroutine-safe with **no mutex on the audio path**:

- **Readers** (`Active`, `Probability`, `LastTransition`, `DecisionLatency`, `GainDB`) are single-word atomics.
- **Live setters** (`SetThresholdsDB`, `SetAbsoluteFloorDB`, `SetOnset`, `SetHangover`; `SetFloorDB`/`SetAttack`/`SetRelease`; `SetDepthDB`/…) store into atomics that the audio goroutine snapshots once per decision frame (detectors) or per `Process` call (Gate/Ducker).
- **Pairs whose relative order matters are packed into one word**: the energy on/off thresholds share a single `atomic.Uint64` (two packed float32s, < 0.0001 dB quantisation), so a torn pair — an off threshold momentarily above on, inverting the hysteresis — is impossible.

**Event delivery contract**: events are published synchronously from `Process`, on the audio goroutine, per the [`events`](../events) bus contract — subscriber callbacks must be fast and non-blocking (spawn a goroutine for real work). State atomics are updated **before** `Publish`, so a subscriber observing a `SpeechStart` already sees `Active() == true`; the ordering is pinned by tests. Transitions are per-utterance rare, so a well-behaved subscriber adds no meaningful audio-path cost.

## The Energy engine's DSP (for tuning)

Non-overlapping ~10 ms frames on the band-limited (200 Hz–4 kHz Butterworth) mono stream; per-frame energy `E = 10·log10(mean(x²)+1e-12)` dBFS. An adaptive noise floor `F` (clamped to [−90, 0] dBFS, **frozen while raw-voiced** so speech can't drag its own reference up) tracks `E` asymmetrically — fast down (`FloorFall` 200 ms), slow up (`FloorRise` 4 s). A frame is raw-voiced when `E > F + OnThresholdDB` **and** `E > AbsoluteFloorDB`, and stays raw-voiced until `E < F + OffThresholdDB`. Onset/hangover debouncing turns the raw decisions into transitions. `Probability()` reports a binary 0/1 mirroring `Active()`.

Two consequences worth knowing:

- The floor initialises from the **first** frame's energy, so audio already in progress at construction reads as background, not speech — the detector needs to hear some quiet before it can tell loud from background (inherent to any adaptive-floor design).
- It's an energy detector, not a speech classifier: sustained in-band music or tones will fire it. That's what the WebRTC/Silero engines are for.

## Verification

The **WebRTC engine** is verified by a bit-exact cgo parity oracle: the vendored libfvad C (see [`libfvad/VERSION`](libfvad/VERSION)) is compiled inside five self-contained test slices under `internal/parity_tests/` — `fvad_sp` (SPL primitives, the full resampler family, downsampling, minimum tracking, with carried state compared per call), `fvad_filterbank` (six band energies + filter states per frame), `fvad_gmm` (probability/delta over dense Q-format grids), `fvad_core` (all four CalcVad rates × modes with a **complete state snapshot compared after every frame**), and `fvad_e2e` (the public API over the full {8,16,32,48} kHz × {10,20,30} ms × modes 0–3 matrix on 60 s streams, plus mid-stream reset and error-return parity). libfvad is pure integer, so every assertion is exact equality with **no** strict-mode build tag and **no** special CGO flags — `mise run //vad:parity` is a plain `go test`. The shipped package never compiles the C: `CGO_ENABLED=0 go build ./vad/...` stays green.

The Energy engine, Gate/Ducker, adapter and events are pinned behaviourally (the Energy engine is an original design with no external reference — like `loudness.Leveller`, its behaviour is defined by its doc comments and pinned by tests):

- **Energy cases** with exact event-position tolerances: burst start back-timestamps assert **equality** on frame-aligned bursts; ends within one decision frame (the band-limit filters' ring-down can hold one extra frame). Hum (50 Hz) and out-of-band (10 kHz) rejection with in-band controls at identical level; floor-step adaptation (a sub-absolute-floor level step fires nothing and re-references the relative threshold); hangover bridging (100 ms gap → one utterance) vs splitting (400 ms → two); live-setter semantics including pair validation.
- **Adapter**: the downmix→resample→chunk→convert pipeline (built now, consumed fully by the WebRTC/Silero phases) against a hand-built reference — manual downmix + one-shot `resample.Simple` + manual chunking; buffer-size invariance (1-frame / prime / giant pushes byte-identical); no trailing-sample loss; impulse-probe position mapping.
- **Gate/Ducker**: floor and depth exactness (the linear ramps clamp, so steady-state gains are exact); a hard per-sample click bound; lookahead delay exactness (impulse at `k` emerges at exactly `k+LatencyFrames()`); onset preservation with `Lookahead = DecisionLatency()` vs a zero-lookahead gate; the ducker's cross-chain chunk-interleave staleness case.
- **Events**: strict Start/End alternation, strictly monotonic positions, atomics-before-publish asserted from inside a subscriber.
- **Race**: `go test -race` drives `Process` against all setters and readers concurrently, including the documented cross-goroutine ducker topology.
- **Integration**: Gate in a `timeline.EffectSource` with `WithTail` flushing; the ducking topology on a live `mixer` — no changes were needed in `mixer`/`timeline`/`mutations`/`events`/`resample`, the `Processor` contract is the only coupling.

The **Silero engine** is verified against the original model three ways (see [`internal/silero/VERSION`](internal/silero/VERSION) for the full graph dump and weight provenance):

- **Opt-in onnxruntime oracle** (`internal/parity_tests/silero_ort`, `mise run //vad:parity:silero`): the vendored original `silero_vad_16k_op15.onnx` is executed by onnxruntime (pinned 1.27.0) and the pure-Go port must match **per-window within |Δp| ≤ 1e-4** over 60 s each of seeded noise ladders, synthetic speech-like pulse trains, SNR mixtures, silence, and square-wave edges — measured worst case is **max|Δp| = 1.6e-5** (the speech-pulses signal; every other signal class is ≤ 6e-6), logged per-run and asserted against the documented ≤ 2e-5 bound; the tolerance justification (fp32 summation-order drift) lives in the slice's package doc. The slice also byte-verifies the embedded safetensors against the onnx initializers. Skipped unless `ONNXRUNTIME_SHARED_LIB` points at the runtime.
- **Default-CI golden gate** (`silero_golden_test.go`, no onnxruntime needed): [`testdata/silero_golden.json`](testdata/silero_golden.json) pins oracle-produced probabilities for the exact same signals at the same 1e-4 bound, and the `silero-parity` workflow re-runs the oracle and fails if the committed values drift beyond the same tolerance (a byte-diff would flake: fp32 oracle outputs vary ~1e-6 across CPU ISAs) — the goldens cannot drift from the oracle.
- **Kernel unit tests** (`internal/silero`): hand-computed conv/reflect-pad cases and a 2-unit LSTM cell that pins the PyTorch i,f,g,o weight packing, for instant fault localisation.

The Silero decision layer (thresholds/MinSpeech/MinSilence/SpeechPad) is pinned behaviourally: the detector's event stream over the golden speech signal must match an independent simulation of the documented VADIterator semantics driven by the golden probabilities, across default, instant, long-gate, big-pad, and long-silence configurations.

```sh
go test ./vad/...                  # behavioural + integration suites (+ parity when cgo is on)
go test -race -count=1 ./vad/      # concurrency suite
CGO_ENABLED=0 go build ./vad/...   # confirms the package is cgo-free

# via mise (from the repo root)
MISE_EXPERIMENTAL=1 mise run //vad:test
MISE_EXPERIMENTAL=1 mise run //vad:test:race
MISE_EXPERIMENTAL=1 mise run //vad:parity
MISE_EXPERIMENTAL=1 mise run //vad:parity:silero   # needs ONNXRUNTIME_SHARED_LIB
MISE_EXPERIMENTAL=1 mise run //vad:golden:gen      # regenerate the Silero goldens
MISE_EXPERIMENTAL=1 mise run //vad:vet
```

## Units

| Convention | Meaning | Used by |
|---|---|---|
| **dBFS** | Sample-domain energy level; 0 dBFS == full-scale amplitude 1.0. Frame energy uses mean-square (a full-scale sine reads ≈ −3 dBFS). | `AbsoluteFloorDB`, the adaptive floor |
| **dB (relative)** | A margin above the adaptive floor, or a gain change. | `OnThresholdDB`/`OffThresholdDB`, `FloorDB`, `DepthDB`, `GainDB` |
| **frames** | One sample per channel, at the detector's input rate. | `SpeechEvent.Frame`, `LatencyFrames` |
| **probability** | Speech likelihood in [0, 1]; binary engines report exactly 0/1. | `Probability()`, `SpeechEvent.Probability` |

Convert between linear amplitude and dB with `mutations.Decibels` / `mutations.AmplitudeToDecibels`, as everywhere else in the toolkit.

## Known limitations

- **Energy is not a speech model.** Sustained in-band tones/music fire it; use it where the signal is "voice or quiet", or use the WebRTC engine for real discrimination.
- **Adaptive-floor warm-up.** Constructed mid-programme, the Energy detector treats the in-progress audio as background until it has heard genuine quiet (see the DSP section).
- **Gate control granularity.** The gate gain target updates at the detector's decision-frame cadence (~10 ms for Energy); feed `Process` reasonably small buffers (≤ ~10 ms) to keep announcement delay near `DecisionLatency` — a large buffer batches decisions at its end.
- **Ducker staleness.** Cross-chain reads lag up to one processing chunk; inaudible under the default ramps, but don't drive a ducker from a detector fed in multi-second batches.
