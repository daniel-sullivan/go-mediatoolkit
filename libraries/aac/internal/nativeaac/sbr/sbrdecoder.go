// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// This file ports the public SBR decoder API/dispatch 1:1 from
// libSBRdec/src/sbrdecoder.cpp for the HE-AAC v1 path: SbrDecoderOpen,
// SbrDecoderInitElement, sbrDecoder_HeaderUpdate, sbrDecoder_ResetElement,
// SbrDecoderParse (sbr_extension_data parse driving env_extr + env_dec),
// SbrDecoderDecodeElement (sbrdecoder.cpp:1576), SbrDecoderApply
// (sbrdecoder.cpp:1800), plus the element/header slot helpers (getHeaderSlot,
// setFrameErrorFlag, copySbrHeader, compareSbrHeader).
//
// HE-AAC v1 ONLY. EXCLUDED (with notes at each site): PS (hParametricStereoDec /
// DecodePs / psPossible), DRC feed, USAC/RSVD50/DRM syntax, ELD config, CRC
// (EXT_SBR_DATA_CRC — the AAC-LC general-audio path uses EXT_SBR_DATA, crcFlag==0),
// and the SBRDEC_FLUSH/concealment machinery beyond single-AU decode. Mono
// (ID_SCE) and stereo (ID_CPE) are supported.

// setFrameErrorFlag ports setFrameErrorFlag (sbrdecoder.cpp:169-180).
func setFrameErrorFlag(pSbrElement *SbrDecoderElement, value uint8) {
	if pSbrElement == nil {
		return
	}
	if value == frameErrorAllSlots {
		for i := range pSbrElement.frameErrorFlag {
			pSbrElement.frameErrorFlag[i] = frameError
		}
		return
	}
	pSbrElement.frameErrorFlag[pSbrElement.useFrameSlot] = value
}

// getHeaderSlot ports getHeaderSlot (sbrdecoder.cpp:182-212): pick a free header
// delay slot not referenced by a frame slot other than the current one.
func getHeaderSlot(currentSlot uint8, hdrSlotUsage []uint8) uint8 {
	slot := hdrSlotUsage[currentSlot]
	occupied := false
	for s := 0; s < 2; s++ {
		if hdrSlotUsage[s] == slot && s != int(slot) {
			occupied = true
			break
		}
	}
	if occupied {
		mask := uint(0)
		for s := 0; s < 2; s++ {
			mask |= 1 << hdrSlotUsage[s]
		}
		for s := 0; s < 2; s++ {
			if mask&0x1 == 0 {
				slot = uint8(s)
				break
			}
			mask >>= 1
		}
	}
	return slot
}

// copySbrHeader ports copySbrHeader (sbrdecoder.cpp:214-222): copy the whole
// header. The Go SbrHeaderData carries the band tables inline (no alias pointers
// to fix up), so a value copy suffices.
func copySbrHeader(dst, src *SbrHeaderData) { *dst = *src }

// compareSbrHeader ports compareSbrHeader (sbrdecoder.cpp:224-263): return
// non-zero if the two headers differ in any compared field. The C memcmps the
// bs_data / bs_info / freqBandData blocks; the Go port compares the same fields
// value-wise (the structs are plain value types).
func compareSbrHeader(h1, h2 *SbrHeaderData) int {
	if h1.SyncState != h2.SyncState || h1.Status != h2.Status ||
		h1.FrameError != h2.FrameError || h1.NumberTimeSlots != h2.NumberTimeSlots ||
		h1.NumberOfAnalysisBands != h2.NumberOfAnalysisBands || h1.TimeStep != h2.TimeStep ||
		h1.SbrProcSmplRate != h2.SbrProcSmplRate {
		return 1
	}
	if h1.BsData != h2.BsData || h1.BsDflt != h2.BsDflt || h1.BsInfo != h2.BsInfo {
		return 1
	}
	// freqBandData: the C compares the 8+MAX_NUM_LIMITERS+1 leading UCHARs plus the
	// four band tables; the whole FreqBandData is a value type, compare directly.
	if h1.FreqBandData != h2.FreqBandData {
		return 1
	}
	return 0
}

