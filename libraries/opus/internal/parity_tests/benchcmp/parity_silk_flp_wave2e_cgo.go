//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FLP.h"
#include "main_FLP.h"
#include "tuning_parameters.h"
#include "structs.h"
#include <string.h>
#include <stdlib.h>

// Parity harness for silk_noise_shape_analysis_FLP.
//
// Payload mirrors SilkNoiseShapeAnalysisFLPPayload on the Go side.
// Input/output arrays use flat fixed-size buffers so they can be passed
// through a single cgo struct without additional copy shims.

struct nsa_payload {
    // Inputs (sCmn).
    int   Fs_kHz;
    int   nb_subfr;
    int   la_shape;
    int   shapeWinLength;
    int   shapingLPCOrder;
    int   subfr_length;
    int   SNR_dB_Q7;
    int   input_quality_bands_Q15[VAD_N_BANDS];
    int   useCBR;
    int   speech_activity_Q8;
    signed char signalType_in;
    int   warping_Q16;
    int   arch;

    // Encoder control inputs.
    float LTPCorr_in;
    float predGain;
    int   pitchL[MAX_NB_SUBFR];

    // Shape-state smoothing memory (in/out).
    float HarmShapeGain_smth_in;
    float Tilt_smth_in;
    float HarmShapeGain_smth_out;
    float Tilt_smth_out;

    // Outputs.
    signed char quantOffsetType;
    float input_quality_out;
    float coding_quality_out;
    float Gains[MAX_NB_SUBFR];
    float AR[MAX_NB_SUBFR * MAX_SHAPE_LPC_ORDER];
    float LF_MA_shp[MAX_NB_SUBFR];
    float LF_AR_shp[MAX_NB_SUBFR];
    float Tilt[MAX_NB_SUBFR];
    float HarmShapeGain[MAX_NB_SUBFR];
};

