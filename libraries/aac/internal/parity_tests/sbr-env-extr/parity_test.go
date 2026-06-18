// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrenvextr

import (
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/require"
)

// tok is a (value, nBits) bitstream token. The bridge writes these via the
// genuine FDK bit writer; the Go port reads back the identical bytes. Building
// the payload through the real writer guarantees both decoders see exactly the
// same bits — the comparison is then C-parsed-struct vs Go-parsed-struct.
type tok struct {
	v uint32
	n uint8
}

// flatten splits a token list into the parallel (values,nBits) arrays the bridge
// expects and counts the total bit length.
func flatten(toks []tok) (vals []uint32, nbits []uint8, totalBits int) {
	for _, t := range toks {
		vals = append(vals, t.v)
		nbits = append(nbits, t.n)
		totalBits += int(t.n)
	}
	return
}

const payloadBufBytes = 256 // power of two, comfortably larger than any payload

// pad appends a run of alternating-ish filler tokens so the Huffman/PCM
// envelope+noise+harmonics+extended-data tail always has bits to read. The
// concrete values are irrelevant to parity — both decoders consume them
// identically; they only need to be present and decode without running past the
// buffer. (extended_data is the last bit consumed; we make it 0 by ending the
// filler on a 0.)
func pad(toks []tok) []tok {
	for i := 0; i < 24; i++ {
		toks = append(toks, tok{0x5A, 8})
	}
	// Ensure extended_data flag reads 0 by following with a zero byte run.
	for i := 0; i < 8; i++ {
		toks = append(toks, tok{0x00, 8})
	}
	return toks
}

// requireFrameDataEqual asserts every populated SBR_FRAME_DATA field is exactly
// equal between the genuine C parse and the Go port.
func requireFrameDataEqual(t *testing.T, ci int, label string, c frameDataC, g sbr.FrameDataResult) {
	t.Helper()
	require.Equal(t, c.nScaleFactors, g.NScaleFactors, "case %d %s nScaleFactors", ci, label)
	require.Equal(t, c.frameClass, g.FrameClass, "case %d %s frameClass", ci, label)
	require.Equal(t, c.nEnvelopes, g.NEnvelopes, "case %d %s nEnvelopes", ci, label)
	require.Equal(t, c.borders, g.Borders, "case %d %s borders", ci, label)
	require.Equal(t, c.freqRes, g.FreqRes, "case %d %s freqRes", ci, label)
	require.Equal(t, c.tranEnv, g.TranEnv, "case %d %s tranEnv", ci, label)
	require.Equal(t, c.nNoiseEnvelopes, g.NNoiseEnv, "case %d %s nNoiseEnvelopes", ci, label)
	require.Equal(t, c.bordersNoise, g.BordersNoise, "case %d %s bordersNoise", ci, label)
	require.Equal(t, c.noisePosition, g.NoisePosition, "case %d %s noisePosition", ci, label)
	require.Equal(t, c.varLength, g.VarLength, "case %d %s varLength", ci, label)
	require.Equal(t, c.domainVec, g.DomainVec, "case %d %s domainVec", ci, label)
	require.Equal(t, c.domainVecNoise, g.DomainVecNoise, "case %d %s domainVecNoise", ci, label)
	require.Equal(t, c.sbrInvfMode, g.SbrInvfMode, "case %d %s sbrInvfMode", ci, label)
	require.Equal(t, c.coupling, g.Coupling, "case %d %s coupling", ci, label)
	require.Equal(t, c.ampResolutionCurrFrame, g.AmpResolutionCurrFrame, "case %d %s ampResolutionCurrFrame", ci, label)
	require.Equal(t, c.addHarmonics, g.AddHarmonics, "case %d %s addHarmonics", ci, label)
	require.Equal(t, c.iEnvelope, g.IEnvelope, "case %d %s iEnvelope", ci, label)
	require.Equal(t, c.sbrNoiseFloorLevel, g.SbrNoiseFloorLevel, "case %d %s sbrNoiseFloorLevel", ci, label)
}

// hdrConfig is the SBR header configuration the parser derives band counts from.
type hdrConfig struct {
	fs                                                                uint
	startFreq, stopFreq, freqScale, alterScale, noiseBands, xoverBand uint8
	analysisBands, ampResolution, numberTimeSlots, timeStep           uint8
}