// SbrDecoderOpen ports sbrDecoder_Open (sbrdecoder.cpp:446-486): allocate the
// instance, wire the QMF domain, set numDelayFrames=1, and mark all header slots
// uninitialised.
func SbrDecoderOpen(pQmfDomain *qmfDomain) *SbrDecoderInstance {
	self := new(SbrDecoderInstance)
	self.pQmfDomain = pQmfDomain
	self.numDelayFrames = 1
	for elIdx := 0; elIdx < 8; elIdx++ {
		for i := 0; i < 2; i++ {
			self.sbrHeader[elIdx][i].SyncState = sbrNotInitialized
		}
	}
	return self
}

// sbrDecoderIsCoreCodecValid ports sbrDecoder_isCoreCodecValid
// (sbrdecoder.cpp:493-507) for the HE-AAC v1 core codecs (AOT_AAC_LC / AOT_SBR).
func sbrDecoderIsCoreCodecValid(coreCodec int) bool {
	switch coreCodec {
	case aotAACLC, aotSBR, aotPS:
		return true
	default:
		return false
	}
}

// sbrDecoderAssignQmfChannels2SbrChannels ports
// sbrDecoder_AssignQmfChannels2SbrChannels (sbrdecoder.cpp:429-444).
func sbrDecoderAssignQmfChannels2SbrChannels(self *SbrDecoderInstance) {
	absChOffset := 0
	for el := 0; el < self.numSbrElements; el++ {
		if self.pSbrElement[el] != nil {
			for ch := 0; ch < self.pSbrElement[el].nChannels; ch++ {
				self.pSbrElement[el].pSbrChannel[ch].SbrDec.qmfDomainInCh = &self.pQmfDomain.QmfDomainIn[absChOffset+ch]
				self.pSbrElement[el].pSbrChannel[ch].SbrDec.qmfDomainOutCh = &self.pQmfDomain.QmfDomainOut[absChOffset+ch]
			}
			absChOffset += self.pSbrElement[el].nChannels
		}
	}
}

// SbrDecoderInitElement ports sbrDecoder_InitElement (sbrdecoder.cpp:527-729) for
// the HE-AAC v1 path. coreCodec is AOT_AAC_LC or AOT_SBR, elementID is ID_SCE or
// ID_CPE. PS-related elChannels promotion is excluded (HE-AAC v2). Returns 0 on
// success or a negative error.
func SbrDecoderInitElement(self *SbrDecoderInstance, sampleRateIn, sampleRateOut, samplesPerFrame,
	coreCodec, elementID, elementIndex int) sbrError {
	if self == nil {
		return sbrdecUnsupportedConfig
	}
	if !sbrDecoderIsCoreCodecValid(coreCodec) || elementIndex >= 8 {
		return sbrdecUnsupportedConfig
	}
	if elementID != mp4ElementSCE && elementID != mp4ElementCPE && elementID != mp4ElementLFE {
		return sbrdecUnsupportedConfig
	}

	// flags: keep only FORCE_RESET|FLUSH then set syntax bits. HE-AAC v1: none of
	// the USAC/ELD/DRM/QUAD bits are set (AOT_AAC_LC / AOT_SBR).
	self.flags &= sbrdecForceReset | sbrdecFlush
	// (downscaleFactor==1, coreCodec in {AAC_LC,SBR}: no extra syntax flags.)

	self.sampleRateIn = sampleRateIn
	self.codecFrameSize = samplesPerFrame
	self.coreCodec = coreCodec
	self.harmonicSBR = 0
	self.downscaleFactor = 1

	// Init SBR element.
	if self.pSbrElement[elementIndex] == nil {
		self.pSbrElement[elementIndex] = new(SbrDecoderElement)
		self.numSbrElements++
	} else {
		self.numSbrChannels -= self.pSbrElement[elementIndex].nChannels
	}

	elChannels := 0
	switch elementID {
	case mp4ElementCPE:
		elChannels = 2
	case mp4ElementLFE, mp4ElementSCE:
		elChannels = 1
	}
	// PS elChannels promotion (sbrdecoder.cpp:635-649): a single ID_SCE element on
	// an HE-AAC v2-capable core codec (AAC_LC/SBR/PS) is promoted to 2 channels so
	// the second SbrDec / QmfDomainOut channel exists for the PS right-channel
	// synthesis. The right channel is only ever driven when PS data is present and
	// psPossible is set; for a plain v1 mono stream (psPossible==0) it is reset but
	// never decoded, so the mono output is unchanged.
	if elementIndex == 0 && elementID == mp4ElementSCE && sbrDecoderIsCoreCodecValid(coreCodec) {
		elChannels = 2
	}

	self.pSbrElement[elementIndex].elementID = elementID
	self.pSbrElement[elementIndex].nChannels = elChannels

	for ch := 0; ch < elChannels; ch++ {
		if self.pSbrElement[elementIndex].pSbrChannel[ch] == nil {
			self.pSbrElement[elementIndex].pSbrChannel[ch] = new(SbrChannel)
		}
		self.numSbrChannels++
		// sbrDecoder_drcInitChannel (DRC) excluded.
	}

	self.pQmfDomain.globalConf.nInputChannelsRequested = uint8(self.numSbrChannels)
	if int(self.pQmfDomain.globalConf.nOutputChannelsRequested) < self.numSbrChannels {
		self.pQmfDomain.globalConf.nOutputChannelsRequested = uint8(self.numSbrChannels)
	}

	sbrDecoderAssignQmfChannels2SbrChannels(self)

	for i := range self.pSbrElement[elementIndex].frameErrorFlag {
		self.pSbrElement[elementIndex].frameErrorFlag[i] = 0
	}

	// overlap: AAC-LC/SBR non-ELD non-quad => (3*2) == 6.
	overlap := 3 * 2
	return sbrDecoderResetElement(self, sampleRateIn, sampleRateOut, samplesPerFrame, elementID, elementIndex, overlap)
}

