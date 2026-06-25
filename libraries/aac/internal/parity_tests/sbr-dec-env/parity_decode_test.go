// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrdecenv

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/require"
)

// This file pins the Go port of env_dec.cpp (decodeSbrData / decodeEnvelope /
// decodeNoiseFloorlevels / requantizeEnvelopeData / deltaToLinearPcmEnvelopeDecoding
// / sbr_envelope_unmapping) against the genuine vendored decodeSbrData. Both
// sides parse the SAME bytes (written by the genuine FDK bit writer) with the
// already-parity-verified sbrGetChannelElement, then run decodeSbrData over a
// fresh zero previous-frame whose stopPos matches the FIX-FIX start border (so
// the normal, non-concealment delta-decode path runs). The dequantized
// pseudo-float energy/noise arrays + the prev-frame delta carry are then asserted
// EXACT-integer equal (the whole SBR subsystem is fixed-point — no tolerance).

// tok is a (value, nBits) bitstream token. The bridge writes these via the
// genuine FDK bit writer; the Go port reads back the identical bytes.
type tok struct {
	v uint32
	n uint8
}

func flatten(toks []tok) (vals []uint32, nbits []uint8, totalBits int) {
	for _, t := range toks {
		vals = append(vals, t.v)
		nbits = append(nbits, t.n)
		totalBits += int(t.n)
	}
	return
}

const payloadBufBytes = 256 // power of two, larger than any payload here

// pad appends filler so the Huffman/PCM envelope+noise+harmonics+extended-data
// tail always has bits to read; ends on a zero run so extended_data reads 0. The
// concrete filler values are irrelevant to parity — both decoders consume them
// identically.
func pad(toks []tok) []tok {
	for i := 0; i < 24; i++ {
		toks = append(toks, tok{0x5A, 8})
	}
	for i := 0; i < 8; i++ {
		toks = append(toks, tok{0x00, 8})
	}
	return toks
}

// sceFixFix builds a frameClass-0 (FIXFIX) single-channel-element grid.
func sceFixFix(eSel, staticFreqRes uint32) []tok {
	return pad([]tok{
		{0, 1},             // bs_data_extra = 0
		{0, 2},             // frameClass = 0 (FIXFIX)
		{eSel, 2},          // E -> nEnv = 1<<eSel
		{staticFreqRes, 1}, // staticFreqRes
	})
}

// sceVarFix builds a frameClass-2 (VARFIX) grid.
func sceVarFix(a, n uint32, rDeltas []uint32, pointer uint32, pointerBits uint8, freqRes []uint32) []tok {
	toks := []tok{
		{0, 1}, // bs_data_extra = 0
		{2, 2}, // frameClass = 2 (VARFIX)
		{a, 2}, // A
		{n, 2}, // N (nEnv = n+1)
	}
	for _, r := range rDeltas {
		toks = append(toks, tok{r, 2})
	}
	toks = append(toks, tok{pointer, pointerBits})
	for _, f := range freqRes {
		toks = append(toks, tok{f, 1})
	}
	return pad(toks)
}

// hdrConfig is the SBR header configuration the parser derives band counts from.
type hdrConfig struct {
	fs                                                                uint
	startFreq, stopFreq, freqScale, alterScale, noiseBands, xoverBand uint8
	analysisBands, ampResolution, numberTimeSlots, timeStep           uint8
}

// canonical44k is the canonical HE-AAC v1 header used across the grid cases.
var canonical44k = hdrConfig{44100, 5, 0, 2, 0, 2, 0, 32, 1, 16, 2}

// requireDecodeEqual asserts every dequantized field is EXACT between the genuine
// C decodeSbrData and the Go port.
func requireDecodeEqual(t *testing.T, ci int, label string, c decodeC, g sbr.DecodeResult) {
	t.Helper()
	require.Equal(t, c.nScaleFactors, g.NScaleFactors, "case %d %s nScaleFactors", ci, label)
	require.Equal(t, c.coupling, g.Coupling, "case %d %s coupling", ci, label)
	require.Equal(t, c.iEnvelope, g.IEnvelope, "case %d %s iEnvelope", ci, label)
	require.Equal(t, c.sbrNoiseFloorLevel, g.SbrNoiseFloorLevel, "case %d %s sbrNoiseFloorLevel", ci, label)
	require.Equal(t, c.sfbNrgPrev, g.SfbNrgPrev, "case %d %s sfbNrgPrev", ci, label)
	require.Equal(t, c.prevNoiseLevel, g.PrevNoiseLevel, "case %d %s prevNoiseLevel", ci, label)
	require.Equal(t, c.frameError, g.FrameError, "case %d %s frameError", ci, label)
}

