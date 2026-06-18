// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Thin exported drivers for the PS-decode cgo parity oracles
// (internal/parity_tests/ps-dec-*). They add no logic: each mirrors exactly what
// the C bridge does, so the oracle can drive the Go port and the genuine C with
// identical inputs and assert EXACT integer equality.

// PsParseResult is the flat result of the PS bitstream parse, mirroring the C
// bridge's psParseOut.
type PsParseResult struct {
	PsProcessFlag int
	BitsRead      int
	NoEnv         uint8
	FreqResIid    uint8
	FreqResIcc    uint8
	BFineIidQ     uint8
	EnvStartStop  [6]uint8
	IidMapped     [5 * 34]int8
	IccMapped     [5 * 34]int8
}

// PsParse mirrors the C qparity_psParse bridge: it allocates a fresh psDec
// (single slot, slot indices 0), parses the raw ps_data payload via readPsData,
// runs decodePs, and returns the dequantized + 34<->20-mapped IID/ICC index
// arrays plus the resolved envelope borders. pBuffer must be a power-of-two byte
// buffer (the bit reader's wrap mask requires it); validBits is the number of
// valid payload bits.
func PsParse(pBuffer []byte, bufSize, validBits uint32, noSubSamples, prevDecoded, frameError int) PsParseResult {
	h := new(psDec)
	h.NoSubSamples = int8(noSubSamples)
	h.NoChannels = psNoQmfChannels
	h.PsDecodedPrv = uint8(prevDecoded)
	h.BsLastSlot = 0
	h.BsReadSlot = 0
	h.ProcessSlot = 0
	h.BPsDataAvail[0] = pptNone
	h.BPsDataAvail[1] = pptNone

	bs := newSbrBitStream(pBuffer, bufSize, validBits)

	var coef psDecCoefficients
	bitsRead := int(readPsData(h, bs, int(validBits)))
	flag := decodePs(h, uint8(frameError), &coef)

	bsData := &h.BsData[h.ProcessSlot]

	var r PsParseResult
	r.PsProcessFlag = flag
	r.BitsRead = bitsRead
	r.NoEnv = bsData.NoEnv
	r.FreqResIid = bsData.FreqResIid
	r.FreqResIcc = bsData.FreqResIcc
	r.BFineIidQ = bsData.BFineIidQ
	for i := 0; i < 6; i++ {
		r.EnvStartStop[i] = bsData.AEnvStartStop[i]
	}
	for e := 0; e < psMaxNoPsEnv; e++ {
		for b := 0; b < psNoHiResIidBins; b++ {
			r.IidMapped[e*34+b] = coef.AaIidIndexMapped[e][b]
			r.IccMapped[e*34+b] = coef.AaIccIndexMapped[e][b]
		}
	}
	return r
}