// canonical44k is the canonical HE-AAC v1 header used across the grid cases.
var canonical44k = hdrConfig{44100, 5, 0, 2, 0, 2, 0, 32, 1, 16, 2}

// sceFixFix builds a frameClass-0 (FIXFIX) single-channel-element grid: E (env
// count selector), staticFreqRes, then the dtdf/invf/envelope tail filler.
func sceFixFix(eSel, staticFreqRes uint32) []tok {
	toks := []tok{
		{0, 1}, // bs_data_extra = 0
		// (nCh==1, not SCAL: no coupling bit)
		{0, 2},             // frameClass = 0 (FIXFIX)
		{eSel, 2},          // E -> nEnv = 1<<eSel
		{staticFreqRes, 1}, // staticFreqRes
	}
	return pad(toks)
}

// sceVarFix builds a frameClass-2 (VARFIX) grid: A, N, the N R-deltas, the
// pointer, the (n+1) freqRes bits, then filler. rDeltas/freqRes/pointer are the
// raw 2-/1-bit field values.
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

// sceFixVar builds a frameClass-1 (FIXVAR) grid: A, N, the N R-deltas, the
// pointer, then the (n+1) freqRes bits (written k=n..0, MSB->LSB as the parser
// reads them in that order), then filler.
func sceFixVar(a, n uint32, rDeltas []uint32, pointer uint32, pointerBits uint8, freqResHiToLo []uint32) []tok {
	toks := []tok{
		{0, 1}, // bs_data_extra = 0
		{1, 2}, // frameClass = 1 (FIXVAR)
		{a, 2}, // A
		{n, 2}, // N (nEnv = n+1)
	}
	for _, r := range rDeltas {
		toks = append(toks, tok{r, 2})
	}
	toks = append(toks, tok{pointer, pointerBits})
	for _, f := range freqResHiToLo {
		toks = append(toks, tok{f, 1})
	}
	return pad(toks)
}

// sceVarVar builds a frameClass-3 (VARVAR) grid: AL, AR, NL, NR, then nL L-Rs,
// nR R-Rs, pointer, then nEnv freqRes bits, then filler.
func sceVarVar(aL, aR, nL, nR uint32, lR, rR []uint32, pointer uint32, pointerBits uint8, freqRes []uint32) []tok {
	toks := []tok{
		{0, 1},  // bs_data_extra = 0
		{3, 2},  // frameClass = 3 (VARVAR)
		{aL, 2}, // AL
		{aR, 2}, // AR
		{nL, 2}, // NL
		{nR, 2}, // NR
	}
	for _, r := range lR {
		toks = append(toks, tok{r, 2})
	}
	for _, r := range rR {
		toks = append(toks, tok{r, 2})
	}
	toks = append(toks, tok{pointer, pointerBits})
	for _, f := range freqRes {
		toks = append(toks, tok{f, 1})
	}
	return pad(toks)
}

