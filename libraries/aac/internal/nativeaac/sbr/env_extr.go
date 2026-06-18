// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// SBR bitstream extraction, ported 1:1 from the vendored Fraunhofer FDK-AAC
// reference libSBRdec/src/env_extr.cpp. This file parses the raw SBR extension
// payload bits into a fully populated SBR_FRAME_DATA: the time/frequency grid
// (FRAME_INFO), the delta-coding direction vectors, the inverse-filtering
// modes, the raw (Huffman/PCM) envelope and noise scale-factor index data, and
// the add-harmonics flags.
//
// HE-AAC v1 (STD) scope. The following C branches are deliberately EXCLUDED
// (out of HE-AAC v1 scope) and are noted at their call sites:
//   - Parametric Stereo (PS) decode in extractExtendedData — PS is HE-AAC v2.
//   - USAC / RSVD50 syntax (SBRDEC_SYNTAX_USAC|RSVD50): HBE patching mode,
//     inter-TES, independency flag, USAC envelope/grid limits.
//   - PVC (Predictive Vector Coding): sbrGetPvcEnvelope, extractPvcFrameInfo.
//   - ELD low-delay grid: extractLowDelayGrid, generateFixFixOnly (and the
//     FDK_sbrDecoder_envelopeTable_* ROMs they index — not ported).
// For the HE-AAC v1 (AAC) path, `flags` is 0 and `configMode` is 0, so every
// `flags & (USAC|RSVD50|ELD|SCAL)` test is false and those branches fold away,
// exactly as in the C.
//
// fdk-aac SBR is fixed-point: FIXP_SGL == int16 (the iEnvelope[] /
// sbrNoiseFloorLevel[] raw indices), ULONG == uint32 (addHarmonics), the rest
// UCHAR/SCHAR/INT. Parsing is pure integer work — bit-exact in any build.

// AC_CM_DET_CFG_CHANGE is the config-change-detection mode flag passed as
// configMode (transport_decoder/conceal). For HE-AAC v1 decode it is 0.
//
// C counterpart: AC_CM_DET_CFG_CHANGE (libAACdec/include/aacdecoder.h).
const acCmDetCfgChange = 0x2

// SBR syntax flags (env_extr.h:204-237). sbrdecSyntaxScal/Usac/Rsvd50 and
// sbrdecQuadRate are already defined in freq_sca.go (the single coherent copy);
// only the ones additionally consulted by the parse path are named here.
const (
	sbrdecEldGrid     = 1    // SBRDEC_ELD_GRID
	sbrdecUsacIndep   = 16   // SBRDEC_USAC_INDEP
	sbrdecUsacHarmSbr = 256  // SBRDEC_USAC_HARMONICSBR
	sbrdecUsacItes    = 1024 // SBRDEC_USAC_ITES
)

const extensionIDPSCoding = 2 // EXTENSION_ID_PS_CODING (env_extr.cpp:165)

// srMapping is SR_MAPPING (env_extr.cpp:202-206): a sampling-frequency mapping
// table entry.
type srMapping struct {
	fsRangeLo uint32 // fsRangeLo
	fsMapped  uint32 // fsMapped
}

// stdSampleRatesMapping is the non-USAC std-sample-rate map (env_extr.cpp:208).
var stdSampleRatesMapping = []srMapping{
	{0, 8000}, {9391, 11025}, {11502, 12000}, {13856, 16000},
	{18783, 22050}, {23004, 24000}, {27713, 32000}, {37566, 44100},
	{46009, 48000}, {55426, 64000}, {75132, 88200}, {92017, 96000},
}

// stdSampleRatesMappingUsac is the USAC std-sample-rate map (env_extr.cpp:212).
// Out of HE-AAC v1 scope but kept for a coherent 1:1 sbrdec_mapToStdSampleRate.
var stdSampleRatesMappingUsac = []srMapping{
	{0, 16000}, {18783, 22050}, {23004, 24000}, {27713, 32000},
	{35777, 40000}, {42000, 44100}, {46009, 48000}, {55426, 64000},
	{75132, 88200}, {92017, 96000},
}

// sbrdecMapToStdSampleRate maps an arbitrary sampling frequency to the nearest
// standard rate for which setup tables exist (e.g. 25600 -> 24000), per
// 14496-3 (4.6.18.2.6) table 4.82. Used for startFreq calculation.
//
// C counterpart: sbrdec_mapToStdSampleRate (env_extr.cpp:217-240).
func sbrdecMapToStdSampleRate(fs uint32, isUsac uint32) uint32 {
	fsMapped := fs
	var mappingTable []srMapping
	if isUsac == 0 {
		mappingTable = stdSampleRatesMapping
	} else {
		mappingTable = stdSampleRatesMappingUsac
	}
	for i := len(mappingTable) - 1; i >= 0; i-- {
		if fs >= mappingTable[i].fsRangeLo {
			fsMapped = mappingTable[i].fsMapped
			break
		}
	}
	return fsMapped
}

