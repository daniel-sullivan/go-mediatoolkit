// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// SBR extension-payload bitstream WRITER — the 1:1 port of libSBRenc/src/
// bit_sbr.cpp. This is the ENCODE counterpart of the decode-side env_extr parse:
// it emits the exact sbr_single_channel_element() / sbr_channel_pair_element()
// bits — header, grid, dtdf direction flags, inverse-filtering modes, the
// Huffman-coded envelope + noise-floor scalefactors, the add-harmonic synthetic
// coding flags and the (empty, v1) extended data — into an FdkBitStream.
//
// Scope: HE-AAC v1 only. The PS / HE-AAC v2 extended-data path
// (FDKsbrEnc_PSEnc_WritePSData, hParametricStereo) and the low-delay/ELD grid
// (encodeLowDelaySbrGrid, ldGrid) are EXCLUDED: hParametricStereo is always nil
// and ldGrid is always 0 in the v1 path, so those branches are ported as the
// taken-false case (encodeExtendedData writes the single "no extended data" bit,
// getSbrExtendedDataSize returns 0). fdk-aac SBR is FIXED-POINT — byte-identical.
package sbr

// SI_* bitfield widths (sbr_def.h:192-241).
const (
	siSbrAmpResBitsW         = 1 // SI_SBR_AMP_RES_BITS
	siSbrCouplingBits        = 1 // SI_SBR_COUPLING_BITS
	siSbrStartFreqBits       = 4 // SI_SBR_START_FREQ_BITS
	siSbrStopFreqBits        = 4 // SI_SBR_STOP_FREQ_BITS
	siSbrXoverBandBits       = 3 // SI_SBR_XOVER_BAND_BITS
	siSbrReservedBits        = 2 // SI_SBR_RESERVED_BITS
	siSbrDataExtraBits       = 1 // SI_SBR_DATA_EXTRA_BITS
	siSbrHeaderExtra1Bits    = 1 // SI_SBR_HEADER_EXTRA_1_BITS
	siSbrHeaderExtra2Bits    = 1 // SI_SBR_HEADER_EXTRA_2_BITS
	siSbrFreqScaleBits       = 2 // SI_SBR_FREQ_SCALE_BITS
	siSbrAlterScaleBits      = 1 // SI_SBR_ALTER_SCALE_BITS
	siSbrNoiseBandsBits      = 2 // SI_SBR_NOISE_BANDS_BITS
	siSbrLimiterBandsBits    = 2 // SI_SBR_LIMITER_BANDS_BITS
	siSbrLimiterGainsBits    = 2 // SI_SBR_LIMITER_GAINS_BITS
	siSbrInterpolFreqBits    = 1 // SI_SBR_INTERPOL_FREQ_BITS
	siSbrSmoothingLengthBits = 1 // SI_SBR_SMOOTHING_LENGTH_BITS
	sbrClaBits               = 2 // SBR_CLA_BITS
	sbrEnvBits               = 2 // SBR_ENV_BITS
	sbrAbsBits               = 2 // SBR_ABS_BITS
	sbrNumBits               = 2 // SBR_NUM_BITS
	sbrRelBits               = 2 // SBR_REL_BITS
	sbrResBits               = 1 // SBR_RES_BITS
	sbrDirBits               = 1 // SBR_DIR_BITS
	siSbrInvfModeBitsW       = 2 // SI_SBR_INVF_MODE_BITS
	siSbrExtendedDataBits    = 1 // SI_SBR_EXTENDED_DATA_BITS
	siSbrExtensionSizeBits   = 4 // SI_SBR_EXTENSION_SIZE_BITS
	siSbrExtensionEscCntBits = 8 // SI_SBR_EXTENSION_ESC_COUNT_BITS
	siSbrExtensionIDBits     = 2 // SI_SBR_EXTENSION_ID_BITS
)

// SBR element IDs (sbr_def.h SBR_ELEMENT_TYPE).
const (
	sbrIDSce = 0 // SBR_ID_SCE
	sbrIDCpe = 1 // SBR_ID_CPE
)

// ceilLn2 is the 1:1 port of ceil_ln2 (bit_sbr.cpp:557-562).
func ceilLn2(x int) int {
	tmp := -1
	for {
		tmp++
		if (1 << tmp) >= x {
			break
		}
	}
	return tmp
}

