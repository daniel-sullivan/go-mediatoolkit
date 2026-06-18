// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// This file ports the SBR decoder state buffers/handles 1:1 from the vendored
// libSBRdec headers (sbr_ram.h, sbr_dec.h) — the SBR_DEC / SBR_CHANNEL /
// SBR_DECODER_ELEMENT / SBR_DECODER_INSTANCE struct definitions that hold the
// per-channel decode state and the per-element delay-line bookkeeping. The
// matching C allocations live in sbr_ram.cpp:1-188 (H_ALLOC_MEM macros); in Go
// the slices/arrays are allocated inline by the create/init helpers below and in
// qmf_domain.go.
//
// HE-AAC v1 (STD) scope: the HBE/USAC (hQmfHBESlots*, codecQMFBuffer*,
// tmp_memory, hHBE, savedStates), PVC (PvcStaticData) and DRC (sbrDrcChannel)
// fields are EXCLUDED — they belong to HE-AAC v2 / USAC / DRM which are out of
// scope. The structs carry only the fields the legacy SBR decode path touches.

// SBRDEC_* runtime flag bits (env_extr.h:204-237). Only the HE-AAC v1 subset is
// reachable; the USAC/ELD/DRM/PS bits are defined for faithful flag arithmetic
// but their code paths are excluded.
const (
	sbrdecEldGridFlag    = 1     // SBRDEC_ELD_GRID (also in env_extr.go as sbrdecEldGrid)
	sbrdecLowPower       = 32    // SBRDEC_LOW_POWER
	sbrdecPsDecoded      = 64    // SBRDEC_PS_DECODED
	sbrdecLdMpsQmf       = 512   // SBRDEC_LD_MPS_QMF
	sbrdecSyntaxDrm      = 2048  // SBRDEC_SYNTAX_DRM
	sbrdecEldDownscale   = 4096  // SBRDEC_ELD_DOWNSCALE
	sbrdecDownsample     = 8192  // SBRDEC_DOWNSAMPLE
	sbrdecFlush          = 16384 // SBRDEC_FLUSH
	sbrdecForceReset     = 32768 // SBRDEC_FORCE_RESET
	sbrdecSkipQmfAna     = 1 << 21
	sbrdecSkipQmfSyn     = 1 << 22
	sbrdecUsacHarmonicSb = 256 // SBRDEC_USAC_HARMONICSBR (== sbrdecUsacHarmSbr)
)

// SBRDEC_HDR_STAT_* header status bits (env_extr.h:239-240).
const (
	sbrdecHdrStatReset  = 1 // SBRDEC_HDR_STAT_RESET
	sbrdecHdrStatUpdate = 2 // SBRDEC_HDR_STAT_UPDATE
)

// FRAME_OK / FRAME_ERROR (sbr_ram.h:117-119).
const (
	frameOk            = 0
	frameError         = 1
	frameErrorAllSlots = 2
)

// MP4 element IDs the SBR element bookkeeping uses (mp4 syntax: ID_SCE/ID_CPE/
// ID_LFE). Mirrors the AAC-LC core element IDs (idSCE/idCPE in nativeaac) but
// kept local to the sbr package.
const (
	mp4ElementSCE  = 0 // ID_SCE
	mp4ElementCPE  = 1 // ID_CPE
	mp4ElementLFE  = 3 // ID_LFE
	mp4ElementNone = -1
)

// AOT core codec values (FDK_audio.h:163-190) the SBR decoder dispatch checks.
const (
	aotAACLC = 2  // AOT_AAC_LC
	aotSBR   = 5  // AOT_SBR
	aotPS    = 29 // AOT_PS (includes SBR)
)

// sbrDecChannelMax is SBRDEC_MAX_CH_PER_ELEMENT (sbr_ram.h:115).
const sbrDecChannelMax = 2

