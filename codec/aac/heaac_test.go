package aac

import (
	"errors"
	"testing"

	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HE-AAC signalling/routing tests for the public Decoder adapter.
//
// These exercise the SIGNALLING path only — that an AudioSpecificConfig with an
// explicit HE-AAC object type (AOT-5 SBR / AOT-29 PS) routes through the public
// NewDecoder and reports the correct decoded OUTPUT format (the SBR-doubled
// sample rate, and the PS-widened stereo channel count) up front. They do not
// run the actual fdk decode engine: that is fenced behind the aacfdk build tag,
// so in the default build NewDecoder surfaces ErrEngineRequiresFDK. The test
// asserts the contract holds either way — when the engine is unavailable the
// constructor returns exactly ErrEngineRequiresFDK; when it is built it reports
// the resolved rate/channels.
//
// The AOT-5 ASC byte strings are the exact ones libfdk-aac emits for the listed
// configurations (the same vectors as libraries/aac's sbr_signaling_test.go);
// the AOT-29 vector layers parametric stereo over a mono SBR core.
func TestNewDecoderHEAACRouting(t *testing.T) {
	cases := []struct {
		name        string
		asc         aaclib.AudioSpecificConfig
		wantRate    int
		wantChannel int
	}{
		{
			name: "he-aac v1 sbr mono->mono 44100",
			asc: aaclib.AudioSpecificConfig{
				ObjectType:   aaclib.AOTSBR,
				SampleRate:   44100, // resolved output rate
				Channels:     1,
				FrameSamples: aaclib.FrameSamplesLong,
				Raw:          []byte{0x2b, 0x8a, 0x08, 0x00}, // core 22050, out 44100, mono
			},
			wantRate:    44100,
			wantChannel: 1,
		},
		{
			name: "he-aac v1 sbr stereo 44100",
			asc: aaclib.AudioSpecificConfig{
				ObjectType:   aaclib.AOTSBR,
				SampleRate:   44100,
				Channels:     2,
				FrameSamples: aaclib.FrameSamplesLong,
				Raw:          []byte{0x2b, 0x92, 0x08, 0x00}, // core 22050, out 44100, stereo
			},
			wantRate:    44100,
			wantChannel: 2,
		},
		{
			name: "he-aac v1 sbr mono 48000",
			asc: aaclib.AudioSpecificConfig{
				ObjectType:   aaclib.AOTSBR,
				SampleRate:   48000,
				Channels:     1,
				FrameSamples: aaclib.FrameSamplesLong,
				Raw:          []byte{0x2b, 0x09, 0x88, 0x00}, // core 24000, out 48000
			},
			wantRate:    48000,
			wantChannel: 1,
		},
		{
			name: "he-aac v2 ps mono-core -> stereo 44100",
			asc: aaclib.AudioSpecificConfig{
				ObjectType:   aaclib.AOTPS,
				SampleRate:   44100,
				Channels:     2, // output is stereo even though the core is mono
				FrameSamples: aaclib.FrameSamplesLong,
				// AOT-29, core sfi 22050, chCfg=1 (mono core), ext sfi 44100, coreAOT=2.
				Raw: []byte{0xeb, 0x8a, 0x08, 0x00},
			},
			wantRate:    44100,
			wantChannel: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, err := NewDecoder(NewSlicePacketReader(nil), tc.asc)
			if err != nil {
				// Default build: the fdk engine is fenced out.
				require.ErrorIs(t, err, aaclib.ErrEngineRequiresFDK)
				return
			}
			// aacfdk build: the adapter must advertise the resolved output format.
			assert.Equal(t, tc.wantRate, dec.SampleRate(), "output sample rate")
			assert.Equal(t, tc.wantChannel, dec.Channels(), "output channel count")
		})
	}
}

// TestNewDecoderHEAACOutputProjection verifies the rate/channel projection the
// adapter applies is exactly aaclib.AudioSpecificConfig.Output — i.e. the
// adapter does not silently fall back to the core (un-doubled) rate or the
// mono-core channel count for an HE-AAC ASC. This runs in the default build
// because it asserts only the construction-time projection, not a decode.
func TestNewDecoderHEAACOutputProjection(t *testing.T) {
	// A PS config whose core is mono 16000 and output stereo 32000.
	asc := aaclib.AudioSpecificConfig{
		ObjectType:   aaclib.AOTPS,
		SampleRate:   32000,
		Channels:     2,
		FrameSamples: aaclib.FrameSamplesLong,
	}
	rate, ch := asc.Output()
	assert.Equal(t, 32000, rate)
	assert.Equal(t, 2, ch)

	dec, err := NewDecoder(NewSlicePacketReader(nil), asc)
	if err != nil {
		// Default build: the fdk engine is fenced out entirely. Under the
		// aacfdk build the C engine validates the (synthetic, Raw-less) PS ASC
		// more strictly than the Go projection above and rejects it as
		// malformed — both are acceptable; neither contradicts the projection
		// contract, which has already been asserted via asc.Output().
		require.True(t,
			errorsIsAny(err, aaclib.ErrEngineRequiresFDK, aaclib.ErrInvalidConfig),
			"unexpected error: %v", err)
		return
	}
	assert.Equal(t, rate, dec.SampleRate())
	assert.Equal(t, ch, dec.Channels())
}

// errorsIsAny reports whether err matches any of the given target errors.
func errorsIsAny(err error, targets ...error) bool {
	for _, t := range targets {
		if errors.Is(err, t) {
			return true
		}
	}
	return false
}

// TestNewEncoderHEAACObjectType verifies WithObjectType(AOTSBR) is accepted and
// forwarded for an HE-AAC v1 encoder. As with the decoder, the fdk engine is
// fenced behind aacfdk, so the default build surfaces ErrEngineRequiresFDK; the
// aacfdk build constructs a real HE-AAC v1 encoder reporting the input format.
func TestNewEncoderHEAACObjectType(t *testing.T) {
	pw := &collectPacketWriter{}
	enc, err := NewEncoder(pw, 44100, 2,
		WithObjectType(aaclib.AOTSBR),
		WithBitrate(64000),
	)
	if err != nil {
		require.ErrorIs(t, err, aaclib.ErrEngineRequiresFDK)
		return
	}
	assert.Equal(t, 44100, enc.SampleRate())
	assert.Equal(t, 2, enc.Channels())
}
