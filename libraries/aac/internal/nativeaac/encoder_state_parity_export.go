// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parity-only export of the native Encoder's carried inter-frame state for the
// multi-frame psyMain state-parity oracle under
// internal/parity_tests/psymain-multiframe/. After each EncodeOneFrame the
// oracle reads EncoderState (the per-channel PSY_STATIC pre-echo carry, the
// adj_thr ATS_ELEMENT pe-correction carry and the qcKernel bit-reservoir) and
// compares it EXACTLY against the same fields read out of the genuine vendored
// fdk encoder handle. fdk-aac encode is fixed-point so every value is exact
// int32/int.

package nativeaac

// ChannelStateDump mirrors the per-channel carried PSY_STATIC pre-echo /
// block-switch state and the per-channel adj_thr ATS_ELEMENT pe-correction
// carry that determine the next frame's thresholds and bit demand.
type ChannelStateDump struct {
	// PSY_STATIC pre-echo carry (psy_data.h:147-149).
	SfbThresholdNm1 [maxSfbLong]int32
	MdctScaleNm1    int
	CalcPreEcho     int

	// BlockSwitchingControl carried fields that select the window for the next
	// frame (block_switch.h BLOCK_SWITCHING_CONTROL).
	LastWindowSequence int
	WindowShape        int
	LastWindowShape    int
	NoOfGroups         int

	// adj_thr ATS_ELEMENT carry (adj_thr_data.h:153-156, 160).
	PeLast              int
	DynBitsLast         int
	PeCorrectionFactorM int32
	PeCorrectionFactorE int
	ChaosMeasureOld     int32

	// MdctScale is the per-frame psyData[ch].mdctScale (psy_data.h:163) retained
	// in PsyDynamic after the frame; it is the exponent preEchoControl carries
	// into mdctScalenm1, so a divergence here drives the pre-echo carry.
	MdctScale int
}

// EncoderStateDump is the full inter-frame carried state of the encoder after a
// frame: per-channel PSY_STATIC + adj_thr carry, plus the qcKernel bit
// reservoir (qc_data.h:280).
type EncoderStateDump struct {
	Channels       []ChannelStateDump
	BitResTot      int
	Pe             int
	ConstPart      int
	NActiveLines   int
	GrantedDynBits int
	GrantedPe      int

	// PsyOut is the per-channel POST-psyMain per-SFB output for the current
	// frame (the threshold-advance / M-S / PNS result that feeds peData.pe).
	// Captured so the multi-frame oracle can localize the FIRST per-SFB value
	// that diverges (vs. the carried-state fields above, which only show the
	// downstream pe carry). MsMask/MsDigest are the element-level M-S decision.
	PsyOut   []PsyOutChannelDump
	MsDigest int
	MsMask   [60]int
}

// PsyOutChannelDump mirrors the per-channel POST-TNS psyMain per-SFB outputs the
// rate-control machine reads: the ld-domain threshold/energy arrays
// (psyOutChannel.sfb*LdData) and the post-IS intensity book. These are the
// values the qc tier consumes to compute peData.pe; they are stable on the
// encoder handle after psyMain (unlike the linear psyData.sfbEnergy/sfbThreshold
// scratch unions, which are reused downstream and are NOT a parity target).
// Sized to C MAX_GROUPED_SFB == 60.
type PsyOutChannelDump struct {
	MaxSfbPerGroup     int
	SfbCnt             int
	SfbPerGroup        int
	SfbThresholdLdData [60]int32
	SfbEnergyLdData    [60]int32
	// IsBook is the post-IntensityStereoProcessing intensity-stereo code book
	// per band (psyOutChannel.isBook); nonzero == band coded as intensity.
	IsBook [60]int
}

// DumpState snapshots every value the native encoder carries from one frame to
// the next that can influence a subsequent frame's bitstream: the PSY_STATIC
// pre-echo history, the block-switch window-sequence carry, the adj_thr
// pe-correction carry and the bit reservoir.
func (e *Encoder) DumpState() EncoderStateDump {
	qe := e.enc.QcOut[0].QcElement[0]
	out := EncoderStateDump{
		Channels:       make([]ChannelStateDump, e.channels),
		BitResTot:      e.enc.QcKernel.BitResTot,
		Pe:             int(qe.PeData.pe),
		ConstPart:      int(qe.PeData.constPart),
		NActiveLines:   int(qe.PeData.nActiveLines),
		GrantedDynBits: qe.GrantedDynBits,
		GrantedPe:      qe.GrantedPe,
	}
	el := e.enc.PsyKernel.PsyElement[0]
	ats := e.enc.QcKernel.HAdjThr.adjThrStateElem[0]
	for ch := 0; ch < e.channels; ch++ {
		ps := el.PsyStatic[ch]
		bsc := &ps.BlockSwitchingControl
		cs := ChannelStateDump{
			SfbThresholdNm1:    ps.SfbThresholdNm1,
			MdctScaleNm1:       ps.MdctScaleNm1,
			CalcPreEcho:        ps.CalcPreEcho,
			LastWindowSequence: bsc.LastWindowSequence,
			WindowShape:        bsc.WindowShape,
			LastWindowShape:    bsc.LastWindowShape,
			NoOfGroups:         bsc.NoOfGroups,
			// adj_thr state is per-element (one ATS_ELEMENT per element, shared by
			// both channels of a CPE), so both channels report the same element
			// carry — matching the genuine adjThrStateElem[el] single instance.
			PeLast:              ats.peLast,
			DynBitsLast:         ats.dynBitsLast,
			PeCorrectionFactorM: ats.peCorrectionFactorM,
			PeCorrectionFactorE: ats.peCorrectionFactorE,
			ChaosMeasureOld:     ats.chaosMeasureOld,
			MdctScale:           e.enc.PsyKernel.PsyDynamic.PsyData[ch].MdctScale,
		}
		out.Channels[ch] = cs
	}

	// Capture the POST-psyMain per-SFB outputs for the current frame. psyMain
	// wrote psyOutElement (and the per-channel psyData M-S arrays) just before
	// the qc tier consumed them; they persist on the handle until the next
	// EncodeOneFrame overwrites them.
	psyEl := e.enc.PsyOut[0].PsyOutElement[0]
	out.MsDigest = psyEl.ToolsInfo.MsDigest
	copy(out.MsMask[:], psyEl.ToolsInfo.MsMask[:60])
	out.PsyOut = make([]PsyOutChannelDump, e.channels)
	for ch := 0; ch < e.channels; ch++ {
		poc := psyEl.PsyOutChannel[ch]
		d := PsyOutChannelDump{
			MaxSfbPerGroup: poc.MaxSfbPerGroup,
			SfbCnt:         poc.SfbCnt,
			SfbPerGroup:    poc.SfbPerGroup,
		}
		copy(d.SfbThresholdLdData[:], poc.SfbThresholdLdData[:60])
		copy(d.SfbEnergyLdData[:], poc.SfbEnergyLdData[:60])
		for i := 0; i < 60; i++ {
			d.IsBook[i] = poc.IsBook[i]
		}
		out.PsyOut[ch] = d
	}
	return out
}
