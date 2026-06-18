// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package ms_stereo_decode

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// jointStereoMaximumBands mirrors JointStereoMaximumBands (stereo.h:130) and
// the native nativeaac.JointStereoData.MsUsed array length. The oracle clears
// exactly this many bytes when msMaskPresent==2.
const jointStereoMaximumBands = 64

// strictGate skips FP-bit-exact-only assertions on a bare (non-strict) go
// test, per the aac_strict parity discipline. The M/S upmix is an integer
// kernel and matches in any build, but the gate is kept for convention so the
// strict run is the one that asserts. Mirrors huffman-spectral-decode.
func strictGate(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("requires -tags=aac_strict (integer-parity gate); see libraries/aac/mise.toml")
	}
}

// msStereoCase is one fabricated channel_pair_element M/S decode shape.
type msStereoCase struct {
	msMaskPresent     uint8
	msUsed            [jointStereoMaximumBands]uint8
	spectrumL         []int32
	spectrumR         []int32
	sfbLeftScale      []int16
	sfbRightScale     []int16
	sfbOffsets        []int16
	windowGroupLength []uint8
	windowGroups      int
	maxSfbSteOutside  int
	transmittedBandsL int
	transmittedBandsR int
	granuleLength     int
}

// fabricate builds a random but structurally valid M/S decode case: a
// window-group layout, monotone band offsets within the granule, random L/R
// spectra, small SFB scale exponents (so the common-scale right-shifts stay in
// [0, DFRACT_BITS-1)) and a random MsUsed bitset.
func fabricate(r *rand.Rand) msStereoCase {
	granuleLength := 64 + r.IntN(64) // per-window SPEC stride

	windowGroups := 1 + r.IntN(4) // 1..4 groups
	windowGroupLength := make([]uint8, windowGroups)
	totalWindows := 0
	for g := range windowGroupLength {
		windowGroupLength[g] = uint8(1 + r.IntN(3))
		totalWindows += int(windowGroupLength[g])
	}

	transmittedBands := 1 + r.IntN(10) // 1..10 SFB

	// Asymmetric transmitted-band counts exercise the left-only / right-only
	// fill-down branches (stereo.cpp:1122-1150). Half the cases stay symmetric
	// (the common common_window case), the rest skew one channel up.
	transmittedBandsL := transmittedBands
	transmittedBandsR := transmittedBands
	switch r.IntN(3) {
	case 1:
		transmittedBandsL = transmittedBands + 1 + r.IntN(2)
	case 2:
		transmittedBandsR = transmittedBands + 1 + r.IntN(2)
	}
	maxBands := transmittedBandsL
	if transmittedBandsR > maxBands {
		maxBands = transmittedBandsR
	}
	if maxBands > jointStereoMaximumBands {
		maxBands = jointStereoMaximumBands
		transmittedBandsL = min(transmittedBandsL, maxBands)
		transmittedBandsR = min(transmittedBandsR, maxBands)
	}

	// max_sfb_ste_outside bounds the M/S-upmix band loop; in the common
	// common_window case it is min(L,R). Vary it across the full [1, max] range
	// to exercise the boundary where the loop hands off to the fill-down loops.
	minBands := transmittedBandsL
	if transmittedBandsR < minBands {
		minBands = transmittedBandsR
	}
	maxSfbSteOutside := 1 + r.IntN(minBands)

	// Monotone band offsets in [0, granuleLength].
	sfbOffsets := make([]int16, maxBands+1)
	off := 0
	for b := 0; b <= maxBands; b++ {
		sfbOffsets[b] = int16(off)
		// step a multiple of 4: GenerateMSOutput unrolls by 4 and requires the
		// band length to be a multiple of 4 (ISO SFB widths always are).
		step := 4 + 4*r.IntN(3)
		if off+step > granuleLength {
			step = (granuleLength - off) / 4 * 4
		}
		off += step
	}

	specLen := totalWindows * granuleLength
	spectrumL := make([]int32, specLen)
	spectrumR := make([]int32, specLen)
	for i := range spectrumL {
		spectrumL[i] = int32(r.Uint32())
		spectrumR[i] = int32(r.Uint32())
	}

	// 16 SFB scale exponents per window. Keep them small and non-negative so
	// commonScale - lScale / rScale stays a sane right-shift count; the kernel
	// clamps at DFRACT_BITS-1 anyway, but realistic decoder scales are small.
	sfbLeftScale := make([]int16, totalWindows*16)
	sfbRightScale := make([]int16, totalWindows*16)
	for i := range sfbLeftScale {
		sfbLeftScale[i] = int16(r.IntN(20))
		sfbRightScale[i] = int16(r.IntN(20))
	}

	var msUsed [jointStereoMaximumBands]uint8
	for b := 0; b < maxBands; b++ {
		// one bit per window group; only the low windowGroups bits matter.
		msUsed[b] = uint8(r.IntN(1 << windowGroups))
	}

	// msMaskPresent: 1 == explicit per-band mask (kept), 2 == all bands on +
	// cleared afterward. Both reach the same upmix loop here.
	msMaskPresent := uint8(1)
	if r.IntN(2) == 0 {
		msMaskPresent = 2
	}

	return msStereoCase{
		msMaskPresent:     msMaskPresent,
		msUsed:            msUsed,
		spectrumL:         spectrumL,
		spectrumR:         spectrumR,
		sfbLeftScale:      sfbLeftScale,
		sfbRightScale:     sfbRightScale,
		sfbOffsets:        sfbOffsets,
		windowGroupLength: windowGroupLength,
		windowGroups:      windowGroups,
		maxSfbSteOutside:  maxSfbSteOutside,
		transmittedBandsL: transmittedBandsL,
		transmittedBandsR: transmittedBandsR,
		granuleLength:     granuleLength,
	}
}

