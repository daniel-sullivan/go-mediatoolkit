// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Psychoacoustic configuration, ported 1:1 from libAACenc/src/
// psy_configuration.cpp. FDKaacEnc_InitPsyConfiguration fills the
// PSY_CONFIGURATION the encoder's psychoacoustic model reads every frame: the
// scalefactor-band layout (sfbCnt/sfbActive/sfbOffset), the masking spreading
// factors (sfbMaskLow/HighFactor plus their SprEn variants), the per-band PCM
// quantisation thresholds, the minimum-SNR ld-data, the pre-echo control ratios,
// the lowpass line and the level-dependent clip energy. The barc/spreading/
// minSnr sub-inits are static helpers in the same C TU, ported alongside.
//
// fdk-aac encode is FIXED-POINT: every value is an int32 FIXP_DBL / int16
// FIXP_SGL Q-format quantity. The whole init is pure integer fixed-point
// arithmetic (fixmul int64 products, arithmetic shifts, the table-free
// fixp_atan / f2Pow / fPow / fDivNorm helpers in fixpoint_pow.go and the
// table-driven fLog2/CalcLdData) — no float, no transcendental — so it is
// bit-identical regardless of vectorization and carries only the aacfdk fence
// (no aac_strict FP split).
//
// SCOPE: FDKaacEnc_InitPsyConfiguration only zeroes the embedded tnsConf /
// pnsConf via FDKmemclear and never populates them — their init
// (FDKaacEnc_InitTnsConfiguration / FDKaacEnc_InitPnsConfiguration) are sibling
// calls in psy_main.cpp, out of scope here. So InitPsyConfiguration leaves
// TnsConf / PnsConf zero-valued, exactly as the C does.

// Window block-type constants (psy_const.h:120-127). encShortWindow (==2) and
// encTransFac (==8) are defined in enc_tns_detect.go.
const (
	encLongWindow  = 0 // LONG_WINDOW
	encStartWindow = 1 // START_WINDOW
	encStopWindow  = 3 // STOP_WINDOW
)

// encMaxSfb is MAX_SFB (psy_const.h:149) == 51.
const encMaxSfb = 51

// lfeLowpassLine is LFE_LOWPASS_LINE (psy_const.h:165) == 12.
const lfeLowpassLine = 12

// Filterbank type (psy_const.h:116). FB_LC is AAC-LC.
const (
	fbLC  = 0 // FB_LC
	fbLD  = 1 // FB_LD
	fbELD = 2 // FB_ELD
)

// AAC encoder error codes (the subset InitPsyConfiguration returns,
// aacenc.h AAC_ENCODER_ERROR enum). 0 == AAC_ENC_OK.
const (
	aacEncOK                    = 0
	aacEncInvalidFrameLength    = 0x0020 // AAC_ENC_INVALID_FRAME_LENGTH
	aacEncUnsupportedSamplerate = 0x0021 // AAC_ENC_UNSUPPORTED_SAMPLINGRATE
)

// PNSConfig ports PNS_CONFIG (aacenc_pns.h:111-116). InitPsyConfiguration only
// zeroes this (FDKmemclear); its real init is the sibling
// FDKaacEnc_InitPnsConfiguration (InitPnsConfiguration in aacenc_pns.go).
// NoiseParams mirrors NOISEPARAMS (pnsparam.h).
type PNSConfig struct {
	NoiseParams            NoiseParams // np
	MinCorrelationEnergy   int32       // minCorrelationEnergy (FIXP_DBL)
	NoiseCorrelationThresh int32       // noiseCorrelationThresh (FIXP_DBL)
	UsePns                 int         // usePns
}

