// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// This file ports the HE-AAC v1 subset of the FDK_QmfDomain orchestration layer
// (libFDK/include/FDK_qmf_domain.h + libFDK/src/FDK_qmf_domain.cpp) — the wrapper
// sbr_dec() drives the raw QMF FilterBank/ScaleFactor primitives (qmf.go) through.
// It owns the per-channel analysis/synthesis state, the overlap (persistent) and
// frame (work) QMF slot buffers, and the global config (band counts, slot
// counts). The QmfDomain wraps the existing FilterBank by value; it does not
// redefine it.
//
// HE-AAC v1 ONLY: 32-band analysis / 64-band synthesis, full analysis+synthesis
// (no SBRDEC_SKIP_QMF_*). EXCLUDED: CLDFB / MPS-LDFB, the QMF-based resampler,
// the parkChannel (USAC stereoConfigIndex3) path, the WorkBuffer2ProcChannel /
// QmfData2HBE HBE accessors, FDK_QmfDomain_GetSlot (an MPS/PS accessor not used
// on the legacy SBR decode path), and the size-variant Get*QmfStates16/24/32
// pools. The Go port allocates plain []int32 / [][]int32 slices directly instead
// of the C work-buffer-section arithmetic (cpp:429-476), which is purely a
// memory-pooling concern with no effect on the computed samples.

// qmfDomainGC is FDK_QMF_DOMAIN_GC (FDK_qmf_domain.h:167-220): the per-instance
// global QMF configuration. Only the fields the HE-AAC v1 path sets/reads are
// kept; the *_requested churn drives FDK_QmfDomain_Configure below.
type qmfDomainGC struct {
	qmfDomainExplicitConfig uint8 // qmfDomainExplicitConfig (stays 0 for SBR)

	nInputChannels           uint8 // nInputChannels
	nInputChannelsRequested  uint8 // nInputChannels_requested
	nOutputChannels          uint8 // nOutputChannels
	nOutputChannelsRequested uint8 // nOutputChannels_requested

	flags          uint // flags
	flagsRequested uint // flags_requested

	nBandsAnalysis           uint8 // nBandsAnalysis (32 for SBR analysis)
	nBandsAnalysisRequested  uint8
	nBandsSynthesis          uint16 // nBandsSynthesis (64 for dual-rate SBR)
	nBandsSynthesisRequested uint16

	nQmfTimeSlots             uint8 // nQmfTimeSlots (== numberTimeSlots*timeStep)
	nQmfTimeSlotsRequested    uint8
	nQmfOvTimeSlots           uint8 // nQmfOvTimeSlots (overlap, == 6 for AAC)
	nQmfOvTimeSlotsRequested  uint8
	nQmfProcBands             uint8 // nQmfProcBands (always 64)
	nQmfProcBandsRequested    uint8
	nQmfProcChannels          uint8 // nQmfProcChannels (1 for legacy SBR)
	nQmfProcChannelsRequested uint8
}

// qmfDomainIn is FDK_QMF_DOMAIN_IN (FDK_qmf_domain.h:229-251): one QMF input
// channel — analysis filter bank, scaling, and the time-slot pointer arrays.
type qmfDomainIn struct {
	pGlobalConf *qmfDomainGC // pGlobalConf
	fb          FilterBank   // fb (analysis)
	scaling     ScaleFactor  // scaling

	pAnaQmfStates  []int32   // pAnaQmfStates (9*64 FIXP_QAS, persistent)
	pOverlapBuffer []int32   // pOverlapBuffer (nQmfOvTimeSlots*nProcBands*2, persistent)
	pWorkBuffer    []int32   // pWorkBuffer (nQmfTimeSlots*nProcBands*2, frame)
	hQmfSlotsReal  [][]int32 // hQmfSlotsReal (ov+frame slot rows)
	hQmfSlotsImag  [][]int32 // hQmfSlotsImag
}

// qmfDomainOut is FDK_QMF_DOMAIN_OUT (FDK_qmf_domain.h:257-261): one QMF output
// channel — synthesis filter bank + synthesis states.
type qmfDomainOut struct {
	fb            FilterBank // fb (synthesis)
	pSynQmfStates []int32    // pSynQmfStates (9*nBandsSynthesis FIXP_QSS)
}

