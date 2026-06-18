// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Pure-Go 1:1 port of the top-level encode driver FDKaacEnc_EncodeFrame
// (libAACenc/src/aacenc.cpp:769-998) plus the minimal raw-AU transport seam it
// needs. EncodeFrame runs one access unit: psyMain -> QCMainPrepare per element,
// then AdjustBitrate -> QCMain -> updateFillBits -> FinalizeBitConsumption ->
// updateBitres -> WriteBitstream. All callees are the already-ported,
// parity-verified driver/leaf kernels (psy_main.go, qc_main_loop.go,
// qc_main_driver.go, qc_main_finalize.go, bitenc.go).
//
// Scope: AAC-LC CBR, single sub-frame (nSubFrames == 1), transport type
// TT_MP4_RAW. EXCLUDED (and noted): VBR/two-pass, super-frames (nSubFrames>1),
// HE-AAC/SBR, ELD, and ancillary/DRC extension payloads — EncodeFrame is given
// no ext payloads (the AAC-LC raw path), so the per-element / global extension
// accumulation loops collapse to the empty case. Every value is fixed-point
// INT; the produced raw_data_block is byte-identical to the genuine FDK AU.

package nativeaac

// acEr is AC_ER (FDK_audio.h:305) == 0x000040: the ER-syntax bitstream flag.
// EncodeFrame's ID_END accounting skips the ID_END bits under AC_SCALABLE|AC_ER;
// AAC-LC raw carries neither. acScalable (== AC_SCALABLE 0x000008) is reused
// from ics_parse.go.
const acEr = 0x000040

// rawTransportEnc is the minimal TT_MP4_RAW transport encoder the AAC-LC CBR
// path needs: it owns the output bit writer and, for the raw access-unit type,
// makes transportEnc_GetStaticBits == 0, transportEnc_WriteAccessUnit a no-op
// and transportEnc_GetFrame return the byte count. It satisfies the in-package
// TransportEnc interface (bitenc_util.go) that WriteBitstream drives.
type rawTransportEnc struct {
	bs *bitStream
}

// newRawTransportEnc builds a raw-AU transport over a fresh power-of-two byte
// buffer large enough for one AAC-LC AU (MaxFrameBytes per channel-pair, padded
// to a power of two for the FDK ring buffer).
func newRawTransportEnc() *rawTransportEnc {
	const bufBytes = 16384 // > 6144/8 * MAX_CHANNELS, power of two
	return &rawTransportEnc{bs: newWriteBitStream(make([]byte, bufBytes))}
}

// GetBitstream returns the AU output bit writer (transportEnc_GetBitstream).
func (t *rawTransportEnc) GetBitstream() *bitStream { return t.bs }

// CrcStartReg / CrcEndReg are no-ops on the raw path (no ADTS CRC).
func (t *rawTransportEnc) CrcStartReg(mBits int) int { return 0 }
func (t *rawTransportEnc) CrcEndReg(reg int)         {}

// EndAccessUnit byte-aligns and reports the AU bit length
// (transportEnc_EndAccessUnit on the raw path). WriteBitstream byte-aligns the
// payload itself; *frameBits already holds the written bit count.
func (t *rawTransportEnc) EndAccessUnit(frameBits *int) {}

// getStaticBits mirrors transportEnc_GetStaticBits for TT_MP4_RAW: zero header
// overhead (matching the Go nil StaticBitsProvider the qc tier already uses).
func (t *rawTransportEnc) getStaticBits(auBits int) int { return 0 }

// Bytes returns the finished access unit, the getValidBitsWrite()/8 bytes
// WriteBitstream emitted (transportEnc_GetFrame returns this nOutBytes).
func (t *rawTransportEnc) Bytes() []byte {
	nBits := int(t.bs.getValidBitsWrite())
	return append([]byte(nil), t.bs.bitBuf.buffer[:(nBits+7)/8]...)
}

// encBitresToTpBitres is the 1:1 port of FDKaacEnc_EncBitresToTpBitres
// (aacenc.cpp:295) for the CBR path: the transport bit-reservoir level is the
// encoder bit-reservoir level. (VBR -> INT_MAX, SFR/FF -> 0 excluded.)
func encBitresToTpBitres(hAacEnc *AacEnc) int {
	if hAacEnc.BitrateMode == AacBitrateModeCBR {
		return hAacEnc.QcKernel.BitResTot
	}
	return 0
}

