// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// Exported stateful PS-downmix driver for the ps-enc-downmix cgo parity oracle
// (internal/parity_tests/ps-enc-downmix). It mirrors EXACTLY what the genuine fdk
// bridge does: create a fresh PS instance + two 64-band analysis QMF banks + a
// half-band (32) synthesis QMF bank over cleared states, init PS, then run
// FDKsbrEnc_PSEnc_ParametricStereoProcessing (PSEncParametricStereoProcessing
// here) across multiple frames feeding the same planar stereo int16 input, and
// dump the downmixed mono QMF core (real/imag, per slot, noQmfBands wide) plus
// the per-frame downmix qmfScale. Adds no logic — same call sequence the SBR
// encoder's EnvEncodeFrame PS branch uses (enc_sbr_encoder_drive.go:264-283).

// RunPSDownmixDriver drives the native PS downmix across nFrames frames.
//
//   - pcmInterleaved: interleaved STEREO int16 (L,R,L,R,...), at least
//     nFrames*noQmfSlots*noQmfBands*2 samples.
//   - noQmfSlots / noQmfBands: the core SBR QMF grid (32 / 64 on the dual-rate
//     HE-AAC v2 GA path).
//   - nStereoBands / maxEnvelopes / iidQuantErrorThreshold: the per-bitrate PS
//     tuning (psTuningTable) the heaac glue passes to PSEncInit.
//
// Returns, per frame, the downmixed real/imag mono QMF flattened as
// noQmfSlots*noQmfBands int32, the half-rate downsampled MONO time signal
// (noQmfSlots*(noQmfBands>>1) int16 — the actual AAC-core input), plus the
// frame's downmix qmfScale.
func RunPSDownmixDriver(pcmInterleaved []int16, nFrames, noQmfSlots, noQmfBands,
	nStereoBands, maxEnvelopes int, iidQuantErrorThreshold int32) (
	mixReal, mixImag [][]int32, down [][]int16, qmfScales []int) {

	// Two 64-band analysis QMF banks over cleared 10*noQmfBands states, exactly as
	// the PS path allocates them (enc_sbr_encoder_frame.go:219-232).
	var hQmf [maxPsChannels]*FilterBank
	for ch := 0; ch < maxPsChannels; ch++ {
		hQmf[ch] = new(FilterBank)
		states := make([]int32, 10*noQmfBands)
		InitAnalysisFilterBank(hQmf[ch], states, noQmfSlots, noQmfBands, noQmfBands, noQmfBands, 0)
	}

	// Half-band (noQmfBands>>1) synthesis QMF bank for the downsampled mono core
	// (enc_sbr_encoder_drive.go:166-176).
	halfBands := noQmfBands >> 1
	sbrSynthQmf := new(FilterBank)
	synMem := make([]int32, (2*qmfNoPoly-1)*halfBands)
	InitSynthesisFilterBank(sbrSynthQmf, synMem, noQmfSlots, halfBands, halfBands, halfBands, 0)

	// Create + init the PS instance (enc_sbr_encoder_drive.go:178-185).
	ps := PSEncCreate()
	cfg := &psEncConfig{
		nStereoBands:           nStereoBands,
		maxEnvelopes:           maxEnvelopes,
		iidQuantErrorThreshold: iidQuantErrorThreshold,
		frameSize:              noQmfSlots,
	}
	if PSEncInit(ps, cfg, noQmfSlots, noQmfBands) != psencOK {
		return nil, nil, nil, nil
	}

	perFrame := noQmfSlots * noQmfBands // mono samples per channel per frame.

	for f := 0; f < nFrames; f++ {
		// De-interleave this frame's stereo PCM into two planar channel buffers,
		// each perFrame long (the PS path feeds samples[ch] indexed [i*nch+j]).
		var pSamples [maxPsChannels][]int16
		for ch := 0; ch < maxPsChannels; ch++ {
			pSamples[ch] = make([]int16, perFrame)
		}
		base := f * perFrame * 2
		for i := 0; i < perFrame; i++ {
			pSamples[0][i] = pcmInterleaved[base+i*2+0]
			pSamples[1][i] = pcmInterleaved[base+i*2+1]
		}

		// Downmixed mono QMF output buffers (noQmfSlots slots of 64 int32).
		dmxReal := make([][]int32, noQmfSlots)
		dmxImag := make([][]int32, noQmfSlots)
		for s := 0; s < noQmfSlots; s++ {
			dmxReal[s] = make([]int32, 64)
			dmxImag[s] = make([]int32, 64)
		}
		// Downsampled mono time signal: noQmfSlots*halfBands int16 (one half-rate
		// frame) — the actual AAC-core input. The synthesis slot writes halfBands
		// samples per slot.
		downBuf := make([]int16, noQmfSlots*halfBands)

		var qmfScale int
		// sendHeader == 0 here: the downmix numerics do not depend on the PS header
		// flag (only ps_data() bitstream emission does), and this oracle isolates
		// the downmix, not the bitstream.
		PSEncParametricStereoProcessing(ps, pSamples, hQmf, dmxReal, dmxImag, downBuf,
			sbrSynthQmf, &qmfScale, 0)

		flatR := make([]int32, noQmfSlots*noQmfBands)
		flatI := make([]int32, noQmfSlots*noQmfBands)
		for s := 0; s < noQmfSlots; s++ {
			copy(flatR[s*noQmfBands:s*noQmfBands+noQmfBands], dmxReal[s][:noQmfBands])
			copy(flatI[s*noQmfBands:s*noQmfBands+noQmfBands], dmxImag[s][:noQmfBands])
		}
		mixReal = append(mixReal, flatR)
		mixImag = append(mixImag, flatI)
		down = append(down, downBuf)
		qmfScales = append(qmfScales, qmfScale)
	}
	return mixReal, mixImag, down, qmfScales
}
