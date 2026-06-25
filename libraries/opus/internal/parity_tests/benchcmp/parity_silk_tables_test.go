//go:build cgo && opus_strict

package benchcmp

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func eqBytes(t *testing.T, name string, c, g []byte) {
	t.Helper()
	if !bytes.Equal(c, g) {
		for i := range c {
			if i >= len(g) || c[i] != g[i] {
				t.Errorf("%s: first diff at [%d] C=%d Go=%d (lenC=%d lenG=%d)",
					name, i, c[i], func() byte {
						if i < len(g) {
							return g[i]
						}
						return 0
					}(), len(c), len(g))
				return
			}
		}
		t.Errorf("%s: lengths differ C=%d Go=%d", name, len(c), len(g))
	}
}

func eqI16(t *testing.T, name string, c, g []int16) {
	t.Helper()
	if !reflect.DeepEqual(c, g) {
		for i := range c {
			if i >= len(g) || c[i] != g[i] {
				gv := int16(0)
				if i < len(g) {
					gv = g[i]
				}
				t.Errorf("%s: first diff at [%d] C=%d Go=%d", name, i, c[i], gv)
				return
			}
		}
		t.Errorf("%s: lengths differ C=%d Go=%d", name, len(c), len(g))
	}
}

func eqI32(t *testing.T, name string, c, g []int32) {
	t.Helper()
	if !reflect.DeepEqual(c, g) {
		t.Errorf("%s: differ C=%v Go=%v", name, c, g)
	}
}

func eqI8(t *testing.T, name string, c, g []int8) {
	t.Helper()
	if !reflect.DeepEqual(c, g) {
		for i := range c {
			if i >= len(g) || c[i] != g[i] {
				gv := int8(0)
				if i < len(g) {
					gv = g[i]
				}
				t.Errorf("%s: first diff at [%d] C=%d Go=%d", name, i, c[i], gv)
				return
			}
		}
		t.Errorf("%s: lengths differ", name)
	}
}

func TestParity_SilkTables(t *testing.T) {
	eqBytes(t, "gain_iCDF", cSilkGainICDF(), nativeopus.ExportTestSilkTable_GainICDF())
	eqBytes(t, "delta_gain_iCDF", cSilkDeltaGainICDF(), nativeopus.ExportTestSilkTable_DeltaGainICDF())
	eqBytes(t, "pitch_lag_iCDF", cSilkPitchLagICDF(), nativeopus.ExportTestSilkTable_PitchLagICDF())
	eqBytes(t, "pitch_delta_iCDF", cSilkPitchDeltaICDF(), nativeopus.ExportTestSilkTable_PitchDeltaICDF())
	eqBytes(t, "pitch_contour_iCDF", cSilkPitchContourICDF(), nativeopus.ExportTestSilkTable_PitchContourICDF())
	eqBytes(t, "pitch_contour_NB_iCDF", cSilkPitchContourNBICDF(), nativeopus.ExportTestSilkTable_PitchContourNBICDF())
	eqBytes(t, "pitch_contour_10ms_iCDF", cSilkPitchContour10msICDF(), nativeopus.ExportTestSilkTable_PitchContour10msICDF())
	eqBytes(t, "pitch_contour_10ms_NB_iCDF", cSilkPitchContour10msNBICDF(), nativeopus.ExportTestSilkTable_PitchContour10msNBICDF())
	eqI16(t, "stereo_pred_quant_Q13", cSilkStereoPredQuantQ13(), nativeopus.ExportTestSilkTable_StereoPredQuantQ13())
	eqBytes(t, "stereo_pred_joint_iCDF", cSilkStereoPredJointICDF(), nativeopus.ExportTestSilkTable_StereoPredJointICDF())
	eqBytes(t, "LTPscale_iCDF", cSilkLTPscaleICDF(), nativeopus.ExportTestSilkTable_LTPscaleICDF())
	eqBytes(t, "LTP_per_index_iCDF", cSilkLTPPerIndexICDF(), nativeopus.ExportTestSilkTable_LTPPerIndexICDF())
	eqBytes(t, "sign_iCDF", cSilkSignICDF(), nativeopus.ExportTestSilkTable_SignICDF())
	eqBytes(t, "shell_code_table0", cSilkShellCodeTable0(), nativeopus.ExportTestSilkTable_ShellCodeTable0())
	eqBytes(t, "shell_code_table3", cSilkShellCodeTable3(), nativeopus.ExportTestSilkTable_ShellCodeTable3())
	eqI32(t, "Transition_LP_B_Q28", cSilkTransitionLPBQ28(), nativeopus.ExportTestSilkTable_TransitionLPBQ28())
	eqI8(t, "CB_lags_stage2", cSilkCBLagsStage2(), nativeopus.ExportTestSilkTable_CBLagsStage2())
	eqI8(t, "CB_lags_stage3", cSilkCBLagsStage3(), nativeopus.ExportTestSilkTable_CBLagsStage3())
	eqI16(t, "Resampler_3_4_COEFS", cSilkResampler34(), nativeopus.ExportTestSilkTable_Resampler_3_4_COEFS())
	eqI16(t, "Resampler_1_2_COEFS", cSilkResampler12(), nativeopus.ExportTestSilkTable_Resampler_1_2_COEFS())
	eqI16(t, "resampler_frac_FIR_12", cSilkResamplerFracFIR12(), nativeopus.ExportTestSilkTable_ResamplerFracFIR12())
	eqBytes(t, "NLSF_CB1_NB_MB_Q8", cSilkNLSFCB1NBMBQ8(), nativeopus.ExportTestSilkTable_NLSFCB1NBMBQ8())
	eqBytes(t, "NLSF_CB1_WB_Q8", cSilkNLSFCB1WBQ8(), nativeopus.ExportTestSilkTable_NLSFCB1WBQ8())
	eqI16(t, "NLSF_CB1_Wght_Q9", cSilkNLSFCB1WghtQ9(), nativeopus.ExportTestSilkTable_NLSFCB1WghtQ9())
	eqI16(t, "NLSF_CB1_WB_Wght_Q9", cSilkNLSFCB1WBWghtQ9(), nativeopus.ExportTestSilkTable_NLSFCB1WBWghtQ9())
}
