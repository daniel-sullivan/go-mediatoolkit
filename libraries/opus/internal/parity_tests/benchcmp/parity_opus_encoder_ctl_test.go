//go:build cgo && opus_strict

package benchcmp

import (
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_OpusEncoderCtl — opus_encoder_ctl SET/GET parity sweep.
//
// Table-driven over every non-gated CTL request. For SET CTLs we
// initialise a fresh Go encoder and a fresh C encoder, apply the SET,
// and compare the full OpusEncoderStateSnapshot. For GET CTLs we apply
// an optional pre-mutating SET, then the GET, and compare the returned
// value (and the CTL return code).
func TestParity_OpusEncoderCtl(t *testing.T) {
	Fs := int32(48000)
	channels := 2
	application := nativeopus.OPUS_APPLICATION_AUDIO

	// ── SET CTLs ──────────────────────────────────────────────────
	setCases := []struct {
		name    string
		request int
		values  []int32
	}{
		{"SET_APPLICATION", nativeopus.ExportOPUS_SET_APPLICATION_REQUEST, []int32{
			int32(nativeopus.OPUS_APPLICATION_VOIP),
			int32(nativeopus.OPUS_APPLICATION_AUDIO),
			int32(nativeopus.OPUS_APPLICATION_RESTRICTED_LOWDELAY),
			// Invalid
			int32(nativeopus.OPUS_APPLICATION_RESTRICTED_SILK),
			12345,
		}},
		{"SET_BITRATE", nativeopus.ExportOPUS_SET_BITRATE_REQUEST, []int32{
			int32(nativeopus.OPUS_AUTO),
			int32(nativeopus.OPUS_BITRATE_MAX),
			500, 1000, 32000, 128000, 512000, 2000000,
			// Invalid
			0, -500,
		}},
		{"SET_MAX_BANDWIDTH", nativeopus.ExportOPUS_SET_MAX_BANDWIDTH_REQUEST, []int32{
			int32(nativeopus.OPUS_BANDWIDTH_NARROWBAND),
			int32(nativeopus.OPUS_BANDWIDTH_MEDIUMBAND),
			int32(nativeopus.OPUS_BANDWIDTH_WIDEBAND),
			int32(nativeopus.OPUS_BANDWIDTH_SUPERWIDEBAND),
			int32(nativeopus.OPUS_BANDWIDTH_FULLBAND),
			// Invalid
			100, 9999,
		}},
		{"SET_BANDWIDTH", nativeopus.ExportOPUS_SET_BANDWIDTH_REQUEST, []int32{
			int32(nativeopus.OPUS_AUTO),
			int32(nativeopus.OPUS_BANDWIDTH_NARROWBAND),
			int32(nativeopus.OPUS_BANDWIDTH_MEDIUMBAND),
			int32(nativeopus.OPUS_BANDWIDTH_WIDEBAND),
			int32(nativeopus.OPUS_BANDWIDTH_SUPERWIDEBAND),
			int32(nativeopus.OPUS_BANDWIDTH_FULLBAND),
			// Invalid
			100, 9999,
		}},
		{"SET_VBR", nativeopus.ExportOPUS_SET_VBR_REQUEST, []int32{0, 1, -1, 2}},
		{"SET_VBR_CONSTRAINT", nativeopus.ExportOPUS_SET_VBR_CONSTRAINT_REQUEST, []int32{0, 1, -1, 2}},
		{"SET_COMPLEXITY", nativeopus.ExportOPUS_SET_COMPLEXITY_REQUEST, []int32{0, 1, 5, 9, 10, -1, 11}},
		{"SET_INBAND_FEC", nativeopus.ExportOPUS_SET_INBAND_FEC_REQUEST, []int32{0, 1, 2, -1, 3}},
		{"SET_PACKET_LOSS_PERC", nativeopus.ExportOPUS_SET_PACKET_LOSS_PERC_REQUEST, []int32{0, 10, 50, 100, -1, 101}},
		{"SET_DTX", nativeopus.ExportOPUS_SET_DTX_REQUEST, []int32{0, 1, -1, 2}},
		{"SET_FORCE_CHANNELS", nativeopus.ExportOPUS_SET_FORCE_CHANNELS_REQUEST, []int32{
			int32(nativeopus.OPUS_AUTO),
			1, 2,
			// Invalid (> st.channels=2, or 0)
			0, 3,
		}},
		{"SET_SIGNAL", nativeopus.ExportOPUS_SET_SIGNAL_REQUEST, []int32{
			int32(nativeopus.OPUS_AUTO),
			int32(nativeopus.ExportOPUS_SIGNAL_VOICE),
			int32(nativeopus.ExportOPUS_SIGNAL_MUSIC),
			// Invalid
			9999,
		}},
		{"SET_LSB_DEPTH", nativeopus.ExportOPUS_SET_LSB_DEPTH_REQUEST, []int32{8, 16, 24, 7, 25}},
		{"SET_EXPERT_FRAME_DURATION", nativeopus.ExportOPUS_SET_EXPERT_FRAME_DURATION_REQUEST, []int32{
			int32(nativeopus.OPUS_FRAMESIZE_ARG),
			int32(nativeopus.OPUS_FRAMESIZE_2_5_MS),
			int32(nativeopus.OPUS_FRAMESIZE_5_MS),
			int32(nativeopus.OPUS_FRAMESIZE_10_MS),
			int32(nativeopus.OPUS_FRAMESIZE_20_MS),
			int32(nativeopus.OPUS_FRAMESIZE_40_MS),
			int32(nativeopus.OPUS_FRAMESIZE_60_MS),
			int32(nativeopus.OPUS_FRAMESIZE_80_MS),
			int32(nativeopus.OPUS_FRAMESIZE_100_MS),
			int32(nativeopus.OPUS_FRAMESIZE_120_MS),
			// Invalid
			9999,
		}},
		{"SET_PREDICTION_DISABLED", nativeopus.ExportOPUS_SET_PREDICTION_DISABLED_REQUEST, []int32{0, 1, -1, 2}},
		{"SET_PHASE_INVERSION_DISABLED", nativeopus.ExportOPUS_SET_PHASE_INVERSION_DISABLED_REQUEST, []int32{0, 1, -1, 2}},
		{"SET_VOICE_RATIO", nativeopus.ExportOPUS_SET_VOICE_RATIO_REQUEST, []int32{-1, 0, 50, 100, -2, 101}},
		{"SET_FORCE_MODE", nativeopus.ExportOPUS_SET_FORCE_MODE_REQUEST, []int32{
			int32(nativeopus.OPUS_AUTO),
			int32(nativeopus.MODE_SILK_ONLY),
			int32(nativeopus.MODE_HYBRID),
			int32(nativeopus.MODE_CELT_ONLY),
			// Invalid
			999,
		}},
		{"SET_LFE", nativeopus.ExportOPUS_SET_LFE_REQUEST, []int32{0, 1, -1, 7}},
	}

	for _, tc := range setCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, v := range tc.values {
				gInit, gCtl, gSnap := nativeopus.ExportOpusEncoderCtlSetAndSnapshot(Fs, channels, application, tc.request, v)
				cInit, cCtl, cSnap := cOpusEncoderCtlSetAndSnapshot(Fs, channels, application, tc.request, v)
				if gInit != cInit {
					t.Fatalf("%s(v=%d): init return mismatch: Go=%d C=%d", tc.name, v, gInit, cInit)
				}
				if gInit != 0 {
					continue
				}
				if gCtl != cCtl {
					t.Errorf("%s(v=%d): ctl return mismatch: Go=%d C=%d", tc.name, v, gCtl, cCtl)
				}
				compareEncoderSnapshot(t, gSnap, cSnap)
			}
		})
	}

	// ── OPUS_RESET_STATE (SET-like, no argument) ──────────────────
	t.Run("RESET_STATE", func(t *testing.T) {
		gInit, gCtl, gSnap := nativeopus.ExportOpusEncoderCtlSetAndSnapshot(Fs, channels, application, nativeopus.ExportOPUS_RESET_STATE, 0)
		cInit, cCtl, cSnap := cOpusEncoderCtlSetAndSnapshot(Fs, channels, application, nativeopus.ExportOPUS_RESET_STATE, 0)
		if gInit != cInit {
			t.Fatalf("init return mismatch: Go=%d C=%d", gInit, cInit)
		}
		if gInit != 0 {
			return
		}
		if gCtl != cCtl {
			t.Errorf("ctl return mismatch: Go=%d C=%d", gCtl, cCtl)
		}
		compareEncoderSnapshot(t, gSnap, cSnap)
	})

	// ── GET CTLs ──────────────────────────────────────────────────
	// For each GET we optionally pre-apply a SET to exercise a non-
	// default value, then fetch.
	getI32Cases := []struct {
		name          string
		preSetRequest int
		preSetValue   int32
		getRequest    int
	}{
		{"GET_APPLICATION", 0, 0, nativeopus.ExportOPUS_GET_APPLICATION_REQUEST},
		{"GET_APPLICATION_set", nativeopus.ExportOPUS_SET_APPLICATION_REQUEST, int32(nativeopus.OPUS_APPLICATION_VOIP), nativeopus.ExportOPUS_GET_APPLICATION_REQUEST},
		{"GET_BITRATE", 0, 0, nativeopus.ExportOPUS_GET_BITRATE_REQUEST},
		{"GET_BITRATE_set", nativeopus.ExportOPUS_SET_BITRATE_REQUEST, 96000, nativeopus.ExportOPUS_GET_BITRATE_REQUEST},
		{"GET_MAX_BANDWIDTH", 0, 0, nativeopus.ExportOPUS_GET_MAX_BANDWIDTH_REQUEST},
		{"GET_MAX_BANDWIDTH_set", nativeopus.ExportOPUS_SET_MAX_BANDWIDTH_REQUEST, int32(nativeopus.OPUS_BANDWIDTH_WIDEBAND), nativeopus.ExportOPUS_GET_MAX_BANDWIDTH_REQUEST},
		{"GET_BANDWIDTH", 0, 0, nativeopus.ExportOPUS_GET_BANDWIDTH_REQUEST},
		{"GET_VBR", 0, 0, nativeopus.ExportOPUS_GET_VBR_REQUEST},
		{"GET_VBR_set0", nativeopus.ExportOPUS_SET_VBR_REQUEST, 0, nativeopus.ExportOPUS_GET_VBR_REQUEST},
		{"GET_VBR_CONSTRAINT", 0, 0, nativeopus.ExportOPUS_GET_VBR_CONSTRAINT_REQUEST},
		{"GET_COMPLEXITY", 0, 0, nativeopus.ExportOPUS_GET_COMPLEXITY_REQUEST},
		{"GET_COMPLEXITY_set", nativeopus.ExportOPUS_SET_COMPLEXITY_REQUEST, 3, nativeopus.ExportOPUS_GET_COMPLEXITY_REQUEST},
		{"GET_INBAND_FEC", 0, 0, nativeopus.ExportOPUS_GET_INBAND_FEC_REQUEST},
		{"GET_INBAND_FEC_set", nativeopus.ExportOPUS_SET_INBAND_FEC_REQUEST, 2, nativeopus.ExportOPUS_GET_INBAND_FEC_REQUEST},
		{"GET_PACKET_LOSS_PERC", 0, 0, nativeopus.ExportOPUS_GET_PACKET_LOSS_PERC_REQUEST},
		{"GET_PACKET_LOSS_PERC_set", nativeopus.ExportOPUS_SET_PACKET_LOSS_PERC_REQUEST, 42, nativeopus.ExportOPUS_GET_PACKET_LOSS_PERC_REQUEST},
		{"GET_DTX", 0, 0, nativeopus.ExportOPUS_GET_DTX_REQUEST},
		{"GET_FORCE_CHANNELS", 0, 0, nativeopus.ExportOPUS_GET_FORCE_CHANNELS_REQUEST},
		{"GET_FORCE_CHANNELS_set1", nativeopus.ExportOPUS_SET_FORCE_CHANNELS_REQUEST, 1, nativeopus.ExportOPUS_GET_FORCE_CHANNELS_REQUEST},
		{"GET_SIGNAL", 0, 0, nativeopus.ExportOPUS_GET_SIGNAL_REQUEST},
		{"GET_SIGNAL_set", nativeopus.ExportOPUS_SET_SIGNAL_REQUEST, int32(nativeopus.ExportOPUS_SIGNAL_MUSIC), nativeopus.ExportOPUS_GET_SIGNAL_REQUEST},
		{"GET_LOOKAHEAD", 0, 0, nativeopus.ExportOPUS_GET_LOOKAHEAD_REQUEST},
		{"GET_SAMPLE_RATE", 0, 0, nativeopus.ExportOPUS_GET_SAMPLE_RATE_REQUEST},
		{"GET_LSB_DEPTH", 0, 0, nativeopus.ExportOPUS_GET_LSB_DEPTH_REQUEST},
		{"GET_LSB_DEPTH_set", nativeopus.ExportOPUS_SET_LSB_DEPTH_REQUEST, 16, nativeopus.ExportOPUS_GET_LSB_DEPTH_REQUEST},
		{"GET_EXPERT_FRAME_DURATION", 0, 0, nativeopus.ExportOPUS_GET_EXPERT_FRAME_DURATION_REQUEST},
		{"GET_EXPERT_FRAME_DURATION_set", nativeopus.ExportOPUS_SET_EXPERT_FRAME_DURATION_REQUEST, int32(nativeopus.OPUS_FRAMESIZE_20_MS), nativeopus.ExportOPUS_GET_EXPERT_FRAME_DURATION_REQUEST},
		{"GET_PREDICTION_DISABLED", 0, 0, nativeopus.ExportOPUS_GET_PREDICTION_DISABLED_REQUEST},
		{"GET_PREDICTION_DISABLED_set", nativeopus.ExportOPUS_SET_PREDICTION_DISABLED_REQUEST, 1, nativeopus.ExportOPUS_GET_PREDICTION_DISABLED_REQUEST},
		{"GET_PHASE_INVERSION_DISABLED", 0, 0, nativeopus.ExportOPUS_GET_PHASE_INVERSION_DISABLED_REQUEST},
		{"GET_PHASE_INVERSION_DISABLED_set", nativeopus.ExportOPUS_SET_PHASE_INVERSION_DISABLED_REQUEST, 1, nativeopus.ExportOPUS_GET_PHASE_INVERSION_DISABLED_REQUEST},
		{"GET_VOICE_RATIO", 0, 0, nativeopus.ExportOPUS_GET_VOICE_RATIO_REQUEST},
		{"GET_VOICE_RATIO_set", nativeopus.ExportOPUS_SET_VOICE_RATIO_REQUEST, 60, nativeopus.ExportOPUS_GET_VOICE_RATIO_REQUEST},
		{"GET_IN_DTX", 0, 0, nativeopus.ExportOPUS_GET_IN_DTX_REQUEST},
		{"GET_IN_DTX_dtx_on", nativeopus.ExportOPUS_SET_DTX_REQUEST, 1, nativeopus.ExportOPUS_GET_IN_DTX_REQUEST},
	}

	for _, tc := range getI32Cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gInit, gPre, gGet, gVal := nativeopus.ExportOpusEncoderCtlGetI32(Fs, channels, application, tc.preSetRequest, tc.preSetValue, tc.getRequest)
			cInit, cPre, cGet, cVal := cOpusEncoderCtlGetI32(Fs, channels, application, tc.preSetRequest, tc.preSetValue, tc.getRequest)
			if gInit != cInit {
				t.Fatalf("init return mismatch: Go=%d C=%d", gInit, cInit)
			}
			if gInit != 0 {
				return
			}
			if gPre != cPre {
				t.Errorf("preSet return mismatch: Go=%d C=%d", gPre, cPre)
			}
			if gGet != cGet {
				t.Errorf("get return mismatch: Go=%d C=%d", gGet, cGet)
			}
			if gVal != cVal {
				t.Errorf("value mismatch: Go=%d C=%d", gVal, cVal)
			}
		})
	}

	// ── OPUS_GET_FINAL_RANGE (uint32) ─────────────────────────────
	t.Run("GET_FINAL_RANGE", func(t *testing.T) {
		gInit, _, gGet, gVal := nativeopus.ExportOpusEncoderCtlGetU32(Fs, channels, application, 0, 0, nativeopus.ExportOPUS_GET_FINAL_RANGE_REQUEST)
		cInit, _, cGet, cVal := cOpusEncoderCtlGetU32(Fs, channels, application, 0, 0, nativeopus.ExportOPUS_GET_FINAL_RANGE_REQUEST)
		if gInit != cInit {
			t.Fatalf("init return mismatch: Go=%d C=%d", gInit, cInit)
		}
		if gInit != 0 {
			return
		}
		if gGet != cGet {
			t.Errorf("get return mismatch: Go=%d C=%d", gGet, cGet)
		}
		if gVal != cVal {
			t.Errorf("value mismatch: Go=%d C=%d", gVal, cVal)
		}
	})
}