// WriteEnvSingleChannelElement is the 1:1 port of
// FDKsbrEnc_WriteEnvSingleChannelElement (bit_sbr.cpp:174-196). It writes the
// header + SCE SBR data into bs (cmonData->sbrBitbuf) and returns the bit count.
// hdrBits/dataBits mirror cmonData->sbrHdrBits / sbrDataBits.
func WriteEnvSingleChannelElement(sbrHeaderData *EncSbrHeaderData,
	sbrBitstreamData *SbrBitstreamData, sbrEnvData *SbrEnvData, bs *FdkBitStream,
	sbrSyntaxFlags uint) (payloadBits, hdrBits, dataBits int) {

	if sbrEnvData == nil {
		return 0, 0, 0
	}
	hdrBits = encodeSbrHeader(sbrHeaderData, sbrBitstreamData, bs)
	dataBits = encodeSbrData(sbrEnvData, nil, nil, bs, sbrIDSce, 0, sbrSyntaxFlags)
	payloadBits = hdrBits + dataBits
	return
}

// WriteEnvSingleChannelElementPS is the HE-AAC v2 SCE writer: identical to
// WriteEnvSingleChannelElement but threads the parametric-stereo output ps so
// the ps_data() lands in the SBR extension payload (encodeExtendedData ->
// EXTENSION_ID_PS). A nil ps reproduces the HE-AAC v1 path byte-for-byte.
func WriteEnvSingleChannelElementPS(sbrHeaderData *EncSbrHeaderData,
	sbrBitstreamData *SbrBitstreamData, sbrEnvData *SbrEnvData, ps *PSOutHandle,
	bs *FdkBitStream, sbrSyntaxFlags uint) (payloadBits, hdrBits, dataBits int) {

	if sbrEnvData == nil {
		return 0, 0, 0
	}
	var pso *psOut
	if ps != nil {
		pso = ps.p
	}
	hdrBits = encodeSbrHeader(sbrHeaderData, sbrBitstreamData, bs)
	dataBits = encodeSbrData(sbrEnvData, nil, pso, bs, sbrIDSce, 0, sbrSyntaxFlags)
	payloadBits = hdrBits + dataBits
	return
}

// WriteEnvChannelPairElement is the 1:1 port of
// FDKsbrEnc_WriteEnvChannelPairElement (bit_sbr.cpp:207-230).
func WriteEnvChannelPairElement(sbrHeaderData *EncSbrHeaderData,
	sbrBitstreamData *SbrBitstreamData, sbrEnvDataLeft, sbrEnvDataRight *SbrEnvData,
	bs *FdkBitStream, sbrSyntaxFlags uint) (payloadBits, hdrBits, dataBits int) {

	if sbrEnvDataLeft == nil || sbrEnvDataRight == nil {
		return 0, 0, 0
	}
	hdrBits = encodeSbrHeader(sbrHeaderData, sbrBitstreamData, bs)
	dataBits = encodeSbrData(sbrEnvDataLeft, sbrEnvDataRight, nil, bs, sbrIDCpe,
		sbrHeaderData.Coupling, sbrSyntaxFlags)
	payloadBits = hdrBits + dataBits
	return
}

// CountSbrChannelPairElement is the 1:1 port of
// FDKsbrEnc_CountSbrChannelPairElement (bit_sbr.cpp:232-249): a trial write that
// is immediately rewound, returning only the bit count.
func CountSbrChannelPairElement(sbrHeaderData *EncSbrHeaderData,
	sbrBitstreamData *SbrBitstreamData, sbrEnvDataLeft, sbrEnvDataRight *SbrEnvData,
	bs *FdkBitStream, sbrSyntaxFlags uint) int {

	bitPos := bs.GetValidBits()
	payloadBits, _, _ := WriteEnvChannelPairElement(sbrHeaderData, sbrBitstreamData,
		sbrEnvDataLeft, sbrEnvDataRight, bs, sbrSyntaxFlags)
	bs.PushBack(bs.GetValidBits() - bitPos)
	return payloadBits
}