// qmfDomain is FDK_QMF_DOMAIN (FDK_qmf_domain.h:267-273): the multi-channel QMF
// domain. Sized to the HE-AAC v1 max of (8)+(1) channels like the C.
type qmfDomain struct {
	globalConf   qmfDomainGC     // globalConf
	QmfDomainIn  [9]qmfDomainIn  // QmfDomainIn[(8)+(1)]
	QmfDomainOut [9]qmfDomainOut // QmfDomainOut[(8)+(1)]
}

// cmplxMod is CMPLX_MOD (==2): real+imag interleave factor.
const cmplxMod = 2

// qmfDomainAllocatePersistentMemory ports FDK_QmfDomain_AllocatePersistentMemory
// (FDK_qmf_domain.cpp:259-381) for the HE-AAC v1 STD case. It allocates, per
// input channel: the analysis state buffer (nBandsAnalysis*10), the slot pointer
// arrays (nQmfOvTimeSlots+nQmfTimeSlots rows) and the overlap buffer
// (nQmfOvTimeSlots*nQmfProcBands*CMPLX_MOD); and per output channel the synthesis
// state buffer (nBandsSynthesis*9). The size-variant ROM pools (16/24/32) are
// excluded — only the generic allocation path is taken. Returns -1 on bad config.
func qmfDomainAllocatePersistentMemory(qd *qmfDomain) int {
	gc := &qd.globalConf
	if int(gc.nInputChannels) > 9 || int(gc.nOutputChannels) > 9 {
		return -1
	}
	nProcBands := int(gc.nQmfProcBands)
	for ch := 0; ch < int(gc.nInputChannels); ch++ {
		size := int(gc.nBandsAnalysis) * 10
		if size > 0 {
			if qd.QmfDomainIn[ch].pAnaQmfStates == nil {
				qd.QmfDomainIn[ch].pAnaQmfStates = make([]int32, size)
			}
		} else {
			qd.QmfDomainIn[ch].pAnaQmfStates = nil
		}

		size = int(gc.nQmfOvTimeSlots) + int(gc.nQmfTimeSlots)
		if size > 0 {
			if qd.QmfDomainIn[ch].hQmfSlotsReal == nil {
				qd.QmfDomainIn[ch].hQmfSlotsReal = make([][]int32, size)
			}
			if qd.QmfDomainIn[ch].hQmfSlotsImag == nil {
				qd.QmfDomainIn[ch].hQmfSlotsImag = make([][]int32, size)
			}
		} else {
			qd.QmfDomainIn[ch].hQmfSlotsReal = nil
			qd.QmfDomainIn[ch].hQmfSlotsImag = nil
		}

		size = int(gc.nQmfOvTimeSlots) * nProcBands * cmplxMod
		if size > 0 {
			if qd.QmfDomainIn[ch].pOverlapBuffer == nil {
				qd.QmfDomainIn[ch].pOverlapBuffer = make([]int32, size)
			}
		} else {
			qd.QmfDomainIn[ch].pOverlapBuffer = nil
		}

		// Frame work buffer: nQmfTimeSlots rows * nProcBands * (real+imag). The C
		// shares a section-pooled work buffer between channels (FDK_getWorkBuffer);
		// the Go port allocates one per channel since the result is identical.
		size = int(gc.nQmfTimeSlots) * nProcBands * cmplxMod
		if size > 0 {
			if qd.QmfDomainIn[ch].pWorkBuffer == nil {
				qd.QmfDomainIn[ch].pWorkBuffer = make([]int32, size)
			}
		} else {
			qd.QmfDomainIn[ch].pWorkBuffer = nil
		}
	}

	for ch := 0; ch < int(gc.nOutputChannels); ch++ {
		size := int(gc.nBandsSynthesis) * 9
		if size > 0 {
			if qd.QmfDomainOut[ch].pSynQmfStates == nil {
				qd.QmfDomainOut[ch].pSynQmfStates = make([]int32, size)
			}
		} else {
			qd.QmfDomainOut[ch].pSynQmfStates = nil
		}
	}
	return 0
}