// sbrGetHeaderData reads SBR header data from the bitstream.
//
// Returns one of the SBR_HEADER_STATUS values (headerOK / headerReset). The
// USAC/RSVD50 default-header (bs_dflt) split and config-change-detection
// (configMode & AC_CM_DET_CFG_CHANGE) branches are ported faithfully; for the
// HE-AAC v1 path flags==0 and configMode==0, so pBsData is always bs_data.
//
// C counterpart: sbrGetHeaderData (env_extr.cpp:377-468).
func sbrGetHeaderData(hHeaderData *SbrHeaderData, hBs *bitStream, flags uint, fIsSbrData int, configMode uint8) int {
	var headerExtra1, headerExtra2 int

	// Read and discard new header in config change detection mode.
	if configMode&acCmDetCfgChange != 0 {
		if flags&(sbrdecSyntaxRsvd50|sbrdecSyntaxUsac) == 0 {
			hBs.readBits(1) // ampResolution
		}
		hBs.pushFor(8) // startFreq, stopFreq
		if flags&(sbrdecSyntaxRsvd50|sbrdecSyntaxUsac) == 0 {
			hBs.readBits(3) // xover_band
			hBs.readBits(2) // reserved bits
		}
		headerExtra1 = int(hBs.readBit())
		headerExtra2 = int(hBs.readBit())
		hBs.pushFor(uint32(5*headerExtra1 + 6*headerExtra2))
		return headerOK
	}

	// Copy SBR bit stream header to temporary header.
	lastHeader := hHeaderData.BsData
	lastInfo := hHeaderData.BsInfo

	// Read new header from bitstream.
	var pBsData *SbrHeaderDataBS
	if flags&(sbrdecSyntaxRsvd50|sbrdecSyntaxUsac) != 0 && fIsSbrData == 0 {
		pBsData = &hHeaderData.BsDflt
	} else {
		pBsData = &hHeaderData.BsData
	}

	if flags&(sbrdecSyntaxRsvd50|sbrdecSyntaxUsac) == 0 {
		hHeaderData.BsInfo.AmpResolution = uint8(hBs.readBits(1))
	}

	pBsData.StartFreq = uint8(hBs.readBits(4))
	pBsData.StopFreq = uint8(hBs.readBits(4))

	if flags&(sbrdecSyntaxRsvd50|sbrdecSyntaxUsac) == 0 {
		hHeaderData.BsInfo.XoverBand = uint8(hBs.readBits(3))
		hBs.readBits(2)
	}

	headerExtra1 = int(hBs.readBits(1))
	headerExtra2 = int(hBs.readBits(1))

	// Handle extra header information.
	if headerExtra1 != 0 {
		pBsData.FreqScale = uint8(hBs.readBits(2))
		pBsData.AlterScale = uint8(hBs.readBits(1))
		pBsData.NoiseBands = uint8(hBs.readBits(2))
	} else {
		pBsData.FreqScale = 2
		pBsData.AlterScale = 1
		pBsData.NoiseBands = 2
	}

	if headerExtra2 != 0 {
		pBsData.LimiterBands = uint8(hBs.readBits(2))
		pBsData.LimiterGains = uint8(hBs.readBits(2))
		pBsData.InterpolFreq = uint8(hBs.readBits(1))
		pBsData.SmoothingLength = uint8(hBs.readBits(1))
	} else {
		pBsData.LimiterBands = 2
		pBsData.LimiterGains = 2
		pBsData.InterpolFreq = 1
		pBsData.SmoothingLength = 1
	}

	// Look for new settings. IEC 14496-3, 4.6.18.3.1.
	if int(hHeaderData.SyncState) < sbrHeaderState ||
		lastHeader.StartFreq != pBsData.StartFreq ||
		lastHeader.StopFreq != pBsData.StopFreq ||
		lastHeader.FreqScale != pBsData.FreqScale ||
		lastHeader.AlterScale != pBsData.AlterScale ||
		lastHeader.NoiseBands != pBsData.NoiseBands ||
		lastInfo.XoverBand != hHeaderData.BsInfo.XoverBand {
		return headerReset // New settings
	}

	return headerOK
}

// sbrGetSyntheticCodedData reads the missing-harmonics (synthetic sine) flags.
// Only used for AAC+SBR. Returns the number of bits read.
//
// C counterpart: sbrGetSyntheticCodedData (env_extr.cpp:475-514).
func sbrGetSyntheticCodedData(hHeaderData *SbrHeaderData, hFrameData *SbrFrameData, hBs *bitStream, flags uint) int {
	bitsRead := 0

	addHarmonicFlag := int(hBs.readBits(1))
	bitsRead++

	if addHarmonicFlag != 0 {
		nSfb := int(hHeaderData.FreqBandData.NSfb[1])
		for i := 0; i < addHarmonicsFlagsSz; i++ {
			// read maximum 32 bits and align them to the MSB
			readBits := nativeaac.FMinI(32, nSfb)
			nSfb -= readBits
			if readBits > 0 {
				hFrameData.AddHarmonics[i] = hBs.readBits(uint32(readBits)) << (32 - readBits)
			} else {
				hFrameData.AddHarmonics[i] = 0
			}
			bitsRead += readBits
		}
		// bs_pvc_mode = 0 for Rsvd50; the USAC sinusoidal_position branch is out of
		// HE-AAC v1 scope (flags & SBRDEC_SYNTAX_USAC is false for AAC).
		if flags&sbrdecSyntaxUsac != 0 {
			if hHeaderData.BsInfo.PvcMode != 0 {
				bsSinusoidalPosition := uint32(31)
				if hBs.readBit() != 0 { // bs_sinusoidal_position_flag
					bsSinusoidalPosition = hBs.readBits(5)
				}
				hFrameData.SinusoidalPosition = uint8(bsSinusoidalPosition)
			}
		}
	} else {
		for i := 0; i < addHarmonicsFlagsSz; i++ {
			hFrameData.AddHarmonics[i] = 0
		}
	}

	return bitsRead
}