// encodeSbrHeader is the 1:1 port of encodeSbrHeader (bit_sbr.cpp:275-290).
func encodeSbrHeader(sbrHeaderData *EncSbrHeaderData, sbrBitstreamData *SbrBitstreamData,
	bs *FdkBitStream) int {
	payloadBits := 0
	if sbrBitstreamData.HeaderActive != 0 {
		payloadBits += int(bs.WriteBits(1, 1))
		payloadBits += encodeSbrHeaderData(sbrHeaderData, bs)
	} else {
		payloadBits += int(bs.WriteBits(0, 1))
	}
	return payloadBits
}

// encodeSbrHeaderData is the 1:1 port of encodeSbrHeaderData (bit_sbr.cpp:302-348).
func encodeSbrHeaderData(h *EncSbrHeaderData, bs *FdkBitStream) int {
	payloadBits := 0
	if h == nil {
		return 0
	}
	payloadBits += int(bs.WriteBits(uint32(h.SbrAmpRes), siSbrAmpResBitsW))
	payloadBits += int(bs.WriteBits(uint32(h.SbrStartFrequency), siSbrStartFreqBits))
	payloadBits += int(bs.WriteBits(uint32(h.SbrStopFrequency), siSbrStopFreqBits))
	payloadBits += int(bs.WriteBits(uint32(h.SbrXoverBand), siSbrXoverBandBits))
	payloadBits += int(bs.WriteBits(0, siSbrReservedBits))
	payloadBits += int(bs.WriteBits(uint32(h.HeaderExtra1), siSbrHeaderExtra1Bits))
	payloadBits += int(bs.WriteBits(uint32(h.HeaderExtra2), siSbrHeaderExtra2Bits))

	if h.HeaderExtra1 != 0 {
		payloadBits += int(bs.WriteBits(uint32(h.FreqScale), siSbrFreqScaleBits))
		payloadBits += int(bs.WriteBits(uint32(h.AlterScale), siSbrAlterScaleBits))
		payloadBits += int(bs.WriteBits(uint32(h.SbrNoiseBands), siSbrNoiseBandsBits))
	}
	if h.HeaderExtra2 != 0 {
		payloadBits += int(bs.WriteBits(uint32(h.SbrLimiterBands), siSbrLimiterBandsBits))
		payloadBits += int(bs.WriteBits(uint32(h.SbrLimiterGains), siSbrLimiterGainsBits))
		payloadBits += int(bs.WriteBits(uint32(h.SbrInterpolFreq), siSbrInterpolFreqBits))
		payloadBits += int(bs.WriteBits(uint32(h.SbrSmoothingLength), siSbrSmoothingLengthBits))
	}
	return payloadBits
}

// encodeSbrData is the 1:1 port of encodeSbrData (bit_sbr.cpp:359-385). ps is
// the parametric-stereo output threaded to the SCE path (nil for HE-AAC v1 / the
// CPE path).
func encodeSbrData(sbrEnvDataLeft, sbrEnvDataRight *SbrEnvData, ps *psOut, bs *FdkBitStream,
	sbrElem, coupling int, sbrSyntaxFlags uint) int {
	payloadBits := 0
	switch sbrElem {
	case sbrIDSce:
		payloadBits += encodeSbrSingleChannelElement(sbrEnvDataLeft, ps, bs, sbrSyntaxFlags)
	case sbrIDCpe:
		payloadBits += encodeSbrChannelPairElement(sbrEnvDataLeft, sbrEnvDataRight, bs,
			coupling, sbrSyntaxFlags)
	}
	return payloadBits
}

// encodeSbrSingleChannelElement is the 1:1 port (bit_sbr.cpp:401-439). The
// ldGrid branch (encodeLowDelaySbrGrid) is out of scope (ldGrid==0 in v1).
func encodeSbrSingleChannelElement(sbrEnvData *SbrEnvData, ps *psOut, bs *FdkBitStream,
	sbrSyntaxFlags uint) int {
	payloadBits := 0

	payloadBits += int(bs.WriteBits(0, siSbrDataExtraBits)) // no reserved bits

	if sbrSyntaxFlags&sbrSyntaxScalable != 0 {
		payloadBits += int(bs.WriteBits(1, siSbrCouplingBits))
	}
	payloadBits += encodeSbrGrid(sbrEnvData, bs)

	payloadBits += encodeSbrDtdf(sbrEnvData, bs)

	for i := 0; i < sbrEnvData.NoOfnoisebands; i++ {
		payloadBits += int(bs.WriteBits(uint32(sbrEnvData.SbrInvfModeVec[i]), siSbrInvfModeBitsW))
	}

	payloadBits += writeEnvelopeData(sbrEnvData, bs, 0)
	payloadBits += writeNoiseLevelData(sbrEnvData, bs, 0)
	payloadBits += writeSyntheticCodingData(sbrEnvData, bs)
	payloadBits += encodeExtendedData(ps, bs)

	return payloadBits
}