// qmfDomainInitFilterBank ports FDK_QmfDomain_InitFilterBank
// (FDK_qmf_domain.cpp:486-566) for the HE-AAC v1 STD case. It wires each input
// channel's slot pointer rows — the first nQmfOvTimeSlots rows to the persistent
// overlap buffer, the rest to the frame work buffer — then initialises the
// analysis and synthesis filter banks. Returns non-zero on error.
func qmfDomainInitFilterBank(qd *qmfDomain, extraFlags uint) int {
	err := 0
	gc := &qd.globalConf
	noCols := int(gc.nQmfTimeSlots)
	lsb := int(gc.nBandsAnalysis)
	usb := fMinI(int(gc.nBandsSynthesis), 64)
	nProcBands := int(gc.nQmfProcBands)

	// MPSLDFB excluded; no flag rewrite needed (cpp:497-500 is MPS-only).

	for ch := 0; ch < int(gc.nInputChannels); ch++ {
		qdc := &qd.QmfDomainIn[ch]
		ptrOv := qdc.pOverlapBuffer
		if ptrOv == nil && gc.nQmfOvTimeSlots != 0 {
			return 1
		}
		work := qdc.pWorkBuffer
		if work == nil && gc.nQmfTimeSlots != 0 {
			return 1
		}

		qdc.pGlobalConf = gc

		ovOff := 0
		ts := 0
		for ; ts < int(gc.nQmfOvTimeSlots); ts++ {
			qdc.hQmfSlotsReal[ts] = ptrOv[ovOff : ovOff+nProcBands : ovOff+nProcBands]
			ovOff += nProcBands
			qdc.hQmfSlotsImag[ts] = ptrOv[ovOff : ovOff+nProcBands : ovOff+nProcBands]
			ovOff += nProcBands
		}
		wbOff := 0
		for ; ts < int(gc.nQmfOvTimeSlots)+int(gc.nQmfTimeSlots); ts++ {
			qdc.hQmfSlotsReal[ts] = work[wbOff : wbOff+nProcBands : wbOff+nProcBands]
			wbOff += nProcBands
			qdc.hQmfSlotsImag[ts] = work[wbOff : wbOff+nProcBands : wbOff+nProcBands]
			wbOff += nProcBands
		}

		// (qd->QmfDomainIn[ch].fb.lsb == 0) ? lsb : fb.lsb — fb starts zeroed.
		fbLsb := lsb
		if qdc.fb.lsb != 0 {
			fbLsb = qdc.fb.lsb
		}
		fbUsb := usb
		if qdc.fb.usb != 0 {
			fbUsb = qdc.fb.usb
		}
		err |= InitAnalysisFilterBank(&qdc.fb, qdc.pAnaQmfStates, noCols, fbLsb, fbUsb,
			int(gc.nBandsAnalysis), gc.flags|extraFlags)
	}

	for ch := 0; ch < int(gc.nOutputChannels); ch++ {
		qdo := &qd.QmfDomainOut[ch]
		outGainM := qdo.fb.outGainM
		outGainE := qdo.fb.outGainE
		outScale := GetOutScalefactor(&qdo.fb)
		fbLsb := lsb
		if qdo.fb.lsb != 0 {
			fbLsb = qdo.fb.lsb
		}
		fbUsb := usb
		if qdo.fb.usb != 0 {
			fbUsb = qdo.fb.usb
		}
		err |= InitSynthesisFilterBank(&qdo.fb, qdo.pSynQmfStates, noCols, fbLsb, fbUsb,
			int(gc.nBandsSynthesis), gc.flags|extraFlags)
		// FDK_qmf_domain.cpp:557: only re-apply the out gain if it was explicitly
		// set (!= 0); the default-init 0x80000000 sentinel means "ignore", and a
		// fresh fb reads outGain_m == 0 here.
		if outGainM != 0 {
			ChangeOutGain(&qdo.fb, outGainM, outGainE)
		}
		if outScale != 0 {
			ChangeOutScalefactor(&qdo.fb, outScale)
		}
	}
	return err
}

