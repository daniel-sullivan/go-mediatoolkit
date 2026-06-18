// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Bandwidth expert: the encoder's per-element audio-bandwidth selector. 1:1 port
// of libAACenc/src/bandwidth.cpp. FDKaacEnc_DetermineBandWidth maps the proposed
// bandwidth + channel bitrate + sample rate + frame length onto the coded audio
// bandwidth (the highest frequency the quantizer keeps), interpolating the
// bitrate->bandwidth ROM tables in the fixed-point ld/fDivNorm domain for the
// low-delay frame lengths.
//
// AAC-LC path only: frameLength is 1024 or 960, so GetBandwidthEntry selects
// bandWidthTable and takes the plain table-lookup branch (no fDivNorm
// interpolation). bitrateMode is CBR. The VBR (AACENC_BR_MODE_VBR_*) and the
// low-delay (frameLength 120/128/240/256/480/512) branches are ported verbatim
// for ROM/struct fidelity but are DEAD on the AAC-LC CBR path. Pure integer
// (FIXP_DBL == int32, INT == int); no floating point. aacfdk-fenced.

package nativeaac

// bandwidthTab is the 1:1 port of BANDWIDTH_TAB (bandwidth.cpp:115-119): one
// chanBitRate->bandwidth breakpoint for mono and for 2+ channels.
type bandwidthTab struct {
	chanBitRate           int // chanBitRate
	bandWidthMono         int // bandWidthMono
	bandWidth2AndMoreChan int // bandWidth2AndMoreChan
}

// bandWidthTable is the 1:1 port of bandWidthTable[] (bandwidth.cpp:121-124):
// the LC/main bitrate->bandwidth breakpoints for frameLength 960/1024.
var bandWidthTable = []bandwidthTab{
	{0, 3700, 5000}, {12000, 5000, 6400}, {20000, 6900, 9640},
	{28000, 9600, 13050}, {40000, 12060, 14260}, {56000, 13950, 15500},
	{72000, 14200, 16120}, {96000, 17000, 17000}, {576001, 17000, 17000},
}

// bandWidthTable_LD_22050 is the 1:1 port of bandWidthTable_LD_22050[]
// (bandwidth.cpp:126-130). Low-delay ROM (dead on the AAC-LC path).
var bandWidthTableLD22050 = []bandwidthTab{
	{8000, 2000, 2400}, {12000, 2500, 2700}, {16000, 3300, 3100},
	{24000, 6250, 7200}, {32000, 9200, 10500}, {40000, 16000, 16000},
	{48000, 16000, 16000}, {282241, 16000, 16000},
}

// bandWidthTable_LD_24000 is the 1:1 port of bandWidthTable_LD_24000[]
// (bandwidth.cpp:132-136). Low-delay ROM (dead on the AAC-LC path).
var bandWidthTableLD24000 = []bandwidthTab{
	{8000, 2000, 2000}, {12000, 2000, 2300}, {16000, 2200, 2500},
	{24000, 5650, 7200}, {32000, 11600, 12000}, {40000, 12000, 16000},
	{48000, 16000, 16000}, {64000, 16000, 16000}, {307201, 16000, 16000},
}

// bandWidthTable_LD_32000 is the 1:1 port of bandWidthTable_LD_32000[]
// (bandwidth.cpp:138-142). Low-delay ROM (dead on the AAC-LC path).
var bandWidthTableLD32000 = []bandwidthTab{
	{8000, 2000, 2000}, {12000, 2000, 2000}, {24000, 4250, 7200},
	{32000, 8400, 9000}, {40000, 9400, 11300}, {48000, 11900, 14700},
	{64000, 14800, 16000}, {76000, 16000, 16000}, {409601, 16000, 16000},
}

// bandWidthTable_LD_44100 is the 1:1 port of bandWidthTable_LD_44100[]
// (bandwidth.cpp:144-149). Low-delay ROM (dead on the AAC-LC path).
var bandWidthTableLD44100 = []bandwidthTab{
	{8000, 2000, 2000}, {24000, 2000, 2000}, {32000, 4400, 5700},
	{40000, 7400, 8800}, {48000, 9000, 10700}, {56000, 11000, 12900},
	{64000, 14400, 15500}, {80000, 16000, 16200}, {96000, 16500, 16000},
	{128000, 16000, 16000}, {564481, 16000, 16000},
}