// NoiseParams ports NOISEPARAMS (pnsparam.h:122-137): the PNS tuning block. Left
// zero-valued by InitPsyConfiguration; populated by FDKaacEnc_GetPnsParam
// (GetPnsParam in pnsparam.go) via FDKaacEnc_InitPnsConfiguration.
type NoiseParams struct {
	StartSfb                int16                // startSfb (PNS start band)
	DetectionAlgorithmFlags uint16               // detectionAlgorithmFlags (USHORT)
	RefPower                int32                // refPower (FIXP_DBL)
	RefTonality             int32                // refTonality (FIXP_DBL)
	TnsGainThreshold        int                  // tnsGainThreshold (INT, scaled by 1000)
	TnsPNSGainThreshold     int                  // tnsPNSGainThreshold (INT, scaled by 1000)
	MinSfbWidth             int                  // minSfbWidth (INT)
	PowDistPSDcurve         [maxGroupedSfb]int16 // powDistPSDcurve[MAX_GROUPED_SFB] (FIXP_SGL)
	GapFillThr              int16                // gapFillThr (FIXP_SGL)
}

// PsyConfiguration ports PSY_CONFIGURATION (psy_configuration.h:120-151). The
// fixed-size MAX_SFB arrays are sized [encMaxSfb] and sfbOffset [encMaxSfb+1],
// exactly as the C struct, so the parity oracle can memcmp the footprint.
type PsyConfiguration struct {
	SfbCnt       int                  // number of existing sf bands
	SfbActive    int                  // sf bands containing energy after lowpass
	SfbActiveLFE int                  // sfbActiveLFE
	SfbOffset    [encMaxSfb + 1]int32 // sfbOffset (INT)

	Filterbank int // LC, LD or ELD

	SfbPcmQuantThreshold [encMaxSfb]int32 // sfbPcmQuantThreshold (FIXP_DBL)

	MaxAllowedIncreaseFactor    int   // preecho control (INT)
	MinRemainingThresholdFactor int16 // FIXP_SGL

	LowpassLine    int   // lowpassLine
	LowpassLineLFE int   // lowpassLineLFE
	ClipEnergy     int32 // for level dependend tmn (FIXP_DBL)

	SfbMaskLowFactor  [encMaxSfb]int32 // sfbMaskLowFactor (FIXP_DBL)
	SfbMaskHighFactor [encMaxSfb]int32 // sfbMaskHighFactor (FIXP_DBL)

	SfbMaskLowFactorSprEn  [encMaxSfb]int32 // sfbMaskLowFactorSprEn (FIXP_DBL)
	SfbMaskHighFactorSprEn [encMaxSfb]int32 // sfbMaskHighFactorSprEn (FIXP_DBL)

	SfbMinSnrLdData [encMaxSfb]int32 // minimum snr, ld domain (FIXP_DBL)

	TnsConf TNSConfig // tnsConf (zeroed by InitPsyConfiguration)
	PnsConf PNSConfig // pnsConf (zeroed by InitPsyConfiguration)

	GranuleLength int // granuleLength
	AllowIS       int // allowIS
	AllowMS       int // allowMS
}

// initSfbTable is the 1:1 port of FDKaacEnc_initSfbTable
// (psy_configuration.cpp:191-259): select the SFB-width table for
// (sampleRate, blockType, granuleLength), then accumulate the per-band widths
// into sfbOffset[] until the granule length is reached, clamping sfbCnt. Writes
// sfbOffset and returns (sfbCnt, error code).
func initSfbTable(sampleRate, blockType, granuleLength int, sfbOffset []int32) (sfbCnt int, err int) {
	specStartOffset := 0
	granuleLengthWindow := granuleLength
	var sfbWidth []int
	var sfbInfo []sfbInfoTabEntry
	var size int

	// select table
	switch granuleLength {
	case 1024, 960:
		sfbInfo = sfbInfoTab
		size = len(sfbInfoTab)
	case 512:
		sfbInfo = sfbInfoTabLD512
		// C: size = sizeof(sfbInfoTabLD512); a BUG-FOR-BUG byte-count, not an
		// element count — but the matching loop below breaks on the sampleRate
		// hit before i reaches the element count, and the i==size "not found"
		// check only triggers for an unsupported rate. The element count is the
		// faithful equivalent for the supported-rate path; reproduced as len().
		size = len(sfbInfoTabLD512)
	case 480:
		sfbInfo = sfbInfoTabLD480
		size = len(sfbInfoTabLD480)
	default:
		return 0, aacEncInvalidFrameLength
	}

	var i int
	for i = 0; i < size; i++ {
		if sfbInfo[i].sampleRate == sampleRate {
			switch blockType {
			case encLongWindow, encStartWindow, encStopWindow:
				sfbWidth = sfbInfo[i].paramLong.sfbWidth
				sfbCnt = sfbInfo[i].paramLong.sfbCnt
			case encShortWindow:
				sfbWidth = sfbInfo[i].paramShort.sfbWidth
				sfbCnt = sfbInfo[i].paramShort.sfbCnt
				granuleLengthWindow /= encTransFac
			}
			break
		}
	}
	if i == size {
		return 0, aacEncUnsupportedSamplerate
	}

	// calc sfb offsets
	for i = 0; i < sfbCnt; i++ {
		sfbOffset[i] = int32(specStartOffset)
		specStartOffset += sfbWidth[i]
		if specStartOffset >= granuleLengthWindow {
			i++
			break
		}
	}
	sfbCnt = fixMin(i, sfbCnt)
	sfbOffset[sfbCnt] = int32(fixMin(specStartOffset, granuleLengthWindow))
	return sfbCnt, aacEncOK
}

