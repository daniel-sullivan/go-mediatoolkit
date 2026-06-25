// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Package heaac wires the pure-Go AAC-LC core decoder (internal/nativeaac) to
// the SBR subsystem (internal/nativeaac/sbr) to form a complete HE-AAC v1
// decoder: AAC-LC 1024-sample core output -> SBR upsample -> 2048 interleaved
// int16 PCM at the doubled sample rate. It mirrors the SBR branch of
// aacDecoder_DecodeFrame (aacdecoder_lib.cpp:1496-1682).
//
// This package imports BOTH nativeaac and nativeaac/sbr; the import-cycle
// constraint (sbr already depends on nativeaac for fixed-point math) is why the
// glue lives here rather than in nativeaac. HE-AAC v1 ONLY (mono ID_SCE / stereo
// ID_CPE); PS / DRC / MPS excluded. FDK-AAC-derived; see libfdk/COPYING. Fenced
// behind the aacfdk build tag.
package heaac

import (
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// AOT_SBR (5) — explicit HE-AAC core-codec AOT for the legacy SBR path.
// AOT_PS (29) — HE-AAC v2 (SBR + parametric stereo) core-codec AOT.
const (
	aotSBR = 5
	aotPS  = 29
)

// element IDs in the SBR element-id space (== nativeaac idSCE / idCPE).
const (
	elementSCE = 0
	elementCPE = 1
)

// Decoder is a complete HE-AAC decoder: an AAC-LC core plus the SBR upsampler,
// optionally with HE-AAC v2 parametric stereo (a mono core upmixed to stereo).
type Decoder struct {
	core    *nativeaac.Decoder
	sbrInst *sbr.SbrDecoderInstance

	coreFrameLen  int
	sbrFrameLen   int
	channels      int // core channel count (1 for PS)
	outChannels   int // output channel count (2 for PS)
	ps            bool
	sampleRateIn  int
	sampleRateOut int

	coreInput []int32 // planar core output (channels*coreFrameLen), SBR input
	sbrOut    []int32 // interleaved SBR output (outChannels*sbrFrameLen)
}

// NewDecoder builds an HE-AAC v1 decoder. coreFrameLen is the AAC-LC core frame
// length (1024); sampleRateIn is the core sampling rate; channels is 1 or 2;
// sampleRateOut is the SBR output rate (0 => implicit dual-rate, 2*sampleRateIn).
func NewDecoder(coreFrameLen, sampleRateIn, channels, sampleRateOut int) (*Decoder, error) {
	core, err := nativeaac.NewDecoder(coreFrameLen, uint32(sampleRateIn), channels)
	if err != nil {
		return nil, err
	}
	if sampleRateOut == 0 {
		sampleRateOut = sampleRateIn << 1
	}
	elementID := elementSCE
	if channels == 2 {
		elementID = elementCPE
	}
	inst := sbr.NewDecoderInstance(sampleRateIn, sampleRateOut, coreFrameLen, aotSBR, elementID, 0)
	if inst == nil {
		return nil, errUnsupportedConfig
	}
	return &Decoder{
		core:          core,
		sbrInst:       inst,
		coreFrameLen:  coreFrameLen,
		sbrFrameLen:   coreFrameLen * 2,
		channels:      channels,
		outChannels:   channels,
		sampleRateIn:  sampleRateIn,
		sampleRateOut: sampleRateOut,
		coreInput:     make([]int32, channels*coreFrameLen),
		sbrOut:        make([]int32, channels*coreFrameLen*2),
	}, nil
}

// NewPSDecoder builds an HE-AAC v2 decoder: a MONO AAC-LC core (coreFrameLen,
// sampleRateIn) plus SBR plus parametric stereo, producing a STEREO output at the
// doubled rate. The core stream is a single mono SCE carrying ps_data in its SBR
// extension; the SBR instance is built with AOT_PS so the PS machinery is
// allocated and the second (right) channel exists. sampleRateOut == 0 requests
// implicit dual-rate (2*sampleRateIn).
func NewPSDecoder(coreFrameLen, sampleRateIn, sampleRateOut int) (*Decoder, error) {
	core, err := nativeaac.NewDecoder(coreFrameLen, uint32(sampleRateIn), 1)
	if err != nil {
		return nil, err
	}
	if sampleRateOut == 0 {
		sampleRateOut = sampleRateIn << 1
	}
	// AOT_PS, single ID_SCE element: InitElement promotes to 2 channels and
	// creates the PS decoder.
	inst := sbr.NewDecoderInstance(sampleRateIn, sampleRateOut, coreFrameLen, aotPS, elementSCE, 0)
	if inst == nil {
		return nil, errUnsupportedConfig
	}
	return &Decoder{
		core:          core,
		sbrInst:       inst,
		coreFrameLen:  coreFrameLen,
		sbrFrameLen:   coreFrameLen * 2,
		channels:      1,
		outChannels:   2,
		ps:            true,
		sampleRateIn:  sampleRateIn,
		sampleRateOut: sampleRateOut,
		coreInput:     make([]int32, coreFrameLen),
		// Stereo output: 2 channels * 2*coreFrameLen samples.
		sbrOut: make([]int32, 2*coreFrameLen*2),
	}, nil
}

// SampleRate returns the SBR output sample rate (doubled).
func (d *Decoder) SampleRate() int { return d.sampleRateOut }

// FrameSamples returns the samples-per-channel one access unit decodes to (2048).
func (d *Decoder) FrameSamples() int { return d.sbrFrameLen }

// Channels returns the output channel count (2 for a PS stereo decoder).
func (d *Decoder) Channels() int { return d.outChannels }

// Reset clears the core overlap-add state for a new stream.
func (d *Decoder) Reset() { d.core.Reset() }

// DecodeAccessUnitInt32 is like DecodeAccessUnit but returns the interleaved
// int32 SBR output (pre-narrowing) plus the planar int32 core input and the SBR
// payload location, for the sbr-dec int32 parity oracle to compare each stage.
func (d *Decoder) DecodeAccessUnitInt32(pkt []byte) (sbrOut, coreInput []int32,
	startBit, countBits, crcFlag, prevElement int, err error) {
	for i := range d.coreInput {
		d.coreInput[i] = 0
	}
	loc, e := d.core.DecodeAccessUnitCorePlanar(pkt, d.coreInput)
	if e != nil {
		return nil, nil, 0, 0, 0, 0, e
	}
	if loc.Present {
		remaining := loc.CountBits
		sbr.SbrDecoderParseAU(d.sbrInst, loc.Buf, loc.BufSize, loc.StartBit,
			&remaining, loc.CrcFlag, loc.PrevElement, 0)
	}
	if !d.applySBR() {
		return nil, nil, 0, 0, 0, 0, errUnsupportedConfig
	}
	ci := make([]int32, len(d.coreInput))
	copy(ci, d.coreInput)
	so := make([]int32, len(d.sbrOut))
	copy(so, d.sbrOut)
	return so, ci, int(loc.StartBit), loc.CountBits, loc.CrcFlag, loc.PrevElement, nil
}

// DecodeAccessUnit decodes one HE-AAC v1 access unit (AAC-LC raw_data_block with
// an SBR fill element) into interleaved int16 PCM at the doubled rate, returning
// the samples-per-channel produced (== 2*coreFrameLen). out must hold
// 2*coreFrameLen*channels int16.
func (d *Decoder) DecodeAccessUnit(pkt []byte, out []int16) (int, error) {
	for i := range d.coreInput {
		d.coreInput[i] = 0
	}

	// 1. Decode the AAC-LC core to a planar int32 buffer and locate the SBR
	//    extension payload.
	loc, err := d.core.DecodeAccessUnitCorePlanar(pkt, d.coreInput)
	if err != nil {
		return 0, err
	}

	// 2. Parse the SBR extension payload (sbr_extension_data) if present.
	if loc.Present {
		remaining := loc.CountBits
		sbr.SbrDecoderParseAU(d.sbrInst, loc.Buf, loc.BufSize, loc.StartBit,
			&remaining, loc.CrcFlag, loc.PrevElement, 0)
	}

	// 3. Apply SBR (+PS when enabled): planar core int32 -> interleaved output.
	if !d.applySBR() {
		return 0, errUnsupportedConfig
	}

	// 4. Narrow the interleaved SBR output to int16 (no headroom shift).
	n := d.sbrFrameLen * d.outChannels
	for i := 0; i < n; i++ {
		out[i] = nativeaac.SbrPcmToInt16(d.sbrOut[i])
	}
	return d.sbrFrameLen, nil
}

// applySBR runs the SBR (and, for a PS decoder, parametric-stereo) apply pass over
// the current core input, writing the interleaved output into d.sbrOut. For PS it
// requests psPossible (a mono SCE upmixed to stereo); the right channel is a copy
// of the left when no ps_data is present in the frame.
func (d *Decoder) applySBR() bool {
	numCh := d.channels
	rate := d.sampleRateOut
	if d.ps {
		psDecoded := 1 // request PS (psPossible) for the mono SCE.
		return sbr.SbrDecoderApplyPS(d.sbrInst, d.coreInput, d.sbrOut, &numCh, &rate,
			true, nativeaac.AacOutDataHeadroom, &psDecoded)
	}
	return sbr.SbrDecoderApplyExt(d.sbrInst, d.coreInput, d.sbrOut, &numCh, &rate,
		true, nativeaac.AacOutDataHeadroom)
}
