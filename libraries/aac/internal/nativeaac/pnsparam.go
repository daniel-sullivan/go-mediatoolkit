// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// PNS parameter selection, ported 1:1 from libAACenc/src/pnsparam.cpp.
// FDKaacEnc_GetPnsParam fills the NOISEPARAMS tuning block the encoder's
// PNS detect chain (aacenc_pns.go) reads: the per-bitrate/samplerate tuning
// row (startSfb, refPower, refTonality, the TNS gain thresholds, gapFillThr,
// minSfbWidth, the detection-algorithm flags) plus the per-band power-distribution
// PSD curve. FDKaacEnc_lookUpPnsUse picks the tuning-table row from the
// auto level tables; FDKaacEnc_FreqToBandWidthRounding (defined in aacenc_tns.cpp)
// maps the table start frequency to a scalefactor band.
//
// fdk-aac encode is FIXED-POINT: every value is an int32 FIXP_DBL / int16
// FIXP_SGL Q-format quantity. The selection is integer table lookup plus the
// integer fPow / scaleValue helpers, bit-identical regardless of vectorization,
// so it carries only the aacfdk fence (no aac_strict FP split).

// Detection algorithm flags (pnsparam.h:113-120).
const (
	usePowerDistribution = 1 << 0 // USE_POWER_DISTRIBUTION
	usePsychTonality     = 1 << 1 // USE_PSYCH_TONALITY
	useTnsGainThr        = 1 << 2 // USE_TNS_GAIN_THR
	useTnsPns            = 1 << 3 // USE_TNS_PNS
	justLongWindow       = 1 << 4 // JUST_LONG_WINDOW
	isLowComplexity      = 1 << 5 // IS_LOW_COMPLEXITY
)

// PNS table sizing/error sentinels (pnsparam.h:110-111).
const (
	pnsTableError = -1 // PNS_TABLE_ERROR
)

// aacEncPnsTableError is AAC_ENC_PNS_TABLE_ERROR (aacenc.h:171).
const aacEncPnsTableError = 0x4060

// pnsInfoTabEntry ports the static PNS_INFO_TAB struct (pnsparam.cpp:107-117):
// one tuning row. refPower/refTonality/gapFillThr are FIXP_SGL; the gain
// thresholds are SHORT scaled by TNS_PREDGAIN_SCALE (==1000); minSfbWidth and
// the algorithm-flags are SHORT/USHORT.
type pnsInfoTabEntry struct {
	startFreq               int16
	refPower                int16 // FIXP_SGL
	refTonality             int16 // FIXP_SGL
	tnsGainThreshold        int16
	tnsPNSGainThreshold     int16
	gapFillThr              int16 // FIXP_SGL
	minSfbWidth             int16
	detectionAlgorithmFlags uint16
}

// autoPnsTabEntry ports the static AUTO_PNS_TAB struct (pnsparam.cpp:119-128):
// a bitrate bracket [brFrom, brTo] with the PNS-level index per sampling rate.
type autoPnsTabEntry struct {
	brFrom uint32
	brTo   uint32
	s16000 uint8
	s22050 uint8
	s24000 uint8
	s32000 uint8
	s44100 uint8
	s48000 uint8
}

// levelTableMono is the 1:1 port of levelTable_mono (pnsparam.cpp:130-221).
var levelTableMono = []autoPnsTabEntry{
	{0, 11999, 0, 1, 1, 1, 1, 1},
	{12000, 19999, 0, 1, 1, 1, 1, 1},
	{20000, 28999, 0, 2, 1, 1, 1, 1},
	{29000, 40999, 0, 4, 4, 4, 2, 2},
	{41000, 55999, 0, 9, 9, 7, 7, 7},
	{56000, 61999, 0, 0, 0, 0, 9, 9},
	{62000, 75999, 0, 0, 0, 0, 0, 0},
	{76000, 92999, 0, 0, 0, 0, 0, 0},
	{93000, 999999, 0, 0, 0, 0, 0, 0},
}

// levelTableStereo is the 1:1 port of levelTable_stereo (pnsparam.cpp:223-304).
var levelTableStereo = []autoPnsTabEntry{
	{0, 11999, 0, 1, 1, 1, 1, 1},
	{12000, 19999, 0, 3, 1, 1, 1, 1},
	{20000, 28999, 0, 3, 3, 3, 2, 2},
	{29000, 40999, 0, 7, 6, 6, 5, 5},
	{41000, 55999, 0, 9, 9, 7, 7, 7},
	{56000, 79999, 0, 0, 0, 0, 0, 0},
	{80000, 99999, 0, 0, 0, 0, 0, 0},
	{100000, 999999, 0, 0, 0, 0, 0, 0},
}

