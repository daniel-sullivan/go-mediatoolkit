package hwaccel

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// fakeBackend is a configurable Backend for exercising the registry and
// selection policy without touching real hardware.
type fakeBackend struct {
	name      string
	available bool
	caps      Capabilities
	encErr    error
	decErr    error
}

func (f *fakeBackend) Name() string    { return f.name }
func (f *fakeBackend) Available() bool { return f.available }
func (f *fakeBackend) Probe() (Capabilities, error) {
	c := f.caps
	c.Backend = f.name
	return c, nil
}
func (f *fakeBackend) NewEncoder(cfg Config) (Encoder, error) {
	if f.encErr != nil {
		return nil, f.encErr
	}
	return &nopEncoder{}, nil
}
func (f *fakeBackend) NewDecoder(cfg Config) (Decoder, error) {
	if f.decErr != nil {
		return nil, f.decErr
	}
	return &nopDecoder{}, nil
}

type nopEncoder struct{}

func (nopEncoder) Encode(video.Frame) ([]video.Packet, error) { return nil, nil }
func (nopEncoder) Flush() ([]video.Packet, error)             { return nil, nil }
func (nopEncoder) Close() error                               { return nil }

type nopDecoder struct{}

func (nopDecoder) Decode(video.Packet) ([]video.Frame, error) { return nil, nil }
func (nopDecoder) Flush() ([]video.Frame, error)              { return nil, nil }
func (nopDecoder) Close() error                               { return nil }

func h264EncCaps(name string) Capabilities {
	return Capabilities{
		Backend: name,
		Codecs:  []CodecCapability{{Codec: video.H264, Encode: true, Profiles: []string{"high"}}},
	}
}

func encCfg() Config {
	return NewConfig(WithCodec(video.H264), WithResolution(640, 480), WithBitrate(1_000_000))
}

func TestOpenEncoderSelection(t *testing.T) {
	tests := []struct {
		name        string
		backends    []*fakeBackend
		mode        Mode
		wantErr     error
		wantOK      bool
		wantFellTo  string
		wantPublish bool
	}{
		{
			name: "prefer picks first available supporting backend",
			backends: []*fakeBackend{
				{name: "a", available: false, caps: h264EncCaps("a")},
				{name: "b", available: true, caps: h264EncCaps("b")},
			},
			mode:   PreferHardware,
			wantOK: true,
		},
		{
			name: "prefer falls back loudly when no hardware works",
			backends: []*fakeBackend{
				{name: "a", available: true, caps: Capabilities{}}, // supports nothing
			},
			mode:        PreferHardware,
			wantErr:     ErrNoBackend, // no software tier wired in
			wantPublish: true,
			wantFellTo:  "",
		},
		{
			name: "require errors instead of degrading",
			backends: []*fakeBackend{
				{name: "a", available: true, caps: Capabilities{}},
			},
			mode:    RequireHardware,
			wantErr: ErrHardwareUnavailable,
		},
		{
			name: "require succeeds when a backend satisfies",
			backends: []*fakeBackend{
				{name: "a", available: true, caps: h264EncCaps("a")},
			},
			mode:   RequireHardware,
			wantOK: true,
		},
		{
			name: "software-only never tries hardware, errors (no sw tier)",
			backends: []*fakeBackend{
				{name: "a", available: true, caps: h264EncCaps("a")},
			},
			mode:    SoftwareOnly,
			wantErr: ErrNoBackend,
		},
		{
			name: "construction error on supporting backend falls back",
			backends: []*fakeBackend{
				{name: "a", available: true, caps: h264EncCaps("a"), encErr: errors.New("boom")},
			},
			mode:        PreferHardware,
			wantErr:     ErrNoBackend,
			wantPublish: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewRegistry()
			for _, b := range tc.backends {
				reg.Register(b)
			}
			bus := NewFallbackBus()
			var got *HardwareFallbackEvent
			bus.Subscribe(func(e HardwareFallbackEvent) { ev := e; got = &ev })

			enc, err := Policy{Mode: tc.mode, Bus: bus}.OpenEncoder(reg, encCfg())

			if tc.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, enc)
			}
			if tc.wantOK {
				require.NoError(t, err)
				require.NotNil(t, enc)
				assert.NoError(t, enc.Close())
			}
			if tc.wantPublish {
				require.NotNil(t, got, "expected a HardwareFallbackEvent")
				assert.Equal(t, video.H264, got.Codec)
				assert.Equal(t, Encode, got.Direction)
				assert.Equal(t, tc.wantFellTo, got.FellBackTo)
				assert.NotEmpty(t, got.Reasons)
			} else {
				assert.Nil(t, got, "no fallback event expected")
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantEnc error
		wantDec error
	}{
		{"valid", NewConfig(WithCodec(video.H264), WithResolution(640, 480)), nil, nil},
		{"no codec", NewConfig(WithResolution(640, 480)), ErrInvalidConfig, ErrInvalidConfig},
		{"no resolution encode-only", NewConfig(WithCodec(video.H264)), ErrInvalidConfig, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.cfg.validateEncode(), tc.wantEnc)
			assert.ErrorIs(t, tc.cfg.validateDecode(), tc.wantDec)
		})
	}
}

func TestCapabilitiesSupports(t *testing.T) {
	caps := Capabilities{Codecs: []CodecCapability{
		{Codec: video.H264, Encode: true, Decode: false},
		{Codec: video.H265, Encode: false, Decode: true},
	}}
	assert.True(t, caps.Supports(video.H264, Encode))
	assert.False(t, caps.Supports(video.H264, Decode))
	assert.False(t, caps.Supports(video.H265, Encode))
	assert.True(t, caps.Supports(video.H265, Decode))
	assert.False(t, caps.Supports(video.CodecUnknown, Encode))
}

func TestConfigFrameRate(t *testing.T) {
	assert.Equal(t, 30.0, NewConfig().frameRate())
	assert.Equal(t, 30.0, NewConfig(WithFrameRate(30, 1)).frameRate())
	assert.InDelta(t, 29.97, NewConfig(WithFrameRate(30000, 1001)).frameRate(), 0.01)
	assert.Equal(t, 25.0, NewConfig(WithFrameRate(25, 0)).frameRate()) // den 0 => /1
}