static void c_silk_noise_shape_analysis_flp(
    struct nsa_payload *p,
    const float *pitch_res,
    const float *x   // pointer to x[0] inside a larger buffer
) {
    silk_encoder_state_FLP psEnc;
    silk_encoder_control_FLP psEncCtrl;
    memset(&psEnc, 0, sizeof(psEnc));
    memset(&psEncCtrl, 0, sizeof(psEncCtrl));

    psEnc.sCmn.fs_kHz              = p->Fs_kHz;
    psEnc.sCmn.nb_subfr            = p->nb_subfr;
    psEnc.sCmn.la_shape            = p->la_shape;
    psEnc.sCmn.shapeWinLength      = p->shapeWinLength;
    psEnc.sCmn.shapingLPCOrder     = p->shapingLPCOrder;
    psEnc.sCmn.subfr_length        = p->subfr_length;
    psEnc.sCmn.SNR_dB_Q7           = p->SNR_dB_Q7;
    for (int i = 0; i < VAD_N_BANDS; i++) {
        psEnc.sCmn.input_quality_bands_Q15[i] = p->input_quality_bands_Q15[i];
    }
    psEnc.sCmn.useCBR              = p->useCBR;
    psEnc.sCmn.speech_activity_Q8  = p->speech_activity_Q8;
    psEnc.sCmn.indices.signalType  = p->signalType_in;
    psEnc.sCmn.warping_Q16         = p->warping_Q16;
    psEnc.sCmn.arch                = p->arch;

    psEnc.LTPCorr                  = p->LTPCorr_in;
    psEncCtrl.predGain             = p->predGain;
    for (int i = 0; i < MAX_NB_SUBFR; i++) {
        psEncCtrl.pitchL[i] = p->pitchL[i];
    }

    psEnc.sShape.HarmShapeGain_smth = p->HarmShapeGain_smth_in;
    psEnc.sShape.Tilt_smth          = p->Tilt_smth_in;

    silk_noise_shape_analysis_FLP(&psEnc, &psEncCtrl, pitch_res, x);

    p->quantOffsetType       = psEnc.sCmn.indices.quantOffsetType;
    p->input_quality_out     = psEncCtrl.input_quality;
    p->coding_quality_out    = psEncCtrl.coding_quality;
    p->HarmShapeGain_smth_out = psEnc.sShape.HarmShapeGain_smth;
    p->Tilt_smth_out          = psEnc.sShape.Tilt_smth;
    for (int i = 0; i < MAX_NB_SUBFR; i++) {
        p->Gains[i]         = psEncCtrl.Gains[i];
        p->LF_MA_shp[i]     = psEncCtrl.LF_MA_shp[i];
        p->LF_AR_shp[i]     = psEncCtrl.LF_AR_shp[i];
        p->Tilt[i]          = psEncCtrl.Tilt[i];
        p->HarmShapeGain[i] = psEncCtrl.HarmShapeGain[i];
    }
    for (int i = 0; i < MAX_NB_SUBFR * MAX_SHAPE_LPC_ORDER; i++) {
        p->AR[i] = psEncCtrl.AR[i];
    }
}
*/
import "C"
import (
	"unsafe"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func cSilkNoiseShapeAnalysisFLP(in nativeopus.SilkNoiseShapeAnalysisFLPPayload) nativeopus.SilkNoiseShapeAnalysisFLPPayload {
	cp := (*C.struct_nsa_payload)(C.calloc(1, C.size_t(unsafe.Sizeof(C.struct_nsa_payload{}))))
	defer C.free(unsafe.Pointer(cp))

	cp.Fs_kHz = C.int(in.FsKHz)
	cp.nb_subfr = C.int(in.NbSubfr)
	cp.la_shape = C.int(in.LaShape)
	cp.shapeWinLength = C.int(in.ShapeWinLength)
	cp.shapingLPCOrder = C.int(in.ShapingLPCOrder)
	cp.subfr_length = C.int(in.SubfrLength)
	cp.SNR_dB_Q7 = C.int(in.SNR_dB_Q7)
	for i := 0; i < 4; i++ { // VAD_N_BANDS = 4
		cp.input_quality_bands_Q15[i] = C.int(in.InputQualityBandsQ15[i])
	}
	cp.useCBR = C.int(in.UseCBR)
	cp.speech_activity_Q8 = C.int(in.SpeechActivityQ8)
	cp.signalType_in = C.schar(in.SignalType)
	cp.warping_Q16 = C.int(in.WarpingQ16)
	cp.arch = C.int(in.Arch)

	cp.LTPCorr_in = C.float(in.LTPCorrIn)
	cp.predGain = C.float(in.PredGain)
	for i := 0; i < 4; i++ { // MAX_NB_SUBFR = 4
		cp.pitchL[i] = C.int(in.PitchL[i])
	}
	cp.HarmShapeGain_smth_in = C.float(in.HarmShapeGainSmthIn)
	cp.Tilt_smth_in = C.float(in.TiltSmthIn)

	var pitchResPtr *C.float
	if len(in.PitchRes) > 0 {
		pitchResPtr = (*C.float)(unsafe.Pointer(&in.PitchRes[0]))
	}
	xPtr := (*C.float)(unsafe.Pointer(&in.X[in.XOff]))

	C.c_silk_noise_shape_analysis_flp(cp, pitchResPtr, xPtr)

	out := in
	out.QuantOffsetType = int8(cp.quantOffsetType)
	out.InputQuality = float32(cp.input_quality_out)
	out.CodingQuality = float32(cp.coding_quality_out)
	out.HarmShapeGainSmthOut = float32(cp.HarmShapeGain_smth_out)
	out.TiltSmthOut = float32(cp.Tilt_smth_out)
	for i := 0; i < 4; i++ {
		out.Gains[i] = float32(cp.Gains[i])
		out.LF_MA_shp[i] = float32(cp.LF_MA_shp[i])
		out.LF_AR_shp[i] = float32(cp.LF_AR_shp[i])
		out.Tilt[i] = float32(cp.Tilt[i])
		out.HarmShapeGain[i] = float32(cp.HarmShapeGain[i])
	}
	// MAX_NB_SUBFR * MAX_SHAPE_LPC_ORDER = 4 * 24 = 96
	for i := 0; i < 96; i++ {
		out.AR[i] = float32(cp.AR[i])
	}
	return out
}