func runDecodeCase(t *testing.T, ci int, h hdrConfig, toks []tok, nCh int) (decodeC, sbr.DecodeResult, decodeC, sbr.DecodeResult) {
	t.Helper()
	vals, nbits, totalBits := flatten(toks)
	buf, _ := cBuildPayload(vals, nbits, payloadBufBytes)
	validBits := uint32(totalBits)

	cOk, cL, cR := cDecodeChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, nCh, 0, append([]byte(nil), buf...), validBits, 0)
	gL, gR := sbr.RunDecodeSbrData(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, nCh, 0, append([]byte(nil), buf...), payloadBufBytes, validBits, 0)

	require.Equal(t, cOk, gL.Ok, "case %d ok", ci)
	return cL, gL, cR, gR
}

// TestDecodeSbrDataFixFix drives decodeSbrData over the FIXFIX (frameClass 0)
// grid across the env-count selector E (1/2/4/8 envelopes) and both freq
// resolutions, asserting the dequantized iEnvelope / noise / prev-carry are exact
// against the genuine reference. Exercises deltaToLinearPcmEnvelopeDecoding (both
// delta-time and delta-freq domains chosen by the parsed dtdf flags),
// requantizeEnvelopeData, decodeNoiseFloorlevels, mapLowResEnergyVal.
func TestDecodeSbrDataFixFix(t *testing.T) {
	h := canonical44k
	for ci, c := range []struct {
		eSel, staticFreqRes uint32
	}{
		{0, 1}, {0, 0}, // 1 env
		{1, 1}, {1, 0}, // 2 env
		{2, 1}, {2, 0}, // 4 env
		{3, 1}, {3, 0}, // 8 env
	} {
		cL, gL, _, _ := runDecodeCase(t, ci, h, sceFixFix(c.eSel, c.staticFreqRes), 1)
		requireDecodeEqual(t, ci, "L", cL, gL)
	}
}

// TestDecodeSbrDataAmpRes0 drives the ampResolution==0 path (3 dB steps, the
// wider envelope start value and the alternate codebooks), exercising the
// requantizeEnvelopeData ampShift branch and the sbr_max_energy<<1 range check.
func TestDecodeSbrDataAmpRes0(t *testing.T) {
	h := canonical44k
	h.ampResolution = 0
	for ci, eSel := range []uint32{0, 1, 2, 3} {
		cL, gL, _, _ := runDecodeCase(t, ci, h, sceFixFix(eSel, 1), 1)
		requireDecodeEqual(t, ci, "L", cL, gL)
	}
}

// TestDecodeSbrDataVarFix drives decodeSbrData over the VARFIX (frameClass 2)
// grid: multi-envelope frames whose borders/freqRes vary, exercising the
// multi-envelope delta chain in deltaToLinearPcmEnvelopeDecoding and the
// two-noise-envelope path in decodeNoiseFloorlevels.
func TestDecodeSbrDataVarFix(t *testing.T) {
	h := canonical44k
	cases := []struct {
		a, n        uint32
		rDeltas     []uint32
		pointer     uint32
		pointerBits uint8
		freqRes     []uint32
	}{
		{0, 0, nil, 0, 1, []uint32{1}},
		{0, 0, nil, 0, 1, []uint32{0}},
		{0, 1, []uint32{0}, 0, 2, []uint32{1, 1}},
		{0, 1, []uint32{0}, 0, 2, []uint32{0, 1}},
		{0, 2, []uint32{0, 0}, 0, 2, []uint32{1, 0, 1}},
		{0, 3, []uint32{0, 0, 0}, 0, 3, []uint32{1, 1, 1, 1}},
	}
	for ci, c := range cases {
		cL, gL, _, _ := runDecodeCase(t, ci, h, sceVarFix(c.a, c.n, c.rDeltas, c.pointer, c.pointerBits, c.freqRes), 1)
		requireDecodeEqual(t, ci, "L", cL, gL)
	}
}

// TestDecodeSbrDataStereo drives a channel-pair element (nCh==2) with and without
// coupling. The coupling==1 case exercises sbr_envelope_unmapping (the
// FDK_add_MantExp / FDK_divide_MantExp / fMult Q-format kernels) on both the
// envelope energies and the noise floor levels; both channels' dequantized data
// are asserted exact.
func TestDecodeSbrDataStereo(t *testing.T) {
	h := canonical44k
	for ci, c := range []struct {
		coupling uint32
	}{
		{0}, // no coupling: separate L/R envelopes
		{1}, // coupling: sbr_envelope_unmapping converts level/balance -> L/R
	} {
		toks := []tok{
			{0, 1},          // bs_data_extra = 0
			{c.coupling, 1}, // bs_coupling (nCh==2)
			{0, 2},          // Left frameClass = 0
			{1, 2},          // E -> 2 env
			{1, 1},          // staticFreqRes
		}
		if c.coupling == 0 {
			toks = append(toks,
				tok{0, 2}, // Right frameClass = 0
				tok{1, 2}, // E -> 2 env
				tok{1, 1}, // staticFreqRes
			)
		}
		toks = pad(toks)
		cL, gL, cR, gR := runDecodeCase(t, ci, h, toks, 2)
		requireDecodeEqual(t, ci, "L", cL, gL)
		requireDecodeEqual(t, ci, "R", cR, gR)
	}
}
