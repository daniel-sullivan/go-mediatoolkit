// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psdecapply

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/require"
)

// TestPsApplyInt32Parity is the HE-AAC v2 PS apply / full-synthesis parity gate.
// It drives BOTH the genuine vendored CreatePsDec -> ReadPsData -> DecodePs ->
// per-slot (initSlotBasedRotation + ApplyPsSlot) (psdec.cpp, via bridge.cpp) and
// the pure-Go sbr.PsApplyRun over the IDENTICAL random QMF input + ps_data
// payload, and asserts the in-place-modified left channel AND the synthesised
// right channel are EXACTLY equal across all slots.
//
// This exercises the full PS pipeline together: the hybrid analysis filterbank,
// the decorrelator (the INDEP_CPLX_PS allpass cascade + the PS ducker's
// transient/peak-decay smoothing + sqrt/invSqrt ducking), the H-matrix rotation
// with per-slot interpolation, and the hybrid synthesis. The cross-slot state
// (hybrid ringbuffers, decorr delay lines + state buffers, the Hxx prev
// coefficients) is carried across all noCol columns of the frame.
//
// Payloads are deterministic pseudo-random bytes; the test keeps the subset that
// yields psProcess==1 (a valid PS header with supported modes) so the apply path
// is genuinely exercised. The PS parse itself is already proven bit-exact by the
// ps-dec-parse slice; here both decoders take the identical decode branch, so any
// divergence is in the synthesis math.
func TestPsApplyInt32Parity(t *testing.T) {
	r := rand.New(rand.NewSource(424242))

	const (
		aacSamplesPerFrame = 1024
		noCol              = 32
		bands              = 64
		lsb                = 5
		usb                = 40
		highSubband        = 40
		bufBytes           = 256
	)

	applied := 0
	const wantApplied = 150
	const maxIters = 60000

	for it := 0; it < maxIters && applied < wantApplied; it++ {
		// Random valid-ish ps_data payload.
		payload := make([]byte, bufBytes)
		nRand := 10 + r.Intn(10)
		for i := 0; i < nRand; i++ {
			payload[i] = byte(r.Intn(256))
		}
		validBits := bufBytes * 8

		// Moderate-amplitude QMF input (avoids permanent full-scale saturation
		// that would make every value MAXVAL; a few high-amp iters add coverage).
		amp := int32(1 << 24)
		if it%7 == 0 {
			amp = 1 << 29
		}
		totalRows := noCol + 6 // HYBRID_FILTER_DELAY
		lowRe := make([]int32, totalRows*bands)
		lowImg := make([]int32, totalRows*bands)
		for i := range lowRe {
			lowRe[i] = int32(r.Int63())%amp - amp/2
			lowImg[i] = int32(r.Int63())%amp - amp/2
		}

		// Probe the C first to learn psProcess; only assert when PS is applied so
		// we accumulate meaningful coverage (skip == both sides do nothing).
		cLeftRe, cLeftImg, cRightRe, cRightImg, cFlag := cPsApply(
			aacSamplesPerFrame, payload, validBits, noCol, lsb, usb,
			0, 0, 0, highSubband, lowRe, lowImg)

		goLeftRe, goLeftImg, goRightRe, goRightImg, goFlag := sbr.PsApplyRun(
			aacSamplesPerFrame, payload, uint32(bufBytes), uint32(validBits),
			noCol, lsb, usb, 0, 0, 0, highSubband, lowRe, lowImg)

		require.Equalf(t, cFlag, goFlag, "it=%d psProcess flag", it)
		require.Equalf(t, cLeftRe, goLeftRe, "it=%d left real mismatch", it)
		require.Equalf(t, cLeftImg, goLeftImg, "it=%d left imag mismatch", it)
		require.Equalf(t, cRightRe, goRightRe, "it=%d right real mismatch", it)
		require.Equalf(t, cRightImg, goRightImg, "it=%d right imag mismatch", it)

		if cFlag == 1 {
			applied++
		}
	}

	require.GreaterOrEqual(t, applied, 1, "no payload exercised the PS apply path")
	t.Logf("PS apply exercised on %d payloads", applied)
}