// AacEncExtPayload is the 1:1 port of struct AACENC_EXT_PAYLOAD
// (aacenc_lib.h): one external extension payload (e.g. EXT_SBR_DATA) handed to
// EncodeFrame for the per-element / global extension accumulation loops.
type AacEncExtPayload struct {
	Payload             []byte // pData
	DataSize            int    // dataSize, in bits
	DataType            int    // EXT_PAYLOAD_TYPE
	AssociatedChElement int    // associatedChElement (-1 == unassigned)
}

// EncodeFrame is the 1:1 port of FDKaacEnc_EncodeFrame (aacenc.cpp:769-998). It
// encodes one AAC-LC (or HE-AAC v1 core) CBR access unit from inputBuffer
// (interleaved int16 PCM) and returns the raw_data_block bytes. extPayload
// carries the external extension payloads accumulated in aacenc_lib.cpp
// (aacenc_lib.cpp:1853-1980) — the SBR EXT_SBR_DATA fill element on the HE-AAC
// v1 path; nil for plain AAC-LC.
func EncodeFrame(hAacEnc *AacEnc, hTpEnc *rawTransportEnc, inputBuffer []int16,
	inputBufferBufSize uint, extPayload []AacEncExtPayload) ([]byte, EncoderError) {

	extPayloadUsed := make([]bool, len(extPayload))

	cm := &hAacEnc.ChannelMapping
	const c = 0
	psyOut := hAacEnc.PsyOut[c]
	qcOut := hAacEnc.QcOut[c]

	qcOut.ElementExtBits = 0 // sum up all extended bit of each element
	qcOut.StaticBits = 0     // sum up side info bits of each element
	qcOut.TotalNoRedPe = 0   // sum up PE

	// advance psychoacoustics
	for el := 0; el < cm.NElements; el++ {
		elInfo := cm.ElInfo[el]

		if elInfo.ElType == IDSCE || elInfo.ElType == IDCPE || elInfo.ElType == IDLFE {
			// update pointer! (psyOutChan->X = qcOutChan->X). The Go port keeps
			// PSY_OUT_CHANNEL's own arrays; this aliasing is reproduced by copying
			// PSY_OUT_CHANNEL -> QC_OUT_CHANNEL after psyMain (below).
			psyOutEl := psyOut.PsyOutElement[el]
			qcEl := qcOut.QcElement[el]

			errorStatus := psyMain(elInfo.NChannelsInEl, hAacEnc.PsyKernel.PsyElement[el],
				hAacEnc.PsyKernel.PsyDynamic, hAacEnc.PsyKernel.PsyConf[:],
				psyOutEl, inputBuffer, inputBufferBufSize,
				cm.ElInfo[el].ChannelIndex[:], cm.NChannels)
			if errorStatus != AacEncOK {
				return nil, errorStatus
			}

			// Reproduce the C pointer aliasing: psyMain wrote PSY_OUT_CHANNEL's
			// mdctSpectrum / sfb* arrays; in C those alias QC_OUT_CHANNEL memory.
			// Copy them into the QC channels so QCMainPrepare/QCMain/WriteBitstream
			// read the same cells the genuine encoder does.
			for ch := 0; ch < elInfo.NChannelsInEl; ch++ {
				poc := psyOutEl.PsyOutChannel[ch]
				qoc := qcEl.QcOutChannel[ch]
				copy(qoc.MdctSpectrum[:], poc.MdctSpectrum[:])
				copy(qoc.SfbSpreadEnergy[:], poc.SfbSpreadEnergy[:])
				copy(qoc.SfbEnergy[:], poc.SfbEnergy[:])
				copy(qoc.SfbEnergyLdData[:], poc.SfbEnergyLdData[:])
				copy(qoc.SfbMinSnrLdData[:], poc.SfbMinSnrLdData[:])
				copy(qoc.SfbThresholdLdData[:], poc.SfbThresholdLdData[:])
			}

			// FormFactor, Pe and staticBitDemand calculation
			errorStatus = QCMainPrepare(&elInfo,
				hAacEnc.QcKernel.HAdjThr.adjThrStateElem[el], psyOutEl, qcEl,
				hAacEnc.Aot, uint32(hAacEnc.Config.SyntaxFlags), int8(hAacEnc.Config.EpConfig))
			if errorStatus != AacEncOK {
				return nil, errorStatus
			}

			qcEl.ExtBitsUsed = 0
			qcEl.NExtensions = 0
			qcEl.Extension[0] = QcOutExtension{}
			// Per-element extPayload accumulation loop (aacenc.cpp:835-853): assign
			// each unused payload whose associatedChElement == el (the SBR fill
			// element on the HE-AAC v1 path). QcOutElement.Extension holds one slot,
			// which suffices for the single SBR payload per element.
			for n := 0; n < len(extPayload); n++ {
				if !extPayloadUsed[n] && extPayload[n].AssociatedChElement == el &&
					extPayload[n].DataSize > 0 && extPayload[n].Payload != nil {
					idx := qcEl.NExtensions
					qcEl.NExtensions++
					qcEl.Extension[idx].Type = extPayload[n].DataType
					qcEl.Extension[idx].NPayloadBits = extPayload[n].DataSize
					qcEl.Extension[idx].Payload = extPayload[n].Payload
					// Ask the bitstream encoder how many bits the current syntax needs.
					qcEl.ExtBitsUsed += WriteExtensionData(nil, &qcEl.Extension[idx], 0, 0,
						uint32(hAacEnc.Config.SyntaxFlags), hAacEnc.Aot, int8(hAacEnc.Config.EpConfig))
					extPayloadUsed[n] = true
				}
			}

			// sum up extension and static bits for all channel elements
			qcOut.ElementExtBits += qcEl.ExtBitsUsed
			qcOut.StaticBits += qcEl.StaticBitsUsed

			// sum up pe
			qcOut.TotalNoRedPe += int(qcEl.PeData.pe)
		}
	}

	qcOut.NExtensions = 0
	qcOut.GlobalExtBits = 0
	for i := range qcOut.Extension {
		qcOut.Extension[i] = QcOutExtension{}
	}
	// No unassigned (ancillary) extension payloads either (aacenc.cpp:872-912
	// empty).

	// add bits for ID_END (aacenc.cpp:914-916; not AC_SCALABLE/AC_ER on AAC-LC)
	if hAacEnc.Config.SyntaxFlags&(acScalable|acEr) == 0 {
		qcOut.GlobalExtBits += ElIDBits
	}

	// build bitstream all nSubFrames (nSubFrames == 1 for AAC-LC CBR)
	totalBits := 0
	avgTotalBits := 0

	// frame wise bitrate adaption
	avgTotalBits = AdjustBitrate(hAacEnc.QcKernel, hAacEnc.Config.BitRate,
		hAacEnc.Config.SampleRate, hAacEnc.Config.FrameLength)
	// adjust super frame bitrate (nSubFrames == 1)
	avgTotalBits *= hAacEnc.Config.NSubFrames

	// First estimate of transport header overhead. TT_MP4_RAW -> 0.
	hAacEnc.QcKernel.GlobHdrBits = hTpEnc.getStaticBits(avgTotalBits + hAacEnc.QcKernel.BitResTot)

	errorStatus := QCMain(hAacEnc.QcKernel, hAacEnc.PsyOut[:], hAacEnc.QcOut[:],
		avgTotalBits, cm, hAacEnc.Aot, uint32(hAacEnc.Config.SyntaxFlags), int8(hAacEnc.Config.EpConfig))
	if errorStatus != AacEncOK {
		return nil, errorStatus
	}

	errorStatus = updateFillBits(cm, hAacEnc.QcKernel, hAacEnc.QcKernel.ElementBits, hAacEnc.QcOut[:])
	if errorStatus != AacEncOK {
		return nil, errorStatus
	}

	errorStatus = FinalizeBitConsumption(cm, hAacEnc.QcKernel, qcOut, qcOut.QcElement[:],
		hTpEnc.getStaticBits, hAacEnc.Aot, uint32(hAacEnc.Config.SyntaxFlags), int8(hAacEnc.Config.EpConfig))
	if errorStatus != AacEncOK {
		return nil, errorStatus
	}
	totalBits += qcOut.TotalBits

	updateBitres(cm, hAacEnc.QcKernel, hAacEnc.QcOut[:])

	// write bitstream header (transportEnc_WriteAccessUnit is a no-op for raw)
	_ = totalBits
	_ = encBitresToTpBitres(hAacEnc)

	// write bitstream (the raw_data_block)
	errorStatus = WriteBitstream(hTpEnc, cm, qcOut, psyOut, hAacEnc.QcKernel,
		hAacEnc.Aot, uint32(hAacEnc.Config.SyntaxFlags), int8(hAacEnc.Config.EpConfig))
	if errorStatus != AacEncOK {
		return nil, errorStatus
	}

	return hTpEnc.Bytes(), AacEncOK
}