// extractExtendedData reads (and skips) the SBR extension data from the
// bitstream. The only decoded extension element is the PS coding element
// (EXTENSION_ID_PS_CODING): when a PS decoder is attached (hParametricStereoDec
// != nil) its ps_data is read via readPsData; otherwise (HE-AAC v1) the bytes
// are skipped like an unknown element so the read-bit accounting stays exact.
// Returns frameOk (1 ok, 0 on a length sanity-check failure).
//
// C counterpart: extractExtendedData (env_extr.cpp:525-611).
func extractExtendedData(hHeaderData *SbrHeaderData, hBs *bitStream, hParametricStereoDec *psDec) int {
	frameOk := 1
	bPsRead := false

	extendedData := int(hBs.readBits(1))

	if extendedData != 0 {
		cnt := int(hBs.readBits(4))
		if cnt == (1<<4)-1 {
			cnt += int(hBs.readBits(8))
		}

		nBitsLeft := 8 * cnt

		// sanity check for cnt
		if nBitsLeft > int(hBs.getValidBits()) {
			nBitsLeft = int(hBs.getValidBits())
			frameOk = 0
		}

		for nBitsLeft > 7 {
			extensionID := int(hBs.readBits(2))
			nBitsLeft -= 2

			switch extensionID {
			case extensionIDPSCoding:
				// PS data (env_extr.cpp:559-590). With a PS decoder attached, read
				// ps_data into the read slot. The bPsRead guard mirrors the C: a
				// second PS extension in the same frame without a fresh header is
				// skipped. With no PS decoder (HE-AAC v1) nothing is read and the
				// bytes fall through to the fill consumption below.
				if hParametricStereoDec != nil {
					if bPsRead && hParametricStereoDec.BsData[hParametricStereoDec.BsReadSlot].BPsHeaderValid == 0 {
						cnt = nBitsLeft >> 3
						for i := 0; i < cnt; i++ {
							hBs.readBits(8)
						}
						nBitsLeft -= cnt * 8
					} else {
						nBitsLeft -= int(readPsData(hParametricStereoDec, hBs, nBitsLeft))
						bPsRead = true
					}
				}
			default:
				cnt = nBitsLeft >> 3 // number of remaining bytes
				for i := 0; i < cnt; i++ {
					hBs.readBits(8)
				}
				nBitsLeft -= cnt * 8
			}
		}

		if nBitsLeft < 0 {
			frameOk = 0
		} else {
			// Read fill bits for byte alignment.
			hBs.readBits(uint32(nBitsLeft))
		}
	}

	return frameOk
}