// qmfDomainSaveOverlap ports FDK_QmfDomain_SaveOverlap
// (FDK_qmf_domain.cpp:568-594): copy the frame-tail slots into the overlap slots
// for use by the next frame, and carry the low-band scale into ov_lb_scale.
func qmfDomainSaveOverlap(qdc *qmfDomainIn, offset int) {
	gc := qdc.pGlobalConf
	ovSlots := int(gc.nQmfOvTimeSlots)
	nCols := int(gc.nQmfTimeSlots)
	nProcBands := int(gc.nQmfProcBands)
	qmfReal := qdc.hQmfSlotsReal
	qmfImag := qdc.hQmfSlotsImag

	if qmfImag != nil {
		for ts := offset; ts < ovSlots; ts++ {
			copy(qmfReal[ts][:nProcBands], qmfReal[nCols+ts][:nProcBands])
			copy(qmfImag[ts][:nProcBands], qmfImag[nCols+ts][:nProcBands])
		}
	} else {
		for ts := 0; ts < ovSlots; ts++ {
			copy(qmfReal[ts][:nProcBands], qmfReal[nCols+ts][:nProcBands])
		}
	}
	qdc.scaling.OvLbScale = qdc.scaling.LbScale
}

// qmfDomainConfigure ports the HE-AAC v1 happy path of FDK_QmfDomain_Configure
// (FDK_qmf_domain.cpp:818-996): copy the requested band/slot/channel counts into
// the active config, (re-)allocate persistent memory, apply the downsampled-SBR
// flag request, then (re-)init the filter banks. EXCLUDED: the resampler /
// parkChannel / CLDFB / explicit-MPS branches and the work-buffer section sizing
// (Go allocates per-channel work buffers directly). Returns 0 on success.
func qmfDomainConfigure(qd *qmfDomain) int {
	gc := &qd.globalConf
	hasChanged := false

	// 1. processing channels / proc bands / time slots change.
	if gc.nQmfProcChannels != gc.nQmfProcChannelsRequested ||
		gc.nQmfProcBands != gc.nQmfProcBandsRequested ||
		gc.nQmfTimeSlots != gc.nQmfTimeSlotsRequested {
		gc.nQmfProcBands = gc.nQmfProcBandsRequested
		gc.nQmfProcChannels = gc.nQmfProcChannelsRequested
		hasChanged = true
	}

	// 2. reallocate persistent memory if necessary.
	if gc.nInputChannels != gc.nInputChannelsRequested ||
		gc.nBandsAnalysis != gc.nBandsAnalysisRequested ||
		gc.nQmfTimeSlots != gc.nQmfTimeSlotsRequested ||
		gc.nQmfOvTimeSlots != gc.nQmfOvTimeSlotsRequested ||
		gc.nOutputChannels != gc.nOutputChannelsRequested ||
		gc.nBandsSynthesis != gc.nBandsSynthesisRequested {
		gc.nInputChannels = gc.nInputChannelsRequested
		gc.nBandsAnalysis = gc.nBandsAnalysisRequested
		gc.nQmfTimeSlots = gc.nQmfTimeSlotsRequested
		gc.nQmfOvTimeSlots = gc.nQmfOvTimeSlotsRequested
		gc.nOutputChannels = gc.nOutputChannelsRequested
		gc.nBandsSynthesis = gc.nBandsSynthesisRequested

		if qmfDomainAllocatePersistentMemory(qd) != 0 {
			return -1
		}

		// 3. downsampled-SBR flag request (32:32 STD only).
		if gc.nBandsAnalysis == 32 && gc.nBandsSynthesis == 32 &&
			gc.flags&(flagCLDFB|flagMPSLDFB) == 0 {
			gc.flagsRequested |= flagDownsampled
		}
		hasChanged = true
	}

	// 5. set requested flags.
	if gc.flags != gc.flagsRequested {
		gc.flags = gc.flagsRequested
		hasChanged = true
	}

	if hasChanged {
		// 9. (re-)init filterbank, patching default synthesis lsb/usb (cpp:965-973).
		for i := 0; i < int(gc.nOutputChannels); i++ {
			if qd.QmfDomainOut[i].fb.lsb == 0 && qd.QmfDomainOut[i].fb.usb == 0 {
				qd.QmfDomainOut[i].fb.lsb = int(gc.nBandsAnalysisRequested)
				qd.QmfDomainOut[i].fb.usb = fMinI(int(gc.nBandsSynthesisRequested), 64)
			}
		}
		if qmfDomainInitFilterBank(qd, 0) != 0 {
			return -1
		}
	}
	return 0
}