// barcLineValue is the 1:1 port of the static FDKaacEnc_BarcLineValue
// (psy_configuration.cpp:270-317): the barc (Bark-scale) value of one frequency
// line, q25. fMult == fixmul_DD == fixmulDDarm8; fixp_atan via fixpAtan
// (fixpoint_pow.go).
func barcLineValue(noOfLines, fftLine, samplingFreq int) int32 {
	const fourBy3Em4 = int32(0x45e7b273) // 4.0/3 * 0.0001 in q43
	const pzzz76 = int32(0x639d5e4a)     // 0.00076 in q41
	const one3p3 = int32(0x35333333)     // 13.3 in q26
	const threep5 = int32(0x1c000000)    // 3.5 in q27
	const inv480 = int32(0x44444444)     // 1/480 in q39

	centerFreq := int32(fftLine * samplingFreq) // q11 or q8

	switch noOfLines {
	case 1024:
		centerFreq = centerFreq << 2 // q13
	case 128:
		centerFreq = centerFreq << 5 // q13
	case 512:
		centerFreq = int32(fftLine*samplingFreq) << 3 // q13
	case 480:
		centerFreq = fixmulDDarm8(centerFreq, inv480) << 4 // q13
	default:
		centerFreq = 0
	}

	x1 := fixmulDDarm8(centerFreq, fourBy3Em4)  // q25
	x2 := fixmulDDarm8(centerFreq, pzzz76) << 2 // q25

	atan1 := fixpAtan(x1)
	atan2 := fixpAtan(x2)

	bvalFFTLine := fixmulDDarm8(one3p3, atan2) +
		fixmulDDarm8(threep5, fixmulDDarm8(atan1, atan1))
	return bvalFFTLine
}

// initMinPCMResolution is the 1:1 port of the static
// FDKaacEnc_InitMinPCMResolution (psy_configuration.cpp:325-334): the per-band
// PCM quantisation noise threshold = bandWidth * PCM_QUANT_NOISE.
func initMinPCMResolution(numPb int, pbOffset []int32, sfbPCMquantThreshold []int32) {
	// PCM_QUANT_NOISE = pow(10, -20/10) * ABS_LOW * NORM_PCM_ENERGY *
	// pow(2, PCM_QUANT_THR_SCALE).
	const pcmQuantNoise = int32(0x00547062)

	for i := 0; i < numPb; i++ {
		sfbPCMquantThreshold[i] = (pbOffset[i+1] - pbOffset[i]) * pcmQuantNoise
	}
}

// getMaskFactor is the 1:1 port of the static getMaskFactor
// (psy_configuration.cpp:336-352): fPow(ten, .., -dbVal, ..), clamping the
// exponent and saturating to MAXVAL_DBL on positive-exponent overflow.
func getMaskFactor(dbValFix int32, dbValE int, tenFix int32, tenE int) int32 {
	maskFactor, qMsk := fPow(tenFix, int32(dfractBits-1-tenE), -dbValFix, int32(dfractBits-1-dbValE))
	qMsk = int32(fixMin(dfractBits-1, fixMax(-(dfractBits-1), int(qMsk))))

	if qMsk > 0 && maskFactor > (maxvalDBL>>uint(qMsk)) {
		maskFactor = maxvalDBL
	} else {
		maskFactor = scaleValue(maskFactor, qMsk)
	}
	return maskFactor
}

