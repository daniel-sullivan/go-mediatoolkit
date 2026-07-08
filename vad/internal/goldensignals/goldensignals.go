// Package goldensignals is the deterministic 16 kHz test-signal set
// shared by the Silero parity machinery: the opt-in onnxruntime
// oracle slice (vad/internal/parity_tests/silero_ort), the golden
// generator (silero_ort/gen), and the default-CI golden test
// (vad/silero_golden_test.go). All three MUST see bit-identical
// samples, so everything here is fixed-point deterministic — a
// self-contained PRNG (no math/rand, whose sequences are not part of
// this package's contract) and closed-form synthesis with
// float64 phase accumulation rounded to float32 once per sample.
//
// The set covers the classes the plan's Silero verification calls
// for: seeded noise across an amplitude ladder, synthetic speech-like
// pulse trains, SNR mixtures of the two, digital silence, and hard
// square-wave edges (a reflect-pad/STFT stress). Each signal is 60 s
// — 1875 exact 512-sample windows — for ≥ 60 s of coverage per class.
package goldensignals

import "math"

// SampleRate is the engine rate all signals are generated at.
const SampleRate = 16000

// WindowSize is the Silero decision window the consumers cut the
// signals into. Every signal length is an exact multiple.
const WindowSize = 512

// signalSeconds is the duration of each class signal.
const signalSeconds = 60

// Signal is one named deterministic test signal.
type Signal struct {
	Name    string
	Samples []float32
}

// Signals generates the full ordered signal set. The result is freshly
// allocated on every call; ~15 MB total.
func Signals() []Signal {
	return []Signal{
		{Name: "noise-amp-ladder", Samples: noiseAmpLadder()},
		{Name: "speech-pulses", Samples: speechPulses()},
		{Name: "snr-mixture", Samples: snrMixture()},
		{Name: "silence", Samples: make([]float32, signalSeconds*SampleRate)},
		{Name: "square-edges", Samples: squareEdges()},
	}
}

// prng is splitmix64 — tiny, seedable, and stable by construction.
type prng struct{ state uint64 }

