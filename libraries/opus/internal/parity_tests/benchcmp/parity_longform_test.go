//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// Thin Go encoder/decoder wrappers for this file — call libopus
// directly so the longform test doesn't depend on the parent
// libraries/opus package cycle.
type goLongformEnc struct {
	st *nativeopus.OpusEncoder
}

func newGoEncoderForLongform(t *testing.T, cfg longformConfig) *goLongformEnc {
	t.Helper()
	st, code := nativeopus.NewEncoder(int32(longformFs), cfg.channels, goAppFromC(cfg.app))
	if code != nativeopus.ErrorOK {
		t.Fatalf("Go encoder init: %d", code)
	}
	if c := nativeopus.EncoderCtl(st, nativeopus.CtlSetBitrate, int32(cfg.bitrate)); c != nativeopus.ErrorOK {
		t.Fatalf("Go SET_BITRATE: %d", c)
	}
	if c := nativeopus.EncoderCtl(st, nativeopus.CtlSetComplexity, int32(10)); c != nativeopus.ErrorOK {
		t.Fatalf("Go SET_COMPLEXITY: %d", c)
	}
	return &goLongformEnc{st: st}
}

func (e *goLongformEnc) encode(pcm []float32, frameSize int, pkt []byte) int {
	return nativeopus.EncodeFloat(e.st, pcm, frameSize, pkt)
}
func (e *goLongformEnc) destroy() { nativeopus.DestroyEncoder(e.st) }

type goLongformDec struct {
	st       *nativeopus.OpusDecoder
	channels int
}

func newGoDecoderForLongform(t *testing.T, channels int) *goLongformDec {
	t.Helper()
	st, code := nativeopus.NewDecoder(int32(longformFs), channels)
	if code != nativeopus.ErrorOK {
		t.Fatalf("Go decoder init: %d", code)
	}
	return &goLongformDec{st: st, channels: channels}
}

func (d *goLongformDec) decode(pkt []byte, outLen int, pcm []float32) int {
	return nativeopus.DecodeFloat(d.st, pkt, pcm, outLen/d.channels, 0)
}
func (d *goLongformDec) destroy() { nativeopus.DestroyDecoder(d.st) }

func goAppFromC(cApp int) int {
	switch cApp {
	case AppVOIP:
		return nativeopus.ApplicationVoIP
	case AppRestrictedLowDel:
		return nativeopus.ApplicationRestrictedLowdelay
	default:
		return nativeopus.ApplicationAudio
	}
}

// Longer-duration parity: encode 5 s of synthetic audio frame-by-frame
// through both C and Go, asserting byte-exact bitstream per frame and
// byte-exact PCM on the C→Go and Go→C roundtrips. These are the
// "did we hit every corner" sanity checks — previous matrices drive 4
// frames per config, which is not enough to exercise state evolution
// across seconds of signal.

// pinkNoiseGen is Paul Kellet's economy pink-noise filter (7-tap).
type pinkNoiseGen struct {
	b0, b1, b2, b3, b4, b5, b6 float64
	r                          *rand.Rand
}

func newPinkGen(seed int64) *pinkNoiseGen {
	return &pinkNoiseGen{r: rand.New(rand.NewSource(seed))}
}

func (p *pinkNoiseGen) next() float32 {
	white := p.r.Float64()*2 - 1
	p.b0 = 0.99886*p.b0 + white*0.0555179
	p.b1 = 0.99332*p.b1 + white*0.0750759
	p.b2 = 0.96900*p.b2 + white*0.1538520
	p.b3 = 0.86650*p.b3 + white*0.3104856
	p.b4 = 0.55000*p.b4 + white*0.5329522
	p.b5 = -0.7616*p.b5 - white*0.0168980
	pink := p.b0 + p.b1 + p.b2 + p.b3 + p.b4 + p.b5 + p.b6 + white*0.5362
	p.b6 = white * 0.115926
	// Empirical scale so peak ≲ 0.5.
	return float32(pink * 0.11)
}

// sineSweep produces a logarithmic sweep from 80 Hz to 8 kHz.
func sineSweep(n int, Fs float64, seed int64) []float32 {
	r := rand.New(rand.NewSource(seed))
	out := make([]float32, n)
	phase := 0.0
	startHz := 80.0
	endHz := 8000.0
	durSec := float64(n) / Fs
	for i := 0; i < n; i++ {
		t := float64(i) / Fs
		// Log sweep.
		freq := startHz * math.Pow(endHz/startHz, t/durSec)
		phase += 2 * math.Pi * freq / Fs
		// Add a bit of noise so the encoder doesn't always pick the
		// same code path.
		out[i] = float32(0.35*math.Sin(phase) + 0.02*(r.Float64()*2-1))
	}
	return out
}

