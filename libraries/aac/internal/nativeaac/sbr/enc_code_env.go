// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// DPCM envelope/noise-floor coding — the 1:1 port of libSBRenc/src/code_env.cpp
// ($Revision: 92790$). This is the ENCODE counterpart of the decode-side
// huff_dec/env_dec: it delta-codes the quantised envelope and noise
// scalefactors in time or frequency direction (whichever costs fewer bits via
// the Huffman length tables), choosing the direction per envelope and updating
// the SBR_CODE_ENVELOPE state. fdk-aac SBR is FIXED-POINT — every value is an
// int32 FIXP_DBL (Q-format), so this is EXACT-integer parity (no FP discipline).
//
// Scope: HE-AAC v1 only. The low-delay/ELD-specific paths are excluded.
package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// fMultCE is fMult(FIXP_DBL, FIXP_DBL) == fixmul_DD (common_fix.h:241).
func fMultCE(a, b int32) int32 { return nativeaac.FMultDD(a, b) }

// fixMinI / fixMaxI are fixMin / fixMax on plain ints (common_fix.h).
func fixMinI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func fixMaxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// FREQ / TIME direction constants (sbr_def.h:244-245).
const (
	codeEnvDirFreq = 0 // FREQ
	codeEnvDirTime = 1 // TIME
)

// Codebook LAV + start-bit constants (sbr_def.h:226-256).
const (
	codeBookScfLav10         = 60 // CODE_BOOK_SCF_LAV10
	codeBookScfLav11         = 31 // CODE_BOOK_SCF_LAV11
	codeBookScfLavBalance11  = 12 // CODE_BOOK_SCF_LAV_BALANCE11
	codeBookScfLavBalance10  = 24 // CODE_BOOK_SCF_LAV_BALANCE10
	siSbrStartEnvBitsAmp30   = 6  // SI_SBR_START_ENV_BITS_AMP_RES_3_0
	siSbrStartEnvBitsBal30   = 5  // SI_SBR_START_ENV_BITS_BALANCE_AMP_RES_3_0
	siSbrStartNoiseBits30    = 5  // SI_SBR_START_NOISE_BITS_AMP_RES_3_0
	siSbrStartNoiseBitsBal30 = 5  // SI_SBR_START_NOISE_BITS_BALANCE_AMP_RES_3_0
	siSbrStartEnvBitsAmp15   = 7  // SI_SBR_START_ENV_BITS_AMP_RES_1_5
	siSbrStartEnvBitsBal15   = 6  // SI_SBR_START_ENV_BITS_BALANCE_AMP_RES_1_5
	dfractBitsCE             = 32 // DFRACT_BITS
)

// AmpRes is the 1:1 port of enum AMP_RES (sbr_def.h:258).
type AmpRes int

const (
	SbrAmpRes15 AmpRes = 0 // SBR_AMP_RES_1_5
	SbrAmpRes30 AmpRes = 1 // SBR_AMP_RES_3_0
)

// SbrCodeEnvelope is the 1:1 port of struct SBR_CODE_ENVELOPE (code_env.h:113-145).
type SbrCodeEnvelope struct {
	Offset             int
	UpDate             int
	NSfb               [2]int
	SfbNrgPrev         [encMaxFreqCoeffs]int8 // sfb_nrg_prev (SCHAR)
	DeltaTAcrossFrames int
	DFEdge1stEnv       int32 // dF_edge_1stEnv (FIXP_DBL)
	DFEdgeIncr         int32 // dF_edge_incr (FIXP_DBL)
	DFEdgeIncrFac      int   // dF_edge_incr_fac

	CodeBookScfLavTime int
	CodeBookScfLavFreq int

	CodeBookScfLavLevelTime   int
	CodeBookScfLavLevelFreq   int
	CodeBookScfLavBalanceTime int
	CodeBookScfLavBalanceFreq int

	StartBits        int
	StartBitsBalance int

	HufftableTimeL []uint8
	HufftableFreqL []uint8

	HufftableLevelTimeL   []uint8
	HufftableBalanceTimeL []uint8
	HufftableLevelFreqL   []uint8
	HufftableBalanceFreqL []uint8
}

