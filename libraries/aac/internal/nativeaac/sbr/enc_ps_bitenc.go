// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parametric-stereo ps_data() bitstream ENCODER — 1:1 port of
// libSBRenc/src/ps_bitenc.cpp (N. Rettelbach). Writes the ps_data() element
// (PS header + frame info + DPCM/Huffman-coded IID/ICC) that goes into the SBR
// extension payload as EXTENSION_ID_PS. A nil bit-buffer counts bits only
// (matches FDKsbrEnc_WriteBits_ps's NULL handling), which the rate-decision
// trials in ps_encode.cpp rely on.
//
// FDK-AAC-derived; see libfdk/COPYING. Fenced behind the aacfdk build tag.
// FIXED-POINT => byte-identical. IPD/OPD (PSENC IpdOpd extension) is wired for
// completeness but the GA baseline never enables it (enableIpdOpd stays 0).
package sbr

// writeBitsPS is the 1:1 port of FDKsbrEnc_WriteBits_ps (ps_bitenc.cpp:107-115):
// writes only if hBitStream != nil, always returns numberOfBits.
func writeBitsPS(bs *FdkBitStream, value uint32, numberOfBits uint32) int {
	if bs != nil {
		bs.WriteBits(value, numberOfBits)
	}
	return int(numberOfBits)
}

// ps_bitenc.cpp:123-126 IID delta table offsets/maxvals.
const (
	iidDeltaCoarseOffset = 14
	iidDeltaCoarseMaxVal = 28
	iidDeltaFineOffset   = 30
	iidDeltaFineMaxVal   = 60
)

// PS Stereo Huffmantable: iidDeltaFreqCoarse (ps_bitenc.cpp:129-137).
var iidDeltaFreqCoarseLength = []uint32{
	17, 17, 17, 17, 16, 15, 13, 10, 9, 7, 6, 5, 4, 3, 1,
	3, 4, 5, 6, 6, 8, 11, 13, 14, 14, 15, 17, 18, 18,
}
var iidDeltaFreqCoarseCode = []uint32{
	0x0001fffb, 0x0001fffc, 0x0001fffd, 0x0001fffa, 0x0000fffc, 0x00007ffc,
	0x00001ffd, 0x000003fe, 0x000001fe, 0x0000007e, 0x0000003c, 0x0000001d,
	0x0000000d, 0x00000005, 0x00000000, 0x00000004, 0x0000000c, 0x0000001c,
	0x0000003d, 0x0000003e, 0x000000fe, 0x000007fe, 0x00001ffc, 0x00003ffc,
	0x00003ffd, 0x00007ffd, 0x0001fffe, 0x0003fffe, 0x0003ffff,
}

// PS Stereo Huffmantable: iidDeltaFreqFine (ps_bitenc.cpp:140-156).
var iidDeltaFreqFineLength = []uint32{
	18, 18, 18, 18, 18, 18, 18, 18, 18, 17, 18, 17, 17, 16, 16, 15,
	14, 14, 13, 12, 12, 11, 10, 10, 8, 7, 6, 5, 4, 3, 1, 3,
	4, 5, 6, 7, 8, 9, 10, 11, 11, 12, 13, 14, 14, 15, 16, 16,
	17, 17, 18, 17, 18, 18, 18, 18, 18, 18, 18, 18, 18,
}
var iidDeltaFreqFineCode = []uint32{
	0x0001feb4, 0x0001feb5, 0x0001fd76, 0x0001fd77, 0x0001fd74, 0x0001fd75,
	0x0001fe8a, 0x0001fe8b, 0x0001fe88, 0x0000fe80, 0x0001feb6, 0x0000fe82,
	0x0000feb8, 0x00007f42, 0x00007fae, 0x00003faf, 0x00001fd1, 0x00001fe9,
	0x00000fe9, 0x000007ea, 0x000007fb, 0x000003fb, 0x000001fb, 0x000001ff,
	0x0000007c, 0x0000003c, 0x0000001c, 0x0000000c, 0x00000000, 0x00000001,
	0x00000001, 0x00000002, 0x00000001, 0x0000000d, 0x0000001d, 0x0000003d,
	0x0000007d, 0x000000fc, 0x000001fc, 0x000003fc, 0x000003f4, 0x000007eb,
	0x00000fea, 0x00001fea, 0x00001fd6, 0x00003fd0, 0x00007faf, 0x00007f43,
	0x0000feb9, 0x0000fe83, 0x0001feb7, 0x0000fe81, 0x0001fe89, 0x0001fe8e,
	0x0001fe8f, 0x0001fe8c, 0x0001fe8d, 0x0001feb2, 0x0001feb3, 0x0001feb0,
	0x0001feb1,
}

