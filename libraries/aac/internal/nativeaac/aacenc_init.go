// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Pure-Go 1:1 port of the AAC encoder top-level configuration / allocation /
// initialisation tier of libAACenc/src/aacenc.cpp:
//
//	FDKaacEnc_CalcBitsPerFrame        (aacenc.cpp:124)
//	FDKaacEnc_CalcBitrate             (aacenc.cpp:135)
//	FDKaacEnc_LimitBitrate            (aacenc.cpp:150)
//	FDKaacEnc_AacInitDefaultConfig    (aacenc.cpp:337)
//	FDKaacEnc_Open                    (aacenc.cpp:377)
//	FDKaacEnc_Initialize              (aacenc.cpp:428)
//
// AAC-LC CBR scope. The excluded paths are ported faithfully but inert for the
// CBR/AAC-LC config: the VBR bitreservoir branch (Initialize:582-592), the
// low-delay bitreservoir interpolation (Initialize:594-608), the ELD downscale,
// ancillary-data (anc_Rate==0 on the default config) and the LD-only
// maxIterations==2. HE-AAC/SBR/ELD/DRC are out of scope (syntaxFlags has no
// AC_SBR_PRESENT). The transport-encoder static-bit demand
// (transportEnc_GetStaticBits) is supplied by the caller as a closure so the
// AAC core stays decoupled from the transport library (for plain raw/ADTS the
// genuine function is deterministic); on the parity path the oracle threads the
// genuine transportEnc_GetStaticBits with an identical hTpEnc.
//
// Pure integer / fixed-point (FIXP_DBL == int32); aacfdk-fenced. The fixed-point
// helpers (fDivNorm/scaleValue/fMultI) and the leaf inits
// (InitChannelMapping/DetermineBandWidth/psyMainInit/QCOutInit/QCInit) are the
// already-ported, parity-verified Go ports — this tier only orchestrates them.

package nativeaac

// acSbrPresent is AC_SBR_PRESENT (FDK_audio.h:311) == 0x008000: the syntax-flag
// bit psyMainInit consults to derive ldSbrPresent. Plain AAC-LC clears it.
const acSbrPresent uint = 0x008000

// tnsEnableMask is TNS_ENABLE_MASK (aacenc_tns.h:123) == 0xf: the full-TNS mask
// FDKaacEnc_AacInitDefaultConfig stores in config->useTns, mapped to tnsMask in
// Initialize.
const tnsEnableMask = 0xf

// AOT_* values used by Initialize's virtual-AOT remap (aacenc.cpp:742-751) and
// psyMainInit's filterbank switch. These extend the typed AudioObjectType set
// (ratecontrol_types.go declared only the low-delay members).
const (
	aotAACLC    AudioObjectType = 2   // AOT_AAC_LC
	aotSBR      AudioObjectType = 5   // AOT_SBR
	aotMp2AACLC AudioObjectType = 129 // AOT_MP2_AAC_LC
	aotMp2SBR   AudioObjectType = 132 // AOT_MP2_SBR
)

// Bit-reservoir thresholds (aacenc.cpp:117-122). BITRES_MIN is the default
// reduced/disabled-bitres threshold; the *_LD variants are the low-delay
// interpolation bounds (inert on the AAC-LC path).
const (
	bitresMinDefault = 300   // BITRES_MIN
	bitresMaxLD      = 4000  // BITRES_MAX_LD
	bitresMinLD      = 500   // BITRES_MIN_LD
	bitrateMaxLD     = 70000 // BITRATE_MAX_LD
	bitrateMinLD     = 12000 // BITRATE_MIN_LD
)

// AC_ER_* error-resilience syntax-flag bits (FDK_audio.h:293,299). Never set on
// the AAC-LC path; consulted by Initialize's unsupported-format guards.
const (
	acErVcb11 uint = 0x000001 // AC_ER_VCB11
	acErHcr   uint = 0x000004 // AC_ER_HCR
)

// aacBrModeIsVBR mirrors AACENC_BR_MODE_IS_VBR (aacenc.h:203): true for the five
// VBR modes (1..5). CBR / SFR / FF / INVALID return false.
func aacBrModeIsVBR(m AacencBitrateMode) bool {
	return m >= 1 && m <= 5
}

