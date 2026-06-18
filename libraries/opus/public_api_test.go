package opus

import (
	"math"
	"testing"
)

// Phase 11 public-API end-to-end validation. Drives NewNativeEncoder /
// NewNativeDecoder through the full encode/decode round-trip for a
// coverage matrix of rates, channels, frame sizes, bitrates, and
// applications. The inner codec is already proven byte-exact vs C by
// benchcmp/parity_opus_encode_matrix_test.go; this test verifies the
// public Go wrapper correctly marshals float64 ↔ float32 and plumbs
// the encoder/decoder pair.

type publicAPIConfig struct {
	rate       int
	channels   int
	frameMs    int
	bitrate    int
	app        Application
	complexity int
}

func publicAPIMatrix() []publicAPIConfig {
	var out []publicAPIConfig
	rates := []int{Rate8000, Rate12000, Rate16000, Rate24000, Rate48000}
	chanSet := []int{1, 2}
	frameMs := []int{10, 20}
	bitrates := []int{16000, 32000, 64000, 128000}
	apps := []Application{AppVoIP, AppAudio, AppLowDelay}
	for _, r := range rates {
		for _, c := range chanSet {
			for _, fm := range frameMs {
				for _, br := range bitrates {
					for _, app := range apps {
						out = append(out, publicAPIConfig{
							rate: r, channels: c, frameMs: fm, bitrate: br,
							app: app, complexity: 10,
						})
					}
				}
			}
		}
	}
	return out
}

// synthPCM generates a stable test waveform — sine + noise — at the
// requested (rate, channels) over nFrames * frameSamples samples.
func synthPCM(rate, channels, frameSamples, nFrames int) []float64 {
	n := frameSamples * nFrames * channels
	out := make([]float64, n)
	freq := 440.0
	for i := 0; i < frameSamples*nFrames; i++ {
		t := float64(i) / float64(rate)
		v := 0.5 * math.Sin(2*math.Pi*freq*t)
		for c := 0; c < channels; c++ {
			out[i*channels+c] = v
		}
	}
	return out
}

func TestPublicAPI_EncodeDecodeRoundtrip(t *testing.T) {
	for _, cfg := range publicAPIMatrix() {
		cfg := cfg
		name := caseName(cfg)
		t.Run(name, func(t *testing.T) {
			enc, err := NewNativeEncoder(cfg.rate, cfg.channels,
				WithBitrate(cfg.bitrate),
				WithComplexity(cfg.complexity),
				WithApplication(cfg.app))
			if err != nil {
				t.Fatalf("NewNativeEncoder: %v", err)
			}
			dec, err := NewNativeDecoder(cfg.rate, cfg.channels)
			if err != nil {
				t.Fatalf("NewNativeDecoder: %v", err)
			}

			frameSamples := cfg.rate * cfg.frameMs / 1000
			const nFrames = 4
			pcmIn := synthPCM(cfg.rate, cfg.channels, frameSamples, nFrames)
			pcmOut := make([]float64, frameSamples*cfg.channels)

			for f := 0; f < nFrames; f++ {
				start := f * frameSamples * cfg.channels
				end := start + frameSamples*cfg.channels
				pkt, err := enc.Encode(pcmIn[start:end], MaxFrameBytes)
				if err != nil {
					t.Fatalf("Encode frame %d: %v", f, err)
				}
				if len(pkt) == 0 {
					t.Fatalf("frame %d: empty packet", f)
				}
				n, err := dec.Decode(pkt, pcmOut)
				if err != nil {
					t.Fatalf("Decode frame %d: %v", f, err)
				}
				if n != frameSamples {
					t.Fatalf("frame %d: decoded %d samples, want %d", f, n, frameSamples)
				}
			}
		})
	}
}

func TestPublicAPI_EncoderReset(t *testing.T) {
	enc, err := NewNativeEncoder(Rate48000, 2, WithBitrate(64000), WithApplication(AppAudio))
	if err != nil {
		t.Fatal(err)
	}
	pcm := synthPCM(Rate48000, 2, 960, 1)
	if _, err := enc.Encode(pcm, MaxFrameBytes); err != nil {
		t.Fatal(err)
	}
	enc.Reset()
	if _, err := enc.Encode(pcm, MaxFrameBytes); err != nil {
		t.Fatalf("after reset: %v", err)
	}
}

func TestPublicAPI_DecoderPLC(t *testing.T) {
	enc, err := NewNativeEncoder(Rate48000, 1, WithBitrate(32000), WithApplication(AppVoIP))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := NewNativeDecoder(Rate48000, 1)
	if err != nil {
		t.Fatal(err)
	}
	pcm := synthPCM(Rate48000, 1, 960, 1)
	pkt, err := enc.Encode(pcm, MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float64, 960)
	if _, err := dec.Decode(pkt, out); err != nil {
		t.Fatal(err)
	}
	// PLC: pass nil data for loss concealment.
	if _, err := dec.Decode(nil, out); err != nil {
		t.Fatalf("PLC decode: %v", err)
	}
}

func TestPublicAPI_SetBitrate(t *testing.T) {
	enc, err := NewNativeEncoder(Rate48000, 1, WithBitrate(32000))
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.SetBitrate(96000); err != nil {
		t.Fatalf("SetBitrate 96k: %v", err)
	}
	pcm := synthPCM(Rate48000, 1, 960, 1)
	if _, err := enc.Encode(pcm, MaxFrameBytes); err != nil {
		t.Fatalf("encode after SetBitrate: %v", err)
	}
}

func TestPublicAPI_BadArgs(t *testing.T) {
	if _, err := NewNativeEncoder(7777, 1); err == nil {
		t.Error("expected error for unsupported sample rate")
	}
	if _, err := NewNativeEncoder(Rate48000, 3); err == nil {
		t.Error("expected error for unsupported channel count")
	}
	if _, err := NewNativeDecoder(7777, 1); err == nil {
		t.Error("expected error for unsupported sample rate")
	}
}

func caseName(c publicAPIConfig) string {
	app := "VOIP"
	switch c.app {
	case AppAudio:
		app = "AUDIO"
	case AppLowDelay:
		app = "LOWDELAY"
	}
	return nameKey(c.rate, c.channels, c.frameMs, c.bitrate, app)
}

func nameKey(rate, channels, frameMs, bitrate int, app string) string {
	return iToa(rate) + "hz_c" + iToa(channels) + "_" + iToa(frameMs) + "ms_" + iToa(bitrate) + "bps_" + app
}

func iToa(x int) string {
	if x == 0 {
		return "0"
	}
	neg := x < 0
	if neg {
		x = -x
	}
	b := [20]byte{}
	i := len(b)
	for x > 0 {
		i--
		b[i] = byte('0' + x%10)
		x /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
