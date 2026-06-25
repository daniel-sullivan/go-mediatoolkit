package mp4

import (
	"testing"

	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseHEAACASC verifies the esds ASC parser resolves the HE-AAC explicit
// extension signalling: an AOT-5 (SBR) ASC must report the SBR-DOUBLED output
// sample rate (not the AAC-LC core rate) and the long 2048-sample frame, and an
// AOT-29 (PS) ASC must additionally report a STEREO output even though its core
// channelConfiguration is mono. The AOT-5 vectors are the exact bytes
// libfdk-aac emits; the AOT-29 vector layers PS over a mono SBR core.
func TestParseHEAACASC(t *testing.T) {
	cases := []struct {
		name      string
		raw       []byte
		wantAOT   aaclib.AudioObjectType
		wantRate  int
		wantChan  int
		wantFrame int
	}{
		{"sbr mono 44100", []byte{0x2b, 0x8a, 0x08, 0x00}, aaclib.AOTSBR, 44100, 1, aaclib.FrameSamplesLong},
		{"sbr stereo 44100", []byte{0x2b, 0x92, 0x08, 0x00}, aaclib.AOTSBR, 44100, 2, aaclib.FrameSamplesLong},
		{"sbr mono 48000", []byte{0x2b, 0x09, 0x88, 0x00}, aaclib.AOTSBR, 48000, 1, aaclib.FrameSamplesLong},
		{"sbr stereo 32000", []byte{0x2c, 0x12, 0x88, 0x00}, aaclib.AOTSBR, 32000, 2, aaclib.FrameSamplesLong},
		// AOT-29 PS: core 22050 mono, ext 44100, coreAOT=2 -> stereo 44100 out.
		{"ps mono-core stereo 44100", []byte{0xeb, 0x8a, 0x08, 0x00}, aaclib.AOTPS, 44100, 2, aaclib.FrameSamplesLong},
		// AAC-LC for contrast: no doubling, short frame.
		{"aac-lc stereo 44100", []byte{0x12, 0x10}, aaclib.AOTAACLC, 44100, 2, aaclib.FrameSamplesShort},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeAudioSpecificConfig(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.wantAOT, got.ObjectType, "object type")
			assert.Equal(t, tc.wantRate, got.SampleRate, "output sample rate")
			assert.Equal(t, tc.wantChan, got.Channels, "output channels")
			assert.Equal(t, tc.wantFrame, got.FrameSamples, "frame samples")
			assert.Equal(t, tc.raw, got.Raw, "raw ASC preserved verbatim")
		})
	}
}

// TestHEAACEsdsRoundTrip verifies a full esds round-trip for HE-AAC v1 (AOT-5)
// and v2 (AOT-29) configs: build the esds box, parse it back, and confirm the
// object type, output rate, and output channel count survive. Two sources of
// ASC bytes are covered:
//
//   - A re-mux config that already carries Raw (the verbatim explicit ASC) — the
//     writer must copy it byte-for-byte so the parsed output matches.
//   - A synthesised config with no Raw — the writer must emit a correct
//     explicit-hierarchical ASC that parses back to the same output format.
func TestHEAACEsdsRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		asc      aaclib.AudioSpecificConfig
		wantAOT  aaclib.AudioObjectType
		wantRate int
		wantChan int
	}{
		{
			name: "sbr verbatim raw 44100 stereo",
			asc: aaclib.AudioSpecificConfig{
				ObjectType:   aaclib.AOTSBR,
				SampleRate:   44100,
				Channels:     2,
				FrameSamples: aaclib.FrameSamplesLong,
				Raw:          []byte{0x2b, 0x92, 0x08, 0x00},
			},
			wantAOT:  aaclib.AOTSBR,
			wantRate: 44100,
			wantChan: 2,
		},
		{
			name: "sbr synthesised (no raw) 48000 mono",
			asc: aaclib.AudioSpecificConfig{
				ObjectType:   aaclib.AOTSBR,
				SampleRate:   48000, // output rate
				Channels:     1,
				FrameSamples: aaclib.FrameSamplesLong,
			},
			wantAOT:  aaclib.AOTSBR,
			wantRate: 48000,
			wantChan: 1,
		},
		{
			name: "ps verbatim raw stereo 44100",
			asc: aaclib.AudioSpecificConfig{
				ObjectType:   aaclib.AOTPS,
				SampleRate:   44100,
				Channels:     2,
				FrameSamples: aaclib.FrameSamplesLong,
				Raw:          []byte{0xeb, 0x8a, 0x08, 0x00},
			},
			wantAOT:  aaclib.AOTPS,
			wantRate: 44100,
			wantChan: 2,
		},
		{
			name: "ps synthesised (no raw) stereo 32000",
			asc: aaclib.AudioSpecificConfig{
				ObjectType:   aaclib.AOTPS,
				SampleRate:   32000, // output rate; core is mono 16000
				Channels:     2,
				FrameSamples: aaclib.FrameSamplesLong,
			},
			wantAOT:  aaclib.AOTPS,
			wantRate: 32000,
			wantChan: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			esds := buildEsds(tc.asc)
			boxes, err := readBoxes(esds, 0)
			require.NoError(t, err)
			require.Len(t, boxes, 1)
			require.Equal(t, "esds", boxes[0].Type.String())

			got, err := parseESDS(boxes[0].Payload)
			require.NoError(t, err)
			assert.Equal(t, tc.wantAOT, got.ObjectType, "object type")
			assert.Equal(t, tc.wantRate, got.SampleRate, "output sample rate")
			assert.Equal(t, tc.wantChan, got.Channels, "output channels")
			assert.Equal(t, aaclib.FrameSamplesLong, got.FrameSamples, "long frame for HE-AAC")

			// A verbatim-Raw config must survive byte-for-byte.
			if len(tc.asc.Raw) > 0 {
				assert.Equal(t, tc.asc.Raw, got.Raw, "verbatim raw ASC preserved")
			}
		})
	}
}

// TestEncodeExplicitHEAACConfig verifies the synthesised explicit-hierarchical
// ASC bytes (no Raw) decode back to the intended HE-AAC output format, and that
// the AOT-5 case matches the byte layout libfdk-aac emits.
func TestEncodeExplicitHEAACConfig(t *testing.T) {
	// SBR stereo, output 44100 (core 22050) -> the canonical fdk vector.
	sbr := aaclib.AudioSpecificConfig{ObjectType: aaclib.AOTSBR, SampleRate: 44100, Channels: 2}
	assert.Equal(t, []byte{0x2b, 0x92, 0x08, 0x00}, encodeAudioSpecificConfig(sbr),
		"synthesised SBR ASC must match the fdk explicit-hierarchical layout")

	// PS, output stereo 44100 (mono core 22050).
	ps := aaclib.AudioSpecificConfig{ObjectType: aaclib.AOTPS, SampleRate: 44100, Channels: 2}
	psRaw := encodeAudioSpecificConfig(ps)
	gotPS, err := decodeAudioSpecificConfig(psRaw)
	require.NoError(t, err)
	assert.Equal(t, aaclib.AOTPS, gotPS.ObjectType)
	assert.Equal(t, 44100, gotPS.SampleRate)
	assert.Equal(t, 2, gotPS.Channels)
}
