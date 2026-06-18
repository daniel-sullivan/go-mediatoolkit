// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encframe

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// QCDATA_BR_MODE values (qc_data.h:113), mirrored by nativeaac.QcdataBrMode.
const (
	brCBR  = 0
	brVBR1 = 1
	brVBR5 = 5
	brSFR  = 7
	brFF   = 6
)

// TestCalcFrameLen asserts FDKaacEnc_calcFrameLen parity over a spread of
// bitrates / sample rates / frame lengths in both INT and MODULO modes.
func TestCalcFrameLen(t *testing.T) {
	rates := []int{8000, 11025, 16000, 22050, 24000, 32000, 44100, 48000, 96000}
	bitrates := []int{8000, 24000, 48000, 64000, 96000, 128000, 192000, 256000, 320000}
	frameLens := []int{960, 1024, 2048}
	modes := []int{nativeaac.FrameLenBytesInt, nativeaac.FrameLenBytesModulo}

	for _, sr := range rates {
		for _, br := range bitrates {
			for _, fl := range frameLens {
				for _, m := range modes {
					got := nativeaac.CalcFrameLenForParity(br, sr, fl, m)
					want := cCalcFrameLen(br, sr, fl, m)
					require.Equal(t, want, got, "calcFrameLen br=%d sr=%d fl=%d mode=%d", br, sr, fl, m)
				}
			}
		}
	}
}

// TestFramePadding asserts FDKaacEnc_framePadding parity, including the
// paddingRest accumulator carried across a run of frames (the C initialises it
// to sampleRate).
func TestFramePadding(t *testing.T) {
	cases := []struct{ br, sr, fl int }{
		{64000, 44100, 1024},
		{128000, 48000, 1024},
		{96000, 44100, 1024},
		{48000, 22050, 1024},
		{320000, 96000, 1024},
		{8000, 8000, 960},
	}
	for _, c := range cases {
		goRest := c.sr
		cRest := c.sr
		// Walk 200 frames so the accumulator wraps multiple times.
		for f := 0; f < 200; f++ {
			gotOn, gotRest := nativeaac.FramePaddingForParity(c.br, c.sr, c.fl, goRest)
			wantOn, wantRest := cFramePadding(c.br, c.sr, c.fl, cRest)
			require.Equal(t, wantOn, gotOn, "framePadding on br=%d sr=%d frame=%d", c.br, c.sr, f)
			require.Equal(t, wantRest, gotRest, "framePadding rest br=%d sr=%d frame=%d", c.br, c.sr, f)
			goRest, cRest = gotRest, wantRest
		}
	}
}

// TestAdjustBitrate asserts FDKaacEnc_AdjustBitrate parity (avgTotalBits and the
// carried paddingRest) over a run of frames.
func TestAdjustBitrate(t *testing.T) {
	cases := []struct{ br, sr, fl int }{
		{64000, 44100, 1024},
		{128000, 48000, 1024},
		{96000, 44100, 1024},
		{256000, 48000, 1024},
	}
	for _, c := range cases {
		goRest := c.sr
		cRest := c.sr
		for f := 0; f < 64; f++ {
			gotAvg, gotRest := nativeaac.AdjustBitrateForParity(goRest, c.br, c.sr, c.fl)
			wantAvg, wantRest := cAdjustBitrate(cRest, c.br, c.sr, c.fl)
			require.Equal(t, wantAvg, gotAvg, "adjustBitrate avg br=%d sr=%d frame=%d", c.br, c.sr, f)
			require.Equal(t, wantRest, gotRest, "adjustBitrate rest br=%d sr=%d frame=%d", c.br, c.sr, f)
			goRest, cRest = gotRest, wantRest
		}
	}
}

