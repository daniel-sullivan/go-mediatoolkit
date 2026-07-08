package loudness

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────
// Signal-synthesis helpers for the EBU Tech 3341/3342 compliance vectors.
//
// All parameters here are transcribed from the official EBU Tech 3341 v4 and
// Tech 3342 v4 (Geneva, Nov 2023) PDFs — see the per-case comments in
// tech3341_test.go / tech3342_test.go. These helpers are test-only and
// file-local; they exist purely to build the exact synthesizable waveforms
// the standards define.
//
// LEVEL CONVENTION (Tech 3341 Table 1, case 1 — "per-channel peak level"):
// EBU sine-wave test levels are PEAK sample levels in dBFS, NOT RMS. A sine
// at X dBFS peak has linear peak amplitude 10^(X/20) (e.g. -23 dBFS peak →
// amplitude ≈ 0.0708). This convention is stated once (case 1) and carried
// forward to every "As #1 …" case. sinePeakAmplitude encodes it.
// ─────────────────────────────────────────────────────────────────────────

// synthRate is the synthesis sample rate for every compliance vector. The
// official EBU pre-synthesized test signals are published at 48 kHz, so all
// cases here are built at 48 kHz to match the documents' fs references
// (e.g. "fs/4 denotes 12 kHz for a sample-rate of 48 kHz").
const synthRate = 48000

// sinePeakAmplitude converts an EBU peak level in dBFS to a linear peak
// sample amplitude, per the Tech 3341 case-1 "per-channel peak level"
// convention: amplitude = 10^(dBFS/20). -23 dBFS → ≈0.0708, 0 dBFS → 1.0.
func sinePeakAmplitude(dbfs float64) float64 { return math.Pow(10, dbfs/20) }

// framesFor returns the whole number of sample frames in `seconds` at
// synthRate, rounding to nearest (durations like 20.1 s or 1.34 s land on a
// clean frame count).
func framesFor(seconds float64) int { return int(math.Round(seconds * synthRate)) }

// seg is one concatenation segment of an equal-level stereo signal: a
// freq-Hz sine at linear peak amplitude `amp` (amp == 0 → digital silence)
// for `seconds`. Phase is continuous across segments (only the amplitude
// steps at boundaries), matching how the EBU tone sequences are constructed.
type seg struct {
	seconds float64
	amp     float64
	freq    float64
}

// tone builds an equal-level stereo segment: both channels a freq-Hz sine at
// peak level dbfs, for the given duration.
func tone(seconds, freq, dbfs float64) seg {
	return seg{seconds: seconds, amp: sinePeakAmplitude(dbfs), freq: freq}
}

// silence builds a stereo digital-silence segment of the given duration. The
// freq is irrelevant (amplitude 0) but kept at 1000 so the continuous-phase
// counter advances consistently.
func silence(seconds float64) seg {
	return seg{seconds: seconds, amp: 0, freq: 1000}
}

// buildStereoSine concatenates segs into one interleaved stereo buffer (both
// channels identical, in phase). The sine phase is continuous across segment
// boundaries: sample g of the whole stream is amp·sin(2π·freq·g/fs), so an
// amplitude change between segments does not introduce a phase discontinuity
// (a click), which would otherwise create spurious inter-sample peaks.
func buildStereoSine(segs []seg) []float64 {
	total := 0
	for _, s := range segs {
		total += framesFor(s.seconds)
	}
	out := make([]float64, total*2)
	g := 0 // global sample index, for continuous phase
	w := 0 // write cursor (samples)
	for _, s := range segs {
		n := framesFor(s.seconds)
		for k := 0; k < n; k++ {
			v := 0.0
			if s.amp != 0 {
				v = s.amp * math.Sin(2*math.Pi*s.freq*float64(g)/synthRate)
			}
			out[w] = v
			out[w+1] = v
			w += 2
			g++
		}
	}
	return out
}

// buildMultiChannelSine builds a single-segment interleaved N-channel signal
// where channel c carries a freq-Hz sine at linear peak amplitude amps[c],
// all in phase. Used for the 5.0 case (Tech 3341 case 6), where each speaker
// position has its own level.
func buildMultiChannelSine(freq, seconds float64, amps []float64) []float64 {
	ch := len(amps)
	n := framesFor(seconds)
	out := make([]float64, n*ch)
	for k := 0; k < n; k++ {
		s := math.Sin(2 * math.Pi * freq * float64(k) / synthRate)
		for c := 0; c < ch; c++ {
			out[k*ch+c] = amps[c] * s
		}
	}
	return out
}

// buildTruePeakTone builds a stereo true-peak test tone (Tech 3341 cases
// 15–19): both channels a freq-Hz sine at linear peak amplitude `amp`
// (a fraction of full scale — may exceed 1.0 for case 19) and phase
// `phaseDeg` degrees, tapered with a linear fade-in and fade-out of
// `fadeSeconds` each. The fade suppresses the onset/offset step that would
// otherwise generate broadband energy and a spurious inter-sample peak.
func buildTruePeakTone(freq, amp, phaseDeg, seconds, fadeSeconds float64) []float64 {
	n := framesFor(seconds)
	fade := framesFor(fadeSeconds)
	if fade*2 > n {
		fade = n / 2
	}
	phase := phaseDeg * math.Pi / 180
	out := make([]float64, n*2)
	for k := 0; k < n; k++ {
		g := 1.0
		if k < fade {
			g = float64(k) / float64(fade) // linear fade-in
		} else if k >= n-fade {
			g = float64(n-1-k) / float64(fade) // linear fade-out
		}
		v := g * amp * math.Sin(2*math.Pi*freq*float64(k)/synthRate+phase)
		out[k*2] = v
		out[k*2+1] = v
	}
	return out
}

// testSine builds an interleaved buffer of `frames` frames at freqHz,
// sampled at rate, with `channels` interleaved channels, every channel
// carrying the same sine at linear peak amplitude amp. startFrame offsets
// the phase (in samples) so successive chunks of one continuous waveform
// can be generated by advancing startFrame — pass 0 for a fresh tone. It
// is the single shared sine generator for the loudness package tests
// (previously duplicated as genSine / measureTestSine / monitorTestChunk).
func testSine(amp, freqHz float64, rate, startFrame, frames, channels int) []float64 {
	data := make([]float64, frames*channels)
	for n := 0; n < frames; n++ {
		v := amp * math.Sin(2*math.Pi*freqHz*float64(startFrame+n)/float64(rate))
		for c := 0; c < channels; c++ {
			data[n*channels+c] = v
		}
	}
	return data
}

// feedAndSampleMax feeds sig (interleaved, `channels` channels) to the meter
// in chunkFrames-frame blocks, calling read after every block. It returns the
// maximum finite value read and the full sequence of finite readings (in feed
// order). This is the Tech 3341 "max reading" / meter-update cadence: the
// meter is polled at fixed intervals (100 ms blocks here) and the reported
// figure is the maximum over the run.
func feedAndSampleMax(t *testing.T, m *Meter, sig []float64, channels, chunkFrames int, read func() (float64, error)) (max float64, readings []float64) {
	t.Helper()
	max = math.Inf(-1)
	step := chunkFrames * channels
	for off := 0; off < len(sig); off += step {
		end := off + step
		if end > len(sig) {
			end = len(sig)
		}
		require.NoError(t, m.AddFrames(sig[off:end]))
		v, err := read()
		require.NoError(t, err)
		if math.IsInf(v, 0) {
			continue
		}
		readings = append(readings, v)
		if v > max {
			max = v
		}
	}
	return max, readings
}