// sbrGetChannelElement reads the bitstream elements of one SBR channel element
// (single channel or channel pair) into hFrameDataLeft (+ hFrameDataRight for a
// CPE). Returns SbrFrameOK (1) or 0 on a parse / consistency error.
//
// hParametricStereoDec is the PS decoder for a mono (non-stereo) element; the
// caller passes nil for a CPE. The PVC / USAC-HBE branches are out of HE-AAC v1
// scope; with flags==0 and pvc_mode==0 they fold away exactly as in the C.
//
// C counterpart: sbrGetChannelElement (env_extr.cpp:617-820).
func sbrGetChannelElement(hHeaderData *SbrHeaderData, hFrameDataLeft, hFrameDataRight *SbrFrameData,
	hFrameDataLeftPrev *SbrPrevFrameData, pvcModeLast uint8, hBs *bitStream, hParametricStereoDec *psDec, flags uint, overlap int) int {
	bsCoupling := couplingOff
	nCh := 1
	if hFrameDataRight != nil {
		nCh = 2
	}

	if flags&(sbrdecSyntaxUsac|sbrdecSyntaxRsvd50) == 0 {
		// Reserved bits
		if hBs.readBits(1) != 0 { // bs_data_extra
			hBs.readBits(4)
			if flags&sbrdecSyntaxScal != 0 || nCh == 2 {
				hBs.readBits(4)
			}
		}
	}

	if nCh == 2 {
		// Read coupling flag
		bsCoupling = int(hBs.readBits(1))
		if bsCoupling != 0 {
			hFrameDataLeft.Coupling = couplingLevel
			hFrameDataRight.Coupling = couplingBal
		} else {
			hFrameDataLeft.Coupling = couplingOff
			hFrameDataRight.Coupling = couplingOff
		}
	} else {
		if flags&sbrdecSyntaxScal != 0 {
			hBs.readBits(1) // bs_coupling
		}
		hFrameDataLeft.Coupling = couplingOff
	}

	// USAC/RSVD50 HBE patching-mode branch — out of HE-AAC v1 scope. For AAC the
	// else-branch runs, fixing the default (legacy) patching mode.
	if flags&(sbrdecSyntaxUsac|sbrdecSyntaxRsvd50) != 0 {
		if flags&sbrdecUsacHarmSbr != 0 {
			hFrameDataLeft.SbrPatchingMode = uint8(hBs.readBit())
			if hFrameDataLeft.SbrPatchingMode == 0 {
				hFrameDataLeft.SbrOversamplingFlag = uint8(hBs.readBit())
				if hBs.readBit() != 0 { // sbrPitchInBinsFlag
					hFrameDataLeft.SbrPitchInBins = uint8(hBs.readBits(7))
				} else {
					hFrameDataLeft.SbrPitchInBins = 0
				}
			} else {
				hFrameDataLeft.SbrOversamplingFlag = 0
				hFrameDataLeft.SbrPitchInBins = 0
			}
			if nCh == 2 {
				if bsCoupling != 0 {
					hFrameDataRight.SbrPatchingMode = hFrameDataLeft.SbrPatchingMode
					hFrameDataRight.SbrOversamplingFlag = hFrameDataLeft.SbrOversamplingFlag
					hFrameDataRight.SbrPitchInBins = hFrameDataLeft.SbrPitchInBins
				} else {
					hFrameDataRight.SbrPatchingMode = uint8(hBs.readBit())
					if hFrameDataRight.SbrPatchingMode == 0 {
						hFrameDataRight.SbrOversamplingFlag = uint8(hBs.readBit())
						if hBs.readBit() != 0 { // sbrPitchInBinsFlag
							hFrameDataRight.SbrPitchInBins = uint8(hBs.readBits(7))
						} else {
							hFrameDataRight.SbrPitchInBins = 0
						}
					} else {
						hFrameDataRight.SbrOversamplingFlag = 0
						hFrameDataRight.SbrPitchInBins = 0
					}
				}
			}
		} else {
			if nCh == 2 {
				hFrameDataRight.SbrPatchingMode = 1
				hFrameDataRight.SbrOversamplingFlag = 0
				hFrameDataRight.SbrPitchInBins = 0
			}
			hFrameDataLeft.SbrPatchingMode = 1
			hFrameDataLeft.SbrOversamplingFlag = 0
			hFrameDataLeft.SbrPitchInBins = 0
		}
	} else {
		if nCh == 2 {
			hFrameDataRight.SbrPatchingMode = 1
			hFrameDataRight.SbrOversamplingFlag = 0
			hFrameDataRight.SbrPitchInBins = 0
		}
		hFrameDataLeft.SbrPatchingMode = 1
		hFrameDataLeft.SbrOversamplingFlag = 0
		hFrameDataLeft.SbrPitchInBins = 0
	}

	// sbr_grid(): Grid control. PVC (bs_info.pvc_mode) is out of HE-AAC v1 scope;
	// for AAC pvc_mode==0 so the extractFrameInfo branch always runs.
	if hHeaderData.BsInfo.PvcMode != 0 {
		// PVC excluded (HE-AAC v2/USAC). Unreachable for HE-AAC v1.
		return 0
	}
	if extractFrameInfo(hBs, hHeaderData, hFrameDataLeft, 1, flags) == 0 {
		return 0
	}
	if checkFrameInfo(&hFrameDataLeft.FrameInfo, int(hHeaderData.NumberTimeSlots), overlap, int(hHeaderData.TimeStep)) == 0 {
		return 0
	}

	if nCh == 2 {
		if hFrameDataLeft.Coupling != 0 {
			hFrameDataRight.FrameInfo = hFrameDataLeft.FrameInfo
			hFrameDataRight.AmpResolutionCurrFrame = hFrameDataLeft.AmpResolutionCurrFrame
		} else {
			if extractFrameInfo(hBs, hHeaderData, hFrameDataRight, 2, flags) == 0 {
				return 0
			}
			if checkFrameInfo(&hFrameDataRight.FrameInfo, int(hHeaderData.NumberTimeSlots), overlap, int(hHeaderData.TimeStep)) == 0 {
				return 0
			}
		}
	}

	// sbr_dtdf(): Fetch domain vectors (time or frequency direction for
	// delta-coding).
	sbrGetDirectionControlData(hFrameDataLeft, hBs, flags, int(hHeaderData.BsInfo.PvcMode))
	if nCh == 2 {
		sbrGetDirectionControlData(hFrameDataRight, hBs, flags, 0)
	}

	// sbr_invf()
	for i := 0; i < int(hHeaderData.FreqBandData.NInvfBands); i++ {
		hFrameDataLeft.SbrInvfMode[i] = invfMode(hBs.readBits(2))
	}
	if nCh == 2 {
		if hFrameDataLeft.Coupling != 0 {
			for i := 0; i < int(hHeaderData.FreqBandData.NInvfBands); i++ {
				hFrameDataRight.SbrInvfMode[i] = hFrameDataLeft.SbrInvfMode[i]
			}
		} else {
			for i := 0; i < int(hHeaderData.FreqBandData.NInvfBands); i++ {
				hFrameDataRight.SbrInvfMode[i] = invfMode(hBs.readBits(2))
			}
		}
	}

	if nCh == 1 {
		// pvc_mode is 0 for HE-AAC v1, so always the sbrGetEnvelope branch.
		if sbrGetEnvelope(hHeaderData, hFrameDataLeft, hBs, flags) == 0 {
			return 0
		}
		sbrGetNoiseFloorData(hHeaderData, hFrameDataLeft, hBs)
	} else if hFrameDataLeft.Coupling != 0 {
		if sbrGetEnvelope(hHeaderData, hFrameDataLeft, hBs, flags) == 0 {
			return 0
		}
		sbrGetNoiseFloorData(hHeaderData, hFrameDataLeft, hBs)
		if sbrGetEnvelope(hHeaderData, hFrameDataRight, hBs, flags) == 0 {
			return 0
		}
		sbrGetNoiseFloorData(hHeaderData, hFrameDataRight, hBs)
	} else { // nCh == 2 && no coupling
		if sbrGetEnvelope(hHeaderData, hFrameDataLeft, hBs, flags) == 0 {
			return 0
		}
		if sbrGetEnvelope(hHeaderData, hFrameDataRight, hBs, flags) == 0 {
			return 0
		}
		sbrGetNoiseFloorData(hHeaderData, hFrameDataLeft, hBs)
		sbrGetNoiseFloorData(hHeaderData, hFrameDataRight, hBs)
	}

	sbrGetSyntheticCodedData(hHeaderData, hFrameDataLeft, hBs, flags)
	if nCh == 2 {
		sbrGetSyntheticCodedData(hHeaderData, hFrameDataRight, hBs, flags)
	}

	if flags&(sbrdecSyntaxUsac|sbrdecSyntaxRsvd50) == 0 {
		if extractExtendedData(hHeaderData, hBs, hParametricStereoDec) == 0 {
			return 0
		}
	}

	_ = hFrameDataLeftPrev // PVC-only; unused in HE-AAC v1.
	_ = pvcModeLast
	return 1
}