// PS Stereo Huffmantable: iidDeltaTimeCoarse (ps_bitenc.cpp:159-167).
var iidDeltaTimeCoarseLength = []uint32{
	19, 19, 19, 20, 20, 20, 17, 15, 12, 10, 8, 6, 4, 2, 1,
	3, 5, 7, 9, 11, 13, 14, 17, 19, 20, 20, 20, 20, 20,
}
var iidDeltaTimeCoarseCode = []uint32{
	0x0007fff9, 0x0007fffa, 0x0007fffb, 0x000ffff8, 0x000ffff9, 0x000ffffa,
	0x0001fffd, 0x00007ffe, 0x00000ffe, 0x000003fe, 0x000000fe, 0x0000003e,
	0x0000000e, 0x00000002, 0x00000000, 0x00000006, 0x0000001e, 0x0000007e,
	0x000001fe, 0x000007fe, 0x00001ffe, 0x00003ffe, 0x0001fffc, 0x0007fff8,
	0x000ffffb, 0x000ffffc, 0x000ffffd, 0x000ffffe, 0x000fffff,
}

// PS Stereo Huffmantable: iidDeltaTimeFine (ps_bitenc.cpp:170-186).
var iidDeltaTimeFineLength = []uint32{
	16, 16, 16, 16, 16, 16, 16, 16, 16, 15, 15, 15, 15, 15, 15, 14,
	14, 13, 13, 13, 12, 12, 11, 10, 9, 9, 7, 6, 5, 3, 1, 2,
	5, 6, 7, 8, 9, 10, 11, 11, 12, 12, 13, 13, 14, 14, 15, 15,
	15, 15, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16,
}
var iidDeltaTimeFineCode = []uint32{
	0x00004ed4, 0x00004ed5, 0x00004ece, 0x00004ecf, 0x00004ecc, 0x00004ed6,
	0x00004ed8, 0x00004f46, 0x00004f60, 0x00002718, 0x00002719, 0x00002764,
	0x00002765, 0x0000276d, 0x000027b1, 0x000013b7, 0x000013d6, 0x000009c7,
	0x000009e9, 0x000009ed, 0x000004ee, 0x000004f7, 0x00000278, 0x00000139,
	0x0000009a, 0x0000009f, 0x00000020, 0x00000011, 0x0000000a, 0x00000003,
	0x00000001, 0x00000000, 0x0000000b, 0x00000012, 0x00000021, 0x0000004c,
	0x0000009b, 0x0000013a, 0x00000279, 0x00000270, 0x000004ef, 0x000004e2,
	0x000009ea, 0x000009d8, 0x000013d7, 0x000013d0, 0x000027b2, 0x000027a2,
	0x0000271a, 0x0000271b, 0x00004f66, 0x00004f67, 0x00004f61, 0x00004f47,
	0x00004ed9, 0x00004ed7, 0x00004ecd, 0x00004ed2, 0x00004ed3, 0x00004ed0,
	0x00004ed1,
}

// ps_bitenc.cpp:188-204 ICC delta tables.
const (
	iccDeltaOffset = 7
	iccDeltaMaxVal = 14
)

var iccDeltaFreqLength = []uint32{14, 14, 12, 10, 7, 5, 3, 1, 2, 4, 6, 8, 9, 11, 13}
var iccDeltaFreqCode = []uint32{
	0x00003fff, 0x00003ffe, 0x00000ffe, 0x000003fe, 0x0000007e,
	0x0000001e, 0x00000006, 0x00000000, 0x00000002, 0x0000000e,
	0x0000003e, 0x000000fe, 0x000001fe, 0x000007fe, 0x00001ffe,
}

