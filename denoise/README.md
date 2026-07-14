# denoise

Streaming single-channel noise suppression for interleaved `float64` audio, as pluggable **engines** behind one interface — mirroring the multi-engine structure of the [`vad`](../vad) package. Two engines ship today: **RNNoise**, a bit-exact 1:1 pure-Go port of Xiph's 48 kHz fullband recurrent denoiser, and **GTCRN**, a pure-Go hand-port of a compact 16 kHz neural speech-enhancement network. Both run with `CGO_ENABLED=0` — the C and ONNX references they are ported from are used only as opt-in, test-only parity oracles, never as a runtime backend.

Unlike a [`vad`](../vad) detector (which observes the stream and leaves it untouched), a denoise `Engine` is a *mutating* [`mutations.Processor`](../mutations): `Process` replaces the samples it is given with their denoised version, in place, so it drops into any effects chain — a `timeline.EffectSource` on a track or a `mixer.Config.Processors` master bus — like any other processor.

## Usage

```go
import "github.com/daniel-sullivan/go-mediatoolkit/denoise"
```

`denoise` operates on interleaved **`float64`** samples in `[-1, 1]` — the same convention as [`mutations`](../mutations), which it builds on.

```go
eng, err := denoise.NewRNNoise(denoise.RNNoiseConfig{SampleRate: 48000})
if err != nil { /* ... */ }

eng.Process(chunk) // denoise in place; output delayed by eng.Latency()
```

### Latency

Suppression is spectral (overlap-add on windowed frames), and for engines whose native rate differs from the stream, resampled — so an engine introduces algorithmic delay. `Latency()` reports it honestly (`time.Duration`) so callers can compensate, e.g. as timeline lookahead. Feed `Latency()`-worth of trailing silence at end-of-stream to flush the tail.

### One feeder per stream

An `Engine` inherits the `mutations.Processor` contract: a single instance is bound to the stream it was constructed for and is not safe to share across logical streams or goroutines without external synchronisation. Tuning that is safe to change mid-stream is exposed through lock-free atomics on the concrete type.

## Engines

### RNNoise — `NewRNNoise`

A **bit-exact 1:1 pure-Go port of [Xiph RNNoise](https://github.com/xiph/rnnoise) v0.2** (BSD-3-Clause), Jean-Marc Valin's hybrid DSP + recurrent-network denoiser: a biquad high-pass, a 960-point FFT, 32-band spectral features and pitch analysis feeding three sparse GRUs that emit per-band gains. It runs natively at **48 kHz fullband**; other input rates are resampled to 48 kHz and back.

```go
eng, _ := denoise.NewRNNoise(denoise.RNNoiseConfig{
    SampleRate: 48000, // default 48000; [8000, 384000] resampled to native
    Channels:   1,     // RNNoise is mono — one engine per channel, or downmix first
})
```

As a by-product of its architecture, RNNoise computes a voice-activity probability for every frame, exposed via **`Probability() float64`** (lock-free) — so a single RNNoise engine can both clean a stream *and* drive [`vad`](../vad)'s `Gate`/`Ducker` without a separate detector.

The port is verified **bit-for-bit** against the vendored C reference: `math.Float32bits`/`Float64bits`-exact parity slices for the tables, biquad, transform, band analysis, pitch chain, feature extraction, the network, and a minutes-long end-to-end run (output PCM *and* the VAD probability). See [parity](#parity--testing).

### GTCRN — `NewGTCRN`

A pure-Go hand-port of the **[GTCRN](https://github.com/Xiaobin-Rong/gtcrn)** streaming speech-enhancement network (MIT code + MIT DNS3-trained weights): a ~24 K-parameter grouped dual-path RNN with an ERB filterbank, sub-band feature extraction, temporal recurrent attention, and a complex-ratio mask. It operates at **16 kHz** on mono speech, carrying three recurrent caches frame-to-frame, at roughly **32 ms** latency.

```go
eng, err := denoise.NewGTCRN(denoise.GTCRNConfig{
    SampleRate: 16000, // >16000 → ErrUnsupportedSampleRate; <16000 resampled up and back
    Channels:   1,     // 1..64 (ErrBadChannels otherwise); multi-channel is downmixed, result broadcast
})
```

Every operator is ported from the vendored **ONNX graph** — not the PyTorch source, because the exporter's GRU gate order (`z,r,h`), `linear_before_reset`, and real `ConvTranspose` decoder differ — and parity-gated against [onnxruntime](https://onnxruntime.ai/) under the mixed criterion `max(|Δ| ≤ 1e-4, rel ≤ 1e-3)`. The STFT/ISTFT front end (sqrt-periodic-Hann, 512-point, 256-hop) is verified separately against a torch golden.

Because GTCRN denoises only up to its 8 kHz Nyquist, input **above 16 kHz is rejected** (`ErrUnsupportedSampleRate`) rather than silently band-limited — resample explicitly and choose whether to preserve the high band. For fullband material, use RNNoise.

## Parity & testing

Both engines are pure Go and compile in the default (`CGO_ENABLED=0`) build. The references are used only to *verify* the ports, behind build tags and env gates, and are never a runtime dependency:

- **RNNoise** — bit-exact against the vendored C RNNoise 0.2 (`libraries/rnnoise/librnnoise/`), compiled through Cgo with the generic (non-SIMD) `vec.h` branch forced (`-DDISABLE_NEON`) and `-ffp-contract=off`, so both sides round identically. Run with `mise run //libraries/rnnoise:parity` (needs a C toolchain; strict-FP mode via `-tags=rnnoise_strict`).
- **GTCRN** — tolerance parity against onnxruntime. The oracle is compiled only under Cgo with `ONNXRUNTIME_SHARED_LIB` set (the [Silero VAD](../vad) precedent); default CI instead compares the pure-Go port against committed golden JSON. Run with `mise run //denoise:parity:gtcrn`.

Default tests (`mise run //denoise:test`) need neither — they exercise the ports against committed goldens with no Cgo.

## Licensing

RNNoise is BSD-3-Clause; GTCRN code and its DNS3 weights are MIT. Both sets of embedded weights and the vendored references are catalogued in the repository [`LICENSING.md`](../LICENSING.md), with per-model provenance (upstream commit + artifact SHA-256) in each engine's `VERSION` file.