// indexLow2High is the 1:1 port of indexLow2High (code_env.cpp:267-283).
func indexLow2HighEnc(offset, index int, res FreqRes) int {
	if res == FreqResLow {
		if offset >= 0 {
			if index < offset {
				return index
			}
			return 2*index - offset
		}
		offset = -offset
		if index < offset {
			return 2*index + index
		}
		return 2*index + offset
	}
	return index
}

// mapLowResEnergyVal is the 1:1 port of mapLowResEnergyVal (code_env.cpp:296-319).
func mapLowResEnergyValEnc(currVal int8, prevData []int8, offset, index int, res FreqRes) {
	if res == FreqResLow {
		if offset >= 0 {
			if index < offset {
				prevData[index] = currVal
			} else {
				prevData[2*index-offset] = currVal
				prevData[2*index+1-offset] = currVal
			}
		} else {
			offset = -offset
			if index < offset {
				prevData[3*index] = currVal
				prevData[3*index+1] = currVal
				prevData[3*index+2] = currVal
			} else {
				prevData[2*index+offset] = currVal
				prevData[2*index+1+offset] = currVal
			}
		}
	} else {
		prevData[index] = currVal
	}
}

// computeBits is the 1:1 port of computeBits (code_env.cpp:336-382). It returns
// the Huffman code-word length for *delta, clamping delta to the codebook LAV
// (returning 10000 — the "infeasible" sentinel — when clamping was necessary,
// after writing the clamped value back through delta).
func computeBits(delta *int8, codeBookScfLavLevel, codeBookScfLavBalance int,
	hufftableLevel, hufftableBalance []uint8, coupling, channel int) int {
	var index int
	deltaBits := 0

	if coupling != 0 {
		if channel == 1 {
			if int(*delta) < 0 {
				index = fixMaxI(int(*delta), -codeBookScfLavBalance)
			} else {
				index = fixMinI(int(*delta), codeBookScfLavBalance)
			}
			if index != int(*delta) {
				*delta = int8(index)
				return 10000
			}
			deltaBits = int(hufftableBalance[index+codeBookScfLavBalance])
		} else {
			if int(*delta) < 0 {
				index = fixMaxI(int(*delta), -codeBookScfLavLevel)
			} else {
				index = fixMinI(int(*delta), codeBookScfLavLevel)
			}
			if index != int(*delta) {
				*delta = int8(index)
				return 10000
			}
			deltaBits = int(hufftableLevel[index+codeBookScfLavLevel])
		}
	} else {
		if int(*delta) < 0 {
			index = fixMaxI(int(*delta), -codeBookScfLavLevel)
		} else {
			index = fixMinI(int(*delta), codeBookScfLavLevel)
		}
		if index != int(*delta) {
			*delta = int8(index)
			return 10000
		}
		deltaBits = int(hufftableLevel[index+codeBookScfLavLevel])
	}
	return deltaBits
}