var iccDeltaTimeLength = []uint32{14, 13, 11, 9, 7, 5, 3, 1, 2, 4, 6, 8, 10, 12, 14}
var iccDeltaTimeCode = []uint32{
	0x00003ffe, 0x00001ffe, 0x000007fe, 0x000001fe, 0x0000007e,
	0x0000001e, 0x00000006, 0x00000000, 0x00000002, 0x0000000e,
	0x0000003e, 0x000000fe, 0x000003fe, 0x00000ffe, 0x00003fff,
}

// ps_bitenc.cpp:206-233 IPD/OPD delta tables (wired for completeness).
const (
	ipdDeltaOffset = 0
	ipdDeltaMaxVal = 7
	opdDeltaOffset = 0
	opdDeltaMaxVal = 7
)

var ipdDeltaFreqLength = []uint32{1, 3, 4, 4, 4, 4, 4, 4}
var ipdDeltaFreqCode = []uint32{0x00000001, 0x00000000, 0x00000006, 0x00000004, 0x00000002, 0x00000003, 0x00000005, 0x00000007}
var ipdDeltaTimeLength = []uint32{1, 3, 4, 5, 5, 4, 4, 3}
var ipdDeltaTimeCode = []uint32{0x00000001, 0x00000002, 0x00000002, 0x00000003, 0x00000002, 0x00000000, 0x00000003, 0x00000003}
var opdDeltaFreqLength = []uint32{1, 3, 4, 4, 5, 5, 4, 3}
var opdDeltaFreqCode = []uint32{0x00000001, 0x00000001, 0x00000006, 0x00000004, 0x0000000f, 0x0000000e, 0x00000005, 0x00000000}
var opdDeltaTimeLength = []uint32{1, 3, 4, 5, 5, 4, 4, 3}
var opdDeltaTimeCode = []uint32{0x00000001, 0x00000002, 0x00000001, 0x00000007, 0x00000006, 0x00000000, 0x00000002, 0x00000003}

// getNoBands ports getNoBands (ps_bitenc.cpp:235-254).
func getNoBands(mode int) int {
	switch mode {
	case 0, 3:
		return psBandsCoarse
	case 1, 4:
		return psBandsMid
	case 2, 5:
		return psBandsCoarse
	default:
		return psBandsCoarse
	}
}

// getIIDRes ports getIIDRes (ps_bitenc.cpp:256-261).
func getIIDRes(iidMode int) int {
	if iidMode < 3 {
		return psIidResCoarse
	}
	return psIidResFine
}

// encodeDeltaFreq ports encodeDeltaFreq (ps_bitenc.cpp:263-283).
func encodeDeltaFreq(bs *FdkBitStream, val []int, nBands int, codeTable, lengthTable []uint32, tableOffset, maxVal int, err *int) int {
	bitCnt := 0
	lastVal := 0
	for band := 0; band < nBands; band++ {
		delta := (val[band] - lastVal) + tableOffset
		lastVal = val[band]
		if delta > maxVal || delta < 0 {
			*err = 1
			if delta > 0 {
				delta = maxVal
			} else {
				delta = 0
			}
		}
		bitCnt += writeBitsPS(bs, codeTable[delta], lengthTable[delta])
	}
	return bitCnt
}

// encodeDeltaTime ports encodeDeltaTime (ps_bitenc.cpp:285-304).
func encodeDeltaTime(bs *FdkBitStream, val, valLast []int, nBands int, codeTable, lengthTable []uint32, tableOffset, maxVal int, err *int) int {
	bitCnt := 0
	for band := 0; band < nBands; band++ {
		delta := (val[band] - valLast[band]) + tableOffset
		if delta > maxVal || delta < 0 {
			*err = 1
			if delta > 0 {
				delta = maxVal
			} else {
				delta = 0
			}
		}
		bitCnt += writeBitsPS(bs, codeTable[delta], lengthTable[delta])
	}
	return bitCnt
}