// TestParityApplyMS sweeps the M/S joint-stereo decode upmix over many random
// channel_pair_element shapes — symmetric and asymmetric transmitted-band
// counts, every msMaskPresent mode, varied window-group layouts and
// max_sfb_ste_outside boundaries — and compares the post-transform L/R spectra,
// the L/R SFB scale exponents and the MsUsed array against the genuine vendored
// CJointStereo_ApplyMS / CJointStereo_GenerateMSOutput bit-for-bit.
func TestParityApplyMS(t *testing.T) {
	strictGate(t)
	r := rand.New(rand.NewPCG(0x5151, 0x6262))

	for trial := 0; trial < 3000; trial++ {
		tc := fabricate(r)

		// C oracle over independent copies of the inputs.
		cRes := cApplyMS(
			tc.msMaskPresent,
			tc.msUsed[:],
			tc.spectrumL, tc.spectrumR,
			tc.sfbLeftScale, tc.sfbRightScale,
			tc.sfbOffsets,
			tc.windowGroupLength,
			tc.windowGroups,
			tc.maxSfbSteOutside,
			tc.transmittedBandsL, tc.transmittedBandsR,
			tc.granuleLength,
		)

		// Native port over its own copies (ApplyMS mutates in place).
		nSpecL := append([]int32(nil), tc.spectrumL...)
		nSpecR := append([]int32(nil), tc.spectrumR...)
		nLeftScale := append([]int16(nil), tc.sfbLeftScale...)
		nRightScale := append([]int16(nil), tc.sfbRightScale...)
		jsd := &nativeaac.JointStereoData{MsMaskPresent: tc.msMaskPresent, MsUsed: tc.msUsed}

		nativeaac.ApplyMS(
			jsd,
			nSpecL, nSpecR,
			nLeftScale, nRightScale,
			tc.sfbOffsets,
			tc.windowGroupLength,
			tc.windowGroups,
			tc.maxSfbSteOutside,
			tc.transmittedBandsL, tc.transmittedBandsR,
			tc.granuleLength,
		)

		require.Equal(t, cRes.spectrumL, nSpecL, "trial=%d spectrumL mismatch", trial)
		require.Equal(t, cRes.spectrumR, nSpecR, "trial=%d spectrumR mismatch", trial)
		require.Equal(t, cRes.leftScale, nLeftScale, "trial=%d SFBleftScale mismatch", trial)
		require.Equal(t, cRes.rightScale, nRightScale, "trial=%d SFBrightScale mismatch", trial)
		require.Equal(t, cRes.msUsed, jsd.MsUsed[:], "trial=%d MsUsed mismatch", trial)
	}
}
