//go:build cgo

package benchcmp

/*
#include "config.h"
#include "typedef.h"
#include "structs.h"
#include "main.h"
#include "API.h"
#include "control.h"
#include "entenc.h"
#include "structs_FLP.h"
#include <string.h>
#include <stdlib.h>

// silk_enc_mono_cfg mirrors benchcmp's SilkEncMonoCfg. All fields are
// C int32 for predictable marshalling.
struct silk_enc_mono_cfg {
    int nChannelsAPI, nChannelsInternal;
    int apiSampleRate;
    int maxInternalSampleRate, minInternalSampleRate, desiredInternalSampleRate;
    int payloadSizeMs;
    int bitRate;
    int packetLossPct;
    int complexity;
    int useInBandFEC, LBRRCoded, useDTX, useCBR;
    int maxBits;
    int toMono, opusCanSwitch, reducedDependency;
    int internalSampleRate;
};

// c_silk_enc_new allocates a silk_encoder, inits it, and returns the
// pointer + the control struct (zero-init).
static void *c_silk_enc_new(int channels, int arch, silk_EncControlStruct *outCtl) {
    void *mem = calloc(1, sizeof(silk_encoder));
    silk_InitEncoder(mem, channels, arch, outCtl);
    return mem;
}

// c_silk_enc_free releases the encoder memory.
static void c_silk_enc_free(void *p) { free(p); }

static void apply_cfg(silk_EncControlStruct *c, const struct silk_enc_mono_cfg *in) {
    c->nChannelsAPI              = in->nChannelsAPI;
    c->nChannelsInternal         = in->nChannelsInternal;
    c->API_sampleRate            = in->apiSampleRate;
    c->maxInternalSampleRate     = in->maxInternalSampleRate;
    c->minInternalSampleRate     = in->minInternalSampleRate;
    c->desiredInternalSampleRate = in->desiredInternalSampleRate;
    c->payloadSize_ms            = in->payloadSizeMs;
    c->bitRate                   = in->bitRate;
    c->packetLossPercentage      = in->packetLossPct;
    c->complexity                = in->complexity;
    c->useInBandFEC              = in->useInBandFEC;
    c->LBRR_coded                = in->LBRRCoded;
    c->useDTX                    = in->useDTX;
    c->useCBR                    = in->useCBR;
    c->maxBits                   = in->maxBits;
    c->toMono                    = in->toMono;
    c->opusCanSwitch             = in->opusCanSwitch;
    c->reducedDependency         = in->reducedDependency;
    c->internalSampleRate        = in->internalSampleRate;
}

// c_silk_encode_frame drives a single silk_Encode call with the given
// PCM (float32, opus_res) and config. Returns ret + nBytesOut via
// out-params and copies up to maxBytes into outBuf; copies pulses into
// outPulses (nb = frame_length); outputs rng/signalType.
static int c_silk_encode_frame(
    void *psEnc, silk_EncControlStruct *ctl,
    const struct silk_enc_mono_cfg *cfg,
    const opus_res *pcm, int nSamplesIn,
    unsigned char *outBuf, int maxBytes,
    int *nBytesOut,
    signed char *outPulses, int *nPulses,
    unsigned int *outRng, int *outSignalType,
    int prefillFlag, int activity
) {
    ec_enc rng;
    ec_enc_init(&rng, outBuf, maxBytes);

    apply_cfg(ctl, cfg);

    opus_int32 nb = (opus_int32)maxBytes;
    int r = silk_Encode(psEnc, ctl, pcm, nSamplesIn, &rng, &nb, prefillFlag, activity);

    ec_enc_done(&rng);

    *nBytesOut = (int)nb;

    silk_encoder *se = (silk_encoder *)psEnc;
    int fl = se->state_Fxx[0].sCmn.frame_length;
    if (fl > *nPulses) fl = *nPulses;
    memcpy(outPulses, se->state_Fxx[0].sCmn.pulses, fl);
    *nPulses = fl;

    *outRng = rng.rng;
    *outSignalType = se->state_Fxx[0].sCmn.indices.signalType;

    return r;
}
*/
import "C"

import (
	"unsafe"
)