// CodeEnvelope is the 1:1 port of FDKsbrEnc_codeEnvelope (code_env.cpp:403-572).
// sfbNrg is modified in place (replaced with the delta-coded samples),
// directionVec[i] receives FREQ/TIME per envelope, and h.SfbNrgPrev/h.UpDate are
// updated.
func CodeEnvelope(sfbNrg []int8, freqRes []FreqRes, h *SbrCodeEnvelope,
	directionVec []int, coupling, nEnvelopes, channel, headerActive int) {
	var (
		noOfBands                      int
		tmp1, tmp2, tmp3, dFEdge1stEnv int32
	)

	var (
		codeBookScfLavLevelTime   int
		codeBookScfLavLevelFreq   int
		codeBookScfLavBalanceTime int
		codeBookScfLavBalanceFreq int
		hufftableLevelTimeL       []uint8
		hufftableBalanceTimeL     []uint8
		hufftableLevelFreqL       []uint8
		hufftableBalanceFreqL     []uint8
	)

	offset := h.Offset
	var envDataTableCompFactor int

	deltaFBits, deltaTBits := 0, 0
	var useDT bool

	var deltaF [encMaxFreqCoeffs]int8
	var deltaT [encMaxFreqCoeffs]int8
	var lastNrg, currNrg int8

	// tmp1 = FL2FXCONST_DBL(0.5f) >> (DFRACT_BITS - 16 - 1)
	tmp1 = fl2f(0.5) >> (dfractBitsCE - 16 - 1)
	tmp2 = h.DFEdge1stEnv >> (dfractBitsCE - 16)
	tmp3 = fMultCE(h.DFEdgeIncr, int32(h.DFEdgeIncrFac)<<15)
	dFEdge1stEnv = tmp1 + tmp2 + tmp3

	if coupling != 0 {
		codeBookScfLavLevelTime = h.CodeBookScfLavLevelTime
		codeBookScfLavLevelFreq = h.CodeBookScfLavLevelFreq
		codeBookScfLavBalanceTime = h.CodeBookScfLavBalanceTime
		codeBookScfLavBalanceFreq = h.CodeBookScfLavBalanceFreq
		hufftableLevelTimeL = h.HufftableLevelTimeL
		hufftableBalanceTimeL = h.HufftableBalanceTimeL
		hufftableLevelFreqL = h.HufftableLevelFreqL
		hufftableBalanceFreqL = h.HufftableBalanceFreqL
	} else {
		codeBookScfLavLevelTime = h.CodeBookScfLavTime
		codeBookScfLavLevelFreq = h.CodeBookScfLavFreq
		codeBookScfLavBalanceTime = h.CodeBookScfLavTime
		codeBookScfLavBalanceFreq = h.CodeBookScfLavFreq
		hufftableLevelTimeL = h.HufftableTimeL
		hufftableBalanceTimeL = h.HufftableTimeL
		hufftableLevelFreqL = h.HufftableFreqL
		hufftableBalanceFreqL = h.HufftableFreqL
	}

	if coupling == 1 && channel == 1 {
		envDataTableCompFactor = 1
	} else {
		envDataTableCompFactor = 0
	}

	if h.DeltaTAcrossFrames == 0 {
		h.UpDate = 0
	}
	if headerActive != 0 {
		h.UpDate = 0
	}

	sfbBase := 0 // running offset into sfbNrg (replaces the moving sfb_nrg pointer)
	for i := 0; i < nEnvelopes; i++ {
		if freqRes[i] == FreqResHigh {
			noOfBands = h.NSfb[FreqResHigh]
		} else {
			noOfBands = h.NSfb[FreqResLow]
		}

		ptrBase := sfbBase
		currNrg = sfbNrg[ptrBase]

		deltaF[0] = currNrg >> envDataTableCompFactor

		if coupling != 0 && channel == 1 {
			deltaFBits = h.StartBitsBalance
		} else {
			deltaFBits = h.StartBits
		}

		if h.UpDate != 0 {
			deltaT[0] = (currNrg - h.SfbNrgPrev[0]) >> envDataTableCompFactor
			deltaTBits = computeBits(&deltaT[0], codeBookScfLavLevelTime,
				codeBookScfLavBalanceTime, hufftableLevelTimeL,
				hufftableBalanceTimeL, coupling, channel)
		}

		mapLowResEnergyValEnc(currNrg, h.SfbNrgPrev[:], offset, 0, freqRes[i])

		// ensure that nrg difference is not higher than codeBookScfLavXXXFreq
		if coupling != 0 && channel == 1 {
			for band := noOfBands - 1; band > 0; band-- {
				if int(sfbNrg[ptrBase+band])-int(sfbNrg[ptrBase+band-1]) > codeBookScfLavBalanceFreq {
					sfbNrg[ptrBase+band-1] = sfbNrg[ptrBase+band] - int8(codeBookScfLavBalanceFreq)
				}
			}
			for band := 1; band < noOfBands; band++ {
				if int(sfbNrg[ptrBase+band-1])-int(sfbNrg[ptrBase+band]) > codeBookScfLavBalanceFreq {
					sfbNrg[ptrBase+band] = sfbNrg[ptrBase+band-1] - int8(codeBookScfLavBalanceFreq)
				}
			}
		} else {
			for band := noOfBands - 1; band > 0; band-- {
				if int(sfbNrg[ptrBase+band])-int(sfbNrg[ptrBase+band-1]) > codeBookScfLavLevelFreq {
					sfbNrg[ptrBase+band-1] = sfbNrg[ptrBase+band] - int8(codeBookScfLavLevelFreq)
				}
			}
			for band := 1; band < noOfBands; band++ {
				if int(sfbNrg[ptrBase+band-1])-int(sfbNrg[ptrBase+band]) > codeBookScfLavLevelFreq {
					sfbNrg[ptrBase+band] = sfbNrg[ptrBase+band-1] - int8(codeBookScfLavLevelFreq)
				}
			}
		}

		// Coding loop
		ptr := ptrBase
		for band := 1; band < noOfBands; band++ {
			lastNrg = sfbNrg[ptr]
			ptr++
			currNrg = sfbNrg[ptr]

			deltaF[band] = (currNrg - lastNrg) >> envDataTableCompFactor

			deltaFBits += computeBits(&deltaF[band], codeBookScfLavLevelFreq,
				codeBookScfLavBalanceFreq, hufftableLevelFreqL,
				hufftableBalanceFreqL, coupling, channel)

			if h.UpDate != 0 {
				deltaT[band] = currNrg - h.SfbNrgPrev[indexLow2HighEnc(offset, band, freqRes[i])]
				deltaT[band] = deltaT[band] >> envDataTableCompFactor
			}

			mapLowResEnergyValEnc(currNrg, h.SfbNrgPrev[:], offset, band, freqRes[i])

			if h.UpDate != 0 {
				deltaTBits += computeBits(&deltaT[band], codeBookScfLavLevelTime,
					codeBookScfLavBalanceTime, hufftableLevelTimeL,
					hufftableBalanceTimeL, coupling, channel)
			}
		}

		// Replace sfb_nrg with deltacoded samples and set flag
		if i == 0 {
			// tmp_bits = (((delta_T_bits * dF_edge_1stEnv) >> (DFRACT_BITS-18)) + 1) >> 1
			tmpBits := ((int32(deltaTBits)*dFEdge1stEnv)>>(dfractBitsCE-18) + 1) >> 1
			useDT = h.UpDate != 0 && int32(deltaFBits) > tmpBits
		} else {
			useDT = deltaTBits < deltaFBits && h.UpDate != 0
		}

		if useDT {
			directionVec[i] = codeEnvDirTime
			copy(sfbNrg[sfbBase:sfbBase+noOfBands], deltaT[:noOfBands])
		} else {
			h.UpDate = 0
			directionVec[i] = codeEnvDirFreq
			copy(sfbNrg[sfbBase:sfbBase+noOfBands], deltaF[:noOfBands])
		}
		sfbBase += noOfBands
		h.UpDate = 1
	}
}