// levelTableLowComplexity is the 1:1 port of levelTable_lowComplexity
// (pnsparam.cpp:354-405).
var levelTableLowComplexity = []autoPnsTabEntry{
	{0, 27999, 0, 0, 0, 0, 0, 0},
	{28000, 31999, 0, 2, 2, 2, 2, 2},
	{32000, 47999, 0, 3, 3, 3, 3, 3},
	{48000, 48000, 0, 4, 4, 4, 4, 4},
	{48001, 999999, 0, 0, 0, 0, 0, 0},
}

// pnsInfoTab is the 1:1 port of pnsInfoTab (pnsparam.cpp:306-352) — the (E)LD
// tuning rows. refPower/refTonality/gapFillThr use FL2FXCONST_SGL exactly as the
// C compiler folds them (fl2fxconstSGL).
var pnsInfoTab = []pnsInfoTabEntry{
	{4000, fl2fxconstSGL(0.04), fl2fxconstSGL(0.06), 1150, 1200, fl2fxconstSGL(0.02), 8,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns},
	{4000, fl2fxconstSGL(0.04), fl2fxconstSGL(0.07), 1130, 1300, fl2fxconstSGL(0.05), 8,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns},
	{4100, fl2fxconstSGL(0.04), fl2fxconstSGL(0.07), 1100, 1400, fl2fxconstSGL(0.10), 8,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns},
	{4100, fl2fxconstSGL(0.03), fl2fxconstSGL(0.10), 1100, 1400, fl2fxconstSGL(0.15), 8,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns},
	{4300, fl2fxconstSGL(0.03), fl2fxconstSGL(0.10), 1100, 1400, fl2fxconstSGL(0.15), 8,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns | justLongWindow},
	{5000, fl2fxconstSGL(0.03), fl2fxconstSGL(0.10), 1100, 1400, fl2fxconstSGL(0.25), 8,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns | justLongWindow},
	{5500, fl2fxconstSGL(0.03), fl2fxconstSGL(0.12), 1100, 1400, fl2fxconstSGL(0.35), 8,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns | justLongWindow},
	{6000, fl2fxconstSGL(0.03), fl2fxconstSGL(0.12), 1080, 1400, fl2fxconstSGL(0.40), 8,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns | justLongWindow},
	{6000, fl2fxconstSGL(0.03), fl2fxconstSGL(0.14), 1070, 1400, fl2fxconstSGL(0.45), 8,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns | justLongWindow},
}

// pnsInfoTabLowComplexity is the 1:1 port of pnsInfoTab_lowComplexity
// (pnsparam.cpp:408-432) — the AAC-LC tuning rows.
var pnsInfoTabLowComplexity = []pnsInfoTabEntry{
	{4100, fl2fxconstSGL(0.03), fl2fxconstSGL(0.16), 1100, 1400, fl2fxconstSGL(0.5), 16,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns | justLongWindow},
	{4100, fl2fxconstSGL(0.05), fl2fxconstSGL(0.10), 1410, 1400, fl2fxconstSGL(0.5), 16,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns | justLongWindow},
	{4100, fl2fxconstSGL(0.05), fl2fxconstSGL(0.10), 1100, 1400, fl2fxconstSGL(0.5), 16,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns | justLongWindow},
	{4100, fl2fxconstSGL(0.20), fl2fxconstSGL(0.10), 1410, 1400, fl2fxconstSGL(0.5), 16,
		usePowerDistribution | usePsychTonality | useTnsGainThr | useTnsPns | justLongWindow},
}

// lookUpPnsUse is the 1:1 port of FDKaacEnc_lookUpPnsUse
// (pnsparam.cpp:437-488): pick the PNS-level index from the auto level table for
// (bitRate, sampleRate, numChan, isLC).
func lookUpPnsUse(bitRate, sampleRate, numChan, isLC int) int {
	hUsePns := 0
	var levelTable []autoPnsTabEntry

	if isLC != 0 {
		levelTable = levelTableLowComplexity
	} else { // (E)LD
		if numChan > 1 {
			levelTable = levelTableStereo
		} else {
			levelTable = levelTableMono
		}
	}

	size := len(levelTable)
	i := 0
	for i = 0; i < size; i++ {
		if uint32(bitRate) >= levelTable[i].brFrom && uint32(bitRate) <= levelTable[i].brTo {
			break
		}
	}

	// sanity check
	if len(pnsInfoTab) < i {
		return pnsTableError
	}

	// Guard the index used below: C reads levelTable[i] only after the loop
	// breaks within range (the bitrate brackets cover [0, 999999]).
	switch sampleRate {
	case 16000:
		hUsePns = int(levelTable[i].s16000)
	case 22050:
		hUsePns = int(levelTable[i].s22050)
	case 24000:
		hUsePns = int(levelTable[i].s24000)
	case 32000:
		hUsePns = int(levelTable[i].s32000)
	case 44100:
		hUsePns = int(levelTable[i].s44100)
	case 48000:
		hUsePns = int(levelTable[i].s48000)
	default:
		if isLC != 0 {
			hUsePns = int(levelTable[i].s48000)
		}
	}

	return hUsePns
}

