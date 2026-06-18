// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// QC/PSY allocation + init tier: the 1:1 port of the FDK-AAC encoder
// quantizer/coder and psychoacoustic-model allocation/wiring entry points
// (qc_main.cpp FDKaacEnc_QCOutNew / FDKaacEnc_QCOutInit; psy_main.cpp
// FDKaacEnc_PsyNew / FDKaacEnc_PsyOutNew / FDKaacEnc_psyInitStates /
// FDKaacEnc_psyInit). In C these thread RAM-pool pointers handed out by
// GetRam_aacEnc_*; the pure-Go port allocates the same struct graph (one Go
// allocation per GetRam_* call) and reproduces the SAME pointer-aliasing —
// qcElement[i]->qcOutChannel[ch] and psyOutElement[i]->psyOutChannel[ch] alias
// the shared per-AU pQcOutChannels[] / pPsyOutChannels[] pools exactly as in C,
// and psyElement[i]->psyStatic[ch] aliases the shared pStaticChannels[] pool.
//
// FDKaacEnc_QCNew / FDKaacEnc_QCInit and FDKaacEnc_psyMainInit are intentionally
// NOT ported here: they reach into component state this struct universe does not
// (yet) model — QCNew/QCInit allocate+init the ADJ_THR_STATE handle (hAdjThr,
// FDKaacEnc_AdjThrNew/AdjThrInit) and the bit-counter (hBitCounter,
// FDKaacEnc_BCNew); psyMainInit calls FDKaacEnc_InitTnsConfiguration and the
// PSY_CONFIGURATION (psyConf[]) it fills, which PsyInternal here deliberately
// does not carry (enc_state.go). Those are owned by the adj-thr / tns-config
// component batches; wiring them is a separate, dependency-gated step. See the
// batch notes. The functions ported here are the ones whose entire observable
// output state is already modelled and verified.
//
// This tier is pure pointer wiring + integer/array clears (the only "compute" is
// the already-ported InitBlockSwitching + mdctInit), so the populated state is
// bit-identical regardless of build tag.

// --- qc_main.cpp:232: FDKaacEnc_QCOutNew ------------------------------------

// QCOutNew is the 1:1 port of FDKaacEnc_QCOutNew (qc_main.cpp:232): allocate, for
// each of the nSubFrames per-AU QC_OUT buffers, the channel pool
// (pQcOutChannels[0..nChannels)) and the element list (qcElement[0..nElements)).
// In C these are GetRam_aacEnc_QCout / GetRam_aacEnc_QCchannel /
// GetRam_aacEnc_QCelement RAM-pool handouts plus the dynMem_* adjust-thresholds
// scratch pointers; the Go port allocates the structs (the dynMem_* scratch is
// per-call working memory the orchestration owns elsewhere and is not modelled
// as persistent state). Returns AAC_ENC_OK.
func QCOutNew(phQC []*QcOut, nElements, nChannels, nSubFrames int) EncoderError {
	for n := 0; n < nSubFrames; n++ {
		phQC[n] = new(QcOut)
		for i := 0; i < nChannels; i++ {
			phQC[n].PQcOutChannels[i] = new(QcOutChannel)
		}
		for i := 0; i < nElements; i++ {
			phQC[n].QcElement[i] = new(QcOutElement)
		}
	}
	return AacEncOK
}

// --- qc_main.cpp:289: FDKaacEnc_QCOutInit -----------------------------------

// QCOutInit is the 1:1 port of FDKaacEnc_QCOutInit (qc_main.cpp:289): wire each
// element's per-channel qcOutChannel[ch] to the shared per-AU pQcOutChannels[]
// pool, walking the channel pool in element/channel order. Returns AAC_ENC_OK.
func QCOutInit(phQC []*QcOut, nSubFrames int, cm *ChannelMapping) EncoderError {
	for n := 0; n < nSubFrames; n++ {
		chInc := 0
		for i := 0; i < cm.NElements; i++ {
			for ch := 0; ch < cm.ElInfo[i].NChannelsInEl; ch++ {
				phQC[n].QcElement[i].QcOutChannel[ch] = phQC[n].PQcOutChannels[chInc]
				chInc++
			}
		}
	}
	return AacEncOK
}

// --- psy_main.cpp:137: FDKaacEnc_PsyNew -------------------------------------

// PsyNew is the 1:1 port of FDKaacEnc_PsyNew (psy_main.cpp:137): allocate the
// PSY_INTERNAL handle, the per-element PSY_ELEMENT pool (psyElement[0..nElements)),
// the per-channel PSY_STATIC pool (pStaticChannels[0..nChannels), each with its
// psyInputBuffer) and the reusable PSY_DYNAMIC working set. The C psyInputBuffer
// is a GetRam_aacEnc_PsyInputBuffer pointer; the Go PsyStatic embeds the buffer
// inline (enc_state.go), so no separate allocation is needed. Returns the handle
// and AAC_ENC_OK.
func PsyNew(nElements, nChannels int) (*PsyInternal, EncoderError) {
	hPsy := new(PsyInternal)

	for i := 0; i < nElements; i++ {
		hPsy.PsyElement[i] = new(PsyElement)
	}

	for i := 0; i < nChannels; i++ {
		hPsy.PStaticChannels[i] = new(PsyStatic)
		// psyInputBuffer is embedded in PsyStatic (no separate GetRam alloc).
	}

	// reusable psych memory
	hPsy.PsyDynamic = new(PsyDynamic)

	return hPsy, AacEncOK
}