// TestParseChannelElementFixVar drives the FIXVAR (frameClass 1) and VARVAR
// (frameClass 3) grids. The field values are chosen so the derived borders pass
// checkFrameInfo at 16 timeslots / timeStep 2 (maxPos = 16 + overlap/timeStep,
// overlap 0 here so stopPos must equal 16).
func TestParseChannelElementFixVar(t *testing.T) {
	h := canonical44k
	type grid struct {
		toks []tok
	}
	cases := []grid{
		// FIXVAR n=0: last border = A + 16; with A=0 -> border 16, first border 0.
		// pointerBits = 31 - clb(1) = 1. freqRes is 1 bit (k=0).
		{sceFixVar(0, 0, nil, 0, 1, []uint32{1})},
		// FIXVAR n=1: last=A+16=17 (A=1), one R-delta of value 0 -> step 2 ->
		// borders {0,15,17}? first border is 0, last is 17, mid = 17-2 = 15.
		// pointerBits = 31 - clb(2) = 2.
		{sceFixVar(1, 1, []uint32{0}, 0, 2, []uint32{1, 1})},
		// FIXVAR n=2: last=16 (A=0), two R-deltas. borders walk back from 16.
		{sceFixVar(0, 2, []uint32{0, 0}, 0, 2, []uint32{1, 0, 1})},
		// VARVAR nL=0,nR=0 (1 env): borders {aL, aR}. aL=0, aR=0+16=16.
		// pointerBits = 31 - clb(1) = 1. freqRes 1 bit.
		{sceVarVar(0, 0, 0, 0, nil, nil, 0, 1, []uint32{1})},
		// VARVAR nL=1,nR=0 (2 env): L border step from aL=0, last aR=16.
		// pointerBits = 31 - clb(2) = 2. freqRes 2 bits.
		{sceVarVar(0, 0, 1, 0, []uint32{0}, nil, 0, 2, []uint32{1, 1})},
		// VARVAR nL=1,nR=1 (3 env): pointerBits = 31 - clb(3) = 2. freqRes 3 bits.
		{sceVarVar(0, 0, 1, 1, []uint32{0}, []uint32{0}, 0, 2, []uint32{1, 0, 1})},
	}
	for ci, c := range cases {
		vals, nbits, totalBits := flatten(c.toks)
		buf, _ := cBuildPayload(vals, nbits, payloadBufBytes)
		validBits := uint32(totalBits)

		cOk, cL, _ := cParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 1, 0, append([]byte(nil), buf...), validBits, 0)
		gL, _ := sbr.RunParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 1, 0, append([]byte(nil), buf...), payloadBufBytes, validBits, 0)

		require.Equal(t, cOk, gL.Ok, "case %d ok", ci)
		requireFrameDataEqual(t, ci, "L", cL, gL)
	}
}

// TestParseChannelElementAmpRes0 drives the ampResolution==0 path (start_bits=7,
// EnvLevel*10* Huffman books) so the wider envelope start value and the alternate
// codebooks are exercised across the FIXFIX env counts.
func TestParseChannelElementAmpRes0(t *testing.T) {
	h := canonical44k
	h.ampResolution = 0
	for ci, eSel := range []uint32{0, 1, 2, 3} {
		toks := sceFixFix(eSel, 1)
		vals, nbits, totalBits := flatten(toks)
		buf, _ := cBuildPayload(vals, nbits, payloadBufBytes)
		validBits := uint32(totalBits)

		cOk, cL, _ := cParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 1, 0, append([]byte(nil), buf...), validBits, 0)
		gL, _ := sbr.RunParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 1, 0, append([]byte(nil), buf...), payloadBufBytes, validBits, 0)

		require.Equal(t, cOk, gL.Ok, "case %d ok", ci)
		requireFrameDataEqual(t, ci, "L", cL, gL)
	}
}

// TestParseChannelElementFixFix drives the FIXFIX (frameClass 0) grid across the
// env-count selector E (1/2/4/8 envelopes) and both freq resolutions, asserting
// every parsed SBR_FRAME_DATA field is exact. FIXFIX copies a ROM frame_info so
// the borders are always valid; the envelope/noise/harmonics tail comes from the
// shared filler and is parsed identically by both sides.
func TestParseChannelElementFixFix(t *testing.T) {
	h := canonical44k
	for ci, c := range []struct {
		eSel, staticFreqRes uint32
	}{
		{0, 1}, {0, 0}, // 1 env
		{1, 1}, {1, 0}, // 2 env
		{2, 1}, {2, 0}, // 4 env
		{3, 1}, {3, 0}, // 8 env
	} {
		toks := sceFixFix(c.eSel, c.staticFreqRes)
		vals, nbits, totalBits := flatten(toks)
		buf, _ := cBuildPayload(vals, nbits, payloadBufBytes)
		validBits := uint32(totalBits)

		cOk, cL, _ := cParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 1, 0, append([]byte(nil), buf...), validBits, 0)
		gL, _ := sbr.RunParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 1, 0, append([]byte(nil), buf...), payloadBufBytes, validBits, 0)

		require.Equal(t, cOk, gL.Ok, "case %d ok", ci)
		requireFrameDataEqual(t, ci, "L", cL, gL)
	}
}

