// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Pure-Go 1:1 port of the crash-recovery and bit-consumption finalisation tail
// of the FDK-AAC quantizer/coder driver (libAACenc/src/qc_main.cpp:
// FDKaacEnc_crashRecovery and FDKaacEnc_FinalizeBitConsumption). These run after
// the QCMain quantization loop converges: crashRecovery brute-forces the bit
// budget by zeroing the highest-frequency scalefactor bands; FinalizeBitConsumption
// reconciles the exact transport-header bit count, distributes fill/alignment
// bits and validates the frame against min/maxBitsPerFrame.
//
// AAC-LC CBR only. Pure integer arithmetic; EXACT int equality is asserted
// against the genuine vendored fdk reference. aacfdk-fenced.

package nativeaac

// crashRecovery is the 1:1 port of FDKaacEnc_crashRecovery (qc_main.cpp:1395):
// fulfil the bit constraint by brute force — cancel spectral lines beginning at
// the highest frequencies until bitsToSave bits are saved, lowering maxSfb in
// every channel, then re-account the static bits and fold the saved bits back
// into the granted dynamic budget.
func crashRecovery(nChannels int, psyOutElement *PsyOutElement, qcOut *QcOut,
	qcElement *QcOutElement, bitsToSave int, aot int, syntaxFlags uint32, epConfig int8) {
	savedBits := 0
	var bitsPerScf [2][MaxGroupedSFB]int
	var sectionToScf [2][MaxGroupedSFB]int

	qcChannel := qcElement.QcOutChannel
	psyChannel := psyOutElement.PsyOutChannel

	// create a table which converts frq-bins to bit-demand... [bitsPerScf]
	// ...and another one which holds the corresponding sections [sectionToScf]
	for ch := 0; ch < nChannels; ch++ {
		sfbOffset := psyChannel[ch].SfbOffsets[:]

		for sect := 0; sect < qcChannel[ch].SectionData.NoOfSections; sect++ {
			codeBook := qcChannel[ch].SectionData.HuffSection[sect].CodeBook

			for sfb := qcChannel[ch].SectionData.HuffSection[sect].SfbStart; sfb < qcChannel[ch].SectionData.HuffSection[sect].SfbStart+
				qcChannel[ch].SectionData.HuffSection[sect].SfbCnt; sfb++ {
				bitsPerScf[ch][sfb] = 0
				if codeBook != codeBookPnsNo {
					sfbStartLine := sfbOffset[sfb]
					noOfLines := sfbOffset[sfb+1] - sfbStartLine
					bitsPerScf[ch][sfb] = countValues(
						qcChannel[ch].QuantSpec[sfbStartLine:], noOfLines, codeBook)
				}
				sectionToScf[ch][sfb] = sect
			}
		}
	}

	// LOWER [maxSfb] IN BOTH CHANNELS!!
	// Attention: in case of stereo: maxSfbL == maxSfbR, GroupingL == GroupingR ;
	sfb := qcChannel[0].SectionData.MaxSfbPerGroup - 1
	for ; sfb >= 0; sfb-- {
		for sfbGrp := 0; sfbGrp < psyChannel[0].SfbCnt; sfbGrp += psyChannel[0].SfbPerGroup {
			for ch := 0; ch < nChannels; ch++ {
				sect := sectionToScf[ch][sfbGrp+sfb]
				qcChannel[ch].SectionData.HuffSection[sect].SfbCnt--
				savedBits += bitsPerScf[ch][sfbGrp+sfb]

				if qcChannel[ch].SectionData.HuffSection[sect].SfbCnt == 0 {
					if psyChannel[ch].LastWindowSequence != encShortWindow {
						savedBits += sideInfoTabLong[0]
					} else {
						savedBits += sideInfoTabShort[0]
					}
				}
			}
		}

		// ...have enough bits been saved?
		if savedBits >= bitsToSave {
			break
		}
	}

	// if not enough bits saved, clean whole spectrum and remove side info overhead
	if sfb == -1 {
		sfb = 0
	}

	for ch := 0; ch < nChannels; ch++ {
		qcChannel[ch].SectionData.MaxSfbPerGroup = sfb
		psyChannel[ch].MaxSfbPerGroup = sfb
		// when no spectrum is coded save tools info in bitstream
		if sfb == 0 {
			psyChannel[ch].TnsInfo = TnsInfo{}
			psyOutElement.ToolsInfo = PsyOutToolsInfo{}
		}
	}
	// dynamic bits will be updated in iteration loop

	// if stop sfb has changed save bits in side info, e.g. MS or TNS coding
	var statBitsNew int
	{
		var elInfo ElementInfo
		elInfo.NChannelsInEl = nChannels
		if nChannels == 2 {
			elInfo.ElType = IDCPE
		} else {
			elInfo.ElType = IDSCE
		}

		ChannelElementWrite(nil, &elInfo, nil, psyOutElement,
			psyChannel[:], syntaxFlags, aot, epConfig, &statBitsNew, 0)
	}

	savedBits = qcElement.StaticBitsUsed - statBitsNew

	// update static and dynamic bits
	qcElement.StaticBitsUsed -= savedBits
	qcElement.GrantedDynBits += savedBits

	qcOut.StaticBits -= savedBits
	qcOut.GrantedDynBits += savedBits
	qcOut.MaxDynBits += savedBits
}