// sbrDecoderResetElement ports sbrDecoder_ResetElement (sbrdecoder.cpp:273-422)
// for HE-AAC v1: validate rates, set the synthesis downsample factor, init the
// per-element default headers, set the QMF-domain requested config, and create
// the per-channel decoders. PS / DRC / ELD QMF flag setup excluded.
func sbrDecoderResetElement(self *SbrDecoderInstance, sampleRateIn, sampleRateOut, samplesPerFrame,
	elementID, elementIndex, overlap int) sbrError {
	if sampleRateIn < 6400 || sampleRateIn > 96000 {
		return sbrdecUnsupportedConfig
	}
	if sampleRateOut > 96000 {
		return sbrdecUnsupportedConfig
	}

	qmfFlags := uint(0) // no SBRDEC_LOW_POWER, no ELD CLDFB/MPSLDFB.

	if sampleRateOut == 0 {
		sampleRateOut = sampleRateIn << 1 // implicit signalling => dual rate.
	}
	var synDownsampleFac uint8
	if sampleRateIn == sampleRateOut {
		synDownsampleFac = 2
		self.flags |= sbrdecDownsample
	} else {
		synDownsampleFac = 1
		self.flags &^= sbrdecDownsample
	}
	self.synDownsampleFac = synDownsampleFac
	self.sampleRateOut = sampleRateOut

	for i := 0; i < 2; i++ {
		hSbrHeader := &self.sbrHeader[elementIndex][i]
		setDflt := hSbrHeader.SyncState == sbrNotInitialized || self.flags&sbrdecForceReset != 0
		if err := initHeaderData(hSbrHeader, sampleRateIn, sampleRateOut, self.downscaleFactor,
			samplesPerFrame, self.flags, setDflt); err != sbrdecOK {
			return err
		}
		if hSbrHeader.SyncState > upsampling {
			hSbrHeader.SyncState = upsampling
		}
	}

	if self.pQmfDomain.globalConf.qmfDomainExplicitConfig == 0 {
		gc := &self.pQmfDomain.globalConf
		gc.flagsRequested |= qmfFlags
		gc.nBandsAnalysisRequested = self.sbrHeader[elementIndex][0].NumberOfAnalysisBands
		if synDownsampleFac == 1 {
			gc.nBandsSynthesisRequested = 64
		} else {
			gc.nBandsSynthesisRequested = 32
		}
		gc.nBandsSynthesisRequested /= uint16(self.downscaleFactor)
		gc.nQmfTimeSlotsRequested = self.sbrHeader[elementIndex][0].NumberTimeSlots * self.sbrHeader[elementIndex][0].TimeStep
		gc.nQmfOvTimeSlotsRequested = uint8(overlap)
		gc.nQmfProcBandsRequested = 64
		gc.nQmfProcChannelsRequested = 1
	}

	for ch := 0; ch < self.pSbrElement[elementIndex].nChannels; ch++ {
		headerIndex := getHeaderSlot(self.pSbrElement[elementIndex].useFrameSlot, self.pSbrElement[elementIndex].useHeaderSlot[:])
		if err := createSbrDec(self.pSbrElement[elementIndex].pSbrChannel[ch],
			&self.sbrHeader[elementIndex][headerIndex],
			&self.pSbrElement[elementIndex].transposerSettings,
			self.flags, overlap, ch, self.codecFrameSize); err != sbrdecOK {
			return err
		}
	}

	// CreatePsDec (sbrdecoder.cpp:395-407): a single SBR element on an
	// HE-AAC v2-capable core codec gets the shared PS decoder. Allocated once;
	// only ever exercised when psPossible and ps_data are present.
	if self.numSbrElements == 1 && sbrDecoderIsCoreCodecValid(self.coreCodec) {
		if self.hParametricStereoDec == nil {
			self.hParametricStereoDec = new(psDec)
		}
		if createPsDec(self.hParametricStereoDec, samplesPerFrame) != 0 {
			return sbrdecCreateError
		}
	}

	self.pSbrElement[elementIndex].useFrameSlot = 0
	for i := 0; i < 2; i++ {
		self.pSbrElement[elementIndex].useHeaderSlot[i] = uint8(i)
	}
	return sbrdecOK
}