// TestParseChannelElementVarFix drives the VARFIX (frameClass 2) grid: chosen A,
// N, R-deltas, pointer and freqRes vectors. The cases are picked so the derived
// borders pass checkFrameInfo at 16 timeslots / timeStep 2.
func TestParseChannelElementVarFix(t *testing.T) {
	h := canonical44k
	cases := []struct {
		a, n        uint32
		rDeltas     []uint32
		pointer     uint32
		pointerBits uint8
		freqRes     []uint32
	}{
		// n=0 (1 env): borders[0]=A, borders[1]=numberTimeSlots; pointerBits =
		// 31 - clb(1) = 1.
		{0, 0, nil, 0, 1, []uint32{1}},
		{2, 0, nil, 0, 1, []uint32{0}},
		// n=1 (2 env): one R-delta. pointerBits = 31 - clb(2) = 2.
		{0, 1, []uint32{0}, 0, 2, []uint32{1, 1}},
		{1, 1, []uint32{1}, 2, 2, []uint32{0, 1}},
		// n=2 (3 env): two R-deltas. pointerBits = 31 - clb(3) = 2.
		{0, 2, []uint32{0, 0}, 0, 2, []uint32{1, 0, 1}},
		{0, 2, []uint32{1, 0}, 3, 2, []uint32{1, 1, 0}},
		// n=3 (4 env): three R-deltas. pointerBits = 31 - clb(4) = 3.
		{0, 3, []uint32{0, 0, 0}, 0, 3, []uint32{1, 1, 1, 1}},
	}
	for ci, c := range cases {
		toks := sceVarFix(c.a, c.n, c.rDeltas, c.pointer, c.pointerBits, c.freqRes)
		vals, nbits, totalBits := flatten(toks)
		buf, _ := cBuildPayload(vals, nbits, payloadBufBytes)
		validBits := uint32(totalBits)

		cOk, cL, _ := cParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 1, 0, append([]byte(nil), buf...), validBits, 0)
		gL, _ := sbr.RunParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 1, 0, append([]byte(nil), buf...), payloadBufBytes, validBits, 0)

		require.Equal(t, cOk, gL.Ok, "case %d ok", ci)
		requireFrameDataEqual(t, ci, "L", cL, gL)
	}
}

// TestParseChannelElementStereo drives a channel-pair element (nCh==2) with and
// without coupling. Both channels' parsed frame-data are asserted exact. The
// left grid is FIXFIX; the right grid (when not coupled) is also FIXFIX.
func TestParseChannelElementStereo(t *testing.T) {
	h := canonical44k
	for ci, c := range []struct {
		coupling uint32
	}{
		{0}, // no coupling: separate L/R grids + envelopes
		{1}, // coupling: R reuses L grid; envelope order differs (balance books)
	} {
		toks := []tok{
			{0, 1},          // bs_data_extra = 0
			{c.coupling, 1}, // bs_coupling (nCh==2)
			// Left FIXFIX grid
			{0, 2}, // frameClass = 0
			{1, 2}, // E -> 2 env
			{1, 1}, // staticFreqRes
		}
		if c.coupling == 0 {
			// Right FIXFIX grid (only read when not coupled)
			toks = append(toks,
				tok{0, 2}, // frameClass = 0
				tok{1, 2}, // E -> 2 env
				tok{1, 1}, // staticFreqRes
			)
		}
		toks = pad(toks)
		vals, nbits, totalBits := flatten(toks)
		buf, _ := cBuildPayload(vals, nbits, payloadBufBytes)
		validBits := uint32(totalBits)

		cOk, cL, cR := cParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 2, 0, append([]byte(nil), buf...), validBits, 0)
		gL, gR := sbr.RunParseChannelElement(h.fs, h.startFreq, h.stopFreq, h.freqScale, h.alterScale, h.noiseBands, h.xoverBand, h.analysisBands, h.ampResolution, h.numberTimeSlots, h.timeStep, 2, 0, append([]byte(nil), buf...), payloadBufBytes, validBits, 0)

		require.Equal(t, cOk, gL.Ok, "case %d ok", ci)
		requireFrameDataEqual(t, ci, "L", cL, gL)
		requireFrameDataEqual(t, ci, "R", cR, gR)
	}
}

