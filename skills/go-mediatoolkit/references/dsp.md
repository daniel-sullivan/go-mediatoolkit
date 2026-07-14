# Loudness, VAD, and echo cancellation — loudness, vad, aec

All three are pure Go in every build (their C/C++ references are parity oracles only, never runtime backends), operate on interleaved `float64` in `[-1, 1]`, and integrate through `mutations.Processor`.

## loudness — EBU R128 / BS.1770-4 metering + normalisation

```go
import "github.com/daniel-sullivan/go-mediatoolkit/loudness"
```

**Units**: LUFS (absolute loudness), LU (relative delta, e.g. LRA), dBTP (inter-sample true peak), dBFS (sample-domain). Presets: `TargetEBUR128` (−23 LUFS), `TargetATSC` (−24), `TargetPodcast` (−16), plus streaming targets; ceiling presets like `CeilingEBUR128` (−1 dBTP). Convert with `mutations.Decibels` / `mutations.AmplitudeToDecibels`.

### One-shot measurement and normalisation

```go
meas, err := loudness.Measure(clip) // clip is a mutations.Audio → *Measurement
// meas.Integrated (LUFS), meas.Range (LU), meas.TruePeak (dBTP), meas.SamplePeak, meas.RMS

res, err := loudness.Normalize(clip, loudness.NormalizeOptions{
    Target: loudness.TargetPodcast,
    Mode:   loudness.NormalizeLimit,
})
// mutates clip.Data in place, length-preserving; res.GainDB, res.Output.Integrated
```

- `NormalizeClamp` (default): one constant gain, reduced so the true-peak ceiling is never exceeded — fully transparent, may land under target on peaky material.
- `NormalizeLimit`: full gain to target, then an internal `Limiter` tames the raised peaks — hits the number, trades a little peak dynamics.
- Silent input → `ErrSilentInput`.

### Streaming metering

```go
m, err := loudness.NewMeter(48000, 2, loudness.ModeAll) // or ModeIntegrated|ModeTruePeak|...
err = m.AddFrames(chunk)                                 // any chunking
momentary, err := m.Momentary()   // LUFS, trailing 400 ms
integrated, err := m.Integrated() // gated, whole stream
lra, err := m.Range()             // LU
tp, err := m.TruePeak(0)          // LINEAR amplitude per channel — convert to dB yourself
```

- A reader whose `Mode` bit wasn't requested returns `ErrInvalidMode`; before enough audio, loudness readers return `(-Inf, nil)` — a valid state, not an error.
- **24/7 streams**: integrated/LRA history is unbounded by default — call `SetMaxHistory` or construct with `ModeHistogram`.

### Streaming processors (all `mutations.Processor`)

- `NewNormalizer(measuredLUFS, targetLUFS)` — fixed gain from a prior measurement.
- `NewLimiter(loudness.LimiterConfig{SampleRate: 48000, Channels: 2})` — lookahead true-peak limiter; output delayed by `LatencyFrames()`. **Flush the tail**: offline `audio.RenderWithEffects(chain, lim.Latency())`; streaming `timeline.NewEffectSource(src, lim).WithTail(lim.Latency())`. Expect inter-sample regrowth of ~0.1–0.3 dB above the ceiling — leave margin (the default −1 dBTP does).
- `NewLeveller(loudness.LevellerConfig{SampleRate: 48000, Channels: 2})` — adaptive AGC steering toward a target; embeds a `Limiter` (disable via `LevellerConfig.DisableLimiter`); `GainDB()` reports the current ride. Original design (no external oracle).
- `NewMonitor(sampleRate, channels, mode)` — a mutex-guarded meter, the **one** processor safe to read from other goroutines while a mixer drives `Process`; drop it into `mixer.Config.Processors` and call `mon.ShortTerm()` etc. from anywhere.

## vad — voice-activity detection + Gate/Ducker

```go
import "github.com/daniel-sullivan/go-mediatoolkit/vad"
```

A `Detector` is a **pass-through** `mutations.Processor` (never modifies samples). Multi-channel input is downmixed internally; detection is mono. State surfaces four ways: `Process` (chain insertion), polled atomics (`Active()`, `Probability()`, `LastTransition()` — safe from any goroutine), an `events.Bus[SpeechEvent]` (`det.Events().Subscribe(...)`, published synchronously on the audio goroutine — callbacks must be fast), and the dynamics processors below.