// sbrDecoderHeaderUpdate ports sbrDecoder_HeaderUpdate (sbrdecoder.cpp:764-796):
// rebuild the freq band tables on a header change and trigger a reset.
func sbrDecoderHeaderUpdate(self *SbrDecoderInstance, hSbrHeader *SbrHeaderData,
	headerStatus int, hSbrChannel []*SbrChannel, numElementChannels int) sbrError {
	errorStatus := resetFreqBandTables(hSbrHeader, self.flags)
	if errorStatus == sbrdecOK {
		if hSbrHeader.SyncState == upsampling && headerStatus != headerReset {
			hSbrHeader.FreqBandData.LowSubband = hSbrHeader.NumberOfAnalysisBands
			hSbrHeader.FreqBandData.HighSubband = hSbrHeader.NumberOfAnalysisBands
		}
		hSbrHeader.Status |= sbrdecHdrStatReset
	}
	return errorStatus
}

// SbrDecoderParse ports sbrDecoder_Parse (sbrdecoder.cpp:1119-1561) for the
// HE-AAC v1 / AAC-LC general-audio path: header-present parse + sbrGetChannelElement
// into the current frame delay slot, sanity-check the remaining bits, set the
// frame error flag, and advance the frame/header slot bookkeeping. countBits is
// the bits available in the sbr_extension_data; *pCount is updated to the bits
// consumed-remaining the same way the C does. crcFlag must be 0 (EXT_SBR_DATA, not
// EXT_SBR_DATA_CRC). EXCLUDED: DRM bit-reverse, USAC/RSVD50 info/header, CRC,
// PS bitstream slot updates. prevElement is the preceding core element ID.
func SbrDecoderParse(self *SbrDecoderInstance, hBs *bitStream, pCount *int, bsPayLen int,
	crcFlag int, prevElement int, elementIndex int) sbrError {
	if *pCount <= 0 {
		setFrameErrorFlag(self.pSbrElement[elementIndex], frameError)
		return sbrdecOK
	}
	if self == nil {
		return sbrdecNotInitialized
	}

	startPos := int(hBs.getValidBits())

	if self.pSbrElement[elementIndex] == nil {
		return sbrdecNotInitialized
	}
	hSbrElement := self.pSbrElement[elementIndex]

	var lastSlot int
	if hSbrElement.useFrameSlot > 0 {
		lastSlot = int(hSbrElement.useFrameSlot) - 1
	} else {
		lastSlot = int(self.numDelayFrames)
	}
	lastHdrSlot := hSbrElement.useHeaderSlot[lastSlot]
	thisHdrSlot := getHeaderSlot(hSbrElement.useFrameSlot, hSbrElement.useHeaderSlot[:])

	hSbrHeader := &self.sbrHeader[elementIndex][thisHdrSlot]
	pSbrChannel := hSbrElement.pSbrChannel[:]
	stereo := hSbrElement.elementID == mp4ElementCPE

	hFrameDataLeft := &pSbrChannel[0].frameData[hSbrElement.useFrameSlot]
	var hFrameDataRight *SbrFrameData
	if stereo {
		hFrameDataRight = &pSbrChannel[1].frameData[hSbrElement.useFrameSlot]
	}

	// store frameData; new parsed frameData possibly corrupted (concealment).
	frameDataLeftCopy := *hFrameDataLeft
	var frameDataRightCopy SbrFrameData
	if stereo {
		frameDataRightCopy = *hFrameDataRight
	}

	self.flags &^= sbrdecPsDecoded

	headerStatus := headerNotPresent
	fDoDecodeSbrData := 1

	if hSbrHeader.Status&sbrdecHdrStatUpdate != 0 {
		headerStatus = headerOK
		hSbrHeader.Status &^= sbrdecHdrStatUpdate
	} else if thisHdrSlot != lastHdrSlot {
		copySbrHeader(hSbrHeader, &self.sbrHeader[elementIndex][lastHdrSlot])
	}

	// Check element context.
	if (prevElement != mp4ElementSCE && prevElement != mp4ElementCPE) || prevElement != hSbrElement.elementID {
		fDoDecodeSbrData = 0
	}
	if fDoDecodeSbrData != 0 && int(hBs.getValidBits()) <= 0 {
		fDoDecodeSbrData = 0
	}

	// CRC excluded: AAC-LC general-audio uses EXT_SBR_DATA (crcFlag==0). The
	// sbr-dec-e2e oracle drives only crcFlag==0; a non-zero crcFlag is rejected.
	if crcFlag != 0 {
		return sbrdecUnsupportedConfig
	}

	if fDoDecodeSbrData != 0 {
		// MPEG-4 legacy: sbr_header() presence flag, then sbrGetHeaderData.
		sbrHeaderPresent := int(hBs.readBit())
		if sbrHeaderPresent != 0 {
			headerStatus = sbrGetHeaderData(hSbrHeader, hBs, self.flags, 1, 0)
		}
		if headerStatus == headerReset {
			errorStatus := sbrDecoderHeaderUpdate(self, hSbrHeader, headerStatus, pSbrChannel, hSbrElement.nChannels)
			if errorStatus == sbrdecOK {
				hSbrHeader.SyncState = sbrHeaderState
			} else {
				hSbrHeader.SyncState = sbrNotInitialized
				headerStatus = headerError
			}
			if errorStatus != sbrdecOK {
				fDoDecodeSbrData = 0
			}
		}
	}

	// read frame data.
	if int(hSbrHeader.SyncState) >= sbrHeaderState && fDoDecodeSbrData != 0 {
		var dataR *SbrFrameData
		if stereo {
			dataR = hFrameDataRight
		}
		// Update the PS read slot for a mono element (sbrdecoder.cpp:1431-1436).
		var hParametricStereoDec *psDec
		if !stereo && self.hParametricStereoDec != nil {
			self.hParametricStereoDec.BsLastSlot = self.hParametricStereoDec.BsReadSlot
			self.hParametricStereoDec.BsReadSlot = hSbrElement.useFrameSlot
			hParametricStereoDec = self.hParametricStereoDec
		}
		sbrFrameOk := sbrGetChannelElement(
			hSbrHeader, hFrameDataLeft, dataR,
			&pSbrChannel[0].prevFrameData, 0, hBs, hParametricStereoDec, self.flags,
			int(hSbrElement.transposerSettings.overlap))
		if sbrFrameOk == 0 {
			fDoDecodeSbrData = 0
		} else {
			var valBits int
			if bsPayLen > 0 {
				valBits = bsPayLen - (startPos - int(hBs.getValidBits()))
			} else {
				valBits = int(hBs.getValidBits())
			}
			if valBits < 0 {
				fDoDecodeSbrData = 0
			} else {
				// AOT_AAC_LC/SBR/PS: remaining bits must be only alignment bits.
				alignBits := valBits & 0x7
				if valBits > alignBits {
					fDoDecodeSbrData = 0
				}
			}
		}
	} else {
		// header not in sync — return parse error so caller upsamples.
		_ = headerStatus
	}

	if fDoDecodeSbrData == 0 {
		setFrameErrorFlag(hSbrElement, frameError)
		*hFrameDataLeft = frameDataLeftCopy
		if stereo {
			*hFrameDataRight = frameDataRightCopy
		}
	} else {
		setFrameErrorFlag(hSbrElement, frameOk)
	}

	if !stereo {
		hFrameDataLeft.Coupling = couplingOff
	}

	errorStatus := sbrdecOK
	if int(hSbrHeader.SyncState) < sbrHeaderState || fDoDecodeSbrData == 0 {
		if !(fDoDecodeSbrData == 0 && int(hSbrHeader.SyncState) >= sbrHeaderState) {
			// keep sbrdecOK unless header was never in sync (parse error path).
		}
	}

	// bail: header-slot selection + frame-slot advance (sbrdecoder.cpp:1520-1556).
	useOldHdr := 0
	if headerStatus == headerNotPresent || headerStatus == headerError ||
		(headerStatus == headerReset && fDoDecodeSbrData == 0) {
		useOldHdr = 1
	}
	if useOldHdr == 0 && thisHdrSlot != lastHdrSlot {
		if compareSbrHeader(hSbrHeader, &self.sbrHeader[elementIndex][lastHdrSlot]) == 0 {
			useOldHdr = 1
		}
	}
	if useOldHdr != 0 {
		hSbrElement.useHeaderSlot[hSbrElement.useFrameSlot] = lastHdrSlot
	} else {
		hSbrElement.useHeaderSlot[hSbrElement.useFrameSlot] = thisHdrSlot
	}
	hSbrElement.useFrameSlot = (hSbrElement.useFrameSlot + 1) % (self.numDelayFrames + 1)

	*pCount -= startPos - int(hBs.getValidBits())
	return errorStatus
}