// bandWidthTable_LD_48000 is the 1:1 port of bandWidthTable_LD_48000[]
// (bandwidth.cpp:151-156). Low-delay ROM (dead on the AAC-LC path).
var bandWidthTableLD48000 = []bandwidthTab{
	{8000, 2000, 2000}, {24000, 2000, 2000}, {32000, 4400, 5700},
	{40000, 7400, 8800}, {48000, 9000, 10700}, {56000, 11000, 12800},
	{64000, 14300, 15400}, {80000, 16000, 16200}, {96000, 16500, 16000},
	{128000, 16000, 16000}, {614401, 16000, 16000},
}

// bandwidthTabVBR is the 1:1 port of BANDWIDTH_TAB_VBR (bandwidth.cpp:158-162).
type bandwidthTabVBR struct {
	bitrateMode           AacencBitrateMode // bitrateMode
	bandWidthMono         int               // bandWidthMono
	bandWidth2AndMoreChan int               // bandWidth2AndMoreChan
}

// bandWidthTableVBR is the 1:1 port of bandWidthTableVBR[] (bandwidth.cpp:164-174).
// VBR ROM (dead on the AAC-LC CBR path). Indexed by bitrateMode value directly
// (matching `bandWidthTableVBR[bitrateMode]` in C), so all eight rows are
// present in enum order CBR(0)..VBR5(5),SFR,FF.
var bandWidthTableVBR = []bandwidthTabVBR{
	{AacBitrateModeCBR, 0, 0},
	{AacBitrateModeVBR1, 13000, 13000},
	{AacBitrateModeVBR2, 13000, 13000},
	{AacBitrateModeVBR3, 15750, 15750},
	{AacBitrateModeVBR4, 16500, 16500},
	{AacBitrateModeVBR5, 19293, 19293},
	{AacBitrateModeSFR, 0, 0}, // C declaration order: AACENC_BR_MODE_SFR row sits at index 6
	{AacBitrateModeFF, 0, 0},  // AACENC_BR_MODE_FF row at index 7 (both rows are {0,0}; only indices 1..5 are ever read)
}

// getBandwidthEntry is the 1:1 port of GetBandwidthEntry (bandwidth.cpp:176-261):
// pick the bitrate->bandwidth table for (frameLength, sampleRate), then locate
// the chanBitRate breakpoint. For frameLength 960/1024 the entry is a plain
// table lookup; for the low-delay lengths it interpolates between breakpoints in
// the fDivNorm/fMult fixed-point domain. Returns -1 when no table or no
// breakpoint matches.
func getBandwidthEntry(frameLength, sampleRate, chanBitRate, entryNo int) int {
	bandwidth := -1
	var pBwTab []bandwidthTab

	switch frameLength {
	case 960, 1024:
		pBwTab = bandWidthTable
	case 120, 128, 240, 256, 480, 512:
		switch sampleRate {
		case 8000, 11025, 12000, 16000, 22050:
			pBwTab = bandWidthTableLD22050
		case 24000:
			pBwTab = bandWidthTableLD24000
		case 32000:
			pBwTab = bandWidthTableLD32000
		case 44100:
			pBwTab = bandWidthTableLD44100
		case 48000, 64000, 88200, 96000:
			pBwTab = bandWidthTableLD48000
		}
	default:
		pBwTab = nil
	}

	if pBwTab != nil {
		bwTabSize := len(pBwTab)
		for i := 0; i < bwTabSize-1; i++ {
			if chanBitRate >= pBwTab[i].chanBitRate &&
				chanBitRate < pBwTab[i+1].chanBitRate {
				switch frameLength {
				case 960, 1024:
					if entryNo == 0 {
						bandwidth = pBwTab[i].bandWidthMono
					} else {
						bandwidth = pBwTab[i].bandWidth2AndMoreChan
					}
				case 120, 128, 240, 256, 480, 512:
					var startBw, endBw int
					if entryNo == 0 {
						startBw = pBwTab[i].bandWidthMono
						endBw = pBwTab[i+1].bandWidthMono
					} else {
						startBw = pBwTab[i].bandWidth2AndMoreChan
						endBw = pBwTab[i+1].bandWidth2AndMoreChan
					}
					startBr := pBwTab[i].chanBitRate
					endBr := pBwTab[i+1].chanBitRate

					bwFacFix, qRes := fDivNorm(int32(chanBitRate-startBr), int32(endBr-startBr))
					// fMult == fixmul_DD == arm8 smull>>31 on this target (see fMultDD
					// note); fixmulDDarm8 is that exact kernel.
					bandwidth = int(scaleValue(fixmulDDarm8(bwFacFix, int32(endBw-startBw)), qRes)) + startBw
				default:
					bandwidth = -1
				}
				break // within bitrate range
			}
		}
	}

	return bandwidth
}

