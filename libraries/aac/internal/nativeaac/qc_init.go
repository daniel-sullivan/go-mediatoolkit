// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Pure-Go 1:1 port of the quantizer/coder allocation + initialisation entry
// points FDKaacEnc_QCNew and FDKaacEnc_QCInit (libAACenc/src/qc_main.cpp). QCNew
// allocates the QC_STATE plus its ADJ_THR_STATE (hAdjThr) and bit-counter
// (hBitCounter); QCInit copies the QC_INIT config into the state, runs
// InitElementBits, picks the VBR quality factor (0 on the CBR path) and dead-zone
// quantizer flag, and seeds the threshold-adjustment state via AdjThrInit.
//
// AAC-LC CBR only. Pure integer / pointer wiring; aacfdk-fenced.

package nativeaac

// QcInit is the 1:1 model of struct QC_INIT (qc_data.h:149): the configuration
// FDKaacEnc_QCInit consumes. AAC-LC CBR uses the same fields the C struct
// declares; the unused sceCpe / chBitrate / nSubFrames helpers are carried for
// fidelity.
type QcInit struct {
	ChannelMapping      *ChannelMapping  // channelMapping
	SceCpe              int              // sceCpe (not used yet)
	MaxBits             int              // maxBits
	AverageBits         int              // averageBits
	BitRes              int              // bitRes
	SampleRate          int              // sampleRate
	IsLowDelay          int              // isLowDelay
	StaticBits          int              // staticBits
	BitrateMode         QcdataBrMode     // bitrateMode
	MeanPe              int              // meanPe
	ChBitrate           int              // chBitrate
	InvQuant            int              // invQuant
	MaxIterations       int              // maxIterations
	MaxBitFac           int32            // maxBitFac (FIXP_DBL)
	Bitrate             int              // bitrate
	NSubFrames          int              // nSubFrames
	MinBits             int              // minBits
	BitResMode          AacencBitresMode // bitResMode
	BitDistributionMode int              // bitDistributionMode
	Padding             Padding          // padding
}

// tableVbrQualFactor is the 1:1 transcription of tableVbrQualFactor
// (qc_main.cpp:122): the per-VBR-mode quality factor (FIXP_DBL hex). Unused on
// the CBR path (QCInit leaves vbrQualFactor at 0) but carried for a faithful
// lookup loop.
var tableVbrQualFactor = []struct {
	bitrateMode   QcdataBrMode
	vbrQualFactor int32
}{
	{QcdataBrModeVBR1, fl2fxconstDBL(0.150)},
	{QcdataBrModeVBR2, fl2fxconstDBL(0.162)},
	{QcdataBrModeVBR3, fl2fxconstDBL(0.176)},
	{QcdataBrModeVBR4, fl2fxconstDBL(0.120)},
	{QcdataBrModeVBR5, fl2fxconstDBL(0.070)},
}

// QCNew is the 1:1 port of FDKaacEnc_QCNew (qc_main.cpp:314): allocate the
// QC_STATE, its ADJ_THR_STATE (FDKaacEnc_AdjThrNew), its bit-counter
// (FDKaacEnc_BCNew) and the per-element ELEMENT_BITS pool. The Go port allocates
// the equivalent struct graph (one Go allocation per GetRam_* call). Returns the
// handle and AAC_ENC_OK.
func QCNew(nElements int) (*QcState, EncoderError) {
	hQC := new(QcState)

	hQC.HAdjThr = new(adjThrState)
	adjThrNew(hQC.HAdjThr, nElements)

	// FDKaacEnc_BCNew: the bit counter is a plain zero-initialised scratch state.
	hQC.HBitCounter = new(bitCntrState)

	for i := 0; i < nElements; i++ {
		hQC.ElementBits[i] = new(ElementBits)
	}

	return hQC, AacEncOK
}

// QCInit is the 1:1 port of FDKaacEnc_QCInit (qc_main.cpp:358): copy the QC_INIT
// config into the QC_STATE, set the bit-reservoir levels (reset bitResTot on a
// fresh init / changed reservoir size), run InitElementBits over the channel
// mapping, look up the VBR quality factor (0 for CBR), decide the dead-zone
// quantizer flag, and seed the ADJ_THR_STATE through AdjThrInit. initFlags!=0
// forces a bit-reservoir reset (cold start).
func QCInit(hQC *QcState, init *QcInit, initFlags uint32) EncoderError {
	var err EncoderError = AacEncOK

	hQC.MaxBitsPerFrame = init.MaxBits
	hQC.MinBitsPerFrame = init.MinBits
	hQC.NElements = init.ChannelMapping.NElements
	if (initFlags != 0) ||
		((init.BitrateMode != QcdataBrModeFF) && (hQC.BitResTotMax != init.BitRes)) {
		hQC.BitResTot = init.BitRes
	}
	hQC.BitResTotMax = init.BitRes
	hQC.MaxBitFac = init.MaxBitFac
	hQC.BitrateMode = init.BitrateMode
	hQC.InvQuant = init.InvQuant
	hQC.MaxIterations = init.MaxIterations

	// 0: full bitreservoir, 1: reduced bitreservoir, 2: disabled bitreservoir
	hQC.BitResMode = init.BitResMode

	hQC.Padding.PaddingRest = init.Padding.PaddingRest

	hQC.GlobHdrBits = init.StaticBits // Bit overhead due to transport

	err = InitElementBits(hQC, init.ChannelMapping, init.Bitrate,
		(init.AverageBits/init.NSubFrames)-hQC.GlobHdrBits,
		hQC.MaxBitsPerFrame/init.ChannelMapping.NChannelsEff)
	if err != AacEncOK {
		return err
	}

	hQC.VbrQualFactor = fl2fxconstDBL(0.0)
	for i := 0; i < len(tableVbrQualFactor); i++ {
		if hQC.BitrateMode == tableVbrQualFactor[i].bitrateMode {
			hQC.VbrQualFactor = tableVbrQualFactor[i].vbrQualFactor
			break
		}
	}

	if init.ChannelMapping.NChannelsEff == 1 &&
		(init.Bitrate/init.ChannelMapping.NChannelsEff) < aacencDzqBrThr &&
		init.IsLowDelay != 0 {
		hQC.DZoneQuantEnable = 1
	} else {
		hQC.DZoneQuantEnable = 0
	}

	adjThrInit(
		hQC.HAdjThr, init.MeanPe, hQC.InvQuant, init.ChannelMapping,
		init.SampleRate, // output sample rate
		init.Bitrate,    // total bitrate
		init.IsLowDelay, // if set, calc bits2PE factor depending on samplerate
		init.BitResMode, // for a small bitreservoir, the pe correction is calc'd differently
		hQC.DZoneQuantEnable, init.BitDistributionMode, hQC.VbrQualFactor)

	return err
}