// encodeSbrChannelPairElement is the 1:1 port (bit_sbr.cpp:451-555). ldGrid is
// out of scope, so the !ldGrid branches are taken.
func encodeSbrChannelPairElement(sbrEnvDataLeft, sbrEnvDataRight *SbrEnvData,
	bs *FdkBitStream, coupling int, sbrSyntaxFlags uint) int {
	payloadBits := 0

	payloadBits += int(bs.WriteBits(0, siSbrDataExtraBits)) // no reserved bits
	payloadBits += int(bs.WriteBits(uint32(coupling), siSbrCouplingBits))

	if coupling != 0 {
		payloadBits += encodeSbrGrid(sbrEnvDataLeft, bs)
		payloadBits += encodeSbrDtdf(sbrEnvDataLeft, bs)
		payloadBits += encodeSbrDtdf(sbrEnvDataRight, bs)

		for i := 0; i < sbrEnvDataLeft.NoOfnoisebands; i++ {
			payloadBits += int(bs.WriteBits(uint32(sbrEnvDataLeft.SbrInvfModeVec[i]), siSbrInvfModeBitsW))
		}

		payloadBits += writeEnvelopeData(sbrEnvDataLeft, bs, 1)
		payloadBits += writeNoiseLevelData(sbrEnvDataLeft, bs, 1)
		payloadBits += writeEnvelopeData(sbrEnvDataRight, bs, 1)
		payloadBits += writeNoiseLevelData(sbrEnvDataRight, bs, 1)

		payloadBits += writeSyntheticCodingData(sbrEnvDataLeft, bs)
		payloadBits += writeSyntheticCodingData(sbrEnvDataRight, bs)
	} else {
		payloadBits += encodeSbrGrid(sbrEnvDataLeft, bs)
		payloadBits += encodeSbrGrid(sbrEnvDataRight, bs)
		payloadBits += encodeSbrDtdf(sbrEnvDataLeft, bs)
		payloadBits += encodeSbrDtdf(sbrEnvDataRight, bs)

		for i := 0; i < sbrEnvDataLeft.NoOfnoisebands; i++ {
			payloadBits += int(bs.WriteBits(uint32(sbrEnvDataLeft.SbrInvfModeVec[i]), siSbrInvfModeBitsW))
		}
		for i := 0; i < sbrEnvDataRight.NoOfnoisebands; i++ {
			payloadBits += int(bs.WriteBits(uint32(sbrEnvDataRight.SbrInvfModeVec[i]), siSbrInvfModeBitsW))
		}

		payloadBits += writeEnvelopeData(sbrEnvDataLeft, bs, 0)
		payloadBits += writeEnvelopeData(sbrEnvDataRight, bs, 0)
		payloadBits += writeNoiseLevelData(sbrEnvDataLeft, bs, 0)
		payloadBits += writeNoiseLevelData(sbrEnvDataRight, bs, 0)

		payloadBits += writeSyntheticCodingData(sbrEnvDataLeft, bs)
		payloadBits += writeSyntheticCodingData(sbrEnvDataRight, bs)
	}

	payloadBits += encodeExtendedData(nil, bs)
	return payloadBits
}