// determineBandWidth is the 1:1 port of FDKaacEnc_DetermineBandWidth
// (bandwidth.cpp:263-385): set *bandWidth from the proposed bandwidth /
// bitrate / mode / sample rate / frame length, returning the AAC_ENCODER_ERROR.
// On the AAC-LC CBR path bitrateMode == AacBitrateModeCBR and frameLength is
// 1024/960. The VBR/SFR/FF branches are ported verbatim for fidelity. Returns
// (bandWidth, error).
func determineBandWidth(proposedBandWidth, bitrate int, bitrateMode AacencBitrateMode,
	sampleRate, frameLength int, cm *ChannelMapping, encoderMode ChannelMode) (int, EncoderError) {
	errorStatus := AacEncOK
	chanBitRate := bitrate / cm.NChannelsEff
	var bandWidth int

	switch bitrateMode {
	case AacBitrateModeVBR1, AacBitrateModeVBR2, AacBitrateModeVBR3,
		AacBitrateModeVBR4, AacBitrateModeVBR5:
		if proposedBandWidth != 0 {
			// use given bw
			bandWidth = proposedBandWidth
		} else {
			// take bw from table
			switch encoderMode {
			case ChannelMode1:
				bandWidth = bandWidthTableVBR[bitrateMode].bandWidthMono
			case ChannelMode2, ChannelMode1_2, ChannelMode1_2_1, ChannelMode1_2_2,
				ChannelMode1_2_2_1, ChannelMode6_1, ChannelMode1_2_2_2_1,
				ChannelMode7_1RearSurr, ChannelMode7_1FrontCent, ChannelMode7_1Back,
				ChannelMode7_1TopFront:
				bandWidth = bandWidthTableVBR[bitrateMode].bandWidth2AndMoreChan
			default:
				return 0, AacEncUnsupportedChannelconf
			}
		}
	case AacBitrateModeCBR, AacBitrateModeSFR, AacBitrateModeFF: // CBR is the AAC-LC path; SFR/FF share the branch (dead on AAC-LC)
		// bandwidth limiting
		if proposedBandWidth != 0 {
			bandWidth = fixMin(proposedBandWidth, fixMin(20000, sampleRate>>1))
		} else { // search reasonable bandwidth
			entryNo := 0
			switch encoderMode {
			case ChannelMode1: // mono
				entryNo = 0 // use mono bandwidth settings
			case ChannelMode2, ChannelMode1_2, ChannelMode1_2_1, ChannelMode1_2_2,
				ChannelMode1_2_2_1, ChannelMode6_1, ChannelMode1_2_2_2_1,
				ChannelMode7_1RearSurr, ChannelMode7_1FrontCent, ChannelMode7_1Back,
				ChannelMode7_1TopFront:
				entryNo = 1 // use stereo bandwidth settings
			default:
				return 0, AacEncUnsupportedChannelconf
			}

			bandWidth = getBandwidthEntry(frameLength, sampleRate, chanBitRate, entryNo)

			if bandWidth == -1 {
				switch frameLength {
				case 120, 128, 240, 256:
					bandWidth = 16000
				default:
					errorStatus = AacEncInvalidChannelBitrate
				}
			}
		}
	default:
		return 0, AacEncUnsupportedBitrateMode
	}

	bandWidth = fixMin(bandWidth, sampleRate/2)

	return bandWidth, errorStatus
}