// initSpreading is the 1:1 port of the static FDKaacEnc_initSpreading
// (psy_configuration.cpp:354-405): the masking-spread factors per partition band
// (and their spread-energy SprEn variants), bitrate- and blockType-dependent.
func initSpreading(numPb int, pbBarcValue, pbMaskLoFactor, pbMaskHiFactor,
	pbMaskLoFactorSprEn, pbMaskHiFactorSprEn []int32, bitrate int, blockType int) {

	const maskHigh = int32(0x30000000)               // 1.5 in q29
	const maskLow = int32(0x60000000)                // 3.0 in q29
	const maskLowSprenLong = int32(0x60000000)       // 3.0 in q29
	const maskHighSprenLong = int32(0x40000000)      // 2.0 in q29
	const maskHighSprenLongLowBr = int32(0x30000000) // 1.5 in q29
	const maskLowSprenShort = int32(0x40000000)      // 2.0 in q29
	const maskHighSprenShort = int32(0x30000000)     // 1.5 in q29
	const ten = int32(0x50000000)                    // 10.0 in q27

	var maskLowSpren, maskHighSpren int32
	if blockType != encShortWindow {
		maskLowSpren = maskLowSprenLong
		if bitrate > 20000 {
			maskHighSpren = maskHighSprenLong
		} else {
			maskHighSpren = maskHighSprenLongLowBr
		}
	} else {
		maskLowSpren = maskLowSprenShort
		maskHighSpren = maskHighSprenShort
	}

	for i := 0; i < numPb; i++ {
		if i > 0 {
			pbMaskHiFactor[i] = getMaskFactor(
				fixmulDDarm8(maskHigh, pbBarcValue[i]-pbBarcValue[i-1]), 23, ten, 27)

			pbMaskLoFactor[i-1] = getMaskFactor(
				fixmulDDarm8(maskLow, pbBarcValue[i]-pbBarcValue[i-1]), 23, ten, 27)

			pbMaskHiFactorSprEn[i] = getMaskFactor(
				fixmulDDarm8(maskHighSpren, pbBarcValue[i]-pbBarcValue[i-1]), 23, ten, 27)

			pbMaskLoFactorSprEn[i-1] = getMaskFactor(
				fixmulDDarm8(maskLowSpren, pbBarcValue[i]-pbBarcValue[i-1]), 23, ten, 27)
		} else {
			pbMaskHiFactor[i] = 0
			pbMaskLoFactor[numPb-1] = 0
			pbMaskHiFactorSprEn[i] = 0
			pbMaskLoFactorSprEn[numPb-1] = 0
		}
	}
}

// initBarcValues is the 1:1 port of the static FDKaacEnc_initBarcValues
// (psy_configuration.cpp:407-419): the average barc value over each partition
// band, clamped to MAX_BARC (24.0 q25).
func initBarcValues(numPb int, pbOffset []int32, numLines, samplingFrequency int, pbBval []int32) {
	const maxBarc = int32(0x30000000) // 24.0 in q25

	for i := 0; i < numPb; i++ {
		v1 := barcLineValue(numLines, int(pbOffset[i]), samplingFrequency)
		v2 := barcLineValue(numLines, int(pbOffset[i+1]), samplingFrequency)
		curBark := (v1 >> 1) + (v2 >> 1)
		pbBval[i] = fixMinDBL(curBark, maxBarc)
	}
}