// sbrDecoderDecodeElement ports sbrDecoder_DecodeElement (sbrdecoder.cpp:1576-1798):
// ensure a default header for upsampling, run resetSbrDec on a header reset,
// decodeSbrData (symbol decode of the parsed envelopes), decode PS data when
// psPossible, then sbr_dec per channel writing the 2048-sample interleaved output
// at strideOut. input holds the AAC-LC core int32 time samples (codecFrameSize per
// channel, channel-blocked); timeData receives the interleaved SBR output.
// numOutChannels is updated to 2 when PS yields a stereo output. EXCLUDED: DRC,
// SBRDEC_FLUSH delay handling, the channel-map indirection (offsets are direct).
func sbrDecoderDecodeElement(self *SbrDecoderInstance, input, timeData []int32,
	channelIndex, elementIndex, numInChannels int, numOutChannels *int, psPossible bool) sbrError {
	hSbrElement := self.pSbrElement[elementIndex]
	pSbrChannel := hSbrElement.pSbrChannel[:]
	hSbrHeader := &self.sbrHeader[elementIndex][hSbrElement.useHeaderSlot[hSbrElement.useFrameSlot]]

	codecFrameSize := self.codecFrameSize
	stereo := hSbrElement.elementID == mp4ElementCPE
	numElementChannels := hSbrElement.nChannels

	hFrameDataLeft := &pSbrChannel[0].frameData[hSbrElement.useFrameSlot]
	var hFrameDataRight *SbrFrameData
	if stereo {
		hFrameDataRight = &pSbrChannel[1].frameData[hSbrElement.useFrameSlot]
	}

	// SBRDEC_FLUSH branch (sbrdecoder.cpp:1611-1639) excluded: no flushing here.

	hSbrHeader.FrameError = hSbrElement.frameErrorFlag[hSbrElement.useFrameSlot]

	// Prepare filterbank for upsampling if no valid bit stream data is available.
	if int(hSbrHeader.SyncState) == sbrNotInitialized {
		if err := initHeaderData(hSbrHeader, self.sampleRateIn, self.sampleRateOut,
			self.downscaleFactor, codecFrameSize, self.flags, true); err != sbrdecOK {
			return err
		}
		hSbrHeader.SyncState = upsampling
		if err := sbrDecoderHeaderUpdate(self, hSbrHeader, headerNotPresent, pSbrChannel, hSbrElement.nChannels); err != sbrdecOK {
			hSbrHeader.SyncState = sbrNotInitialized
			return err
		}
	}

	// reset.
	if hSbrHeader.Status&sbrdecHdrStatReset != 0 {
		applySbrProc := int(hSbrHeader.SyncState) == sbrActive ||
			(hSbrHeader.FrameError == 0 && int(hSbrHeader.SyncState) == sbrHeaderState)
		for ch := 0; ch < numElementChannels; ch++ {
			// resetSbrDec takes frameData slot 0, matching fdk's
			// `pSbrChannel[ch]->frameData` (== &frameData[0], a fixed slot, NOT
			// frameData[useFrameSlot]) — sbrdecoder.cpp:1681. Using useFrameSlot
			// here picks the not-yet-parsed delay slot (sbrPatchingMode still 0),
			// which desynchronizes SbrCalculateEnvelope.sbrPatchingMode from fdk
			// (set to hFrameData->sbrPatchingMode at sbr_dec.cpp:1479) on the
			// 0->1 active transition and corrupts the whole envelope from there.
			if err := resetSbrDec(&pSbrChannel[ch].SbrDec, hSbrHeader, &pSbrChannel[ch].prevFrameData,
				self.flags, &pSbrChannel[ch].frameData[0]); err != sbrdecOK {
				hSbrHeader.SyncState = upsampling
			}
		}
		if applySbrProc {
			hSbrHeader.Status &^= sbrdecHdrStatReset
		}
	}

	// decoding.
	if int(hSbrHeader.SyncState) == sbrActive ||
		(int(hSbrHeader.SyncState) == sbrHeaderState && hSbrHeader.FrameError == 0) {
		var dataR *SbrFrameData
		var prevR *SbrPrevFrameData
		if stereo {
			dataR = hFrameDataRight
			prevR = &pSbrChannel[1].prevFrameData
		}
		DecodeSbrData(hSbrHeader, hFrameDataLeft, &pSbrChannel[0].prevFrameData, dataR, prevR)
		hSbrHeader.SyncState = sbrActive
	}

	// Output-buffer size sanity (sbrdecoder.cpp:1707-1712). PS doubles the channel
	// count for the buffer requirement.
	outChans := numInChannels
	if psPossible && 2 > outChans {
		outChans = 2
	}
	needed := int(hSbrHeader.NumberTimeSlots) * int(hSbrHeader.TimeStep) *
		int(self.pQmfDomain.globalConf.nBandsSynthesis) * outChans
	if len(timeData) < needed {
		return sbrdecOutputBufferTooSmall
	}

	self.flags &^= sbrdecPsDecoded

	// decode PS data if available (sbrdecoder.cpp:1718-1727).
	if self.hParametricStereoDec != nil && psPossible && int(hSbrHeader.SyncState) == sbrActive {
		var pPsScratch psDecCoefficients
		self.hParametricStereoDec.ProcessSlot = hSbrElement.useFrameSlot
		applyPs := decodePs(self.hParametricStereoDec, hSbrHeader.FrameError, &pPsScratch)
		if applyPs != 0 {
			self.flags |= sbrdecPsDecoded
		}
	}

	offset0 := channelIndex
	offset0Block := offset0 * codecFrameSize
	offset1 := 255
	offset1Block := 0
	if stereo || psPossible {
		offset1 = channelIndex + 1
		offset1Block = offset1 * codecFrameSize
	}

	// Set strides for reading and writing (sbrdecoder.cpp:1740-1743).
	strideOut := numInChannels
	if psPossible {
		if numInChannels < 2 {
			strideOut = 2
		} else {
			strideOut = numInChannels
		}
	}

	applyProc := int(hSbrHeader.SyncState) == sbrActive

	// Process left channel. When PS is decoded the second SbrDec + the right
	// output slot drive the dual QMF synthesis inside sbrDecRun.
	var rightDec *SbrDec
	var timeOutRight []int32
	if self.flags&sbrdecPsDecoded != 0 {
		rightDec = &pSbrChannel[1].SbrDec
		timeOutRight = timeData[offset1:]
	}
	sbrDecRun(&pSbrChannel[0].SbrDec, input[offset0Block:], timeData[offset0:],
		rightDec, timeOutRight, strideOut, hSbrHeader, hFrameDataLeft, &pSbrChannel[0].prevFrameData,
		applyProc, self.hParametricStereoDec, self.flags, codecFrameSize, self.sbrInDataHeadroom)

	if stereo {
		sbrDecRun(&pSbrChannel[1].SbrDec, input[offset1Block:], timeData[offset1:],
			nil, nil, strideOut, hSbrHeader, hFrameDataRight, &pSbrChannel[1].prevFrameData,
			applyProc, nil, self.flags, codecFrameSize, self.sbrInDataHeadroom)
	}

	if self.hParametricStereoDec != nil {
		// save PS status for next run (sbrdecoder.cpp:1766-1769).
		if self.flags&sbrdecPsDecoded != 0 {
			self.hParametricStereoDec.PsDecodedPrv = 1
		} else {
			self.hParametricStereoDec.PsDecodedPrv = 0
		}
	}

	// PS stereo output (sbrdecoder.cpp:1771-1795): a PS-capable decoder produces a
	// stereo output even with no PS data — copy left to right when not decoded.
	if psPossible {
		if self.flags&sbrdecPsDecoded == 0 {
			copyFrameSize := codecFrameSize * self.pQmfDomain.QmfDomainOut[0].fb.noChannels
			copyFrameSize /= self.pQmfDomain.QmfDomainIn[0].fb.noChannels
			// strideOut == 2: timeData is interleaved L,R,L,R; replicate L into R.
			for i := 0; i < copyFrameSize; i++ {
				timeData[2*i+1] = timeData[2*i]
			}
		}
		*numOutChannels = 2
	}

	return sbrdecOK
}