// encodeSbrGrid is the 1:1 port of encodeSbrGrid (bit_sbr.cpp:574-666). The
// ldGrid (SBR_CLA_BITS_LD) and FIXFIXonly branches are out of scope.
func encodeSbrGrid(sbrEnvData *SbrEnvData, bs *FdkBitStream) int {
	payloadBits := 0
	g := sbrEnvData.HSbrBSGrid
	bufferFrameStart := g.BufferFrameStart
	numberTimeSlots := g.NumberTimeSlots

	payloadBits += int(bs.WriteBits(uint32(g.FrameClass), sbrClaBits))

	switch g.FrameClass {
	case Fixfix:
		temp := ceilLn2(g.BsNumEnv)
		payloadBits += int(bs.WriteBits(uint32(temp), sbrEnvBits))
		payloadBits += int(bs.WriteBits(uint32(g.VF[0]), sbrResBits))

	case Fixvar, Varfix:
		var temp int
		if g.FrameClass == Fixvar {
			temp = g.BsAbsBord - (bufferFrameStart + numberTimeSlots)
		} else {
			temp = g.BsAbsBord - bufferFrameStart
		}
		payloadBits += int(bs.WriteBits(uint32(temp), sbrAbsBits))
		payloadBits += int(bs.WriteBits(uint32(g.N), sbrNumBits))

		for i := 0; i < g.N; i++ {
			temp = (g.BsRelBord[i] - 2) >> 1
			payloadBits += int(bs.WriteBits(uint32(temp), sbrRelBits))
		}
		temp = ceilLn2(g.N + 2)
		payloadBits += int(bs.WriteBits(uint32(g.P), uint32(temp)))

		for i := 0; i < g.N+1; i++ {
			payloadBits += int(bs.WriteBits(uint32(g.VF[i]), sbrResBits))
		}

	case Varvar:
		temp := g.BsAbsBord0 - bufferFrameStart
		payloadBits += int(bs.WriteBits(uint32(temp), sbrAbsBits))
		temp = g.BsAbsBord1 - (bufferFrameStart + numberTimeSlots)
		payloadBits += int(bs.WriteBits(uint32(temp), sbrAbsBits))

		payloadBits += int(bs.WriteBits(uint32(g.BsNumRel0), sbrNumBits))
		payloadBits += int(bs.WriteBits(uint32(g.BsNumRel1), sbrNumBits))

		for i := 0; i < g.BsNumRel0; i++ {
			temp = (g.BsRelBord0[i] - 2) >> 1
			payloadBits += int(bs.WriteBits(uint32(temp), sbrRelBits))
		}
		for i := 0; i < g.BsNumRel1; i++ {
			temp = (g.BsRelBord1[i] - 2) >> 1
			payloadBits += int(bs.WriteBits(uint32(temp), sbrRelBits))
		}

		temp = ceilLn2(g.BsNumRel0 + g.BsNumRel1 + 2)
		payloadBits += int(bs.WriteBits(uint32(g.P), uint32(temp)))

		temp = g.BsNumRel0 + g.BsNumRel1 + 1
		for i := 0; i < temp; i++ {
			payloadBits += int(bs.WriteBits(uint32(g.VFLR[i]), sbrResBits))
		}
	}
	return payloadBits
}

// encodeSbrDtdf is the 1:1 port of encodeSbrDtdf (bit_sbr.cpp:724-740).
func encodeSbrDtdf(sbrEnvData *SbrEnvData, bs *FdkBitStream) int {
	payloadBits := 0
	noOfNoiseEnvelopes := 1
	if sbrEnvData.NoOfEnvelopes > 1 {
		noOfNoiseEnvelopes = 2
	}
	for i := 0; i < sbrEnvData.NoOfEnvelopes; i++ {
		payloadBits += int(bs.WriteBits(uint32(sbrEnvData.DomainVec[i]), sbrDirBits))
	}
	for i := 0; i < noOfNoiseEnvelopes; i++ {
		payloadBits += int(bs.WriteBits(uint32(sbrEnvData.DomainVecNoise[i]), sbrDirBits))
	}
	return payloadBits
}