// TestParseHeaderData drives sbrGetHeaderData over crafted SBR header element
// bit sequences (ampResolution, startFreq, stopFreq, xover_band, the two extra
// flags + their optional fields), asserting the returned status and every
// bs_data/bs_info field exactly match the genuine parser. The pre-syncState is
// varied so both the RESET (syncState < SBR_HEADER) and OK paths are exercised.
func TestParseHeaderData(t *testing.T) {
	// Header element bit layout (flags==0, configMode==0):
	//   ampResolution[1], startFreq[4], stopFreq[4], xover_band[3], reserved[2],
	//   headerExtra1[1], headerExtra2[1],
	//   if extra1: freqScale[2], alterScale[1], noise_bands[2]
	//   if extra2: limiterBands[2], limiterGains[2], interpolFreq[1], smooth[1]
	build := func(amp, start, stop, xover, e1, e2 uint32, e1f, e2f []tok) []tok {
		toks := []tok{
			{amp, 1}, {start, 4}, {stop, 4}, {xover, 3}, {0, 2}, {e1, 1}, {e2, 1},
		}
		if e1 != 0 {
			toks = append(toks, e1f...)
		}
		if e2 != 0 {
			toks = append(toks, e2f...)
		}
		return toks
	}

	cases := []struct {
		name    string
		preSync int
		toks    []tok
	}{
		{"both-extra", sbrActive(), build(1, 5, 0, 3, 1, 1,
			[]tok{{2, 2}, {1, 1}, {2, 2}}, []tok{{1, 2}, {2, 2}, {1, 1}, {0, 1}})},
		{"no-extra-defaults", sbrActive(), build(0, 7, 5, 2, 0, 0, nil, nil)},
		{"extra1-only", sbrActive(), build(1, 3, 3, 0, 1, 0, []tok{{3, 2}, {0, 1}, {1, 2}}, nil)},
		{"reset-not-init", notInit(), build(1, 5, 0, 3, 1, 1,
			[]tok{{2, 2}, {1, 1}, {2, 2}}, []tok{{1, 2}, {2, 2}, {1, 1}, {0, 1}})},
	}
	for ci, c := range cases {
		vals, nbits, totalBits := flatten(c.toks)
		buf, _ := cBuildPayload(vals, nbits, payloadBufBytes)
		validBits := uint32(totalBits)

		cR := cParseHeaderData(append([]byte(nil), buf...), validBits, c.preSync, 0, 1, 0)
		gR := sbr.RunParseHeaderData(append([]byte(nil), buf...), payloadBufBytes, validBits, c.preSync, 0, 1, 0)

		require.Equal(t, cR.status, gR.Status, "case %d (%s) status", ci, c.name)
		require.Equal(t, cR.fields[0], gR.AmpResolution, "case %d (%s) ampResolution", ci, c.name)
		require.Equal(t, cR.fields[1], gR.XoverBand, "case %d (%s) xoverBand", ci, c.name)
		require.Equal(t, cR.fields[2], gR.StartFreq, "case %d (%s) startFreq", ci, c.name)
		require.Equal(t, cR.fields[3], gR.StopFreq, "case %d (%s) stopFreq", ci, c.name)
		require.Equal(t, cR.fields[4], gR.FreqScale, "case %d (%s) freqScale", ci, c.name)
		require.Equal(t, cR.fields[5], gR.AlterScale, "case %d (%s) alterScale", ci, c.name)
		require.Equal(t, cR.fields[6], gR.NoiseBands, "case %d (%s) noiseBands", ci, c.name)
		require.Equal(t, cR.fields[7], gR.LimiterBands, "case %d (%s) limiterBands", ci, c.name)
		require.Equal(t, cR.fields[8], gR.LimiterGains, "case %d (%s) limiterGains", ci, c.name)
		require.Equal(t, cR.fields[9], gR.InterpolFreq, "case %d (%s) interpolFreq", ci, c.name)
		require.Equal(t, cR.fields[10], gR.SmoothingLength, "case %d (%s) smoothingLength", ci, c.name)
	}
}

// sbrActive / notInit mirror the SBR_SYNC_STATE values (env_extr.h:168-173) used
// as the pre-parse syncState to drive the HEADER_OK vs HEADER_RESET branch.
func sbrActive() int { return 3 } // SBR_ACTIVE
func notInit() int   { return 0 } // SBR_NOT_INITIALIZED
