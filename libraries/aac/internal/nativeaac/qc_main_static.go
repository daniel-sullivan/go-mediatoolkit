// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Minimal static-bit demand of the quantizer/coder. 1:1 port of
// FDKaacEnc_getMinimalStaticBitdemand (libAACenc/src/qc_main.cpp:588-611): the
// smallest possible static-bit cost of the access unit, obtained by accounting
// each SCE/CPE/LFE element's bitstream side info with the spectrum reduced to
// zero and TNS / M-S disabled (the minCnt==1 path of ChannelElementWrite). The
// rate-control loop uses it as the lower bound when distributing the bit budget.
//
// Pure integer. AAC-LC fixed: aot == AOT_AAC_LC, syntaxFlags == 0, epConfig ==
// -1 (the same constants the C function hard-codes). aacfdk-fenced.

package nativeaac

// getMinimalStaticBitdemand is the 1:1 port of
// FDKaacEnc_getMinimalStaticBitdemand (qc_main.cpp:588-611). For every SCE/CPE/
// LFE element it calls ChannelElementWrite with a nil transport handle and
// minCnt==1 to accumulate the minimum static bits, returning their sum.
func getMinimalStaticBitdemand(cm *ChannelMapping, psyOut []*PsyOut) int {
	const aot = AOTAACLC       // AOT_AAC_LC
	var syntaxFlags uint32 = 0 // syntaxFlags
	var epConfig int8 = -1     // epConfig
	bitcount := 0

	for i := 0; i < cm.NElements; i++ {
		elInfo := cm.ElInfo[i]

		if elInfo.ElType == IDSCE || elInfo.ElType == IDCPE || elInfo.ElType == IDLFE {
			minElBits := 0

			ChannelElementWrite(nil, &elInfo, nil,
				psyOut[0].PsyOutElement[i],
				psyOut[0].PsyOutElement[i].PsyOutChannel[:],
				syntaxFlags, aot, epConfig, &minElBits, 1)
			bitcount += minElBits
		}
	}

	return bitcount
}