// CalcBitsPerFrame / CalcBitrate / LimitBitrate (the self-contained integer
// rate-control primitives of aacenc.cpp:124/135/150) and the INT min/max helpers
// fMinI/fMaxI are already ported in ratecontrol.go — this tier reuses them.

// AacInitDefaultConfig is the 1:1 port of FDKaacEnc_AacInitDefaultConfig
// (aacenc.cpp:337): zero the config then set the documented defaults (CBR mode,
// TNS/PNS/IS/MS on, bandwidth-from-table, MPEG channel order, unset bitrate /
// framelength / channel mode). The Go AacencConfig zero value already mirrors
// FDKmemclear, so only the non-zero defaults are assigned.
func AacInitDefaultConfig(config *AacencConfig) {
	*config = AacencConfig{}

	// default ancillary
	config.AncRate = 0        // no ancillary data
	config.AncDataBitRate = 0 // no additional consumed bitrate

	// default configurations
	config.BitRate = -1     // bitrate must be set
	config.AverageBits = -1 // instead of bitrate/s we can configure bits/superframe
	config.BitrateMode = AacBitrateModeCBR
	config.BandWidth = 0    // get bandwidth from table
	config.UseTns = true    // tns enabled completely (TNS_ENABLE_MASK)
	config.UsePns = true    // depending on channelBitrate this might be set to 0 later
	config.UseIS = true     // Intensity Stereo Configuration
	config.UseMS = true     // MS Stereo tool
	config.FrameLength = -1 // Framesize not configured
	config.SyntaxFlags = 0  // default syntax with no specialities
	config.EpConfig = -1    // no ER syntax -> no additional error protection
	config.NSubFrames = 1   // default, no sub frames
	config.ChannelMode = ChannelModeUnknown
	config.MinBitsPerFrame = -1 // minimum number of bits in each AU
	config.MaxBitsPerFrame = -1 // maximum number of bits in each AU
	config.AudioMuxVersion = -1 // audio mux version not configured
	config.DownscaleFactor = 1  // normal (non-ELD) operation
}

// Open is the 1:1 port of FDKaacEnc_Open (aacenc.cpp:377): allocate the AAC_ENC
// handle and its sub-structures (PsyNew / PsyOutNew / QCOutNew / QCNew) and
// record the maxima. The C GetAACdynamic_RAM scratch pool is implicit in the Go
// struct embedding (each GetRam_* maps to a Go allocation in the leaf News).
func Open(nElements, nChannels, nSubFrames int) (*AacEnc, EncoderError) {
	hAacEnc := new(AacEnc)

	// allocate the Psy and Psy Out structures
	psyKernel, err := PsyNew(nElements, nChannels)
	if err != AacEncOK {
		return hAacEnc, err
	}
	hAacEnc.PsyKernel = psyKernel

	err = PsyOutNew(hAacEnc.PsyOut[:], nElements, nChannels, nSubFrames)
	if err != AacEncOK {
		return hAacEnc, err
	}

	// allocate the Q&C Out structure
	err = QCOutNew(hAacEnc.QcOut[:], nElements, nChannels, nSubFrames)
	if err != AacEncOK {
		return hAacEnc, err
	}

	// allocate the Q&C kernel
	qcKernel, err := QCNew(nElements)
	if err != AacEncOK {
		return hAacEnc, err
	}
	hAacEnc.QcKernel = qcKernel

	hAacEnc.MaxChannels = nChannels
	hAacEnc.MaxElements = nElements
	hAacEnc.MaxFrames = nSubFrames

	return hAacEnc, AacEncOK
}

