// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The QC/PSY allocation+init tier (QCOutNew/QCOutInit/PsyNew/PsyOutNew/
// psyInitStates/psyInit) is pure pointer wiring + already-verified compute
// (InitBlockSwitching is bit-exact in the enc-block-switch parity slice;
// mdctInit in the mdct slice; the ChannelMapping it walks is bit-exact in the
// channel-map slice). Its only novel content is the deterministic pointer
// ALIASING: which shared-pool channel each element/channel resolves to, in
// element/channel walk order. These in-package tests pin that aliasing exactly
// as FDKaacEnc_QCOutInit / FDKaacEnc_psyInit produce it (chInc walks the pool in
// element-then-channel order), and the psyInitStates/psyInit isLFE + block-switch
// init invariants. The genuine populated-state-vs-fdk parity is exercised
// end-to-end by the downstream encoder orchestration slice, which compiles the
// full qc_main.cpp / psy_main.cpp chain.

// initTierModes covers a mono, a stereo and the 5.1 (LFE) layout — enough to
// exercise the element/channel walk and the LFE branch.
var initTierModes = []ChannelMode{
	ChannelMode1, ChannelMode2, ChannelMode1_2, ChannelMode1_2_2_1,
}

func TestQCOutInitAliasing(t *testing.T) {
	for _, mode := range initTierModes {
		var cm ChannelMapping
		require.Equal(t, AacEncOK, InitChannelMapping(mode, ChOrderMPEG, &cm))

		nSubFrames := 1
		phQC := make([]*QcOut, nSubFrames)
		require.Equal(t, AacEncOK, QCOutNew(phQC, cm.NElements, cm.NChannels, nSubFrames))
		require.Equal(t, AacEncOK, QCOutInit(phQC, nSubFrames, &cm))

		// Every element's qcOutChannel[ch] must alias the shared pool in
		// element-then-channel order; the walk count must equal nChannels.
		chInc := 0
		for i := 0; i < cm.NElements; i++ {
			for ch := 0; ch < cm.ElInfo[i].NChannelsInEl; ch++ {
				assert.Same(t, phQC[0].PQcOutChannels[chInc], phQC[0].QcElement[i].QcOutChannel[ch],
					"mode=%d el=%d ch=%d aliases pool[%d]", mode, i, ch, chInc)
				chInc++
			}
		}
		assert.Equal(t, cm.NChannels, chInc, "mode=%d total wired channels", mode)
	}
}

func TestPsyInitAliasingAndLFE(t *testing.T) {
	for _, mode := range initTierModes {
		var cm ChannelMapping
		require.Equal(t, AacEncOK, InitChannelMapping(mode, ChOrderMPEG, &cm))

		nSubFrames := 1
		hPsy, err := PsyNew(cm.NElements, cm.NChannels)
		require.Equal(t, AacEncOK, err)
		phpsyOut := make([]*PsyOut, nSubFrames)
		require.Equal(t, AacEncOK, PsyOutNew(phpsyOut, cm.NElements, cm.NChannels, nSubFrames))

		// nMaxChannels == cm.NChannels here (no downmix-reset path).
		require.Equal(t, AacEncOK, psyInit(hPsy, phpsyOut, nSubFrames, cm.NChannels, AOTAACLC, &cm))

		// psyStatic[ch] aliases pStaticChannels in element-then-channel order;
		// isLFE is set iff the element is an LFE.
		chInc := 0
		for i := 0; i < cm.NElements; i++ {
			isLFE := 0
			if cm.ElInfo[i].ElType == IDLFE {
				isLFE = 1
			}
			for ch := 0; ch < cm.ElInfo[i].NChannelsInEl; ch++ {
				assert.Same(t, hPsy.PStaticChannels[chInc], hPsy.PsyElement[i].PsyStatic[ch],
					"mode=%d el=%d ch=%d aliases static pool[%d]", mode, i, ch, chInc)
				assert.Equal(t, isLFE, hPsy.PsyElement[i].PsyStatic[ch].IsLFE,
					"mode=%d el=%d ch=%d isLFE", mode, i, ch)
				chInc++
			}
		}
		assert.Equal(t, cm.NChannels, chInc, "mode=%d total wired channels", mode)

		// psyOut channel pool aliasing mirrors the same walk.
		chInc = 0
		for i := 0; i < cm.NElements; i++ {
			for ch := 0; ch < cm.ElInfo[i].NChannelsInEl; ch++ {
				assert.Same(t, phpsyOut[0].PPsyOutChannels[chInc],
					phpsyOut[0].PsyOutElement[i].PsyOutChannel[ch],
					"mode=%d el=%d ch=%d psyOut aliases pool[%d]", mode, i, ch, chInc)
				chInc++
			}
		}
	}
}

func TestPsyInitStatesResetsBlockSwitch(t *testing.T) {
	// psyInitStates clears the PCM input buffer and runs InitBlockSwitching. The
	// resulting state must equal a freshly InitBlockSwitching'd control (the
	// block-switch start state itself is bit-exact-verified by the enc-block-switch
	// parity slice); lastWindowSequence is LONG_WINDOW for the AAC-LC start.
	ps := new(PsyStatic)
	for i := range ps.PsyInputBuffer {
		ps.PsyInputBuffer[i] = 123 // dirty
	}
	require.Equal(t, AacEncOK, psyInitStates(ps, AOTAACLC))

	for i := range ps.PsyInputBuffer {
		require.Zero(t, ps.PsyInputBuffer[i], "input buffer[%d] cleared", i)
	}
	var want BlockSwitchingControl
	InitBlockSwitching(&want, 0) // AAC-LC is not low-delay
	assert.Equal(t, want, ps.BlockSwitchingControl)
	assert.Equal(t, LongWindow, ps.BlockSwitchingControl.LastWindowSequence)
}
