package mp3

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mp3lib "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// generateSine builds interleaved float64 samples in [-0.8, 0.8] at freq Hz.
// Each channel is phase-offset so multichannel content is not a duplicate.
func generateSine(sampleRate, channels, samplesPerChannel int, freq float64) []float64 {
	out := make([]float64, samplesPerChannel*channels)
	for i := 0; i < samplesPerChannel; i++ {
		for ch := 0; ch < channels; ch++ {
			phase := 2*math.Pi*freq*float64(i)/float64(sampleRate) + float64(ch)*0.3
			out[i*channels+ch] = 0.8 * math.Sin(phase)
		}
	}
	return out
}

// nativeBackendUnimplemented reports whether a full encode→decode round-trip
// can be exercised in this build. The decoder is wired in every build (cgo
// minimp3 or the pure-Go port), but the encoder is LAME-derived and only
// available under the mp3lame build tag: without it NewEncoder returns
// ErrEncoderRequiresLAME, so there is no encoder to produce a stream and the
// round-trip assertions are skipped. With mp3lame the encoder is present (cgo
// libmp3lame or the pure-Go LAME port); the pure-Go port encodes CBR/ABR but
// not yet VBR, which the per-case encode helper handles by skipping. Decode-only
// paths remain fully exercised elsewhere.
func nativeBackendUnimplemented(t *testing.T) bool {
	t.Helper()
	enc, err := NewEncoder(&bytes.Buffer{}, 44100, 2)
	if errors.Is(err, mp3lib.ErrEncoderRequiresLAME) || errors.Is(err, mp3lib.ErrNotImplemented) {
		return true
	}
	require.NoError(t, err)
	if _, err := enc.Write(mutations.Audio{Data: make([]float64, 1152*2), SampleRate: 44100, Channels: 2}); errors.Is(err, mp3lib.ErrNotImplemented) {
		_ = enc.Close()
		return true
	}
	_ = enc.Close()
	return false
}

// requireEncoder skips the calling test when the MP3 encoder is not compiled
// into this build. The encoder is LAME-derived and only available under
// -tags=mp3lame; otherwise NewEncoder returns [mp3lib.ErrEncoderRequiresLAME].
// (A non-VBR construction never returns [mp3lib.ErrNotImplemented]; that sentinel
// is reserved for the pure-Go port's not-yet-ported VBR path.) Decoder and
// argument-validation tests do not call this and keep running in every build.
func requireEncoder(t *testing.T) {
	t.Helper()
	enc, err := NewEncoder(&bytes.Buffer{}, 44100, 2)
	if errors.Is(err, mp3lib.ErrEncoderRequiresLAME) || errors.Is(err, mp3lib.ErrNotImplemented) {
		t.Skip("requires -tags mp3lame (LGPL encoder)")
	}
	require.NoError(t, err)
	_ = enc.Close()
}

// encode runs input through the streaming MP3 encoder and returns the encoded
// byte stream.
func encode(t *testing.T, sampleRate, channels int, opts []EncoderOption, input []float64) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, sampleRate, channels, opts...)
	require.NoError(t, err)
	require.Equal(t, channels, enc.Channels())
	require.Equal(t, sampleRate, enc.SampleRate())

	// Drive Write in several slices to exercise cross-call buffering.
	const slice = 999 // intentionally not a channel multiple of the whole input
	for off := 0; off < len(input); {
		end := off + (slice/channels)*channels
		if end > len(input) {
			end = len(input)
		}
		n, err := enc.Write(mutations.Audio{Data: input[off:end], SampleRate: sampleRate, Channels: channels})
		if errors.Is(err, mp3lib.ErrNotImplemented) {
			// The pure-Go LAME port (CGO_ENABLED=0, -tags mp3lame) does not yet
			// implement the VBR iteration loops, so a VBR encode surfaces
			// ErrNotImplemented by design. The cgo libmp3lame backend handles VBR
			// and never hits this. Skip the case rather than fail on a documented,
			// backend-specific gap.
			_ = enc.Close()
			t.Skip("VBR not supported by the active encoder backend (pure-Go LAME port)")
		}
		require.NoError(t, err)
		require.Equal(t, end-off, n)
		off = end
	}
	require.NoError(t, enc.Close())
	return buf.Bytes()
}