// TestCalcMaxValueInSfb asserts FDKaacEnc_calcMaxValueInSfb parity over random
// long-block quantized spectra with a realistic sfb layout.
func TestCalcMaxValueInSfb(t *testing.T) {
	r := rand.New(rand.NewSource(0x4d41584d))
	for trial := 0; trial < 64; trial++ {
		// long block: one group of maxSfb sfbs over 1024 lines.
		maxSfb := 30 + r.Intn(20) // 30..49
		// build monotonically-increasing sfb offsets up to 1024.
		offs := make([]int32, maxSfb+1)
		offsInt := make([]int, maxSfb+1)
		step := 1024 / maxSfb
		acc := 0
		for i := 0; i < maxSfb; i++ {
			offs[i] = int32(acc)
			offsInt[i] = acc
			acc += step
		}
		offs[maxSfb] = 1024
		offsInt[maxSfb] = 1024

		spec := make([]int16, 1024)
		for i := range spec {
			switch r.Intn(4) {
			case 0:
				spec[i] = 0
			default:
				spec[i] = int16(r.Intn(2*8192) - 8192)
			}
		}

		gotMax := make([]uint32, maxSfb)
		goMax := make([]uint, maxSfb)
		gotAll := cCalcMaxValueInSfb(maxSfb, maxSfb, maxSfb, offs, spec, gotMax)
		goAll := nativeaac.CalcMaxValueInSfbForParity(maxSfb, maxSfb, maxSfb, offsInt, spec, goMax)

		require.Equal(t, gotAll, goAll, "calcMaxValueInSfb all trial=%d", trial)
		for i := 0; i < maxSfb; i++ {
			assert.Equal(t, int(gotMax[i]), int(goMax[i]), "calcMaxValueInSfb sfb=%d trial=%d", i, trial)
		}
	}
}

// TestBitResRedistribution asserts FDKaacEnc_BitResRedistribution parity,
// including the rounding leftover folded back per element.
func TestBitResRedistribution(t *testing.T) {
	r := rand.New(rand.NewSource(0xb17235))
	for trial := 0; trial < 256; trial++ {
		n := 1 + r.Intn(4) // 1..4 elements
		rel := randRelativeBits(r, n)
		maxBitsPerFrame := 6144 * n
		avgTotalBits := r.Intn(maxBitsPerFrame)
		bitResTotMax := r.Intn(maxBitsPerFrame)
		bitResTot := r.Intn(bitResTotMax + 1)

		gotErr, gotLvl, gotMax := nativeaac.BitResRedistributionForParity(n, rel, bitResTot, bitResTotMax, maxBitsPerFrame, avgTotalBits)
		wantErr, wantLvl, wantMax := cBitResRedistribution(rel, bitResTot, bitResTotMax, maxBitsPerFrame, avgTotalBits)

		require.Equal(t, wantErr, gotErr, "bitResRedistribution err trial=%d", trial)
		for i := 0; i < n; i++ {
			assert.Equal(t, int(wantLvl[i]), gotLvl[i], "bitResLevelEl el=%d trial=%d", i, trial)
			assert.Equal(t, int(wantMax[i]), gotMax[i], "maxBitResBitsEl el=%d trial=%d", i, trial)
		}
	}
}

// TestDistributeElementDynBits asserts FDKaacEnc_distributeElementDynBits +
// FDKaacEnc_updateUsedDynBits parity, including the +/- difference correction.
func TestDistributeElementDynBits(t *testing.T) {
	r := rand.New(rand.NewSource(0xd157))
	for trial := 0; trial < 256; trial++ {
		n := 1 + r.Intn(4)
		rel := randRelativeBits(r, n)
		codeBits := r.Intn(6144 * n)
		dyn := make([]int32, n)
		for i := range dyn {
			dyn[i] = int32(r.Intn(6000))
		}

		gotErr, gotGranted, gotSum := nativeaac.DistributeElementDynBitsForParity(n, rel, codeBits, toIntSlice(dyn))
		wantErr, wantGranted, wantSum := cDistributeDynBits(rel, codeBits, dyn)

		require.Equal(t, wantErr, gotErr, "distribute err trial=%d", trial)
		require.Equal(t, wantSum, gotSum, "updateUsedDynBits trial=%d", trial)
		for i := 0; i < n; i++ {
			assert.Equal(t, int(wantGranted[i]), gotGranted[i], "grantedDynBits el=%d trial=%d", i, trial)
		}
	}
}

