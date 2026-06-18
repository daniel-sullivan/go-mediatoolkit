//go:build cgo

package benchcmp

/*
#include "config.h"
#include "silk/typedef.h"
#include "silk/define.h"
#include "silk/pitch_est_defines.h"
#include "silk/resampler_rom.h"
#include "silk/tables.h"

// Forward-declare the table symbols we test.
extern const opus_uint8 silk_gain_iCDF[3][N_LEVELS_QGAIN/8];
extern const opus_uint8 silk_delta_gain_iCDF[MAX_DELTA_GAIN_QUANT - MIN_DELTA_GAIN_QUANT + 1];
extern const opus_uint8 silk_pitch_lag_iCDF[2 * (PITCH_EST_MAX_LAG_MS - PITCH_EST_MIN_LAG_MS)];
extern const opus_uint8 silk_pitch_delta_iCDF[21];
extern const opus_uint8 silk_pitch_contour_iCDF[34];
extern const opus_uint8 silk_pitch_contour_NB_iCDF[11];
extern const opus_uint8 silk_pitch_contour_10_ms_iCDF[12];
extern const opus_uint8 silk_pitch_contour_10_ms_NB_iCDF[3];
extern const opus_int16 silk_stereo_pred_quant_Q13[STEREO_QUANT_TAB_SIZE];
extern const opus_uint8 silk_stereo_pred_joint_iCDF[25];
extern const opus_uint8 silk_LTPscale_iCDF[3];
extern const opus_uint8 silk_LTP_per_index_iCDF[3];
extern const opus_uint8 silk_sign_iCDF[42];
extern const opus_uint8 silk_shell_code_table0[152];
extern const opus_uint8 silk_shell_code_table3[152];
extern const opus_int32 silk_Transition_LP_B_Q28[TRANSITION_INT_NUM][TRANSITION_NB];

extern const opus_int8 silk_CB_lags_stage2[PE_MAX_NB_SUBFR][PE_NB_CBKS_STAGE2_EXT];
extern const opus_int8 silk_CB_lags_stage3[PE_MAX_NB_SUBFR][PE_NB_CBKS_STAGE3_MAX];

extern const opus_int16 silk_Resampler_3_4_COEFS[2 + 3 * RESAMPLER_DOWN_ORDER_FIR0/2];
extern const opus_int16 silk_Resampler_1_2_COEFS[2 + RESAMPLER_DOWN_ORDER_FIR1/2];
extern const opus_int16 silk_resampler_frac_FIR_12[12][RESAMPLER_ORDER_FIR_12/2];

// Wrappers to expose flat byte views.
static const unsigned char* c_gain_iCDF(void)          { return (const unsigned char*)silk_gain_iCDF; }
static int c_gain_iCDF_len(void)                        { return 3 * (N_LEVELS_QGAIN/8); }
static const unsigned char* c_delta_gain_iCDF(void)    { return silk_delta_gain_iCDF; }
static int c_delta_gain_iCDF_len(void)                  { return sizeof(silk_delta_gain_iCDF); }
static const unsigned char* c_pitch_lag_iCDF(void)     { return silk_pitch_lag_iCDF; }
static int c_pitch_lag_iCDF_len(void)                   { return sizeof(silk_pitch_lag_iCDF); }
static const unsigned char* c_pitch_delta_iCDF(void)   { return silk_pitch_delta_iCDF; }
static const unsigned char* c_pitch_contour_iCDF(void) { return silk_pitch_contour_iCDF; }
static const unsigned char* c_pitch_contour_NB_iCDF(void) { return silk_pitch_contour_NB_iCDF; }
static const unsigned char* c_pitch_contour_10ms_iCDF(void) { return silk_pitch_contour_10_ms_iCDF; }
static const unsigned char* c_pitch_contour_10ms_NB_iCDF(void) { return silk_pitch_contour_10_ms_NB_iCDF; }
static const short* c_stereo_pred_quant_Q13(void) { return silk_stereo_pred_quant_Q13; }
static const unsigned char* c_stereo_pred_joint_iCDF(void) { return silk_stereo_pred_joint_iCDF; }
static const unsigned char* c_LTPscale_iCDF(void) { return silk_LTPscale_iCDF; }
static const unsigned char* c_LTP_per_index_iCDF(void) { return silk_LTP_per_index_iCDF; }
static const unsigned char* c_sign_iCDF(void) { return silk_sign_iCDF; }
static const unsigned char* c_shell_code_table0(void) { return silk_shell_code_table0; }
static const unsigned char* c_shell_code_table3(void) { return silk_shell_code_table3; }
static const int* c_transition_LP_B_Q28(void) { return (const int*)silk_Transition_LP_B_Q28; }

static const signed char* c_CB_lags_stage2(void) { return (const signed char*)silk_CB_lags_stage2; }
static const signed char* c_CB_lags_stage3(void) { return (const signed char*)silk_CB_lags_stage3; }

static const short* c_Resampler_3_4(void) { return silk_Resampler_3_4_COEFS; }
static const short* c_Resampler_1_2(void) { return silk_Resampler_1_2_COEFS; }
static const short* c_resampler_frac_FIR_12(void) { return (const short*)silk_resampler_frac_FIR_12; }

// NLSF codebook data reachable via the public struct pointers
// (silk_NLSF_CB_NB_MB / silk_NLSF_CB_WB in silk/tables.h); the raw
// arrays themselves are file-static in the source and not linkable
// directly.
#include "silk/structs.h"
extern const silk_NLSF_CB_struct silk_NLSF_CB_NB_MB;
extern const silk_NLSF_CB_struct silk_NLSF_CB_WB;

static const unsigned char* c_NLSF_CB1_NB_MB_Q8(void) { return silk_NLSF_CB_NB_MB.CB1_NLSF_Q8; }
static const unsigned char* c_NLSF_CB1_WB_Q8(void)    { return silk_NLSF_CB_WB.CB1_NLSF_Q8; }
static const short*         c_NLSF_CB1_Wght_Q9(void)  { return silk_NLSF_CB_NB_MB.CB1_Wght_Q9; }
static const short*         c_NLSF_CB1_WB_Wght_Q9(void){ return silk_NLSF_CB_WB.CB1_Wght_Q9; }
*/
import "C"
import "unsafe"