| Engine | Constructor | Character | Decision latency (defaults) |
|---|---|---|---|
| Energy | `NewEnergyDetector(vad.EnergyConfig{SampleRate, Channels, ...})` | band-limited adaptive threshold; cheapest; tones/music fire it | ~40 ms |
| WebRTC | `NewWebRTCDetector(vad.WebRTCConfig{SampleRate, Channels, Mode: 0–3, ...})` | GMM speech model (bit-exact libfvad port); real discrimination at low latency | ~60 ms |
| Silero | `NewSileroDetector(vad.SileroConfig{SampleRate, Channels, Threshold, ...})` | neural (Silero VAD v6.2, vendored weights, no onnxruntime); best discrimination; `Probability()` is a real posterior | ~288 ms |

- Config zero values are sensible defaults; every knob is live-settable mid-stream from any goroutine (`SetMode`, `SetOnset`, `SetHangover`, `SetThreshold`, ...).
- WebRTC native rates are 8/16/32/48 kHz; Silero is fixed 16 kHz — other rates resample internally (event positions then accurate to one engine frame, not sample-exact).
- Events are **back-timestamped**: `SpeechStart.Frame` points at where speech actually began; the announcement lags by `DecisionLatency()`.
- **Flushing finite streams**: a resampling detector may hold up to `DecisionLatency()` of trailing audio — feed that much trailing silence (`timeline.NewEffectSource(src, det).WithTail(det.DecisionLatency())`) or end-of-stream events may never fire.

### Gate (mute non-speech in the detector's own stream)

```go
gate, err := vad.NewGate(vad.GateConfig{
    SampleRate: 48000, Channels: 1,
    Detector:  det,                   // the Gate FEEDS this detector
    Lookahead: det.DecisionLatency(), // gate is open before the triggering audio emerges
    FloorDB:   -60,                   // 0 → full mute
})
gate.Process(samples) // length-preserving, delayed by gate.LatencyFrames()
```

**Single-feeder contract**: the Gate owns the detector's stream — do not `Process` that detector anywhere else (a second feeder silently corrupts stream positions; readers like a `Ducker` are fine). Keep `Process` buffers small (≤ ~10 ms) so gating decisions stay timely. Flush tails with `WithTail(gate.Latency())`.

### Ducker (attenuate a bed while a sidechain hears speech)

```go
duck, err := vad.NewDucker(vad.DuckerConfig{
    SampleRate: 48000, Channels: 2,
    Detector: det, // READ-ONLY sidechain — fed elsewhere (the voice track's chain)
})
```

Sits in the music/bed track's chain and only reads `det.Active()`. Defaults: depth −12 dB, attack 50 ms, release 400 ms. Cross-chain staleness of ≤ 1 chunk is absorbed by the ramps — don't feed the detector in multi-second batches.

## aec — acoustic echo cancellation (WebRTC AEC3 port)

```go
import "github.com/daniel-sullivan/go-mediatoolkit/aec"

c, err := aec.NewCanceller(aec.CancellerConfig{
    SampleRate:      48000, // MUST be 16000, 32000, or 48000 — resample 44100 first
    CaptureChannels: 1,     // 1..64, independent of RenderChannels
    RenderChannels:  1,
})
err = c.FeedFarEnd(renderChunk) // whatever went to the loudspeaker (e.g. mixer master tap)
c.Process(captureChunk)         // mic input; echo removed IN PLACE (mutations.Processor)
```

- **Two-stream, single-goroutine**: `FeedFarEnd` and `Process` must be serialized onto one goroutine — never concurrent with each other or themselves. Only `SetAudioBufferDelay(d)` and `Metrics()` are safe from any goroutine at any time.
- Latency is fixed at exactly one 10 ms AEC3 frame regardless of chunking.
- Delay handling is built in: continuous estimation (`Metrics().DelayMs`), clockdrift detection (`Metrics().Clockdrift` — `ClockdriftLevelNone/Probable/Verified`), and an optional `SetAudioBufferDelay` hint to converge faster. Tolerates ~20 ms to >1 s of drifting delay.
- `Metrics()` also reports `EchoReturnLoss` / `EchoReturnLossEnhancement` (dB).
- Config is fixed at construction (`Reset()` clears state at the same config). Full AEC3 tuning surface via `CancellerConfig.Tuning *config.Config` (package `aec/config`): `nil` → `config.DefaultConfig()` (upstream defaults); out-of-range fields are rejected with an error wrapping `aec.ErrBadArg`, never silently clamped.
- Converges on broadband content — a pure tone won't drive the delay estimator; use noise/speech when testing.

## Composing the three

Typical voice-chat chain: mixer master output → tap into `aec.FeedFarEnd`; microphone capture → `aec.Process` → `vad` detector (in the voice track's `timeline.EffectSource`) → `vad.Gate`; music bed → `vad.Ducker` reading the same detector; master bus → `loudness.Leveller` + `loudness.Monitor` in `mixer.Config.Processors`.