// CSilkEncoder wraps a C silk_encoder.
type CSilkEncoder struct {
	p   unsafe.Pointer
	ctl C.silk_EncControlStruct
}

// SilkEncMonoCfg mirrors the silk_enc_mono_cfg C struct and the Go
// ExportTestSilkEncoder_Cfg.
type SilkEncMonoCfg struct {
	NChannelsAPI              int
	NChannelsInternal         int
	APISampleRate             int
	MaxInternalSampleRate     int
	MinInternalSampleRate     int
	DesiredInternalSampleRate int
	PayloadSizeMs             int
	BitRate                   int
	PacketLossPct             int
	Complexity                int
	UseInBandFEC              int
	LBRRCoded                 int
	UseDTX                    int
	UseCBR                    int
	MaxBits                   int
	ToMono                    int
	OpusCanSwitch             int
	ReducedDependency         int
	InternalSampleRate        int
}

func (cfg *SilkEncMonoCfg) toC() C.struct_silk_enc_mono_cfg {
	return C.struct_silk_enc_mono_cfg{
		nChannelsAPI:              C.int(cfg.NChannelsAPI),
		nChannelsInternal:         C.int(cfg.NChannelsInternal),
		apiSampleRate:             C.int(cfg.APISampleRate),
		maxInternalSampleRate:     C.int(cfg.MaxInternalSampleRate),
		minInternalSampleRate:     C.int(cfg.MinInternalSampleRate),
		desiredInternalSampleRate: C.int(cfg.DesiredInternalSampleRate),
		payloadSizeMs:             C.int(cfg.PayloadSizeMs),
		bitRate:                   C.int(cfg.BitRate),
		packetLossPct:             C.int(cfg.PacketLossPct),
		complexity:                C.int(cfg.Complexity),
		useInBandFEC:              C.int(cfg.UseInBandFEC),
		LBRRCoded:                 C.int(cfg.LBRRCoded),
		useDTX:                    C.int(cfg.UseDTX),
		useCBR:                    C.int(cfg.UseCBR),
		maxBits:                   C.int(cfg.MaxBits),
		toMono:                    C.int(cfg.ToMono),
		opusCanSwitch:             C.int(cfg.OpusCanSwitch),
		reducedDependency:         C.int(cfg.ReducedDependency),
		internalSampleRate:        C.int(cfg.InternalSampleRate),
	}
}

// NewCSilkEncoder allocates + inits a C SILK encoder (mono only for
// this Phase 8 capstone).
func NewCSilkEncoder(channels, arch int) *CSilkEncoder {
	e := &CSilkEncoder{}
	e.p = C.c_silk_enc_new(C.int(channels), C.int(arch), &e.ctl)
	return e
}

// Free releases the underlying silk_encoder memory.
func (e *CSilkEncoder) Free() {
	if e.p != nil {
		C.c_silk_enc_free(e.p)
		e.p = nil
	}
}

// EncodeFrame drives one silk_Encode call. pkt is the output buffer
// (maxBytes). Returns (ret, nBytesOut, pulses, signalType, rng).
func (e *CSilkEncoder) EncodeFrame(cfg SilkEncMonoCfg, pcm []float32, pkt []byte, prefillFlag, activity int) (int, int, []int8, int, uint32) {
	ccfg := cfg.toC()
	var nb C.int
	pulses := make([]int8, 1024) // frame_length upper bound
	nPulses := C.int(len(pulses))
	var rng C.uint
	var signalType C.int
	var pcmPtr *C.opus_res
	if len(pcm) > 0 {
		pcmPtr = (*C.opus_res)(unsafe.Pointer(&pcm[0]))
	}
	var pktPtr *C.uchar
	if len(pkt) > 0 {
		pktPtr = (*C.uchar)(unsafe.Pointer(&pkt[0]))
	}
	r := C.c_silk_encode_frame(e.p, &e.ctl, &ccfg,
		pcmPtr, C.int(len(pcm)),
		pktPtr, C.int(len(pkt)),
		&nb,
		(*C.schar)(unsafe.Pointer(&pulses[0])), &nPulses,
		&rng, &signalType,
		C.int(prefillFlag), C.int(activity))
	pulses = pulses[:int(nPulses)]
	return int(r), int(nb), pulses, int(signalType), uint32(rng)
}
