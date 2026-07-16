# duplex

A full-duplex voice-session engine: one type pairing a **paced render (playback) path** with a **capture (microphone) path**, with the AEC far-end/near-end coupling handled internally. This is the core loop of a realtime voice agent — play synthesized speech while listening for the user — needing echo cancellation ([`aec`](../aec)), noise suppression ([`denoise`](../denoise) in the capture chain), VAD-driven speech events ([`vad`](../vad)), and pre-roll (`vad.PreRoll`).

```go
import "github.com/daniel-sullivan/go-mediatoolkit/duplex"
```

Like every pipeline package in this toolkit, the engine speaks interleaved **`float64`** samples in `[-1, 1]` at one sample rate and channel count. **Codecs are out of scope**: decode network audio before `Push`, encode after the output callback ([`codec/opus`](../codec/opus), [`codec/pcm`](../codec/pcm)).

## Data flow

One high-precision 10 ms ticker ([go-hpt](https://github.com/daniel-sullivan/go-hpt)) drives a single audio goroutine that owns **all** DSP state:

```
render:  FeedChunk ─▶ jitter buffer ─▶ seam crossfade ─▶ RenderChain ─▶ ambient mix ─▶ clamp ─┬─▶ output callback
                                                                                              └─▶ AEC far-end
capture: Push ─▶ bounded queue ─▶ 10ms re-frame ─▶ AEC echo removal ─▶ CaptureChain ─▶ Detector ─▶ Events / pre-roll
```

```go
det, _ := vad.NewSileroDetector(vad.SileroConfig{SampleRate: 16000, Channels: 1})
e, _ := duplex.New(duplex.Config{
    SampleRate: 16000, Channels: 1,
    Detector: det,
    AEC:      new(duplex.AECConfig),         // nil = no echo cancellation
    PreRoll:  300 * time.Millisecond,
})
e.SetOutput(func(frame []float64, seq int64) { /* encode + send; copy to keep */ })
e.Start(ctx)

e.FeedChunk(ttsChunk)     // stream synthesis as it arrives (any goroutine)
e.MarkChunkBoundary()     // seam between independently-generated chunks
e.Push(micFrame, tsMs)    // capture in, tagged (any goroutine)

for ev := range e.Events() {
    switch ev := ev.(type) {
    case duplex.SpeechStart: // barge-in: e.ClearPending()
    case duplex.AudioFrame:  // feed the ASR: ev.Frame, ev.Tag
    case duplex.SpeechStop:  // finalize the ASR turn
    }
}
```

## Render side

All render methods are safe from any goroutine.

- **`FeedChunk(pcm)`** appends streamed voice audio to a jitter buffer that slices it into fixed 10 ms frames; a trailing partial frame waits for the next chunk to complete it. The buffer grows without bound (the producer is the session's own synthesis stream, not an untrusted peer); `ClearPending` is the pressure valve.
- **`MarkChunkBoundary()`** marks the seam between independently-generated chunks (two TTS generations meet with a phase discontinuity — an audible click). The first frame of the next chunk fed is blended **equal-power** (`cos`/`sin`, midpoint-sampled) against the tail of the frame played before it, over `Config.Crossfade` (default 5 ms, capped at one frame). The seam is positional: it lands on the actual chunk head no matter how far behind the paced reader runs. A pending partial frame is flushed zero-padded first — an independent generation must not splice mid-frame onto the previous one's remainder.
- **`ClearPending()`** drops all queued-but-unplayed voice audio immediately — the barge-in primitive. Ambient keeps playing.
- **Ambient bed** (`Config.Ambient`): a fixed loop mixed under the voice at a linear gain, sample-precise across its wrap, so the far side never hears dead air. The mixed frame is hard-clamped to `[-1, 1]`.
- **`RenderChain`** (`[]mutations.Processor`): gain, `mutations.Biquad` filters, a `loudness.Limiter` — applied to the voice after the crossfade, before the ambient mix.
- **Paced emit**: exactly one frame per 10 ms tick — silence on underrun, so the output cadence never gaps — through the output callback `func(frame []float64, seq int64)` (`SetOutput`, nil-safe, live-swappable). `seq` increments by 1 per tick; the frame slice is reused, copy to keep.

## Capture side

- **`Push(frame, tag)`** — thread-safe bounded enqueue (default 2.5 s) of a capture frame of any whole-channel length, paired with an opaque `int64` tag (typically a capture timestamp). The engine re-frames to 10 ms internally, assigning each internal frame the tag of the push its first sample came from. On overflow the **newest** frame is shed (`ErrCaptureOverflow`, counted in `Metrics`): the buffered frames are older and already owed to the ASR in order — dropping them would tear a hole mid-stream, while the rejected push surfaces to the caller.
- Each tick drains a **bounded batch** (absorbing network bursts a few times faster than real time without stalling the render side), running per frame: AEC echo removal → `CaptureChain` (e.g. a `denoise.GTCRN`) → `Detector` (pass-through observer).

### Speech events

Events arrive on one **FIFO channel** (`Events()`). Per utterance:

1. **`SpeechStart`** — fired once (redundant detector transitions are absorbed). `Timestamp` is back-dated to the oldest pre-roll tag (or the live frame's tag if pre-roll is empty/disabled) minus `LeadIn`, clamped ≥ 0; `OnsetFrame` is the detector's own back-timestamped onset position.
2. **Lead-in silence** — `LeadIn` (default 30 ms) of synthetic zero frames with back-dated tags counting up toward the real start, giving a downstream ASR a beat of empty audio to spin up on.
3. **Pre-roll replay** — the post-DSP out-of-speech frames banked while the detector was still confirming the onset (`Config.PreRoll`, via `vad.PreRoll`), oldest first with their original tags: the first syllables reach the ASR.
4. **Live frames** — post-DSP `AudioFrame`s while speech lasts.
5. **`SpeechStop`** — on the detector's inactive transition; the engine resumes banking pre-roll for the next utterance.

The audio goroutine **blocks** when the events channel is full rather than dropping — a dropped frame would corrupt the downstream ASR transcript, which is strictly worse than a late tick. Size `Config.EventBuffer` (default 256) for the consumer's worst-case lag.

## The AEC coupling

`aec.Canceller` requires `FeedFarEnd` and `Process` to be serialized onto one goroutine, paced roughly together — a contract that is easy to misuse when playback and capture live in different parts of a program. The engine internalises it: the same tick that emits a render frame feeds it to the canceller as far-end reference *before* the output callback fires, and capture frames are echo-cancelled on that same goroutine. `SetAudioBufferDelay` passes an external delay estimate through; `Metrics()` exposes ERL/ERLE/delay/clockdrift. Note the canceller's fixed 10 ms processing latency: a capture frame's post-DSP content lags its tag by one frame.

## Detector ownership and tuning

The engine is the `Detector`'s **exclusive feeder** (the vad package's one-feeder-per-stream contract). Detector tuning setters are lock-free atomics, safe from any goroutine and effective at the next decision window, so adjusting them live mid-stream — even mid-utterance — is supported. Two are passed through on the engine for probability-scored detectors: `SetVADThreshold` (e.g. raising the bar while known far-end audio plays) and `SetVADMinSilence` (the endpointing wait — lengthen it while a long-form answer is expected, shorten it for rapid turn-taking); both return `ErrTuningUnsupported` for detectors without the corresponding setter. Every other knob is reachable on the concrete detector you constructed (also via `Detector()`). For VADIterator-style immediate onsets pair the pre-roll with `MinSpeech: time.Nanosecond, SpeechPad: time.Nanosecond` (see the [vad README](../vad/README.md)).

## Lifecycle and metrics

`New` validates everything up front (constructors return errors, never panic). `Start(ctx)` launches the audio goroutine; `Stop()` (or ctx cancellation) halts it, cancels the detector subscription, and closes the events channel — already-queued events remain readable. An engine is single-use. `Metrics()` is a lock-free snapshot from any goroutine: AEC metrics, capture queue depth, dropped capture frames, rendered frame count.

## Verification

`go test ./duplex/` covers tick pacing, underrun silence, chunk slicing/carry, seam crossfading (with a hard-cut control), ambient looping/clamping, barge-in, capture re-framing and tag assignment, burst absorption with bounded per-tick work, overflow accounting, the full FIFO event sequence with back-dated tags, in-speech guards, and an end-to-end AEC loopback: 8 s of far-end pink noise with a 60 ms/−6 dB synthetic echo path must converge to ≥ 15 dB echo reduction with **no** SpeechStart, then a genuine near-end tone superimposed mid-playout must fire one. `-race` clean.

`BenchmarkEngineTick_AECSileroPreRoll` measures the fully-loaded 10 ms tick (AEC + Silero + pre-roll): ~341 µs on an Apple M3 Pro core — headroom for ~29 concurrent sessions per core.

See [`examples/session`](examples/session/main.go) for a headless end-to-end run.