// initMinSnr is the 1:1 port of the static FDKaacEnc_initMinSnr
// (psy_configuration.cpp:421-532): the per-band minimum-SNR ld-data derived from
// the perceptual-entropy-per-window estimate. fDivNorm/f2Pow via the
// fixpoint_pow.go ports; CalcLdData == calcLdData.
func initMinSnr(bitrate, samplerate, numLines int, sfbOffset []int32,
	sfbActive, blockType int, sfbMinSnrLdData []int32) {

	const maxBarc = int32(0x30000000)    // 24.0 in q25
	const maxBarcP1 = int32(0x32000000)  // 25.0 in q25
	const bits2pefac = int32(0x4b851eb8) // 1.18 in q30
	const pers2p4 = int32(0x624dd2f2)    // 0.024 in q36
	const onep5 = int32(0x60000000)      // 1.5 in q30
	const maxSnr = int32(0x33333333)     // 0.8 in q30
	const minSnr = int32(0x003126e9)     // 0.003 in q30

	// relative number of active barks
	barcFactor, qbfac := fDivNorm(
		fixMinDBL(barcLineValue(numLines, int(sfbOffset[sfbActive]), samplerate), maxBarc),
		maxBarcP1)
	qbfac = dfractBits - 1 - qbfac

	pePerWindow, qperwin := fDivNorm(int32(bitrate), int32(samplerate))
	qperwin = dfractBits - 1 - qperwin
	pePerWindow = fixmulDDarm8(pePerWindow, bits2pefac)
	qperwin = qperwin + 30 - (dfractBits - 1)
	pePerWindow = fixmulDDarm8(pePerWindow, pers2p4)
	qperwin = qperwin + 36 - (dfractBits - 1)

	switch numLines {
	case 1024:
		qperwin = qperwin - 10
	case 128:
		qperwin = qperwin - 7
	case 512:
		qperwin = qperwin - 9
	case 480:
		qperwin = qperwin - 9
		pePerWindow = fixmulDDarm8(pePerWindow, fl2fxconstDBL(480.0/512.0))
	}

	// for short blocks it is assumed that more bits are available
	if blockType == encShortWindow {
		pePerWindow = fixmulDDarm8(pePerWindow, onep5)
		qperwin = qperwin + 30 - (dfractBits - 1)
	}
	pePartConst, qdiv := fDivNorm(pePerWindow, barcFactor)
	qpeprtConst := qperwin - qbfac + dfractBits - 1 - qdiv

	for sfb := 0; sfb < sfbActive; sfb++ {
		barcWidth := barcLineValue(numLines, int(sfbOffset[sfb+1]), samplerate) -
			barcLineValue(numLines, int(sfbOffset[sfb]), samplerate)

		// adapt to sfb bands
		pePart := fixmulDDarm8(pePartConst, barcWidth)
		qpeprt := qpeprtConst + 25 - (dfractBits - 1)

		// pe -> snr calculation
		sfbWidth := sfbOffset[sfb+1] - sfbOffset[sfb]
		pePart, qdiv = fDivNorm(pePart, sfbWidth)
		qpeprt += dfractBits - 1 - qdiv

		tmp, qtmp := f2PowWithExp(pePart, dfractBits-1-qpeprt)
		qtmp = dfractBits - 1 - qtmp

		// Subtract 1.5
		qsnr := int32(fixMin(int(qtmp), 30))
		tmp = tmp >> uint(qtmp-qsnr)

		var onePoint5 int32
		if (30 + 1 - qsnr) > (dfractBits - 1) {
			onePoint5 = 0
		} else {
			onePoint5 = onep5 >> uint(30+1-qsnr)
		}

		snr := (tmp >> 1) - onePoint5
		qsnr -= 1

		// max(snr, 1.0)
		var oneQsnr int32
		if qsnr > 0 {
			oneQsnr = int32(1) << uint(qsnr)
		} else {
			oneQsnr = 0
		}

		snr = fixMaxDBL(oneQsnr, snr)

		// 1/snr
		snr, qsnr = fDivNorm(oneQsnr, snr)
		qsnr = dfractBits - 1 - qsnr
		if qsnr > 30 {
			snr = snr >> uint(qsnr-30)
		}

		// upper limit is -1 dB
		if snr > maxSnr {
			snr = maxSnr
		}
		// lower limit is -25 dB
		if snr < minSnr {
			snr = minSnr
		}
		snr = snr << 1

		sfbMinSnrLdData[sfb] = calcLdData(snr)
	}
}