// --- psy_main.cpp:193: FDKaacEnc_PsyOutNew ----------------------------------

// PsyOutNew is the 1:1 port of FDKaacEnc_PsyOutNew (psy_main.cpp:193): allocate,
// for each of the nSubFrames per-AU PSY_OUT buffers, the channel pool
// (pPsyOutChannels[0..nChannels)) and the element list
// (psyOutElement[0..nElements)). Returns AAC_ENC_OK.
func PsyOutNew(phpsyOut []*PsyOut, nElements, nChannels, nSubFrames int) EncoderError {
	for n := 0; n < nSubFrames; n++ {
		phpsyOut[n] = new(PsyOut)
		for i := 0; i < nChannels; i++ {
			phpsyOut[n].PPsyOutChannels[i] = new(PsyOutChannel)
		}
		for i := 0; i < nElements; i++ {
			phpsyOut[n].PsyOutElement[i] = new(PsyOutElement)
		}
	}
	return AacEncOK
}

// --- psy_main.cpp:232: FDKaacEnc_psyInitStates ------------------------------

// psyInitStates is the 1:1 port of FDKaacEnc_psyInitStates (psy_main.cpp:232):
// clear the per-channel PCM input buffer and run the block-switch init. The Go
// PsyInputBuffer is a fixed array, so the FDKmemclear is a slice clear. Returns
// AAC_ENC_OK.
func psyInitStates(psyStatic *PsyStatic, audioObjectType AudioObjectType) EncoderError {
	// init input buffer: FDKmemclear(psyInputBuffer, MAX_INPUT_BUFFER_SIZE*...)
	for i := range psyStatic.PsyInputBuffer {
		psyStatic.PsyInputBuffer[i] = 0
	}

	var ld int
	if isLowDelay(audioObjectType) {
		ld = 1
	}
	InitBlockSwitching(&psyStatic.BlockSwitchingControl, ld)

	return AacEncOK
}

// --- psy_main.cpp:245: FDKaacEnc_psyInit ------------------------------------

// psyInit is the 1:1 port of FDKaacEnc_psyInit (psy_main.cpp:245): wire each
// element's per-channel psyStatic[ch] to the shared pStaticChannels[] pool,
// (re)initialising the non-LFE channels' state (psyInitStates + mdctInit) and
// setting the per-channel isLFE flag, then wire the per-AU psyOutElement[i]->
// psyOutChannel[ch] to the shared pPsyOutChannels[] pool. The (nMaxChannels>2 &&
// cm->nChannels==2) downmix-reset bookkeeping (chInc/resetChannels) is ported
// 1:1. Returns AAC_ENC_OK.
func psyInit(hPsy *PsyInternal, phpsyOut []*PsyOut, nSubFrames, nMaxChannels int,
	audioObjectType AudioObjectType, cm *ChannelMapping) EncoderError {
	chInc := 0
	resetChannels := 3

	if nMaxChannels > 2 && cm.NChannels == 2 {
		chInc = 1
		psyInitStates(hPsy.PStaticChannels[0], audioObjectType)
	}

	if nMaxChannels == 2 {
		resetChannels = 0
	}

	for i := 0; i < cm.NElements; i++ {
		for ch := 0; ch < cm.ElInfo[i].NChannelsInEl; ch++ {
			hPsy.PsyElement[i].PsyStatic[ch] = hPsy.PStaticChannels[chInc]
			if cm.ElInfo[i].ElType != IDLFE {
				if chInc >= resetChannels {
					psyInitStates(hPsy.PsyElement[i].PsyStatic[ch], audioObjectType)
				}
				mdctInit(&hPsy.PsyElement[i].PsyStatic[ch].MdctPers, nil, 0)
				hPsy.PsyElement[i].PsyStatic[ch].IsLFE = 0
			} else {
				hPsy.PsyElement[i].PsyStatic[ch].IsLFE = 1
			}
			chInc++
		}
	}

	for n := 0; n < nSubFrames; n++ {
		chInc = 0
		for i := 0; i < cm.NElements; i++ {
			for ch := 0; ch < cm.ElInfo[i].NChannelsInEl; ch++ {
				phpsyOut[n].PsyOutElement[i].PsyOutChannel[ch] = phpsyOut[n].PPsyOutChannels[chInc]
				chInc++
			}
		}
	}

	return AacEncOK
}