// decodeAll decodes the entire MP3 stream with a small read buffer to exercise
// the decoder's leftover-sample carry across Read calls.
func decodeAll(t *testing.T, stream []byte) (out []float64, sampleRate, channels int) {
	t.Helper()
	dec, err := NewDecoder(bytes.NewReader(stream))
	require.NoError(t, err)

	tmp := make([]float64, 70) // far smaller than one MP3 frame
	for {
		audio, err := dec.Read(tmp)
		out = append(out, audio.Data...)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	return out, dec.SampleRate(), dec.Channels()
}

// bestCorrelation finds the normalized cross-correlation between in and out at
// the lag (within maxLag) that maximizes it. MP3 introduces encoder/decoder
// delay and is lossy, so a round-trip is matched by waveform correlation rather
// than per-sample equality.
func bestCorrelation(in, out []float64, maxLag int) float64 {
	best := -2.0
	for lag := 0; lag <= maxLag; lag++ {
		var dot, na, nb float64
		n := len(in)
		if n > len(out)-lag {
			n = len(out) - lag
		}
		if n <= 0 {
			break
		}
		for i := 0; i < n; i++ {
			a := in[i]
			b := out[i+lag]
			dot += a * b
			na += a * a
			nb += b * b
		}
		if na == 0 || nb == 0 {
			continue
		}
		c := dot / math.Sqrt(na*nb)
		if c > best {
			best = c
		}
	}
	return best
}

func TestRoundTrip(t *testing.T) {
	if nativeBackendUnimplemented(t) {
		t.Skip("libraries/mp3 native backend not implemented (CGO_ENABLED=0); round-trip requires the cgo minimp3/LAME backend")
	}

	tests := []struct {
		name       string
		sampleRate int
		channels   int
		freq       float64
		samples    int
		opts       []EncoderOption
	}{
		{name: "stereo 44100 cbr", sampleRate: 44100, channels: 2, freq: 1000, samples: 32768, opts: []EncoderOption{WithBitRate(192000)}},
		{name: "stereo 48000 cbr", sampleRate: 48000, channels: 2, freq: 800, samples: 32768, opts: []EncoderOption{WithBitRate(256000)}},
		{name: "mono 44100 cbr", sampleRate: 44100, channels: 1, freq: 440, samples: 24000, opts: []EncoderOption{WithBitRate(128000)}},
		{name: "stereo 44100 vbr", sampleRate: 44100, channels: 2, freq: 660, samples: 32768, opts: []EncoderOption{WithVBR(true), WithQuality(2)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := generateSine(tc.sampleRate, tc.channels, tc.samples, tc.freq)

			stream := encode(t, tc.sampleRate, tc.channels, tc.opts, input)
			require.NotEmpty(t, stream, "encoder produced no output")

			out, sr, ch := decodeAll(t, stream)
			require.NotEmpty(t, out, "decoder produced no samples")

			assert.Equal(t, tc.sampleRate, sr, "decoded sample rate")
			assert.Equal(t, tc.channels, ch, "decoded channel count")
			assert.Zero(t, len(out)%ch, "decoded length must be a channel multiple")

			// All decoded samples stay in normalized range.
			for i, v := range out {
				require.LessOrEqual(t, math.Abs(v), 1.0+1e-9, "sample %d out of range", i)
			}

			// Compare per-channel waveforms by best-lag correlation. MP3 adds
			// encoder/decoder delay (a few thousand samples/channel) and is
			// lossy, so we require a strong correlation rather than equality.
			for c := 0; c < ch; c++ {
				inCh := deinterleave(input, c, ch)
				outCh := deinterleave(out, c, ch)
				corr := bestCorrelation(inCh, outCh, 4000)
				assert.Greater(t, corr, 0.95, "channel %d correlation too low: %v", c, corr)
			}
		})
	}
}

// deinterleave extracts channel c from interleaved samples.
func deinterleave(s []float64, c, channels int) []float64 {
	out := make([]float64, 0, len(s)/channels)
	for i := c; i < len(s); i += channels {
		out = append(out, s[i])
	}
	return out
}

func TestNewDecoderRejectsNilReader(t *testing.T) {
	_, err := NewDecoder(nil)
	assert.ErrorIs(t, err, ErrBadArg)
}

func TestNewEncoderRejectsNilWriter(t *testing.T) {
	_, err := NewEncoder(nil, 44100, 2)
	assert.ErrorIs(t, err, ErrBadArg)
}

func TestEncoderRejectsFormatMismatch(t *testing.T) {
	requireEncoder(t)
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, 44100, 2)
	require.NoError(t, err)
	defer enc.Close()

	_, err = enc.Write(mutations.Audio{Data: []float64{0, 0}, SampleRate: 48000, Channels: 2})
	assert.ErrorIs(t, err, ErrFormatMismatch)

	_, err = enc.Write(mutations.Audio{Data: []float64{0}, SampleRate: 44100, Channels: 1})
	assert.ErrorIs(t, err, ErrFormatMismatch)
}

func TestEncoderRejectsRaggedBuffer(t *testing.T) {
	requireEncoder(t)
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, 44100, 2)
	require.NoError(t, err)
	defer enc.Close()

	// Odd length for a stereo stream is not a whole number of frames.
	_, err = enc.Write(mutations.Audio{Data: []float64{0, 0, 0}, SampleRate: 44100, Channels: 2})
	assert.ErrorIs(t, err, ErrBadArg)
}

func TestEncoderEmptyWriteIsNoop(t *testing.T) {
	requireEncoder(t)
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, 44100, 2)
	require.NoError(t, err)
	defer enc.Close()

	n, err := enc.Write(mutations.Audio{Data: nil, SampleRate: 44100, Channels: 2})
	require.NoError(t, err)
	assert.Zero(t, n)
}