// freqToBandWidthRounding is the 1:1 port of FDKaacEnc_FreqToBandWidthRounding
// (aacenc_tns.cpp:339-362): map a frequency (Hz) to the nearest scalefactor band
// border, given the band-start offsets. Used by GetPnsParam (and the TNS init).
func freqToBandWidthRounding(freq, fs, numOfBands int, bandStartOffset []int32) int {
	lineNumber := (freq*int(bandStartOffset[numOfBands])*4/fs + 1) / 2

	// freq > fs/2
	if lineNumber >= int(bandStartOffset[numOfBands]) {
		return numOfBands
	}

	// find band the line number lies in
	band := 0
	for band = 0; band < numOfBands; band++ {
		if int(bandStartOffset[band+1]) > lineNumber {
			break
		}
	}

	// round to nearest band border
	if lineNumber-int(bandStartOffset[band]) > int(bandStartOffset[band+1])-lineNumber {
		band++
	}

	return band
}

// GetPnsParam is the 1:1 port of FDKaacEnc_GetPnsParam (pnsparam.cpp:501-574):
// fill the NOISEPARAMS tuning block depending on bitrate and bandwidth. usePns
// is read and may be cleared (returned via the second result). Returns the AAC
// encoder error code (0 == AAC_ENC_OK).
func GetPnsParam(np *NoiseParams, bitRate, sampleRate, sfbCnt int, sfbOffset []int32,
	usePns, numChan, isLC int) (newUsePns, err int) {

	var pnsInfo []pnsInfoTabEntry
	var pnsIdx int

	if usePns <= 0 {
		return usePns, aacEncOK
	}

	if isLC != 0 {
		np.DetectionAlgorithmFlags = isLowComplexity

		pnsInfo = pnsInfoTabLowComplexity

		// new pns params
		hUsePns := lookUpPnsUse(bitRate, sampleRate, numChan, isLC)
		if hUsePns == 0 {
			return 0, aacEncOK
		}
		if hUsePns == pnsTableError {
			return usePns, aacEncPnsTableError
		}

		// select correct row of tuning table
		pnsIdx = hUsePns - 1
	} else {
		np.DetectionAlgorithmFlags = 0
		pnsInfo = pnsInfoTab

		// new pns params
		hUsePns := lookUpPnsUse(bitRate, sampleRate, numChan, isLC)
		if hUsePns == 0 {
			return 0, aacEncOK
		}
		if hUsePns == pnsTableError {
			return usePns, aacEncPnsTableError
		}

		// select correct row of tuning table
		pnsIdx = hUsePns - 1
	}

	info := &pnsInfo[pnsIdx]

	np.StartSfb = int16(freqToBandWidthRounding(int(info.startFreq), sampleRate, sfbCnt, sfbOffset))

	np.DetectionAlgorithmFlags |= info.detectionAlgorithmFlags

	np.RefPower = int32(info.refPower) << 16 // FX_SGL2FX_DBL
	np.RefTonality = int32(info.refTonality) << 16
	np.TnsGainThreshold = int(info.tnsGainThreshold)
	np.TnsPNSGainThreshold = int(info.tnsPNSGainThreshold)
	np.MinSfbWidth = int(info.minSfbWidth)

	np.GapFillThr = info.gapFillThr // for LC always FL2FXCONST_SGL(0.5)

	// assuming a constant dB/Hz slope in the signal's PSD curve, the detection
	// threshold needs to be corrected for the width of the band.
	for i := 0; i < sfbCnt-1; i++ {
		sfbWidth := int(sfbOffset[i+1]) - int(sfbOffset[i])

		tmp, qtmp := fPow(np.RefPower, 0, int32(sfbWidth), dfractBits-1-5)
		np.PowDistPSDcurve[i] = int16(scaleValue(tmp, qtmp) >> 16)
	}
	np.PowDistPSDcurve[sfbCnt] = np.PowDistPSDcurve[sfbCnt-1]

	return usePns, aacEncOK
}