// encodeIid ports FDKsbrEnc_EncodeIid (ps_bitenc.cpp:306-364).
func encodeIid(bs *FdkBitStream, iidVal, iidValLast []int, nBands, res, mode int, err *int) int {
	bitCnt := 0
	switch mode {
	case psDeltaFreq:
		switch res {
		case psIidResCoarse:
			bitCnt += encodeDeltaFreq(bs, iidVal, nBands, iidDeltaFreqCoarseCode, iidDeltaFreqCoarseLength, iidDeltaCoarseOffset, iidDeltaCoarseMaxVal, err)
		case psIidResFine:
			bitCnt += encodeDeltaFreq(bs, iidVal, nBands, iidDeltaFreqFineCode, iidDeltaFreqFineLength, iidDeltaFineOffset, iidDeltaFineMaxVal, err)
		default:
			*err = 1
		}
	case psDeltaTime:
		switch res {
		case psIidResCoarse:
			bitCnt += encodeDeltaTime(bs, iidVal, iidValLast, nBands, iidDeltaTimeCoarseCode, iidDeltaTimeCoarseLength, iidDeltaCoarseOffset, iidDeltaCoarseMaxVal, err)
		case psIidResFine:
			bitCnt += encodeDeltaTime(bs, iidVal, iidValLast, nBands, iidDeltaTimeFineCode, iidDeltaTimeFineLength, iidDeltaFineOffset, iidDeltaFineMaxVal, err)
		default:
			*err = 1
		}
	default:
		*err = 1
	}
	return bitCnt
}

// encodeIcc ports FDKsbrEnc_EncodeIcc (ps_bitenc.cpp:366-395).
func encodeIcc(bs *FdkBitStream, iccVal, iccValLast []int, nBands, mode int, err *int) int {
	bitCnt := 0
	switch mode {
	case psDeltaFreq:
		bitCnt += encodeDeltaFreq(bs, iccVal, nBands, iccDeltaFreqCode, iccDeltaFreqLength, iccDeltaOffset, iccDeltaMaxVal, err)
	case psDeltaTime:
		bitCnt += encodeDeltaTime(bs, iccVal, iccValLast, nBands, iccDeltaTimeCode, iccDeltaTimeLength, iccDeltaOffset, iccDeltaMaxVal, err)
	default:
		*err = 1
	}
	return bitCnt
}

// encodeIpd ports FDKsbrEnc_EncodeIpd (ps_bitenc.cpp:397-426).
func encodeIpd(bs *FdkBitStream, ipdVal, ipdValLast []int, nBands, mode int, err *int) int {
	bitCnt := 0
	switch mode {
	case psDeltaFreq:
		bitCnt += encodeDeltaFreq(bs, ipdVal, nBands, ipdDeltaFreqCode, ipdDeltaFreqLength, ipdDeltaOffset, ipdDeltaMaxVal, err)
	case psDeltaTime:
		bitCnt += encodeDeltaTime(bs, ipdVal, ipdValLast, nBands, ipdDeltaTimeCode, ipdDeltaTimeLength, ipdDeltaOffset, ipdDeltaMaxVal, err)
	default:
		*err = 1
	}
	return bitCnt
}

// encodeOpd ports FDKsbrEnc_EncodeOpd (ps_bitenc.cpp:428-457).
func encodeOpd(bs *FdkBitStream, opdVal, opdValLast []int, nBands, mode int, err *int) int {
	bitCnt := 0
	switch mode {
	case psDeltaFreq:
		bitCnt += encodeDeltaFreq(bs, opdVal, nBands, opdDeltaFreqCode, opdDeltaFreqLength, opdDeltaOffset, opdDeltaMaxVal, err)
	case psDeltaTime:
		bitCnt += encodeDeltaTime(bs, opdVal, opdValLast, nBands, opdDeltaTimeCode, opdDeltaTimeLength, opdDeltaOffset, opdDeltaMaxVal, err)
	default:
		*err = 1
	}
	return bitCnt
}

// encodeIpdOpd ports encodeIpdOpd (ps_bitenc.cpp:459-486).
func encodeIpdOpd(p *psOut, bs *FdkBitStream) int {
	bitCnt := 0
	err := 0

	writeBitsPS(bs, uint32(p.enableIpdOpd), 1)

	if p.enableIpdOpd == 1 {
		for env := 0; env < p.nEnvelopes; env++ {
			bitCnt += writeBitsPS(bs, uint32(p.deltaIPD[env]), 1)
			bitCnt += encodeIpd(bs, p.ipd[env][:], p.ipdLast[:], getNoBands(p.iidMode), p.deltaIPD[env], &err)

			bitCnt += writeBitsPS(bs, uint32(p.deltaOPD[env]), 1)
			bitCnt += encodeOpd(bs, p.opd[env][:], p.opdLast[:], getNoBands(p.iidMode), p.deltaOPD[env], &err)
		}
		bitCnt += writeBitsPS(bs, 0, 1)
	}
	return bitCnt
}