// InitPsyConfiguration is the 1:1 port of FDKaacEnc_InitPsyConfiguration
// (psy_configuration.cpp:534-627): fill psyConf for (bitrate, samplerate,
// bandwidth, blocktype, granuleLength, useIS, useMS, filterbank). Returns an
// AAC encoder error code (0 == AAC_ENC_OK). tnsConf / pnsConf are left zeroed
// (their init are sibling calls, out of scope). psyConf is assumed
// zero-initialised by the caller (the C FDKmemclear is the Go zero value of a
// freshly-allocated PsyConfiguration; callers must pass a clean struct, matching
// the FDKmemclear).
func InitPsyConfiguration(bitrate, samplerate, bandwidth, blocktype, granuleLength,
	useIS, useMS int, psyConf *PsyConfiguration, filterbank int) int {

	var sfbBarcVal [encMaxSfb]int32
	frameLengthLong := granuleLength
	frameLengthShort := granuleLength / encTransFac
	downscaleFactor := 1

	switch granuleLength {
	case 256, 240:
		downscaleFactor = 2
	case 128, 120:
		downscaleFactor = 4
	default:
		downscaleFactor = 1
	}

	*psyConf = PsyConfiguration{} // FDKmemclear(psyConf, sizeof(PSY_CONFIGURATION))
	psyConf.GranuleLength = granuleLength
	psyConf.Filterbank = filterbank

	if useIS != 0 && (bitrate/bandwidth) < 5 {
		psyConf.AllowIS = 1
	} else {
		psyConf.AllowIS = 0
	}
	psyConf.AllowMS = useMS

	// init sfb table
	sfbCnt, errorStatus := initSfbTable(samplerate*downscaleFactor, blocktype,
		granuleLength*downscaleFactor, psyConf.SfbOffset[:])
	psyConf.SfbCnt = sfbCnt
	if errorStatus != aacEncOK {
		return errorStatus
	}

	// calculate barc values for each pb
	initBarcValues(psyConf.SfbCnt, psyConf.SfbOffset[:],
		int(psyConf.SfbOffset[psyConf.SfbCnt]), samplerate, sfbBarcVal[:])

	initMinPCMResolution(psyConf.SfbCnt, psyConf.SfbOffset[:], psyConf.SfbPcmQuantThreshold[:])

	// calculate spreading function
	initSpreading(psyConf.SfbCnt, sfbBarcVal[:],
		psyConf.SfbMaskLowFactor[:], psyConf.SfbMaskHighFactor[:],
		psyConf.SfbMaskLowFactorSprEn[:], psyConf.SfbMaskHighFactorSprEn[:],
		bitrate, blocktype)

	// init ratio
	psyConf.MaxAllowedIncreaseFactor = 2         // integer
	psyConf.MinRemainingThresholdFactor = 0x0148 // FL2FXCONST_SGL(0.01f)

	psyConf.ClipEnergy = int32(0x773593ff) // FL2FXCONST_DBL(1.0e9*NORM_PCM_ENERGY)

	if blocktype != encShortWindow {
		psyConf.LowpassLine = (2 * bandwidth * frameLengthLong) / samplerate
		psyConf.LowpassLineLFE = lfeLowpassLine
	} else {
		psyConf.LowpassLine = (2 * bandwidth * frameLengthShort) / samplerate
		psyConf.LowpassLineLFE = 0 // LFE only in long blocks
		psyConf.ClipEnergy >>= 6
	}

	var sfb int
	for sfb = 0; sfb < psyConf.SfbCnt; sfb++ {
		if int(psyConf.SfbOffset[sfb]) >= psyConf.LowpassLine {
			break
		}
	}
	psyConf.SfbActive = fixMax(sfb, 1) // fMax(sfb, 1)

	for sfb = 0; sfb < psyConf.SfbCnt; sfb++ {
		if int(psyConf.SfbOffset[sfb]) >= psyConf.LowpassLineLFE {
			break
		}
	}
	psyConf.SfbActiveLFE = sfb
	psyConf.SfbActive = fixMax(psyConf.SfbActive, psyConf.SfbActiveLFE)

	// calculate minSnr
	initMinSnr(bitrate, samplerate*downscaleFactor,
		int(psyConf.SfbOffset[psyConf.SfbCnt]), psyConf.SfbOffset[:],
		psyConf.SfbActive, blocktype, psyConf.SfbMinSnrLdData[:])

	return aacEncOK
}