// sbrGetDirectionControlData reads the delta-coding direction vectors
// (domain_vec / domain_vec_noise: 0 = frequency, 1 = time) from the bitstream.
//
// C counterpart: sbrGetDirectionControlData (env_extr.cpp:825-855).
func sbrGetDirectionControlData(hFrameData *SbrFrameData, hBs *bitStream, flags uint, bsPvcMode int) {
	indepFlag := 0
	if flags&(sbrdecSyntaxUsac|sbrdecSyntaxRsvd50) != 0 {
		indepFlag = int(flags & sbrdecUsacIndep)
	}

	if bsPvcMode == 0 {
		i := 0
		if indepFlag != 0 {
			hFrameData.DomainVec[i] = 0
			i++
		}
		for ; i < int(hFrameData.FrameInfo.NEnvelopes); i++ {
			hFrameData.DomainVec[i] = uint8(hBs.readBits(1))
		}
	}

	i := 0
	if indepFlag != 0 {
		hFrameData.DomainVecNoise[i] = 0
		i++
	}
	for ; i < int(hFrameData.FrameInfo.NNoiseEnv); i++ {
		hFrameData.DomainVecNoise[i] = uint8(hBs.readBits(1))
	}
}

// sbrGetNoiseFloorData reads the raw noise-floor-level (Huffman/PCM) data from
// the bitstream into sbrNoiseFloorLevel[].
//
// C counterpart: sbrGetNoiseFloorData (env_extr.cpp:860-919).
func sbrGetNoiseFloorData(hHeaderData *SbrHeaderData, hFrameData *SbrFrameData, hBs *bitStream) {
	noNoiseBands := int(hHeaderData.FreqBandData.NNfb)

	coupling := hFrameData.Coupling

	// Select huffman codebook depending on coupling mode.
	var hcbNoise, hcbNoiseF huffman
	var envDataTableCompFactor int
	if coupling == couplingBal {
		hcbNoise = sbrHuffNoiseBalance11T[:]
		hcbNoiseF = sbrHuffEnvBalance11F[:] // "sbr_huffBook_NoiseBalance11F"
		envDataTableCompFactor = 1
	} else {
		hcbNoise = sbrHuffNoiseLevel11T[:]
		hcbNoiseF = sbrHuffEnvLevel11F[:] // "sbr_huffBook_NoiseLevel11F"
		envDataTableCompFactor = 0
	}

	// Read raw noise-envelope data.
	for i := 0; i < int(hFrameData.FrameInfo.NNoiseEnv); i++ {
		if hFrameData.DomainVecNoise[i] == 0 {
			if coupling == couplingBal {
				hFrameData.SbrNoiseFloorLevel[i*noNoiseBands] =
					int16(int(hBs.readBits(5)) << envDataTableCompFactor)
			} else {
				hFrameData.SbrNoiseFloorLevel[i*noNoiseBands] = int16(hBs.readBits(5))
			}
			for j := 1; j < noNoiseBands; j++ {
				delta := decodeHuffmanCW(hcbNoiseF, hBs)
				hFrameData.SbrNoiseFloorLevel[i*noNoiseBands+j] = int16(delta << envDataTableCompFactor)
			}
		} else {
			for j := 0; j < noNoiseBands; j++ {
				delta := decodeHuffmanCW(hcbNoise, hBs)
				hFrameData.SbrNoiseFloorLevel[i*noNoiseBands+j] = int16(delta << envDataTableCompFactor)
			}
		}
	}
}

