// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Exports a full AAC-LC CBR QCMain end-to-end driver to the sibling
// parity_tests/enc-qc-main-loop package, mirroring the genuine-fdk oracle bridge
// (bridge.cpp qcmain_e2e) one-to-one: build the channel mapping, allocate +
// init the QC state (QCNew/QCInit/QCOutNew/QCOutInit), seed the PSY_OUT graph and
// the per-channel mdct spectrum + threshold/energy/minSnr/spread LD-data, run
// QCMainPrepare then QCMain, and return the quantized spectrum, scalefactors,
// global gain and bit accounting. Asserting these EXACT-equal to the oracle pins
// the whole driver tier. aacfdk-fenced; thin wrapper, no port logic.

package nativeaac

// QCMainParityIn is the flat input the parity oracle marshals (mirror of
// bridge.cpp qcm_in). Per-channel arrays are indexed [ch][sfb] / [ch][line].
type QCMainParityIn struct {
	NChannels, Bitrate, SampleRate, MaxBits, MinBits, BitRes, AverageBits int
	StaticBits, MeanPe, MaxIterations, InvQuant, MaxBitFac, AvgTotalBits  int

	SfbCnt, SfbPerGroup, MaxSfbPerGroup, LastWindowSequence int
	SfbOffsets                                              []int

	MdctSpectrum       [2][]int32
	SfbThresholdLdData [2][]int32
	SfbEnergyLdData    [2][]int32
	SfbEnergy          [2][]int32
	SfbMinSnrLdData    [2][]int32
	SfbSpreadEnergy    [2][]int32
	NoiseNrg           [2][]int
	IsBook             [2][]int
	IsScale            [2][]int
}

// QCMainParityOut is the flat output (mirror of bridge.cpp qcm_out).
type QCMainParityOut struct {
	ErrCode                                                               int
	QuantSpec                                                             [2][1024]int16
	Scf                                                                   [2][MaxGroupedSFB]int
	GlobalGain                                                            [2]int
	MaxValueInSfb                                                         [2][MaxGroupedSFB]uint
	StaticBitsUsed, DynBitsUsed, GrantedDynBits, GrantedPe, GrantedPeCorr int
	UsedDynBits, AuGrantedDynBits, MaxDynBits, TotalGrantedPeCorr         int
	NoOfSections0, HuffmanBits0, SideInfoBits0, ScalefacBits0             int
}