// SbrDec is SBR_DEC (sbr_dec.h:129-165): the per-channel decoder state. HE-AAC
// v1 subset — HBE/PVC/DRC fields excluded (see file header).
type SbrDec struct {
	SbrCalculateEnvelope SbrCalculateEnvelope // SbrCalculateEnvelope
	LppTrans             sbrLppTrans          // LppTrans

	// "do scale handling in sbr and not in qmf" (sbr_dec.h:135-137).
	scaleOv  int16 // scale_ov
	scaleLb  int16 // scale_lb
	scaleHbe int16 // scale_hbe

	prevFrameLSbr   int16 // prev_frame_lSbr
	prevFrameHbeSbr int16 // prev_frame_hbeSbr

	codecFrameSize int // codecFrameSize

	// qmfDomainInCh / qmfDomainOutCh (sbr_dec.h:146-147): the QMF-domain in/out
	// channel handles. In Go they point into the SbrDecoderInstance's QmfDomain
	// (assigned by sbrDecoder_AssignQmfChannels2SbrChannels).
	qmfDomainInCh  *qmfDomainIn  // qmfDomainInCh
	qmfDomainOutCh *qmfDomainOut // qmfDomainOutCh

	savedStates     uint8 // savedStates (HBE; stays 0 for legacy SBR)
	applySbrProcOld int   // applySbrProc_old
}

// SbrChannel is SBR_CHANNEL (sbr_dec.h:169-173): two frame-data slots (the +1
// delay slot), the previous-frame data, and the per-channel decoder state.
type SbrChannel struct {
	frameData     [2]SbrFrameData  // frameData[(1)+1]
	prevFrameData SbrPrevFrameData // prevFrameData
	SbrDec        SbrDec           // SbrDec
}

// SbrDecoderElement is SBR_DECODER_ELEMENT (sbr_ram.h:121-143): one SBR element
// (SCE or CPE), its channels, the shared transposer settings, and the frame/
// header delay-slot bookkeeping.
type SbrDecoderElement struct {
	pSbrChannel        [sbrDecChannelMax]*SbrChannel // pSbrChannel
	transposerSettings transposerSettings            // transposerSettings (shared)

	elementID int // elementID (ID_SCE/ID_CPE/ID_LFE)
	nChannels int // nChannels (output channels of the element)

	frameErrorFlag [2]uint8 // frameErrorFlag[(1)+1] (per delay slot)
	useFrameSlot   uint8    // useFrameSlot
	useHeaderSlot  [2]uint8 // useHeaderSlot[(1)+1]
}

// SbrDecoderInstance is struct SBR_DECODER_INSTANCE (sbr_ram.h:145-176): the
// top-level SBR decoder, owning the per-element handles, the per-element header
// delay slots, the shared QMF domain, and the global config.
type SbrDecoderInstance struct {
	pSbrElement [8]*SbrDecoderElement // pSbrElement[(8)]
	sbrHeader   [8][2]SbrHeaderData   // sbrHeader[(8)][(1)+1]

	pQmfDomain *qmfDomain // pQmfDomain

	// hParametricStereoDec is the single shared PS decoder handle
	// (SBR_DECODER_INSTANCE::hParametricStereoDec, sbr_ram.h:160). Non-nil only
	// for a single ID_SCE element on an HE-AAC v2-capable core codec; nil for
	// the HE-AAC v1 mono/stereo path (no PS).
	hParametricStereoDec *psDec

	coreCodec        int   // coreCodec (AOT of core codec)
	numSbrElements   int   // numSbrElements
	numSbrChannels   int   // numSbrChannels
	sampleRateIn     int   // sampleRateIn
	sampleRateOut    int   // sampleRateOut
	codecFrameSize   int   // codecFrameSize (USHORT)
	synDownsampleFac uint8 // synDownsampleFac
	downscaleFactor  int   // downscaleFactor
	numDelayFrames   uint8 // numDelayFrames
	harmonicSBR      uint8 // harmonicSBR
	numFlushedFrames uint8 // numFlushedFrames

	flags uint // flags

	sbrInDataHeadroom int // sbrInDataHeadroom
}