// getEnvIdx ports getEnvIdx (ps_bitenc.cpp:488-524).
func getEnvIdx(nEnvelopes, frameClass int) int {
	switch nEnvelopes {
	case 0:
		return 0
	case 1:
		if frameClass == 0 {
			return 1
		}
		return 0
	case 2:
		if frameClass == 0 {
			return 2
		}
		return 1
	case 3:
		return 2
	case 4:
		return 3
	default:
		return 0
	}
}

// encodePSExtension ports encodePSExtension (ps_bitenc.cpp:526-553).
func encodePSExtension(p *psOut, bs *FdkBitStream) int {
	bitCnt := 0

	if p.enableIpdOpd == 1 {
		ipdOpdBits := 0
		extSize := (2 + encodeIpdOpd(p, nil) + 7) >> 3

		if extSize < 15 {
			bitCnt += writeBitsPS(bs, uint32(extSize), 4)
		} else {
			bitCnt += writeBitsPS(bs, 15, 4)
			bitCnt += writeBitsPS(bs, uint32(extSize-15), 8)
		}

		ipdOpdBits += writeBitsPS(bs, psExtIDV0, 2)
		ipdOpdBits += encodeIpdOpd(p, bs)

		if ipdOpdBits%8 != 0 {
			ipdOpdBits += writeBitsPS(bs, 0, uint32(8-(ipdOpdBits%8)))
		}
		bitCnt += ipdOpdBits
	}
	return bitCnt
}

const psExtIDV0 = 0 // PS_EXT_ID_V0 (ps_bitenc.cpp:121)

// writePSBitstream ports FDKsbrEnc_WritePSBitstream (ps_bitenc.cpp:555-624).
// Writes the ps_data() element and returns the bit count; a nil bs counts only.
func writePSBitstream(p *psOut, bs *FdkBitStream) int {
	psExtEnable := 0
	bitCnt := 0
	err := 0

	if p == nil {
		return 0
	}

	// PS HEADER
	bitCnt += writeBitsPS(bs, uint32(p.enablePSHeader), 1)

	if p.enablePSHeader != 0 {
		bitCnt += writeBitsPS(bs, uint32(p.enableIID), 1)
		if p.enableIID != 0 {
			bitCnt += writeBitsPS(bs, uint32(p.iidMode), 3)
		}
		bitCnt += writeBitsPS(bs, uint32(p.enableICC), 1)
		if p.enableICC != 0 {
			bitCnt += writeBitsPS(bs, uint32(p.iccMode), 3)
		}
		if p.enableIpdOpd != 0 {
			psExtEnable = 1
		}
		bitCnt += writeBitsPS(bs, uint32(psExtEnable), 1)
	}

	// Frame class, number of envelopes
	bitCnt += writeBitsPS(bs, uint32(p.frameClass), 1)
	bitCnt += writeBitsPS(bs, uint32(getEnvIdx(p.nEnvelopes, p.frameClass)), 2)

	if p.frameClass == 1 {
		for env := 0; env < p.nEnvelopes; env++ {
			bitCnt += writeBitsPS(bs, uint32(p.frameBorder[env]), 5)
		}
	}

	if p.enableIID == 1 {
		iidLast := p.iidLast[:]
		for env := 0; env < p.nEnvelopes; env++ {
			bitCnt += writeBitsPS(bs, uint32(p.deltaIID[env]), 1)
			bitCnt += encodeIid(bs, p.iid[env][:], iidLast, getNoBands(p.iidMode), getIIDRes(p.iidMode), p.deltaIID[env], &err)
			iidLast = p.iid[env][:]
		}
	}

	if p.enableICC == 1 {
		iccLast := p.iccLast[:]
		for env := 0; env < p.nEnvelopes; env++ {
			bitCnt += writeBitsPS(bs, uint32(p.deltaICC[env]), 1)
			bitCnt += encodeIcc(bs, p.icc[env][:], iccLast, getNoBands(p.iccMode), p.deltaICC[env], &err)
			iccLast = p.icc[env][:]
		}
	}

	if psExtEnable != 0 {
		bitCnt += encodePSExtension(p, bs)
	}

	return bitCnt
}
