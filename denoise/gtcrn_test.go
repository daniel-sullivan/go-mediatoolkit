package denoise

import (
	"errors"
	"testing"
)

// TestNewGTCRNSampleRateContract pins the resolved design decision: the
// engine restricts rather than resamples, so a rate above 16 kHz is
// rejected with ErrUnsupportedSampleRate; 16 kHz and below are accepted.
func TestNewGTCRNSampleRateContract(t *testing.T) {
	cases := []struct {
		name    string
		rate    int
		wantErr error
	}{
		{"exact-16k", 16000, nil},
		{"below-16k", 8000, nil},
		{"above-16k-48k", 48000, ErrUnsupportedSampleRate},
		{"above-16k-44100", 44100, ErrUnsupportedSampleRate},
		{"above-16k-16001", 16001, ErrUnsupportedSampleRate},
		{"zero", 0, nil}, // handled below (positive-rate error, not the sentinel)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := NewGTCRN(GTCRNConfig{SampleRate: tc.rate, Channels: 1})
			switch {
			case tc.rate <= 0:
				if err == nil {
					t.Fatalf("rate %d: expected an error", tc.rate)
				}
			case tc.wantErr == nil:
				if err != nil {
					t.Fatalf("rate %d: unexpected error %v", tc.rate, err)
				}
				if g.SampleRate() != tc.rate {
					t.Fatalf("SampleRate()=%d, want %d", g.SampleRate(), tc.rate)
				}
			default:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("rate %d: got %v, want %v", tc.rate, err, tc.wantErr)
				}
			}
		})
	}
}

// TestNewGTCRNChannelsContract pins the 1..64 channel bound.
func TestNewGTCRNChannelsContract(t *testing.T) {
	for _, ch := range []int{0, -1, 65, 128} {
		if _, err := NewGTCRN(GTCRNConfig{SampleRate: 16000, Channels: ch}); !errors.Is(err, ErrBadChannels) {
			t.Errorf("channels %d: got %v, want ErrBadChannels", ch, err)
		}
	}
	for _, ch := range []int{1, 2, 64} {
		g, err := NewGTCRN(GTCRNConfig{SampleRate: 16000, Channels: ch})
		if err != nil {
			t.Errorf("channels %d: unexpected error %v", ch, err)
			continue
		}
		if g.Channels() != ch {
			t.Errorf("Channels()=%d, want %d", g.Channels(), ch)
		}
	}
}