// sbrGetEnvelope reads the raw envelope (Huffman/PCM) data from the bitstream
// into iEnvelope[]. Returns 1 on success, 0 on a value-count overflow.
//
// C counterpart: sbrGetEnvelope (env_extr.cpp:1022-1141).
func sbrGetEnvelope(hHeaderData *SbrHeaderData, hFrameData *SbrFrameData, hBs *bitStream, flags uint) int {
	var noBand [maxEnvelopes]uint8
	delta := 0
	offset := 0
	coupling := hFrameData.Coupling
	ampRes := int(hHeaderData.BsInfo.AmpResolution)
	nEnvelopes := int(hFrameData.FrameInfo.NEnvelopes)

	hFrameData.NScaleFactors = 0

	if hFrameData.FrameInfo.FrameClass == 0 && nEnvelopes == 1 {
		if flags&sbrdecEldGrid != 0 {
			ampRes = hFrameData.AmpResolutionCurrFrame
		} else {
			ampRes = 0
		}
	}
	hFrameData.AmpResolutionCurrFrame = ampRes

	// Set number of bits for first value depending on amplitude resolution.
	var startBits, startBitsBalance uint32
	if ampRes == 1 {
		startBits = 6
		startBitsBalance = 5
	} else {
		startBits = 7
		startBitsBalance = 6
	}

	// Calculate number of values for each envelope and altogether.
	for i := 0; i < nEnvelopes; i++ {
		noBand[i] = hHeaderData.FreqBandData.NSfb[hFrameData.FrameInfo.FreqRes[i]]
		hFrameData.NScaleFactors += int(noBand[i])
	}
	if hFrameData.NScaleFactors > maxNumEnvelopeValues {
		return 0
	}

	// Select Huffman codebook depending on coupling mode and amplitude resolution.
	var hcbT, hcbF huffman
	var envDataTableCompFactor int
	if coupling == couplingBal {
		envDataTableCompFactor = 1
		if ampRes == 0 {
			hcbT = sbrHuffEnvBalance10T[:]
			hcbF = sbrHuffEnvBalance10F[:]
		} else {
			hcbT = sbrHuffEnvBalance11T[:]
			hcbF = sbrHuffEnvBalance11F[:]
		}
	} else {
		envDataTableCompFactor = 0
		if ampRes == 0 {
			hcbT = sbrHuffEnvLevel10T[:]
			hcbF = sbrHuffEnvLevel10F[:]
		} else {
			hcbT = sbrHuffEnvLevel11T[:]
			hcbF = sbrHuffEnvLevel11F[:]
		}
	}

	hFrameData.ITESactive = 0 // disable inter-TES by default

	// Now read raw envelope data.
	for j := 0; j < nEnvelopes; j++ {
		if hFrameData.DomainVec[j] == 0 {
			if coupling == couplingBal {
				hFrameData.IEnvelope[offset] = int16(int(hBs.readBits(startBitsBalance)) << envDataTableCompFactor)
			} else {
				hFrameData.IEnvelope[offset] = int16(hBs.readBits(startBits))
			}
		}

		for i := 1 - int(hFrameData.DomainVec[j]); i < int(noBand[j]); i++ {
			if hFrameData.DomainVec[j] == 0 {
				delta = decodeHuffmanCW(hcbF, hBs)
			} else {
				delta = decodeHuffmanCW(hcbT, hBs)
			}
			hFrameData.IEnvelope[offset+i] = int16(delta << envDataTableCompFactor)
		}
		// USAC inter-TES (SBRDEC_SYNTAX_USAC && SBRDEC_USAC_ITES) — out of HE-AAC
		// v1 scope; flags has neither bit set for AAC.
		if flags&sbrdecSyntaxUsac != 0 && flags&sbrdecUsacItes != 0 {
			bsTempShape := int(hBs.readBit())
			hFrameData.ITESactive |= uint8(bsTempShape << j)
			if bsTempShape != 0 {
				hFrameData.InterTempShapeMode[j] = uint8(hBs.read2Bits())
			} else {
				hFrameData.InterTempShapeMode[j] = 0
			}
		}
		offset += int(noBand[j])
	}

	// ENV_EXP_FRACT is 0 (env_extr.h:119), so the int-to-scaled-fract shift loop
	// is compiled out.

	return 1
}