// Initialize is the 1:1 port of FDKaacEnc_Initialize (aacenc.cpp:428) for the
// AAC-LC CBR path: validate the config, resolve the channel mapping + bandwidth,
// (re)init the psychoacoustic states, build the QC_INIT from the bit-reservoir
// arithmetic and seed the quantizer/rate-control + threshold-adjustment state via
// QCOutInit / QCInit.
//
// staticBits is the transport static-bit demand closure (== transportEnc_GetStaticBits
// bound to hTpEnc); on the AAC-LC CBR default config (anc_Rate==0, ADTS/raw) the
// only places it is consulted are the CBR minBits floor and qcInit.staticBits.
// initFlags forces a cold-start reset (psyInit + bit-reservoir reset). Returns
// the first failing leaf's error, else AAC_ENC_OK.
func Initialize(hAacEnc *AacEnc, config *AacencConfig, staticBits func(auBits int) int,
	initFlags uint32) EncoderError {

	var errorStatus EncoderError
	averageBitsPerFrame := 0
	prevChannelMode := hAacEnc.EncoderMode

	if config == nil {
		return AacEncInvalidHandle
	}

	// ---- sanity checks ----
	if config.NChannels < 1 || config.NChannels > 8 {
		return AacEncUnsupportedChannelconf
	}

	switch config.SampleRate {
	case 8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000:
	default:
		return AacEncUnsupportedSampRate
	}

	if config.BitRate == -1 {
		return AacEncUnsupportedBitrate
	}

	// check bit rate. LimitBitrate (ratecontrol.go) takes BitrateMode /
	// StaticBitsProvider; AacencBitrateMode shares identical integer values, so
	// the conversion is value-preserving.
	nChEff := getChannelModeConfiguration(config.ChannelMode).nChannelsEff
	if LimitBitrate(StaticBitsProvider(staticBits), AudioObjectType(config.AudioObjectType),
		config.SampleRate, config.FrameLength, config.NChannels, nChEff, config.BitRate,
		config.AverageBits, &averageBitsPerFrame, BitrateMode(config.BitrateMode),
		config.NSubFrames) != config.BitRate &&
		!aacBrModeIsVBR(config.BitrateMode) {
		return AacEncUnsupportedBitrate
	}

	// AAC-LC: AC_ER_VCB11 / AC_ER_HCR are never set (syntaxFlags==0); the
	// guards are ported for fidelity.
	if config.SyntaxFlags&acErVcb11 != 0 {
		return AacEncUnsupportedERFmt
	}
	if config.SyntaxFlags&acErHcr != 0 {
		return AacEncUnsupportedERFmt
	}

	// check frame length
	switch config.FrameLength {
	case 1024:
		if isLowDelay(AudioObjectType(config.AudioObjectType)) {
			return AacEncInvalidFrameLength
		}
	case 128, 256, 512, 120, 240, 480:
		if !isLowDelay(AudioObjectType(config.AudioObjectType)) {
			return AacEncInvalidFrameLength
		}
	default:
		return AacEncInvalidFrameLength
	}

	// anc_Rate == 0 on the AAC-LC CBR default config: the FDKaacEnc_InitCheckAncillary
	// branch (aacenc.cpp:516-526) is not taken and is intentionally not modelled
	// here (no ancillary data).

	// maximal allowed DSE bytes in frame
	config.MaxAncBytesPerAU = uint(fMinI(256, fMaxI(0,
		CalcBitsPerFrame(config.BitRate-(config.NChannels*8000),
			config.FrameLength, config.SampleRate)>>3)))

	// bind config / lifetime vars
	hAacEnc.Config = config
	hAacEnc.BitrateMode = config.BitrateMode
	hAacEnc.EncoderMode = config.ChannelMode

	errorStatus = InitChannelMapping(hAacEnc.EncoderMode, ChannelOrder(chOrderFromConfig(config)),
		&hAacEnc.ChannelMapping)
	if errorStatus != AacEncOK {
		return errorStatus
	}

	cm := &hAacEnc.ChannelMapping

	bw, errorStatus := determineBandWidth(config.BandWidth, config.BitRate-config.AncDataBitRate,
		hAacEnc.BitrateMode, config.SampleRate, config.FrameLength, cm, hAacEnc.EncoderMode)
	if errorStatus != AacEncOK {
		return errorStatus
	}
	hAacEnc.Config.BandWidth = bw
	hAacEnc.Bandwidth90dB = hAacEnc.Config.BandWidth

	tnsMask := 0
	if config.UseTns {
		tnsMask = tnsEnableMask
	}
	psyBitrate := config.BitRate - config.AncDataBitRate

	if hAacEnc.EncoderMode != prevChannelMode || initFlags != 0 {
		// Reinit psych states on channel-config change or full reset.
		errorStatus = psyInit(hAacEnc.PsyKernel, hAacEnc.PsyOut[:], hAacEnc.MaxFrames,
			hAacEnc.MaxChannels, AudioObjectType(config.AudioObjectType), cm)
		if errorStatus != AacEncOK {
			return errorStatus
		}
	}

	errorStatus = psyMainInit(hAacEnc.PsyKernel, AudioObjectType(config.AudioObjectType), cm,
		config.SampleRate, config.FrameLength, psyBitrate, tnsMask, hAacEnc.Bandwidth90dB,
		boolToInt(config.UsePns), boolToInt(config.UseIS), boolToInt(config.UseMS),
		config.SyntaxFlags, initFlags)
	if errorStatus != AacEncOK {
		return errorStatus
	}

	errorStatus = QCOutInit(hAacEnc.QcOut[:], hAacEnc.MaxFrames, cm)
	if errorStatus != AacEncOK {
		return errorStatus
	}

	var qcInit QcInit
	qcInit.ChannelMapping = &hAacEnc.ChannelMapping
	qcInit.SceCpe = 0

	if aacBrModeIsVBR(config.BitrateMode) {
		// VBR path (out of AAC-LC CBR scope) — ported 1:1 but inert.
		qcInit.AverageBits = (averageBitsPerFrame + 7) &^ 7
		qcInit.BitRes = minBufsizePerEffChan * cm.NChannelsEff
		qcInit.MaxBits = minBufsizePerEffChan * cm.NChannelsEff
		if config.MaxBitsPerFrame != -1 {
			qcInit.MaxBits = fMinI(qcInit.MaxBits, config.MaxBitsPerFrame)
		}
		qcInit.MaxBits = fMaxI(qcInit.MaxBits, (averageBitsPerFrame+7)&^7)
		if config.MinBitsPerFrame != -1 {
			qcInit.MinBits = config.MinBitsPerFrame
		} else {
			qcInit.MinBits = 0
		}
		qcInit.MinBits = fMinI(qcInit.MinBits, averageBitsPerFrame&^7)
	} else {
		bitreservoir := -1 // default bitreservoir size
		if isLowDelay(AudioObjectType(config.AudioObjectType)) {
			// low-delay bitreservoir interpolation (out of AAC-LC scope).
			brPerChannel := config.BitRate / config.NChannels
			brPerChannel = fMinI(bitrateMaxLD, fMaxI(bitrateMinLD, brPerChannel))
			slope, _ := fDivNorm(int32(brPerChannel-bitrateMinLD), int32(bitrateMaxLD-bitrateMinLD))
			bitreservoir = int(fMultI(slope, int32(bitresMaxLD-bitresMinLD))) + bitresMinLD
			bitreservoir = bitreservoir &^ 7
		}

		qcInit.AverageBits = (averageBitsPerFrame + 7) &^ 7
		maxBitres := (minBufsizePerEffChan * cm.NChannelsEff) - qcInit.AverageBits
		if bitreservoir != -1 {
			qcInit.BitRes = fMinI(bitreservoir, maxBitres)
		} else {
			qcInit.BitRes = maxBitres
		}

		qcInit.MaxBits = fMinI(minBufsizePerEffChan*cm.NChannelsEff,
			((averageBitsPerFrame+7)&^7)+qcInit.BitRes)
		if config.MaxBitsPerFrame != -1 {
			qcInit.MaxBits = fMinI(qcInit.MaxBits, config.MaxBitsPerFrame)
		}
		qcInit.MaxBits = fMinI(minBufsizePerEffChan*cm.NChannelsEff,
			fMaxI(qcInit.MaxBits, (averageBitsPerFrame+7+8)&^7))

		qcInit.MinBits = fMaxI(0,
			((averageBitsPerFrame-1)&^7)-qcInit.BitRes-
				callStaticBits(staticBits, ((averageBitsPerFrame+7)&^7)+qcInit.BitRes))
		if config.MinBitsPerFrame != -1 {
			qcInit.MinBits = fMaxI(qcInit.MinBits, config.MinBitsPerFrame)
		}
		qcInit.MinBits = fMinI(qcInit.MinBits,
			(averageBitsPerFrame-callStaticBits(staticBits, qcInit.MaxBits))&^7)
	}

	qcInit.SampleRate = config.SampleRate
	qcInit.IsLowDelay = boolToInt(isLowDelay(AudioObjectType(config.AudioObjectType)))
	qcInit.NSubFrames = config.NSubFrames
	qcInit.Padding.PaddingRest = config.SampleRate

	bitresMin := bitresMinDefault
	if qcInit.IsLowDelay != 0 {
		bitresMin = bitresMinLD
	}
	if qcInit.MaxBits-qcInit.AverageBits >= bitresMin*config.NChannels {
		qcInit.BitResMode = AacencBrModeFull
	} else if qcInit.MaxBits > qcInit.AverageBits {
		qcInit.BitResMode = AacencBrModeReduced
	} else {
		qcInit.BitResMode = AacencBrModeDisabled
	}

	// Configure bitrate distribution strategy.
	switch config.ChannelMode {
	case ChannelMode1_2, ChannelMode1_2_1, ChannelMode1_2_2, ChannelMode1_2_2_1,
		ChannelMode6_1, ChannelMode1_2_2_2_1, ChannelMode7_1Back, ChannelMode7_1TopFront,
		ChannelMode7_1RearSurr, ChannelMode7_1FrontCent:
		qcInit.BitDistributionMode = 0 // over all elements bitrate estimation
	default: // MODE_1 / MODE_2 and all non-mpeg modes
		qcInit.BitDistributionMode = 1 // element-wise bit bitrate estimation
	}

	// meanPe = 10.0f * FRAME_LEN_LONG * bandwidth90dB / (sampleRate/2.0f)
	bwRatio, qbw := fDivNorm(int32(10*config.FrameLength*hAacEnc.Bandwidth90dB), int32(config.SampleRate))
	qcInit.MeanPe = fMaxI(int(scaleValue(bwRatio, qbw+1-(dfractBits-1))), 1)

	// maxBitFac, scaled to 24-bit accuracy
	mbfac, mbfacE := fDivNorm(int32(qcInit.MaxBits), int32(qcInit.AverageBits/qcInit.NSubFrames))
	qcInit.MaxBitFac = scaleValue(mbfac, -(dfractBits - 1 - 24 - mbfacE))

	switch config.BitrateMode {
	case AacBitrateModeCBR:
		qcInit.BitrateMode = QcdataBrModeCBR
	case AacBitrateModeVBR1:
		qcInit.BitrateMode = QcdataBrModeVBR1
	case AacBitrateModeVBR2:
		qcInit.BitrateMode = QcdataBrModeVBR2
	case AacBitrateModeVBR3:
		qcInit.BitrateMode = QcdataBrModeVBR3
	case AacBitrateModeVBR4:
		qcInit.BitrateMode = QcdataBrModeVBR4
	case AacBitrateModeVBR5:
		qcInit.BitrateMode = QcdataBrModeVBR5
	case AacBitrateModeSFR:
		qcInit.BitrateMode = QcdataBrModeSFR
	case AacBitrateModeFF:
		qcInit.BitrateMode = QcdataBrModeFF
	default:
		return AacEncUnsupportedBitrateMode
	}

	if config.UseRequant {
		qcInit.InvQuant = 2
	} else {
		qcInit.InvQuant = 0
	}

	if isLowDelay(AudioObjectType(config.AudioObjectType)) {
		qcInit.MaxIterations = 2
	} else {
		qcInit.MaxIterations = 5
	}

	qcInit.Bitrate = config.BitRate - config.AncDataBitRate

	qcInit.StaticBits = callStaticBits(staticBits, qcInit.AverageBits/qcInit.NSubFrames)

	errorStatus = QCInit(hAacEnc.QcKernel, &qcInit, initFlags)
	if errorStatus != AacEncOK {
		return errorStatus
	}

	// Map virtual aot's to intern aot used in bitstream writer.
	switch AudioObjectType(hAacEnc.Config.AudioObjectType) {
	case aotMp2AACLC:
		hAacEnc.Aot = int(aotAACLC)
	case aotMp2SBR:
		hAacEnc.Aot = int(aotSBR)
	default:
		hAacEnc.Aot = hAacEnc.Config.AudioObjectType
	}

	return AacEncOK
}

// callStaticBits mirrors transportEnc_GetStaticBits with the AAC-core decoupling:
// nil closure (no transport) -> 0 (raw/ADIF return path). On the parity path the
// oracle binds the genuine function.
func callStaticBits(staticBits func(auBits int) int, auBits int) int {
	if staticBits == nil {
		return 0
	}
	return staticBits(auBits)
}

// chOrderFromConfig returns CH_ORDER_MPEG (== 0). The AACENC_CONFIG channelOrder
// field is fixed to CH_ORDER_MPEG by AacInitDefaultConfig and the public API for
// the AAC-LC path; the config struct does not carry it separately here.
func chOrderFromConfig(config *AacencConfig) int {
	return int(ChOrderMPEG)
}