// TestTotalConsumedBits asserts FDKaacEnc_getTotalConsumedBits parity (the
// byte-alignment padding + per-element + global + header accounting).
func TestTotalConsumedBits(t *testing.T) {
	r := rand.New(rand.NewSource(0x7074616c))
	for trial := 0; trial < 256; trial++ {
		n := 1 + r.Intn(4)
		dyn := make([]int32, n)
		stat := make([]int32, n)
		ext := make([]int32, n)
		for i := 0; i < n; i++ {
			dyn[i] = int32(r.Intn(6000))
			stat[i] = int32(r.Intn(200))
			ext[i] = int32(r.Intn(64))
		}
		globalExt := r.Intn(128)
		globHdr := r.Intn(256)

		got := nativeaac.TotalConsumedBitsForParity(n, toIntSlice(dyn), toIntSlice(stat), toIntSlice(ext), globalExt, globHdr)
		want := cTotalConsumedBits(dyn, stat, ext, globalExt, globHdr)
		require.Equal(t, want, got, "totalConsumedBits trial=%d", trial)
	}
}

// TestUpdateFillBits asserts FDKaacEnc_updateFillBits parity across every
// bitrate mode (the &7 alignment, the reservoir-space cap and the
// minBitsPerFrame padding).
func TestUpdateFillBits(t *testing.T) {
	r := rand.New(rand.NewSource(0xf111))
	modes := []int{brCBR, brVBR1, brVBR5, brSFR, brFF}
	for _, m := range modes {
		for trial := 0; trial < 256; trial++ {
			maxBits := 6144
			minBits := r.Intn(maxBits)
			bitResTotMax := r.Intn(maxBits)
			bitResTot := r.Intn(bitResTotMax + 1)
			granted := r.Intn(maxBits)
			used := r.Intn(granted + 1)
			static := r.Intn(512)
			elementExt := r.Intn(64)
			globalExt := r.Intn(64)

			gotFill, gotTotal := nativeaac.UpdateFillBitsForParity(m, minBits, bitResTot, bitResTotMax, granted, used, static, elementExt, globalExt)
			wantFill, wantTotal := cUpdateFillBits(m, minBits, bitResTot, bitResTotMax, granted, used, static, elementExt, globalExt)

			require.Equal(t, wantFill, gotFill, "updateFillBits totFill mode=%d trial=%d", m, trial)
			require.Equal(t, wantTotal, gotTotal, "updateFillBits totalBits mode=%d trial=%d", m, trial)
		}
	}
}

// TestUpdateBitres asserts FDKaacEnc_updateBitres parity across every bitrate
// mode (VBR clamp vs CBR/SFR/INVALID reservoir replenish).
func TestUpdateBitres(t *testing.T) {
	r := rand.New(rand.NewSource(0xb17235e5))
	modes := []int{brCBR, brVBR1, brVBR5, brSFR, brFF}
	for _, m := range modes {
		for trial := 0; trial < 256; trial++ {
			maxBits := 6144
			bitResTotMax := r.Intn(maxBits)
			bitResTot := r.Intn(bitResTotMax + 1)
			granted := r.Intn(maxBits)
			used := r.Intn(granted + 1)
			totFill := r.Intn(64)
			align := r.Intn(8)

			got := nativeaac.UpdateBitresForParity(m, bitResTot, maxBits, bitResTotMax, granted, used, totFill, align)
			want := cUpdateBitres(m, bitResTot, maxBits, bitResTotMax, granted, used, totFill, align)
			require.Equal(t, want, got, "updateBitres mode=%d trial=%d", m, trial)
		}
	}
}

// randRelativeBits returns n FIXP_DBL relativeBits weights that sum to ~1.0 in
// Q31, matching how FDKaacEnc_InitChannelMapping normalises element shares.
func randRelativeBits(r *rand.Rand, n int) []int32 {
	raw := make([]int, n)
	total := 0
	for i := range raw {
		raw[i] = 1 + r.Intn(100)
		total += raw[i]
	}
	out := make([]int32, n)
	acc := int64(0)
	for i := 0; i < n-1; i++ {
		// fraction of 2^31
		v := int64(raw[i]) * (int64(1) << 31) / int64(total)
		out[i] = int32(v)
		acc += v
	}
	out[n-1] = int32((int64(1) << 31) - 1 - acc) // remainder, keep < 2^31
	if out[n-1] < 0 {
		out[n-1] = 0
	}
	return out
}

func toIntSlice(s []int32) []int {
	out := make([]int, len(s))
	for i, v := range s {
		out[i] = int(v)
	}
	return out
}
