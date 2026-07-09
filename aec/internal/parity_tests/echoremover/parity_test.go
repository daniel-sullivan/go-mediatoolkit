//go:build cgo && aec_oracle

package echoremover

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// This slice drives the real, now-complete aec3.EchoRemover against
// the oracle's real webrtc::EchoRemover (created via
// webrtc::EchoRemover::Create -- see shim.h), over full render+capture
// scenarios at 16 kHz / 1 band / 1 render channel / 1 capture channel:
// bit-exact per-block output audio (both the final capture output and
// the linear filter output) plus GetMetrics(), on "clean" scenarios at
// two different echo delays and one "hostile" scenario mixing
// double-talk, saturation bursts and silence. End-to-end sanity checks
// (echo reduction and finite/bounded output) are layered on top of the
// bit-exact comparison.

// lcg is the same minimal PRNG the other slices use.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

func requireFloatSliceEqual(t *testing.T, iter int, name string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("iter %d: %s length mismatch: go %d, c %d", iter, name, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("iter %d: %s[%d] mismatch: go %v, c %v", iter, name, i, got[i], want[i])
		}
	}
}

func requireEqualF64(t *testing.T, iter int, name string, got, want float64) {
	t.Helper()
	if got != want {
		t.Fatalf("iter %d: %s mismatch: go %v, c %v", iter, name, got, want)
	}
}

// scenario mirrors the aecstate slice's scenario shape, extended with
// double-talk and saturation-burst windows for the hostile case.
type scenario struct {
	name        string
	delayBlocks int
	attenuation float32
	numBlocks   int

	gainChangeAt  int
	delayChangeAt int

	externalDelayAt    int
	externalDelayValue int

	// Hostile-only knobs (zero-valued/no-op for the clean scenarios).
	doubleTalkStart, doubleTalkEnd int
	saturationStart, saturationEnd int
	silenceStart, silenceEnd       int
	checkEchoReduction             bool
	checkFiniteBounded             bool
}

func TestEchoRemoverParity(t *testing.T) {
	scenarios := []scenario{
		{
			name:               "delay_6blocks",
			delayBlocks:        6,
			attenuation:        0.5,
			numBlocks:          2500,
			gainChangeAt:       500,
			delayChangeAt:      800,
			externalDelayAt:    20,
			externalDelayValue: 6,
			checkEchoReduction: true,
			checkFiniteBounded: true,
		},
		{
			name:               "delay_11blocks",
			delayBlocks:        11,
			attenuation:        0.7,
			numBlocks:          2700,
			gainChangeAt:       600,
			delayChangeAt:      950,
			externalDelayAt:    20,
			externalDelayValue: 11,
			checkEchoReduction: true,
			checkFiniteBounded: true,
		},
		{
			name:               "hostile_doubletalk_saturation_silence",
			delayBlocks:        8,
			attenuation:        0.6,
			numBlocks:          1500,
			gainChangeAt:       700,
			delayChangeAt:      1100,
			externalDelayAt:    400,
			externalDelayValue: 8,
			doubleTalkStart:    200,
			doubleTalkEnd:      350,
			saturationStart:    500,
			saturationEnd:      560,
			silenceStart:       900,
			silenceEnd:         980,
			checkFiniteBounded: true,
		},
	}

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			runEchoRemoverScenario(t, sc)
		})
	}
}