// genWaveform interleaves the chosen waveform into channels frames.
func genWaveform(kind string, samplesPerCh, channels int, Fs float64, seed int64) []float32 {
	out := make([]float32, samplesPerCh*channels)
	switch kind {
	case "pink":
		for c := 0; c < channels; c++ {
			g := newPinkGen(seed + int64(c)*1000)
			for i := 0; i < samplesPerCh; i++ {
				out[i*channels+c] = g.next()
			}
		}
	case "sweep":
		// Per-channel sweep with slight phase offset.
		for c := 0; c < channels; c++ {
			sw := sineSweep(samplesPerCh, Fs, seed+int64(c)*1000)
			for i := 0; i < samplesPerCh; i++ {
				out[i*channels+c] = sw[i]
			}
		}
	case "mixed":
		// Pink + sweep. Exercises both tonal and noise CELT paths.
		r := rand.New(rand.NewSource(seed))
		for c := 0; c < channels; c++ {
			g := newPinkGen(seed + int64(c)*1000 + 7)
			sw := sineSweep(samplesPerCh, Fs, seed+int64(c)*2000)
			for i := 0; i < samplesPerCh; i++ {
				// Slow amplitude modulation to move between modes.
				env := 0.5 + 0.5*math.Sin(2*math.Pi*float64(i)/float64(samplesPerCh))
				s := float32(env)*sw[i] + 0.3*g.next() + float32(r.Float64()*0.005-0.0025)
				out[i*channels+c] = s
			}
		}
	}
	return out
}

type longformConfig struct {
	name     string
	kind     string
	channels int
	bitrate  int32
	app      int
}

func longformConfigs() []longformConfig {
	return []longformConfig{
		{"pink_mono_16k_VOIP", "pink", 1, 16000, AppVOIP},
		{"pink_mono_32k_AUDIO", "pink", 1, 32000, AppAudio},
		{"pink_mono_64k_AUDIO", "pink", 1, 64000, AppAudio},
		{"pink_mono_128k_LOWDELAY", "pink", 1, 128000, AppRestrictedLowDel},
		{"pink_stereo_32k_VOIP", "pink", 2, 32000, AppVOIP},
		{"pink_stereo_64k_AUDIO", "pink", 2, 64000, AppAudio},
		{"pink_stereo_128k_AUDIO", "pink", 2, 128000, AppAudio},
		{"pink_stereo_192k_LOWDELAY", "pink", 2, 192000, AppRestrictedLowDel},
		{"sweep_mono_48k_AUDIO", "sweep", 1, 48000, AppAudio},
		{"sweep_stereo_96k_AUDIO", "sweep", 2, 96000, AppAudio},
		{"mixed_mono_48k_VOIP", "mixed", 1, 48000, AppVOIP},
		{"mixed_stereo_96k_AUDIO", "mixed", 2, 96000, AppAudio},
	}
}

const longformFs = 48000
const longformFrameSamples = 960 // 20 ms @ 48 kHz
const longformDurationSec = 5

func TestParity_Longform_Encode(t *testing.T) {
	for _, cfg := range longformConfigs() {
		cfg := cfg
		t.Run(cfg.name, func(t *testing.T) {
			runLongformEncode(t, cfg)
		})
	}
}