// FinalizeBitConsumption is the 1:1 port of FDKaacEnc_FinalizeBitConsumption
// (qc_main.cpp:1283): get the exact transport-header bit count, reconcile the
// CBR header-overhead delta against the bit reservoir / fill bits, fake a fill
// extension payload to size the writable fill bits, then split the remaining
// budget into fill + alignment bits and validate the AU size. getStaticBits is
// the transportEnc_GetStaticBits(hTpEnc, nPayloadBits) seam (the same
// StaticBitsProvider model the bitrate limiter uses); a nil provider reproduces
// the C NULL-handle branch (no header reconciliation).
func FinalizeBitConsumption(cm *ChannelMapping, qcKernel *QcState, qcOut *QcOut,
	qcElement []*QcOutElement, getStaticBitsFn StaticBitsProvider, aot int,
	syntaxFlags uint32, epConfig int8) EncoderError {
	var fillExtPayload QcOutExtension
	var totFillBits, alignBits int

	getStaticBits := func(payloadBits int) int {
		if getStaticBitsFn == nil {
			return qcKernel.GlobHdrBits
		}
		return getStaticBitsFn(payloadBits)
	}

	// Get total consumed bits in AU
	qcOut.TotalBits = qcOut.StaticBits + qcOut.UsedDynBits +
		qcOut.TotFillBits + qcOut.ElementExtBits + qcOut.GlobalExtBits

	if qcKernel.BitrateMode == QcdataBrModeCBR {
		// Now we can get the exact transport bit amount, and hopefully it is
		// equal to the estimated value
		exactTpBits := getStaticBits(qcOut.TotalBits)

		if exactTpBits != qcKernel.GlobHdrBits {
			diffFillBits := 0

			// How many bits can be take by bitreservoir
			bitresSpace := qcKernel.BitResTotMax -
				(qcKernel.BitResTot +
					(qcOut.GrantedDynBits - (qcOut.UsedDynBits + qcOut.TotFillBits)))

			// Number of bits which can be moved to bitreservoir.
			bitsToBitres := qcKernel.GlobHdrBits - exactTpBits

			// If bitreservoir can not take all bits, move ramaining bits to fillbits
			diffFillBits = fixMax(0, bitsToBitres-bitresSpace)

			// Assure previous alignment
			diffFillBits = (diffFillBits + 7) &^ 7

			// Move as many bits as possible to bitreservoir
			qcKernel.BitResTot += bitsToBitres - diffFillBits

			// Write remaing bits as fill bits
			qcOut.TotFillBits += diffFillBits
			qcOut.TotalBits += diffFillBits
			qcOut.GrantedDynBits += diffFillBits

			// Get new header bits
			qcKernel.GlobHdrBits = getStaticBits(qcOut.TotalBits)

			if qcKernel.GlobHdrBits != exactTpBits {
				// In previous step, fill bits and corresponding total bits were
				// changed when bitreservoir was completely filled. Now we can take
				// the too much taken bits caused by header overhead from bitreservoir.
				qcKernel.BitResTot -= qcKernel.GlobHdrBits - exactTpBits
			}
		}
	} // MODE_CBR

	// Update exact number of consumed header bits.
	qcKernel.GlobHdrBits = getStaticBits(qcOut.TotalBits)

	// Save total fill bits and distribut to alignment and fill bits
	totFillBits = qcOut.TotFillBits

	// fake a fill extension payload
	fillExtPayload = QcOutExtension{}
	fillExtPayload.Type = ExtFillData
	fillExtPayload.NPayloadBits = totFillBits

	// ask bitstream encoder how many of that bits can be written in a fill
	// extension data entity
	qcOut.TotFillBits = WriteExtensionData(nil, &fillExtPayload, 0, 0,
		syntaxFlags, aot, epConfig)

	// now distribute extra fillbits and alignbits
	alignBits = 7 - (qcOut.StaticBits+qcOut.UsedDynBits+qcOut.ElementExtBits+
		qcOut.TotFillBits+qcOut.GlobalExtBits-1)%8

	// Maybe we could remove this
	if (alignBits+qcOut.TotFillBits-totFillBits) == 8 && qcOut.TotFillBits > 8 {
		qcOut.TotFillBits -= 8
	}

	qcOut.TotalBits = qcOut.StaticBits + qcOut.UsedDynBits +
		qcOut.TotFillBits + alignBits + qcOut.ElementExtBits + qcOut.GlobalExtBits

	if qcOut.TotalBits > qcKernel.MaxBitsPerFrame ||
		qcOut.TotalBits < qcKernel.MinBitsPerFrame {
		return AacEncQuantError
	}

	qcOut.AlignBits = alignBits

	return AacEncOK
}