// next returns the next raw 64-bit value.
func (p *prng) next() uint64 {
	p.state += 0x9E3779B97F4A7C15
	z := p.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// uniform returns a float64 in [-1, 1).
func (p *prng) uniform() float64 {
	return float64(int64(p.next())) / float64(1<<63)
}

// noiseAmpLadder is seeded white noise whose amplitude steps every
// 10 s through {0.01, 0.03, 0.1, 0.3, 0.6, 1.0}.
func noiseAmpLadder() []float32 {
	amps := []float64{0.01, 0.03, 0.1, 0.3, 0.6, 1.0}
	rng := prng{state: 0x5EED_0001}
	out := make([]float32, signalSeconds*SampleRate)
	seg := len(out) / len(amps)
	for i := range out {
		out[i] = float32(amps[i/seg] * rng.uniform())
	}
	return out
}

// speechGen synthesises the speech-like pulse train one sample at a
// time: 1.5 s utterances separated by 0.7 s pauses, built from 250 ms
// syllables — a 50 ms noise burst (consonant) followed by a voiced
// nucleus whose formants sweep across the syllable (diphthong-like),
// over a declining, slightly vibrato'd F0. The temporal spectral
// dynamics matter: the Silero model scores stationary harmonic tones
// close to zero, so a convincing test signal has to MOVE the way
// speech does.
type speechGen struct {
	rng   prng
	phase float64 // accumulated fundamental phase (radians)
	n     int     // sample index
}

// next returns the following sample.
func (g *speechGen) next() float64 {
	t := float64(g.n) / SampleRate
	g.n++

	// Utterance gait: 1.5 s speech, 0.7 s pause.
	ut := math.Mod(t, 2.2)
	if ut >= 1.5 {
		return 0
	}

	// F0: per-utterance declination 130→105 Hz with 5 Hz vibrato.
	f0 := 130 - 25*(ut/1.5) + 6*math.Sin(2*math.Pi*5*t)
	g.phase += 2 * math.Pi * f0 / SampleRate

	// 250 ms syllables: 50 ms consonant burst + 200 ms nucleus.
	syl := math.Mod(ut, 0.25)
	sylIdx := int(ut / 0.25)
	if syl < 0.05 {
		env := math.Sin(math.Pi * syl / 0.05)
		return 0.15 * env * g.rng.uniform()
	}

	// Voiced nucleus: harmonic series under two sweeping formants
	// (direction alternates per syllable).
	fr := (syl - 0.05) / 0.20
	dir := 1.0
	if sylIdx%2 == 1 {
		dir = -1
	}
	f1 := 400 + dir*250*fr
	f2 := 1800 - dir*700*fr
	v := 0.0
	for k := 1; k <= 14; k++ {
		fk := float64(k) * f0
		w := math.Exp(-(fk-f1)*(fk-f1)/(2*150*150)) +
			0.7*math.Exp(-(fk-f2)*(fk-f2)/(2*250*250))
		v += w * math.Sin(float64(k)*g.phase)
	}

	// Fast onset / slower release envelope per nucleus, light AM. The
	// tanh soft limiter bounds worst-case harmonic pile-ups at 0.95
	// (TestSignalGeometry pins the nominal [-1, 1] range) while adding
	// a touch of speech-plausible waveform compression.
	env := math.Min(1, math.Min(fr*8, (1-fr)*4+0.2))
	am := 0.85 + 0.15*math.Sin(2*math.Pi*3.3*t)
	return 0.95 * math.Tanh(0.25*env*am*v)
}

// speechPulses is the 60 s speech-like pulse train.
func speechPulses() []float32 {
	out := make([]float32, signalSeconds*SampleRate)
	gen := speechGen{rng: prng{state: 0x5EED_0003}}
	for i := range out {
		out[i] = float32(gen.next())
	}
	return out
}

// snrMixture is the speech-like pulse train over seeded noise whose
// level steps every 10 s through SNR ≈ {20, 10, 5, 0, −5, −10} dB
// relative to the pulse train's nominal level.
func snrMixture() []float32 {
	snrDB := []float64{20, 10, 5, 0, -5, -10}
	// Nominal pulse-train RMS over the active portions at these
	// settings — good enough for an SNR ladder; determinism is what
	// matters, not calibration. The 0.65 output scale keeps the
	// worst-case −10 dB segment inside the toolkit's nominal [-1, 1]
	// without changing the mixture's SNR.
	const speechRMS = 0.08
	gen := speechGen{rng: prng{state: 0x5EED_0003}}
	rng := prng{state: 0x5EED_0002}
	out := make([]float32, signalSeconds*SampleRate)
	seg := len(out) / len(snrDB)
	for i := range out {
		s := gen.next()

		// Uniform noise in [-a, a) has RMS a/sqrt(3).
		snr := math.Pow(10, snrDB[i/seg]/20)
		a := speechRMS / snr * math.Sqrt(3)
		out[i] = float32(0.65 * (s + a*rng.uniform()))
	}
	return out
}

// squareEdges alternates hard-edged events that stress the reflect
// pad and STFT: 0.4 s of a 250 Hz square wave at 0.8, 0.35 s of
// silence, and every fourth cycle a ±0.5 DC step held for 0.2 s.
func squareEdges() []float32 {
	out := make([]float32, signalSeconds*SampleRate)
	const cycle = 0.4 + 0.35 + 0.2
	for i := range out {
		t := float64(i) / SampleRate
		c := math.Mod(t, cycle)
		n := int(t / cycle)
		switch {
		case c < 0.4:
			if math.Mod(t*250, 1) < 0.5 {
				out[i] = 0.8
			} else {
				out[i] = -0.8
			}
		case c < 0.75:
			out[i] = 0
		case n%4 == 0:
			out[i] = 0.5
		case n%4 == 2:
			out[i] = -0.5
		}
	}
	return out
}
