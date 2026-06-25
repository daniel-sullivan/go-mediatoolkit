//go:build cgo && opus_strict

package benchcmp

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestBisectHarness_Fs24kMono_20ms_48k_LowDelay exercises the
// drift-bisection harness against one of the known-failing
// TestParity_OpusEncode_Matrix configs. The test is NOT a parity
// assertion — it just confirms the harness runs end-to-end and
// surfaces either the first divergence or a clean report. Follow-up
// agents can grep for this test output to pinpoint where Go deviates.
func TestBisectHarness_Fs24kMono_20ms_48k_LowDelay(t *testing.T) {
	_, gm := buildFullGoMode(t)
	SetBisectCeltMode(gm)

	cfg := MatrixConfig{
		Fs:       24000,
		Channels: 1,
		FrameMs:  20,
		Bitrate:  48000,
		App:      nativeopus.OPUS_APPLICATION_RESTRICTED_LOWDELAY,
	}
	report := BisectCELTFrame(cfg, 3)
	t.Logf("\n%s", report.Format())
}

// TestBisectHarness_Fs48kMono_20ms_64k_Audio exercises a
// passing-matrix point, to confirm the harness reports no divergence
// on a config that is already byte-exact.
func TestBisectHarness_Fs48kMono_20ms_64k_Audio(t *testing.T) {
	_, gm := buildFullGoMode(t)
	SetBisectCeltMode(gm)

	cfg := MatrixConfig{
		Fs:       48000,
		Channels: 1,
		FrameMs:  20,
		Bitrate:  64000,
		App:      nativeopus.OPUS_APPLICATION_AUDIO,
	}
	report := BisectCELTFrame(cfg, 3)
	t.Logf("\n%s", report.Format())
	if report.FirstDivergentLocation != "" && report.PacketParityOK {
		t.Errorf("unexpected state diff on passing config: %s", report.FirstDivergentLocation)
	}
}