func cBytes(p unsafe.Pointer, n int) []byte {
	if n <= 0 {
		return nil
	}
	return C.GoBytes(p, C.int(n))
}

func cInt16s(p unsafe.Pointer, n int) []int16 {
	out := make([]int16, n)
	src := unsafe.Slice((*int16)(p), n)
	copy(out, src)
	return out
}

func cInt32s(p unsafe.Pointer, n int) []int32 {
	out := make([]int32, n)
	src := unsafe.Slice((*int32)(p), n)
	copy(out, src)
	return out
}

func cInt8s(p unsafe.Pointer, n int) []int8 {
	out := make([]int8, n)
	src := unsafe.Slice((*int8)(p), n)
	copy(out, src)
	return out
}

func cSilkGainICDF() []byte { return cBytes(unsafe.Pointer(C.c_gain_iCDF()), int(C.c_gain_iCDF_len())) }
func cSilkDeltaGainICDF() []byte {
	return cBytes(unsafe.Pointer(C.c_delta_gain_iCDF()), int(C.c_delta_gain_iCDF_len()))
}
func cSilkPitchLagICDF() []byte {
	return cBytes(unsafe.Pointer(C.c_pitch_lag_iCDF()), int(C.c_pitch_lag_iCDF_len()))
}
func cSilkPitchDeltaICDF() []byte     { return cBytes(unsafe.Pointer(C.c_pitch_delta_iCDF()), 21) }
func cSilkPitchContourICDF() []byte   { return cBytes(unsafe.Pointer(C.c_pitch_contour_iCDF()), 34) }
func cSilkPitchContourNBICDF() []byte { return cBytes(unsafe.Pointer(C.c_pitch_contour_NB_iCDF()), 11) }
func cSilkPitchContour10msICDF() []byte {
	return cBytes(unsafe.Pointer(C.c_pitch_contour_10ms_iCDF()), 12)
}
func cSilkPitchContour10msNBICDF() []byte {
	return cBytes(unsafe.Pointer(C.c_pitch_contour_10ms_NB_iCDF()), 3)
}
func cSilkStereoPredQuantQ13() []int16 {
	return cInt16s(unsafe.Pointer(C.c_stereo_pred_quant_Q13()), 16)
}
func cSilkStereoPredJointICDF() []byte {
	return cBytes(unsafe.Pointer(C.c_stereo_pred_joint_iCDF()), 25)
}
func cSilkLTPscaleICDF() []byte    { return cBytes(unsafe.Pointer(C.c_LTPscale_iCDF()), 3) }
func cSilkLTPPerIndexICDF() []byte { return cBytes(unsafe.Pointer(C.c_LTP_per_index_iCDF()), 3) }
func cSilkSignICDF() []byte        { return cBytes(unsafe.Pointer(C.c_sign_iCDF()), 42) }
func cSilkShellCodeTable0() []byte { return cBytes(unsafe.Pointer(C.c_shell_code_table0()), 152) }
func cSilkShellCodeTable3() []byte { return cBytes(unsafe.Pointer(C.c_shell_code_table3()), 152) }
func cSilkTransitionLPBQ28() []int32 {
	return cInt32s(unsafe.Pointer(C.c_transition_LP_B_Q28()), 5*3)
}

func cSilkCBLagsStage2() []int8 {
	return cInt8s(unsafe.Pointer(C.c_CB_lags_stage2()), 4*11)
}
func cSilkCBLagsStage3() []int8 {
	return cInt8s(unsafe.Pointer(C.c_CB_lags_stage3()), 4*34)
}
func cSilkResampler34() []int16 { return cInt16s(unsafe.Pointer(C.c_Resampler_3_4()), 2+3*18/2) }
func cSilkResampler12() []int16 { return cInt16s(unsafe.Pointer(C.c_Resampler_1_2()), 2+24/2) }
func cSilkResamplerFracFIR12() []int16 {
	return cInt16s(unsafe.Pointer(C.c_resampler_frac_FIR_12()), 12*4)
}

func cSilkNLSFCB1NBMBQ8() []byte    { return cBytes(unsafe.Pointer(C.c_NLSF_CB1_NB_MB_Q8()), 320) }
func cSilkNLSFCB1WBQ8() []byte      { return cBytes(unsafe.Pointer(C.c_NLSF_CB1_WB_Q8()), 512) }
func cSilkNLSFCB1WghtQ9() []int16   { return cInt16s(unsafe.Pointer(C.c_NLSF_CB1_Wght_Q9()), 320) }
func cSilkNLSFCB1WBWghtQ9() []int16 { return cInt16s(unsafe.Pointer(C.c_NLSF_CB1_WB_Wght_Q9()), 512) }