func runEchoRemoverScenario(t *testing.T, sc scenario) {
	const sampleRateHz = 16000
	const numRenderChannels = 1
	const numCaptureChannels = 1
	const blockSize = aec3.BlockSize

	config := config.DefaultConfig()

	goRemover := aec3.NewEchoRemover(config, sampleRateHz, numRenderChannels, numCaptureChannels)
	goRenderBuffer := aec3.NewRenderDelayBuffer(config, sampleRateHz, numRenderChannels)

	cRemover := newEchoRemoverC()
	defer cRemover.close()

	renderRng := lcg(0x2468 ^ uint32(sc.delayBlocks))
	noiseRng := lcg(0x1357 ^ uint32(sc.delayBlocks)<<8)
	nearendRng := lcg(0xABCD ^ uint32(sc.delayBlocks)<<4)

	var renderHistory []float32
	delaySamples := sc.delayBlocks * blockSize

	// Echo-reduction sanity: accumulate power over a converged tail
	// window (after the linear filter has had time to adapt) for both
	// the raw microphone signal (echo + noise) and the final Go output.
	const convergedTailStart = 2000
	var micPower, outputPower float64
	var tailSamples int

	// Finite/bounded sanity: track across all blocks.
	sawNonFinite := false
	const int16Min, int16Max = -32768.0, 32767.0
	outOfBounds := false

	for iter := 0; iter < sc.numBlocks; iter++ {
		silence := sc.silenceEnd != 0 && iter >= sc.silenceStart && iter < sc.silenceEnd

		render := make([]float32, blockSize)
		for i := range render {
			if silence {
				render[i] = 0
			} else {
				render[i] = renderRng.next() * 4000
			}
		}
		renderHistory = append(renderHistory, render...)

		renderBlock := aec3.NewBlock(1, numRenderChannels)
		copy(renderBlock.View(0, 0), render)
		goRenderBuffer.Insert(renderBlock)
		goRenderBuffer.PrepareCaptureProcessing()
		cRemover.insertRenderBlock(render)

		captureCursor := iter * blockSize
		capture := make([]float32, blockSize)
		doubleTalk := sc.doubleTalkEnd != 0 && iter >= sc.doubleTalkStart && iter < sc.doubleTalkEnd
		saturationBurst := sc.saturationEnd != 0 && iter >= sc.saturationStart && iter < sc.saturationEnd
		for i := range capture {
			idx := captureCursor + i
			noise := noiseRng.next() * 20
			srcIdx := idx - delaySamples
			echoSample := float32(0)
			if srcIdx >= 0 && srcIdx < len(renderHistory) {
				echoSample = sc.attenuation * renderHistory[srcIdx]
			}
			sample := echoSample + noise
			if doubleTalk {
				sample += nearendRng.next() * 6000
			}
			if saturationBurst {
				if sample >= 0 {
					sample = 32767
				} else {
					sample = -32768
				}
			}
			capture[i] = sample
		}

		captureSaturation := saturationBurst
		gainChange := sc.gainChangeAt != 0 && iter == sc.gainChangeAt
		delayChange := aec3.DelayAdjustmentNone
		delayChangeInt := 0
		if sc.delayChangeAt != 0 && iter == sc.delayChangeAt {
			delayChange = aec3.DelayAdjustmentNewDetectedDelay
			delayChangeInt = 2
		}
		clockDrift := false

		var externalDelay *aec3.DelayEstimate
		ext := externalDelayArgs{}
		if sc.externalDelayAt != 0 && iter >= sc.externalDelayAt {
			d := aec3.NewDelayEstimate(aec3.DelayEstimateQualityRefined, sc.externalDelayValue)
			externalDelay = &d
			ext = externalDelayArgs{has: true, quality: 1, value: sc.externalDelayValue}
		}

		variability := aec3.NewEchoPathVariability(gainChange, delayChange, clockDrift)

		captureBlock := aec3.NewBlock(1, numCaptureChannels)
		copy(captureBlock.View(0, 0), capture)
		linearOutputBlock := aec3.NewBlock(1, numCaptureChannels)

		goRemover.ProcessCapture(variability, captureSaturation, externalDelay,
			goRenderBuffer.GetRenderBuffer(), linearOutputBlock, captureBlock)

		var goMetrics aec3.Metrics
		goRemover.GetMetrics(&goMetrics)

		result := cRemover.process(capture, captureSaturation, gainChange, delayChangeInt, clockDrift, ext)

		requireFloatSliceEqual(t, iter, "capture", captureBlock.View(0, 0), result.capture[:])
		requireFloatSliceEqual(t, iter, "linearOutput", linearOutputBlock.View(0, 0), result.linearOutput[:])
		requireEqualF64(t, iter, "EchoReturnLoss", goMetrics.EchoReturnLoss, result.erl)
		requireEqualF64(t, iter, "EchoReturnLossEnhancement", goMetrics.EchoReturnLossEnhancement, result.erle)

		out := captureBlock.View(0, 0)
		for _, v := range out {
			f := float64(v)
			if math.IsNaN(f) || math.IsInf(f, 0) {
				sawNonFinite = true
			} else if f < int16Min || f > int16Max {
				outOfBounds = true
			}
		}

		if sc.checkEchoReduction && iter >= convergedTailStart {
			// Measure reduction on the *linear* filter output (the
			// subtractor's residual, before the nonlinear suppressor's
			// gain and comfort-noise injection are applied) rather than
			// the final capture output: the final output's floor is
			// dominated by deliberately-injected comfort noise (masking
			// suppression artifacts), which is independent of how well
			// the echo itself was cancelled and swamps any RMS-ratio
			// signal once the real residual echo drops below it. The
			// linear output isolates the adaptive filter's own
			// cancellation performance.
			linOut := linearOutputBlock.View(0, 0)
			for i, v := range capture {
				micPower += float64(v) * float64(v)
				outputPower += float64(linOut[i]) * float64(linOut[i])
			}
			tailSamples += blockSize
		}
	}

	if sc.checkFiniteBounded {
		if sawNonFinite {
			t.Fatalf("scenario %s: encountered NaN/Inf in Go output", sc.name)
		}
		if outOfBounds {
			t.Fatalf("scenario %s: Go output exceeded int16 bounds", sc.name)
		}
	}

	if sc.checkEchoReduction {
		if tailSamples == 0 {
			t.Fatalf("scenario %s: no converged-tail samples collected", sc.name)
		}
		micRMS := math.Sqrt(micPower / float64(tailSamples))
		outRMS := math.Sqrt(outputPower / float64(tailSamples))
		if outRMS == 0 {
			t.Fatalf("scenario %s: output RMS is exactly zero, cannot compute reduction", sc.name)
		}
		reductionDB := 20 * math.Log10(micRMS/outRMS)
		if reductionDB < 30 {
			t.Fatalf("scenario %s: echo reduction %.2f dB below 30 dB threshold (micRMS=%.2f outRMS=%.2f)",
				sc.name, reductionDB, micRMS, outRMS)
		}
		t.Logf("scenario %s: echo reduction %.2f dB (micRMS=%.2f outRMS=%.2f)", sc.name, reductionDB, micRMS, outRMS)
	}
}