// writeNoiseLevelData is the 1:1 port of writeNoiseLevelData (bit_sbr.cpp:751-844).
func writeNoiseLevelData(sbrEnvData *SbrEnvData, bs *FdkBitStream, coupling int) int {
	payloadBits := 0
	nNoiseEnvelopes := 1
	if sbrEnvData.NoOfEnvelopes > 1 {
		nNoiseEnvelopes = 2
	}
	for i := 0; i < nNoiseEnvelopes; i++ {
		switch sbrEnvData.DomainVecNoise[i] {
		case codeEnvDirFreq:
			if coupling != 0 && sbrEnvData.Balance != 0 {
				payloadBits += int(bs.WriteBits(uint32(int32(sbrEnvData.SbrNoiseLevels[i*sbrEnvData.NoOfnoisebands])),
					uint32(sbrEnvData.SiSbrStartNoiseBitsBalance)))
			} else {
				payloadBits += int(bs.WriteBits(uint32(int32(sbrEnvData.SbrNoiseLevels[i*sbrEnvData.NoOfnoisebands])),
					uint32(sbrEnvData.SiSbrStartNoiseBits)))
			}
			for j := 1 + i*sbrEnvData.NoOfnoisebands; j < sbrEnvData.NoOfnoisebands*(1+i); j++ {
				lvl := int(sbrEnvData.SbrNoiseLevels[j])
				if coupling != 0 {
					if sbrEnvData.Balance != 0 {
						payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableNoiseBalanceFreqC[lvl+codeBookScfLavBalance11]),
							uint32(sbrEnvData.HufftableNoiseBalanceFreqL[lvl+codeBookScfLavBalance11])))
					} else {
						payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableNoiseLevelFreqC[lvl+codeBookScfLav11]),
							uint32(sbrEnvData.HufftableNoiseLevelFreqL[lvl+codeBookScfLav11])))
					}
				} else {
					payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableNoiseFreqC[lvl+codeBookScfLav11]),
						uint32(sbrEnvData.HufftableNoiseFreqL[lvl+codeBookScfLav11])))
				}
			}
		case codeEnvDirTime:
			for j := i * sbrEnvData.NoOfnoisebands; j < sbrEnvData.NoOfnoisebands*(1+i); j++ {
				lvl := int(sbrEnvData.SbrNoiseLevels[j])
				if coupling != 0 {
					if sbrEnvData.Balance != 0 {
						payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableNoiseBalanceTimeC[lvl+codeBookScfLavBalance11]),
							uint32(sbrEnvData.HufftableNoiseBalanceTimeL[lvl+codeBookScfLavBalance11])))
					} else {
						payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableNoiseLevelTimeC[lvl+codeBookScfLav11]),
							uint32(sbrEnvData.HufftableNoiseLevelTimeL[lvl+codeBookScfLav11])))
					}
				} else {
					payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableNoiseLevelTimeC[lvl+codeBookScfLav11]),
						uint32(sbrEnvData.HufftableNoiseLevelTimeL[lvl+codeBookScfLav11])))
				}
			}
		}
	}
	return payloadBits
}

// writeEnvelopeData is the 1:1 port of writeEnvelopeData (bit_sbr.cpp:855-939).
func writeEnvelopeData(sbrEnvData *SbrEnvData, bs *FdkBitStream, coupling int) int {
	payloadBits := 0
	for j := 0; j < sbrEnvData.NoOfEnvelopes; j++ {
		if sbrEnvData.DomainVec[j] == codeEnvDirFreq {
			if coupling != 0 && sbrEnvData.Balance != 0 {
				payloadBits += int(bs.WriteBits(uint32(sbrEnvData.Ienvelope[j][0]),
					uint32(sbrEnvData.SiSbrStartEnvBitsBalance)))
			} else {
				payloadBits += int(bs.WriteBits(uint32(sbrEnvData.Ienvelope[j][0]),
					uint32(sbrEnvData.SiSbrStartEnvBits)))
			}
		}
		for i := 1 - sbrEnvData.DomainVec[j]; i < sbrEnvData.NoScfBands[j]; i++ {
			delta := sbrEnvData.Ienvelope[j][i]
			if coupling != 0 {
				if sbrEnvData.Balance != 0 {
					if sbrEnvData.DomainVec[j] != 0 {
						payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableBalanceTimeC[delta+sbrEnvData.CodeBookScfLavBalance]),
							uint32(sbrEnvData.HufftableBalanceTimeL[delta+sbrEnvData.CodeBookScfLavBalance])))
					} else {
						payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableBalanceFreqC[delta+sbrEnvData.CodeBookScfLavBalance]),
							uint32(sbrEnvData.HufftableBalanceFreqL[delta+sbrEnvData.CodeBookScfLavBalance])))
					}
				} else {
					if sbrEnvData.DomainVec[j] != 0 {
						payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableLevelTimeC[delta+sbrEnvData.CodeBookScfLav]),
							uint32(sbrEnvData.HufftableLevelTimeL[delta+sbrEnvData.CodeBookScfLav])))
					} else {
						payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableLevelFreqC[delta+sbrEnvData.CodeBookScfLav]),
							uint32(sbrEnvData.HufftableLevelFreqL[delta+sbrEnvData.CodeBookScfLav])))
					}
				}
			} else {
				if sbrEnvData.DomainVec[j] != 0 {
					payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableTimeC[delta+sbrEnvData.CodeBookScfLav]),
						uint32(sbrEnvData.HufftableTimeL[delta+sbrEnvData.CodeBookScfLav])))
				} else {
					payloadBits += int(bs.WriteBits(uint32(sbrEnvData.HufftableFreqC[delta+sbrEnvData.CodeBookScfLav]),
						uint32(sbrEnvData.HufftableFreqL[delta+sbrEnvData.CodeBookScfLav])))
				}
			}
		}
	}
	return payloadBits
}