// extractFrameInfo extracts the FRAME_INFO time/frequency grid from the
// bitstream. Returns 1 on success, 0 on a bitstream error.
//
// The ELD low-delay grid (SBRDEC_ELD_GRID) and USAC/RSVD50 envelope-limit
// branches are out of HE-AAC v1 scope; for AAC flags==0 so frameClass is read as
// 2 bits and the FIX-FIX (class 0) ROM-copy + the variable grids (class 1/2/3)
// run exactly as in the C.
//
// C counterpart: extractFrameInfo (env_extr.cpp:1345-1649).
func extractFrameInfo(hBs *bitStream, hHeaderData *SbrHeaderData, hFrameData *SbrFrameData, nrOfChannels uint, flags uint) int {
	pFrameInfo := &hFrameData.FrameInfo
	numberTimeSlots := int(hHeaderData.NumberTimeSlots)
	var pointerBits, nEnv, b, border, i, n, k, p, aL, aR, nL, nR, temp, staticFreqRes int
	var frameClass uint8

	if flags&sbrdecEldGrid != 0 {
		// ELD low-delay grid — out of HE-AAC v1 scope (CODEC_AACLD only). Returns 0
		// here because extractLowDelayGrid/generateFixFixOnly (and their envelope
		// ROMs) are not ported. Unreachable for HE-AAC v1 (flags==0).
		frameClass = uint8(hBs.readBits(1))
		if frameClass == 1 {
			return 0
		}
	} else {
		frameClass = uint8(hBs.readBits(2)) // frameClass = C [2 bits]
	}

	switch frameClass {
	case 0:
		temp = int(hBs.readBits(2)) // E [2 bits]
		nEnv = 1 << temp            // E -> e

		if flags&sbrdecEldGrid != 0 && nEnv == 1 {
			hFrameData.AmpResolutionCurrFrame = int(hBs.readBits(1)) // new ELD syntax
		}

		staticFreqRes = int(hBs.readBits(1))

		if flags&(sbrdecSyntaxUsac|sbrdecSyntaxRsvd50) != 0 {
			if nEnv > maxEnvelopes { // MAX_ENVELOPES_USAC == MAX_ENVELOPES (==8)
				return 0
			}
		} else {
			b = nEnv + 1
		}
		switch nEnv {
		case 1:
			switch numberTimeSlots {
			case 15:
				*pFrameInfo = sbrFrameInfo1_15
			case 16:
				*pFrameInfo = sbrFrameInfo1_16
			}
		case 2:
			switch numberTimeSlots {
			case 15:
				*pFrameInfo = sbrFrameInfo2_15
			case 16:
				*pFrameInfo = sbrFrameInfo2_16
			}
		case 4:
			switch numberTimeSlots {
			case 15:
				*pFrameInfo = sbrFrameInfo4_15
			case 16:
				*pFrameInfo = sbrFrameInfo4_16
			}
		case 8:
			// MAX_ENVELOPES >= 8 holds (maxEnvelopes==8).
			switch numberTimeSlots {
			case 15:
				*pFrameInfo = sbrFrameInfo8_15
			case 16:
				*pFrameInfo = sbrFrameInfo8_16
			}
		}
		// Apply correct freqRes (High is default).
		if staticFreqRes == 0 {
			for i = 0; i < nEnv; i++ {
				pFrameInfo.FreqRes[i] = 0
			}
		}

	case 1, 2:
		temp = int(hBs.readBits(2)) // A [2 bits]
		n = int(hBs.readBits(2))    // n = N [2 bits]
		nEnv = n + 1                // # envelopes
		b = nEnv + 1                // # borders
	}

	switch frameClass {
	case 1:
		// Decode borders:
		pFrameInfo.Borders[0] = 0       // first border
		border = temp + numberTimeSlots // A -> aR
		i = b - 1                       // frame info index for last border
		pFrameInfo.Borders[i] = uint8(border)

		for k = 0; k < n; k++ {
			temp = int(hBs.readBits(2)) // R [2 bits]
			border -= 2*temp + 2        // R -> r
			i--
			pFrameInfo.Borders[i] = uint8(border)
		}

		// Decode pointer:
		pointerBits = dfractBits - 1 - nativeaac.CountLeadingBits(int32(n+1))
		p = int(hBs.readBits(uint32(pointerBits)))
		if p > n+1 {
			return 0
		}
		if p != 0 {
			pFrameInfo.TranEnv = int8(n + 2 - p)
		} else {
			pFrameInfo.TranEnv = -1
		}

		// Decode freq res:
		for k = n; k >= 0; k-- {
			pFrameInfo.FreqRes[k] = uint8(hBs.readBits(1))
		}

		// Calculate noise floor middle border:
		if p == 0 || p == 1 {
			pFrameInfo.BordersNoise[1] = pFrameInfo.Borders[n]
		} else {
			pFrameInfo.BordersNoise[1] = pFrameInfo.Borders[pFrameInfo.TranEnv]
		}

	case 2:
		// Decode borders:
		border = temp                         // A -> aL
		pFrameInfo.Borders[0] = uint8(border) // first border

		for k = 1; k <= n; k++ {
			temp = int(hBs.readBits(2)) // R [2 bits]
			border += 2*temp + 2        // R -> r
			pFrameInfo.Borders[k] = uint8(border)
		}
		pFrameInfo.Borders[k] = uint8(numberTimeSlots) // last border

		// Decode pointer:
		pointerBits = dfractBits - 1 - nativeaac.CountLeadingBits(int32(n+1))
		p = int(hBs.readBits(uint32(pointerBits)))
		if p > n+1 {
			return 0
		}

		if p == 0 || p == 1 {
			pFrameInfo.TranEnv = -1
		} else {
			pFrameInfo.TranEnv = int8(p - 1)
		}

		// Decode freq res:
		for k = 0; k <= n; k++ {
			pFrameInfo.FreqRes[k] = uint8(hBs.readBits(1))
		}

		// Calculate noise floor middle border:
		switch p {
		case 0:
			pFrameInfo.BordersNoise[1] = pFrameInfo.Borders[1]
		case 1:
			pFrameInfo.BordersNoise[1] = pFrameInfo.Borders[n]
		default:
			pFrameInfo.BordersNoise[1] = pFrameInfo.Borders[pFrameInfo.TranEnv]
		}

	case 3:
		// v_ctrlSignal = [frameClass,aL,aR,nL,nR,v_rL,v_rR,p,v_fLR];
		aL = int(hBs.readBits(2))                   // AL [2 bits], AL -> aL
		aR = int(hBs.readBits(2)) + numberTimeSlots // AR [2 bits], AR -> aR
		nL = int(hBs.readBits(2))                   // nL = NL [2 bits]
		nR = int(hBs.readBits(2))                   // nR = NR [2 bits]

		nEnv = nL + nR + 1 // # envelopes
		if nEnv > maxEnvelopes {
			return 0
		}
		b = nEnv + 1 // # borders

		// L-borders:
		border = aL // first border
		pFrameInfo.Borders[0] = uint8(border)

		for k = 1; k <= nL; k++ {
			temp = int(hBs.readBits(2)) // R [2 bits]
			border += 2*temp + 2        // R -> r
			pFrameInfo.Borders[k] = uint8(border)
		}

		// R-borders:
		border = aR // last border
		i = nEnv
		pFrameInfo.Borders[i] = uint8(border)

		for k = 0; k < nR; k++ {
			temp = int(hBs.readBits(2)) // R [2 bits]
			border -= 2*temp + 2        // R -> r
			i--
			pFrameInfo.Borders[i] = uint8(border)
		}

		// decode pointer:
		pointerBits = dfractBits - 1 - nativeaac.CountLeadingBits(int32(nL+nR+1))
		p = int(hBs.readBits(uint32(pointerBits)))
		if p > nL+nR+1 {
			return 0
		}
		if p != 0 {
			pFrameInfo.TranEnv = int8(b - p)
		} else {
			pFrameInfo.TranEnv = -1
		}

		// decode freq res:
		for k = 0; k < nEnv; k++ {
			pFrameInfo.FreqRes[k] = uint8(hBs.readBits(1))
		}

		// Decode noise floors:
		pFrameInfo.BordersNoise[0] = uint8(aL)

		if nEnv == 1 {
			pFrameInfo.BordersNoise[1] = uint8(aR)
		} else {
			if p == 0 || p == 1 {
				pFrameInfo.BordersNoise[1] = pFrameInfo.Borders[nEnv-1]
			} else {
				pFrameInfo.BordersNoise[1] = pFrameInfo.Borders[pFrameInfo.TranEnv]
			}
			pFrameInfo.BordersNoise[2] = uint8(aR)
		}
	}

	// Store number of envelopes, noise floor envelopes and frame class.
	pFrameInfo.NEnvelopes = uint8(nEnv)

	if nEnv == 1 {
		pFrameInfo.NNoiseEnv = 1
	} else {
		pFrameInfo.NNoiseEnv = 2
	}

	pFrameInfo.FrameClass = frameClass

	if pFrameInfo.FrameClass == 2 || pFrameInfo.FrameClass == 1 {
		// calculate noise floor first and last borders:
		pFrameInfo.BordersNoise[0] = pFrameInfo.Borders[0]
		pFrameInfo.BordersNoise[pFrameInfo.NNoiseEnv] = pFrameInfo.Borders[nEnv]
	}

	_ = nrOfChannels // unused in HE-AAC v1 (only referenced by removed PVC/USAC).
	return 1
}