// InitSbrHuffmanTables is the 1:1 port of FDKsbrEnc_InitSbrHuffmanTables
// (code_env.cpp:115-253). It binds the amp-res-dependent Huffman code/length
// ROM pointers into sbrEnvData and the two SBR_CODE_ENVELOPE handles (envelope +
// noise). Returns 1 on a nil handle or undefined amp_res, 0 on success.
func InitSbrHuffmanTables(sbrEnvData *SbrEnvData, henv, hnoise *SbrCodeEnvelope, ampRes AmpRes) int {
	if henv == nil || hnoise == nil || sbrEnvData == nil {
		return 1
	}
	sbrEnvData.InitSbrAmpRes = ampRes

	switch ampRes {
	case SbrAmpRes30:
		sbrEnvData.HufftableLevelTimeC = Tab_v_Huff_envelopeLevelC11T[:]
		sbrEnvData.HufftableLevelTimeL = Tab_v_Huff_envelopeLevelL11T[:]
		sbrEnvData.HufftableBalanceTimeC = Tab_bookSbrEnvBalanceC11T[:]
		sbrEnvData.HufftableBalanceTimeL = Tab_bookSbrEnvBalanceL11T[:]

		sbrEnvData.HufftableLevelFreqC = Tab_v_Huff_envelopeLevelC11F[:]
		sbrEnvData.HufftableLevelFreqL = Tab_v_Huff_envelopeLevelL11F[:]
		sbrEnvData.HufftableBalanceFreqC = Tab_bookSbrEnvBalanceC11F[:]
		sbrEnvData.HufftableBalanceFreqL = Tab_bookSbrEnvBalanceL11F[:]

		sbrEnvData.HufftableTimeC = Tab_v_Huff_envelopeLevelC11T[:]
		sbrEnvData.HufftableTimeL = Tab_v_Huff_envelopeLevelL11T[:]
		sbrEnvData.HufftableFreqC = Tab_v_Huff_envelopeLevelC11F[:]
		sbrEnvData.HufftableFreqL = Tab_v_Huff_envelopeLevelL11F[:]

		sbrEnvData.CodeBookScfLavBalance = codeBookScfLavBalance11
		sbrEnvData.CodeBookScfLav = codeBookScfLav11

		sbrEnvData.SiSbrStartEnvBits = siSbrStartEnvBitsAmp30
		sbrEnvData.SiSbrStartEnvBitsBalance = siSbrStartEnvBitsBal30

	case SbrAmpRes15:
		sbrEnvData.HufftableLevelTimeC = Tab_v_Huff_envelopeLevelC10T[:]
		sbrEnvData.HufftableLevelTimeL = Tab_v_Huff_envelopeLevelL10T[:]
		sbrEnvData.HufftableBalanceTimeC = Tab_bookSbrEnvBalanceC10T[:]
		sbrEnvData.HufftableBalanceTimeL = Tab_bookSbrEnvBalanceL10T[:]

		sbrEnvData.HufftableLevelFreqC = Tab_v_Huff_envelopeLevelC10F[:]
		sbrEnvData.HufftableLevelFreqL = Tab_v_Huff_envelopeLevelL10F[:]
		sbrEnvData.HufftableBalanceFreqC = Tab_bookSbrEnvBalanceC10F[:]
		sbrEnvData.HufftableBalanceFreqL = Tab_bookSbrEnvBalanceL10F[:]

		sbrEnvData.HufftableTimeC = Tab_v_Huff_envelopeLevelC10T[:]
		sbrEnvData.HufftableTimeL = Tab_v_Huff_envelopeLevelL10T[:]
		sbrEnvData.HufftableFreqC = Tab_v_Huff_envelopeLevelC10F[:]
		sbrEnvData.HufftableFreqL = Tab_v_Huff_envelopeLevelL10F[:]

		sbrEnvData.CodeBookScfLavBalance = codeBookScfLavBalance10
		sbrEnvData.CodeBookScfLav = codeBookScfLav10

		sbrEnvData.SiSbrStartEnvBits = siSbrStartEnvBitsAmp15
		sbrEnvData.SiSbrStartEnvBitsBalance = siSbrStartEnvBitsBal15

	default:
		return 1
	}

	// common to both amp_res values — noise data
	sbrEnvData.HufftableNoiseLevelTimeC = Tab_v_Huff_NoiseLevelC11T[:]
	sbrEnvData.HufftableNoiseLevelTimeL = Tab_v_Huff_NoiseLevelL11T[:]
	sbrEnvData.HufftableNoiseBalanceTimeC = Tab_bookSbrNoiseBalanceC11T[:]
	sbrEnvData.HufftableNoiseBalanceTimeL = Tab_bookSbrNoiseBalanceL11T[:]

	sbrEnvData.HufftableNoiseLevelFreqC = Tab_v_Huff_envelopeLevelC11F[:]
	sbrEnvData.HufftableNoiseLevelFreqL = Tab_v_Huff_envelopeLevelL11F[:]
	sbrEnvData.HufftableNoiseBalanceFreqC = Tab_bookSbrEnvBalanceC11F[:]
	sbrEnvData.HufftableNoiseBalanceFreqL = Tab_bookSbrEnvBalanceL11F[:]

	sbrEnvData.HufftableNoiseTimeC = Tab_v_Huff_NoiseLevelC11T[:]
	sbrEnvData.HufftableNoiseTimeL = Tab_v_Huff_NoiseLevelL11T[:]
	sbrEnvData.HufftableNoiseFreqC = Tab_v_Huff_envelopeLevelC11F[:]
	sbrEnvData.HufftableNoiseFreqL = Tab_v_Huff_envelopeLevelL11F[:]

	sbrEnvData.SiSbrStartNoiseBits = siSbrStartNoiseBits30
	sbrEnvData.SiSbrStartNoiseBitsBalance = siSbrStartNoiseBitsBal30

	// init envelope tables and codebooks
	henv.CodeBookScfLavBalanceTime = sbrEnvData.CodeBookScfLavBalance
	henv.CodeBookScfLavBalanceFreq = sbrEnvData.CodeBookScfLavBalance
	henv.CodeBookScfLavLevelTime = sbrEnvData.CodeBookScfLav
	henv.CodeBookScfLavLevelFreq = sbrEnvData.CodeBookScfLav
	henv.CodeBookScfLavTime = sbrEnvData.CodeBookScfLav
	henv.CodeBookScfLavFreq = sbrEnvData.CodeBookScfLav

	henv.HufftableLevelTimeL = sbrEnvData.HufftableLevelTimeL
	henv.HufftableBalanceTimeL = sbrEnvData.HufftableBalanceTimeL
	henv.HufftableTimeL = sbrEnvData.HufftableTimeL
	henv.HufftableLevelFreqL = sbrEnvData.HufftableLevelFreqL
	henv.HufftableBalanceFreqL = sbrEnvData.HufftableBalanceFreqL
	henv.HufftableFreqL = sbrEnvData.HufftableFreqL

	henv.CodeBookScfLavFreq = sbrEnvData.CodeBookScfLav
	henv.CodeBookScfLavTime = sbrEnvData.CodeBookScfLav

	henv.StartBits = sbrEnvData.SiSbrStartEnvBits
	henv.StartBitsBalance = sbrEnvData.SiSbrStartEnvBitsBalance

	// init noise tables and codebooks
	hnoise.CodeBookScfLavBalanceTime = codeBookScfLavBalance11
	hnoise.CodeBookScfLavBalanceFreq = codeBookScfLavBalance11
	hnoise.CodeBookScfLavLevelTime = codeBookScfLav11
	hnoise.CodeBookScfLavLevelFreq = codeBookScfLav11
	hnoise.CodeBookScfLavTime = codeBookScfLav11
	hnoise.CodeBookScfLavFreq = codeBookScfLav11

	hnoise.HufftableLevelTimeL = sbrEnvData.HufftableNoiseLevelTimeL
	hnoise.HufftableBalanceTimeL = sbrEnvData.HufftableNoiseBalanceTimeL
	hnoise.HufftableTimeL = sbrEnvData.HufftableNoiseTimeL
	hnoise.HufftableLevelFreqL = sbrEnvData.HufftableNoiseLevelFreqL
	hnoise.HufftableBalanceFreqL = sbrEnvData.HufftableNoiseBalanceFreqL
	hnoise.HufftableFreqL = sbrEnvData.HufftableNoiseFreqL

	hnoise.StartBits = sbrEnvData.SiSbrStartNoiseBits
	hnoise.StartBitsBalance = sbrEnvData.SiSbrStartNoiseBitsBalance

	// No delta coding in time from the previous frame due to 1.5dB FIX-FIX rule
	henv.UpDate = 0
	hnoise.UpDate = 0
	return 0
}

// InitSbrCodeEnvelope is the 1:1 port of FDKsbrEnc_InitSbrCodeEnvelope
// (code_env.cpp:585-602). It clears the state and seeds the tuning fields.
func InitSbrCodeEnvelope(h *SbrCodeEnvelope, nSfb []int, deltaTAcrossFrames int,
	dFEdge1stEnv, dFEdgeIncr int32) int {
	*h = SbrCodeEnvelope{}
	h.DeltaTAcrossFrames = deltaTAcrossFrames
	h.DFEdge1stEnv = dFEdge1stEnv
	h.DFEdgeIncr = dFEdgeIncr
	h.DFEdgeIncrFac = 0
	h.UpDate = 0
	h.NSfb[FreqResLow] = nSfb[FreqResLow]
	h.NSfb[FreqResHigh] = nSfb[FreqResHigh]
	h.Offset = 2*h.NSfb[FreqResLow] - h.NSfb[FreqResHigh]
	return 0
}
