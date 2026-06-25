// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psdece2e

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/heaac"

	"github.com/stretchr/testify/require"
)

// TestPSDecodeInt32BitExact is the bit-exact HE-AAC v2 parametric-stereo DECODE
// gate. It is the PS analogue of the sbr-dec-e2e int32 gate: it compares the native
// frame-immediate PS int32 STEREO output against the GENUINE fdk SBR+PS decoder
// driven frame-immediate over the IDENTICAL native mono AAC-LC core — NO decoder
// delay, NO int16 narrowing — and asserts EXACT int32 equality (require.Equal, no
// tolerance) for every sample of every frame.
//
// This is the load-bearing proof that the PS INTEGRATION wiring (the
// SbrDecoderInitElement PS promotion + CreatePsDec, the extractExtendedData
// ReadPsData hook, the sbrDecoder_DecodeElement DecodePs + psPossible stride
// handling, and the sbr_dec SBRDEC_PS_DECODED dual-synthesis branch: scale-factor
// computation, PreparePsProcessing, the per-no_col slot loop with
// initSlotBasedRotation at envelope borders + ApplyPsSlot + the dual QMF synthesis)
// is bit-for-bit identical to fdk. fdk-aac SBR + PS is fixed-point, so this is
// EXACT-integer parity. The PS DSP kernels are already proven exact in isolation by
// ps-dec-parse / ps-dec-hybrid / ps-dec-apply; this gate proves the integration
// that drives them produces fdk's exact stereo output.
//
// The companion TestPSDecodeStereoInt16Parity closes the codec-level int16 loop
// against fdk's FULL decoder, which carries an additional one-frame SBR-bitstream
// delay + QMF/PS warmup state-seeding (the same effect documented in
// sbr-dec-codec-e2e) — that is bounded there, not asserted exact. THIS gate, being
// frame-immediate and int32, has no such delay and is EXACT.
//
// This slice compiles its OWN copy of the needed fdk C TUs (fdk_tu_*.cpp here); it
// never imports libraries/aac. It MAY import internal/nativeaac and the heaac glue.
func TestPSDecodeInt32BitExact(t *testing.T) {
	const coreFrameLen = 1024

	cases := []struct {
		name    string
		outRate int
		bitrate int
	}{
		{"out44100", 44100, 32000},
		{"out48000", 48000, 32000},
		{"out32000", 32000, 24000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			coreRate := tc.outRate / 2
			nf := 48

			pcm := makePSStereoChirp(nf*coreFrameLen, tc.outRate)
			aus, _, _, ok := cEncodePSHEAAC(tc.outRate, tc.bitrate, coreFrameLen, pcm)
			require.True(t, ok, "fdk HE-AAC v2 (AOT_PS) encode failed")
			if len(aus) < nf {
				nf = len(aus)
			}
			require.Greater(t, nf, 16, "too few access units")

			dec, err := heaac.NewPSDecoder(coreFrameLen, coreRate, 0)
			require.NoError(t, err)

			// Native frame-immediate PS int32 stereo + the native mono core fed to
			// the fdk oracle, plus the SBR payload bit locations per frame.
			goOut := make([][]int32, nf)
			cores := make([]int32, nf*coreFrameLen)
			var auFlat []byte
			auLens := make([]int32, nf)
			startBits := make([]int32, nf)
			countBits := make([]int32, nf)
			crcFlags := make([]int32, nf)
			prevEls := make([]int32, nf)
			for f := 0; f < nf; f++ {
				so, ci, sb, cb, cf, pe, derr := dec.DecodeAccessUnitInt32(aus[f])
				require.NoErrorf(t, derr, "native PS decode of AU %d failed", f)
				require.Len(t, so, 2*2*coreFrameLen, "native PS output must be stereo int32")
				require.Len(t, ci, coreFrameLen, "PS core must be mono")
				goOut[f] = so
				copy(cores[f*coreFrameLen:], ci)
				auFlat = append(auFlat, aus[f]...)
				auLens[f] = int32(len(aus[f]))
				startBits[f] = int32(sb)
				countBits[f] = int32(cb)
				crcFlags[f] = int32(cf)
				prevEls[f] = int32(pe)
			}

			// Genuine fdk SBR+PS decoder, frame-immediate over the SAME mono core.
			fdkOut, ok := cSbrDirectPS(coreRate, tc.outRate, nf, cores, auFlat,
				auLens, startBits, countBits, crcFlags, prevEls)
			require.True(t, ok, "fdk sbr_direct_ps failed")

			const per = 2 * 2 * coreFrameLen
			for f := 0; f < nf; f++ {
				require.Equalf(t, fdkOut[f*per:(f+1)*per], goOut[f],
					"PS int32 stereo mismatch at frame %d (rate %d) — integration is not bit-exact", f, tc.outRate)
			}
			t.Logf("%s: HE-AAC v2 PS int32 STEREO decode is BIT-EXACT vs fdk across %d frames", tc.name, nf)
		})
	}
}