// writeSyntheticCodingData is the 1:1 port of writeSyntheticCodingData
// (bit_sbr.cpp:1004-1020).
func writeSyntheticCodingData(sbrEnvData *SbrEnvData, bs *FdkBitStream) int {
	payloadBits := 0
	payloadBits += int(bs.WriteBits(uint32(sbrEnvData.AddHarmonicFlag), 1))
	if sbrEnvData.AddHarmonicFlag != 0 {
		for i := 0; i < sbrEnvData.NoHarmonics; i++ {
			payloadBits += int(bs.WriteBits(uint32(sbrEnvData.AddHarmonic[i]), 1))
		}
	}
	return payloadBits
}

// getSbrExtendedDataSize is the 1:1 port of getSbrExtendedDataSize
// (bit_sbr.cpp:937-948). A nil ps (HE-AAC v1) returns 0; otherwise the PS
// extension byte size (SI_SBR_EXTENSION_ID_BITS + ps_data bits, ceil to bytes).
func getSbrExtendedDataSize(ps *psOut) int {
	extDataBits := 0
	if ps != nil {
		extDataBits += siSbrExtensionIDBits
		extDataBits += writePSBitstream(ps, nil) // count only
	}
	return (extDataBits + 7) >> 3
}

// encodeExtendedData is the 1:1 port of encodeExtendedData (bit_sbr.cpp:950-993).
// For HE-AAC v1 (ps == nil) getSbrExtendedDataSize returns 0 and a single "no
// extended data" bit is written. For HE-AAC v2 it writes the extension-data
// flag + size + the EXTENSION_ID_PS-tagged ps_data(), byte-aligned.
func encodeExtendedData(ps *psOut, bs *FdkBitStream) int {
	payloadBits := 0
	extDataSize := getSbrExtendedDataSize(ps)

	if extDataSize != 0 {
		maxExtSize := (1 << siSbrExtensionSizeBits) - 1
		writtenNoBits := 0

		payloadBits += int(bs.WriteBits(1, siSbrExtendedDataBits))

		if extDataSize < maxExtSize {
			payloadBits += int(bs.WriteBits(uint32(extDataSize), siSbrExtensionSizeBits))
		} else {
			payloadBits += int(bs.WriteBits(uint32(maxExtSize), siSbrExtensionSizeBits))
			payloadBits += int(bs.WriteBits(uint32(extDataSize-maxExtSize), siSbrExtensionEscCntBits))
		}

		if ps != nil {
			writtenNoBits += int(bs.WriteBits(extensionIDPSCoding, siSbrExtensionIDBits))
			writtenNoBits += writePSBitstream(ps, bs)
		}

		payloadBits += writtenNoBits

		writtenNoBits = writtenNoBits % 8
		if writtenNoBits != 0 {
			payloadBits += int(bs.WriteBits(0, uint32(8-writtenNoBits)))
		}
	} else {
		payloadBits += int(bs.WriteBits(0, siSbrExtendedDataBits))
	}
	return payloadBits
}