// QCMainE2EForParity drives the full Go-side QCMain pipeline for one AAC-LC CBR
// access unit (single SCE or CPE element, single sub frame), mirroring the C
// oracle bridge so the test can assert exact-integer parity.
func QCMainE2EForParity(in *QCMainParityIn) QCMainParityOut {
	var out QCMainParityOut

	nCh := in.NChannels

	// --- channel mapping (single SCE or CPE) ---
	cm := &ChannelMapping{
		NChannels:    nCh,
		NChannelsEff: nCh,
		NElements:    1,
	}
	if nCh == 2 {
		cm.EncMode = ChannelMode2
		cm.ElInfo[0].ElType = IDCPE
		cm.ElInfo[0].NChannelsInEl = 2
		cm.ElInfo[0].RelativeBits = maxvalDBL
	} else {
		cm.EncMode = ChannelMode1
		cm.ElInfo[0].ElType = IDSCE
		cm.ElInfo[0].NChannelsInEl = 1
		cm.ElInfo[0].RelativeBits = maxvalDBL
	}

	// --- allocate + init QC state ---
	hQC, err := QCNew(1)
	if err != AacEncOK {
		out.ErrCode = int(err)
		return out
	}
	qcOut := make([]*QcOut, 1)
	if err := QCOutNew(qcOut, 1, nCh, 1); err != AacEncOK {
		out.ErrCode = int(err)
		return out
	}

	qcInit := &QcInit{
		ChannelMapping:      cm,
		MaxBits:             in.MaxBits,
		AverageBits:         in.AverageBits,
		BitRes:              in.BitRes,
		SampleRate:          in.SampleRate,
		IsLowDelay:          0,
		StaticBits:          in.StaticBits,
		BitrateMode:         QcdataBrModeCBR,
		MeanPe:              in.MeanPe,
		ChBitrate:           in.Bitrate / nCh,
		InvQuant:            in.InvQuant,
		MaxIterations:       in.MaxIterations,
		MaxBitFac:           int32(in.MaxBitFac),
		Bitrate:             in.Bitrate,
		NSubFrames:          1,
		MinBits:             in.MinBits,
		BitResMode:          AacencBrModeFull,
		BitDistributionMode: 0,
		Padding:             Padding{PaddingRest: in.SampleRate},
	}
	if err := QCInit(hQC, qcInit, 1); err != AacEncOK {
		out.ErrCode = int(err)
		return out
	}
	if err := QCOutInit(qcOut, 1, cm); err != AacEncOK {
		out.ErrCode = int(err)
		return out
	}

	// --- build PSY_OUT graph ---
	psyOut := make([]*PsyOut, 1)
	psyOut[0] = new(PsyOut)
	psyEl := new(PsyOutElement)
	psyOut[0].PsyOutElement[0] = psyEl
	if nCh == 2 {
		psyEl.CommonWindow = 1
	}
	for ch := 0; ch < nCh; ch++ {
		p := new(PsyOutChannel)
		psyEl.PsyOutChannel[ch] = p
		p.SfbCnt = in.SfbCnt
		p.SfbPerGroup = in.SfbPerGroup
		p.MaxSfbPerGroup = in.MaxSfbPerGroup
		p.LastWindowSequence = in.LastWindowSequence
		p.WindowShape = 0
		for i := 0; i <= in.SfbCnt && i < len(p.SfbOffsets); i++ {
			p.SfbOffsets[i] = in.SfbOffsets[i]
		}
		for s := 0; s < MaxGroupedSFB; s++ {
			if s < len(in.SfbEnergy[ch]) {
				p.SfbEnergy[s] = in.SfbEnergy[ch][s]
			}
			if s < len(in.NoiseNrg[ch]) {
				p.NoiseNrg[s] = in.NoiseNrg[ch][s]
			}
			if s < len(in.IsBook[ch]) {
				p.IsBook[s] = in.IsBook[ch][s]
			}
			if s < len(in.IsScale[ch]) {
				p.IsScale[s] = in.IsScale[ch][s]
			}
		}
	}

	// --- seed QC_OUT_CHANNEL raw spectrum + LD-data ---
	qcEl := qcOut[0].QcElement[0]
	for ch := 0; ch < nCh; ch++ {
		q := qcEl.QcOutChannel[ch]
		for i := 0; i < 1024 && i < len(in.MdctSpectrum[ch]); i++ {
			q.MdctSpectrum[i] = in.MdctSpectrum[ch][i]
		}
		for s := 0; s < MaxGroupedSFB; s++ {
			if s < len(in.SfbThresholdLdData[ch]) {
				q.SfbThresholdLdData[s] = in.SfbThresholdLdData[ch][s]
			}
			if s < len(in.SfbEnergyLdData[ch]) {
				q.SfbEnergyLdData[s] = in.SfbEnergyLdData[ch][s]
			}
			if s < len(in.SfbEnergy[ch]) {
				q.SfbEnergy[s] = in.SfbEnergy[ch][s]
			}
			if s < len(in.SfbMinSnrLdData[ch]) {
				q.SfbMinSnrLdData[s] = in.SfbMinSnrLdData[ch][s]
			}
			if s < len(in.SfbSpreadEnergy[ch]) {
				q.SfbSpreadEnergy[s] = in.SfbSpreadEnergy[ch][s]
			}
		}
	}

	// --- QCMainPrepare ---
	if e := QCMainPrepare(&cm.ElInfo[0], hQC.HAdjThr.adjThrStateElem[0],
		psyEl, qcEl, AOTAACLC, 0, -1); e != AacEncOK {
		out.ErrCode = int(e)
		return out
	}
	qcOut[0].StaticBits = qcEl.StaticBitsUsed
	qcOut[0].TotalNoRedPe = int(qcEl.PeData.pe)

	// --- QCMain ---
	e := QCMain(hQC, psyOut, qcOut, in.AvgTotalBits, cm, AOTAACLC, 0, -1)
	out.ErrCode = int(e)
	if e != AacEncOK {
		return out
	}

	// --- copy out ---
	for ch := 0; ch < nCh; ch++ {
		q := qcEl.QcOutChannel[ch]
		for i := 0; i < 1024; i++ {
			out.QuantSpec[ch][i] = q.QuantSpec[i]
		}
		for s := 0; s < MaxGroupedSFB; s++ {
			out.Scf[ch][s] = q.Scf[s]
			out.MaxValueInSfb[ch][s] = q.MaxValueInSfb[s]
		}
		out.GlobalGain[ch] = q.GlobalGain
	}
	out.StaticBitsUsed = qcEl.StaticBitsUsed
	out.DynBitsUsed = qcEl.DynBitsUsed
	out.GrantedDynBits = qcEl.GrantedDynBits
	out.GrantedPe = qcEl.GrantedPe
	out.GrantedPeCorr = qcEl.GrantedPeCorr
	out.UsedDynBits = qcOut[0].UsedDynBits
	out.AuGrantedDynBits = qcOut[0].GrantedDynBits
	out.MaxDynBits = qcOut[0].MaxDynBits
	out.TotalGrantedPeCorr = qcOut[0].TotalGrantedPeCorr
	out.NoOfSections0 = qcEl.QcOutChannel[0].SectionData.NoOfSections
	out.HuffmanBits0 = qcEl.QcOutChannel[0].SectionData.HuffmanBits
	out.SideInfoBits0 = qcEl.QcOutChannel[0].SectionData.SideInfoBits
	out.ScalefacBits0 = qcEl.QcOutChannel[0].SectionData.ScalefacBits

	return out
}