// SbrDecoderApply ports sbrDecoder_Apply (sbrdecoder.cpp:1800-1931): configure the
// QMF domain on first use, loop over the SBR elements running
// sbrDecoder_DecodeElement, and report the doubled output sample rate. input is
// the AAC-LC core int32 time signal (numChannels*codecFrameSize, channel-blocked);
// timeData receives the interleaved SBR output (2*codecFrameSize samples per
// channel). psDecoded is in/out: on entry non-nil and *psDecoded!=0 requests PS
// (psPossible); on return it reports whether PS was actually applied. EXCLUDED:
// DRC, SBRDEC_FLUSH/FORCE_RESET, LOW_POWER re-init. coreDecodedOk forces
// upsampling when false.
func SbrDecoderApply(self *SbrDecoderInstance, input, timeData []int32, numChannels *int,
	sampleRate *int, coreDecodedOk bool, inDataHeadroom int, psDecoded *int) sbrError {
	if self == nil {
		return sbrdecInvalidArgument
	}
	psPossible := psDecoded != nil && *psDecoded != 0
	numCoreChannels := *numChannels
	if numCoreChannels <= 0 {
		return sbrdecInvalidArgument
	}
	if self.numSbrElements < 1 {
		return sbrdecNotInitialized
	}
	for n := 0; n < self.numSbrElements; n++ {
		if self.pSbrElement[n] == nil {
			return sbrdecNotInitialized
		}
	}

	// PS is only possible for a single ID_SCE element (sbrdecoder.cpp:1839-1841).
	if self.numSbrElements != 1 || self.pSbrElement[0].elementID != mp4ElementSCE {
		psPossible = false
	}
	if !psPossible {
		self.flags &^= sbrdecPsDecoded
	}

	self.sbrInDataHeadroom = inDataHeadroom

	// Configure the QMF domain on first use / config change.
	if qmfDomainConfigure(self.pQmfDomain) != 0 {
		return sbrdecUnsupportedConfig
	}
	if self.numSbrChannels > int(self.pQmfDomain.globalConf.nInputChannels) {
		return sbrdecUnsupportedConfig
	}

	numSbrChannels := 0
	for sbrElementNum := 0; sbrElementNum < self.numSbrElements; sbrElementNum++ {
		// Disable PS and decode SBR mono if the second channel is missing
		// (sbrdecoder.cpp:1883-1887).
		if psPossible && self.pSbrElement[sbrElementNum].pSbrChannel[1] == nil {
			psPossible = false
		}

		numElementChan := 1
		if self.pSbrElement[sbrElementNum].elementID == mp4ElementCPE {
			numElementChan = 2
		}
		if !coreDecodedOk {
			setFrameErrorFlag(self.pSbrElement[sbrElementNum], frameErrorAllSlots)
		}
		if err := sbrDecoderDecodeElement(self, input, timeData, numSbrChannels,
			sbrElementNum, numCoreChannels, &numElementChan, psPossible); err != sbrdecOK {
			return err
		}
		numSbrChannels += numElementChan
		if numSbrChannels >= numCoreChannels {
			break
		}
	}

	*numChannels = numSbrChannels
	*sampleRate = self.sampleRateOut
	if psDecoded != nil {
		if self.flags&sbrdecPsDecoded != 0 {
			*psDecoded = 1
		} else {
			*psDecoded = 0
		}
	}
	self.flags &^= sbrdecForceReset
	self.flags &^= sbrdecFlush
	return sbrdecOK
}