func runLongformEncode(t *testing.T, cfg longformConfig) {
	nFrames := longformDurationSec * longformFs / longformFrameSamples
	totalSamplesPerCh := nFrames * longformFrameSamples

	pcm := genWaveform(cfg.kind, totalSamplesPerCh, cfg.channels, float64(longformFs), int64(20260420))

	cEnc := NewCEncoder(longformFs, cfg.channels, cfg.app)
	if cEnc == nil {
		t.Fatalf("C encoder create failed")
	}
	defer cEnc.Destroy()
	cEnc.SetBitrate(int(cfg.bitrate))
	cEnc.SetComplexity(10)

	gEnc := newGoEncoderForLongform(t, cfg)
	defer gEnc.destroy()

	pkt := make([]byte, 8000*cfg.channels)
	gPkt := make([]byte, 8000*cfg.channels)

	for f := 0; f < nFrames; f++ {
		start := f * longformFrameSamples * cfg.channels
		end := start + longformFrameSamples*cfg.channels
		frame := pcm[start:end]

		cN := cEnc.EncodeFrame(frame, longformFrameSamples, pkt)
		gN := gEnc.encode(frame, longformFrameSamples, gPkt)
		if cN <= 0 {
			t.Fatalf("frame %d: C encode returned %d", f, cN)
		}
		if cN != gN {
			t.Fatalf("frame %d: byte-count mismatch C=%d Go=%d", f, cN, gN)
		}
		for i := 0; i < cN; i++ {
			if pkt[i] != gPkt[i] {
				t.Fatalf("frame %d: byte %d C=%#02x Go=%#02x", f, i, pkt[i], gPkt[i])
			}
		}
	}
	t.Logf("encoded %d frames (%d s), all byte-exact", nFrames, longformDurationSec)
}

func TestParity_Longform_Roundtrip(t *testing.T) {
	// Encode via C, decode via Go and C, assert Go and C decode produce
	// bit-exact PCM. Then encode via Go, decode via both, verify again.
	// This covers decoder state evolution across 5 s of signal.
	cfgs := longformConfigs()
	// Run a subset for speed — the encoder matrix above already covers
	// all configs byte-exact, so roundtrip is additional decoder soak.
	for _, cfg := range cfgs[:6] {
		cfg := cfg
		t.Run(cfg.name+"/C_enc", func(t *testing.T) {
			runLongformDecodeParity(t, cfg /*encWithC=*/, true)
		})
		t.Run(cfg.name+"/Go_enc", func(t *testing.T) {
			runLongformDecodeParity(t, cfg /*encWithC=*/, false)
		})
	}
}

func runLongformDecodeParity(t *testing.T, cfg longformConfig, encWithC bool) {
	nFrames := longformDurationSec * longformFs / longformFrameSamples
	totalSamplesPerCh := nFrames * longformFrameSamples

	pcm := genWaveform(cfg.kind, totalSamplesPerCh, cfg.channels, float64(longformFs), int64(20260420))

	// Encoder.
	var cEnc *CEncoder
	var gEnc *goLongformEnc
	if encWithC {
		cEnc = NewCEncoder(longformFs, cfg.channels, cfg.app)
		if cEnc == nil {
			t.Fatalf("C encoder create failed")
		}
		defer cEnc.Destroy()
		cEnc.SetBitrate(int(cfg.bitrate))
		cEnc.SetComplexity(10)
	} else {
		gEnc = newGoEncoderForLongform(t, cfg)
		defer gEnc.destroy()
	}

	// Decoders.
	cDec := NewCDecoder(longformFs, cfg.channels)
	if cDec == nil {
		t.Fatalf("C decoder create failed")
	}
	defer cDec.Destroy()
	gDec := newGoDecoderForLongform(t, cfg.channels)
	defer gDec.destroy()

	pkt := make([]byte, 8000*cfg.channels)
	cOut := make([]float32, longformFrameSamples*cfg.channels)
	gOut := make([]float32, longformFrameSamples*cfg.channels)

	for f := 0; f < nFrames; f++ {
		start := f * longformFrameSamples * cfg.channels
		end := start + longformFrameSamples*cfg.channels
		frame := pcm[start:end]

		var n int
		if encWithC {
			n = cEnc.EncodeFrame(frame, longformFrameSamples, pkt)
		} else {
			n = gEnc.encode(frame, longformFrameSamples, pkt)
		}
		if n <= 0 {
			t.Fatalf("frame %d: encode returned %d", f, n)
		}

		cN := cDec.DecodeFrame(pkt[:n], cOut, longformFrameSamples)
		gN := gDec.decode(pkt[:n], cfg.channels*longformFrameSamples, gOut)
		if cN != gN {
			t.Fatalf("frame %d: decode sample count C=%d Go=%d", f, cN, gN)
		}
		for i := 0; i < cN*cfg.channels; i++ {
			if cOut[i] != gOut[i] {
				sample := i / cfg.channels
				chn := i % cfg.channels
				t.Fatalf("frame %d: PCM mismatch sample=%d ch=%d C=%g Go=%g",
					f, sample, chn, cOut[i], gOut[i])
			}
		}
	}
	t.Logf("decoded %d frames (%d s), all bit-exact", nFrames, longformDurationSec)
}