// checkFrameInfo verifies that the FRAME_INFO vector has reasonable values.
// Returns 1 if correct, 0 on error.
//
// C counterpart: checkFrameInfo (env_extr.cpp:1655-1732).
func checkFrameInfo(pFrameInfo *FrameInfo, numberOfTimeSlots, overlap, timeStep int) int {
	nEnvelopes := int(pFrameInfo.NEnvelopes)
	nNoiseEnvelopes := int(pFrameInfo.NNoiseEnv)

	if nEnvelopes < 1 || nEnvelopes > maxEnvelopes {
		return 0
	}
	if nNoiseEnvelopes > maxNoiseEnvelopes {
		return 0
	}

	startPos := int(pFrameInfo.Borders[0])
	stopPos := int(pFrameInfo.Borders[nEnvelopes])
	tranEnv := int(pFrameInfo.TranEnv)
	startPosNoise := int(pFrameInfo.BordersNoise[0])
	stopPosNoise := int(pFrameInfo.BordersNoise[nNoiseEnvelopes])

	if overlap < 0 || overlap > 3*4 {
		return 0
	}
	if timeStep < 1 || timeStep > 4 {
		return 0
	}
	maxPos := numberOfTimeSlots + (overlap / timeStep)

	// Check that the start and stop positions of the frame are reasonable.
	if startPos < 0 || startPos >= stopPos {
		return 0
	}
	if startPos > maxPos-numberOfTimeSlots {
		return 0
	}
	if stopPos < numberOfTimeSlots {
		return 0
	}
	if stopPos > maxPos {
		return 0
	}

	// Check that the start border for every envelope is strictly later in time.
	for i := 0; i < nEnvelopes; i++ {
		if pFrameInfo.Borders[i] >= pFrameInfo.Borders[i+1] {
			return 0
		}
	}

	// Check that the envelope to be shortened is actually among the envelopes.
	if tranEnv > nEnvelopes {
		return 0
	}

	// Check the noise borders.
	if nEnvelopes == 1 && nNoiseEnvelopes > 1 {
		return 0
	}

	if startPos != startPosNoise || stopPos != stopPosNoise {
		return 0
	}

	// Check that the start border for every noise-envelope is strictly later.
	for i := 0; i < nNoiseEnvelopes; i++ {
		if pFrameInfo.BordersNoise[i] >= pFrameInfo.BordersNoise[i+1] {
			return 0
		}
	}

	// Check that every noise border is the same as an envelope border.
	for i := 0; i < nNoiseEnvelopes; i++ {
		sp := int(pFrameInfo.BordersNoise[i])
		j := 0
		for ; j < nEnvelopes; j++ {
			if int(pFrameInfo.Borders[j]) == sp {
				break
			}
		}
		if j == nEnvelopes {
			return 0
		}
	}

	return 1
}
