// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package enc_psy_main

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// These slices assert the pure-Go psy-main driver leaf ports
// (nativeaac.SpreadingMax / InitPreEchoControl / PreEchoControl /
// CalculateFullTonality / GroupShortData) are bit-for-bit identical to the
// genuine vendored libfdk kernels FDKaacEnc_psyMain assembles. fdk-aac encode
// is fixed-point, so equality is EXACT int32/int16 — no tolerance.

func TestSpreadingMaxParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-psy-main parity asserts under -tags aac_strict")
	}
	rng := rand.New(rand.NewSource(0x59C1))
	for _, pbCnt := range []int{1, 2, 5, 16, 40, 51} {
		mlf := make([]int32, pbCnt)
		mhf := make([]int32, pbCnt)
		energy := make([]int32, pbCnt)
		for i := 0; i < pbCnt; i++ {
			// mask factors are positive fractions in [0,1); energies span the range
			mlf[i] = rng.Int31()
			mhf[i] = rng.Int31()
			energy[i] = rng.Int31() >> uint(rng.Intn(20))
		}
		want := cSpreadingMax(mlf, mhf, energy)

		got := append([]int32(nil), energy...)
		nativeaac.SpreadingMax(pbCnt, mlf, mhf, got)

		require.Len(t, got, len(want))
		assert.Equal(t, want, got, "pbCnt=%d", pbCnt)
	}
}

func TestInitPreEchoControlParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-psy-main parity asserts under -tags aac_strict")
	}
	rng := rand.New(rand.NewSource(0xA17E))
	for _, numPb := range []int{1, 16, 40, 51} {
		thr := make([]int32, numPb)
		for i := range thr {
			thr[i] = rng.Int31()
		}
		wantNm1, wantMs, wantCpe := cInitPreEchoControl(thr, numPb)

		gotNm1 := make([]int32, numPb)
		gotMs, gotCpe := nativeaac.InitPreEchoControl(gotNm1, thr, numPb)

		assert.Equal(t, wantNm1, gotNm1, "numPb=%d nm1", numPb)
		assert.Equal(t, wantMs, gotMs, "numPb=%d mdctScalenm1", numPb)
		assert.Equal(t, wantCpe, gotCpe, "numPb=%d calcPreEcho", numPb)
	}
}

func TestPreEchoControlParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-psy-main parity asserts under -tags aac_strict")
	}
	rng := rand.New(rand.NewSource(0xBEEF))
	// exercise both branches (mdctScale >/<= mdctScalenm1) and calcPreEcho on/off
	cases := []struct {
		numPb, calcPreEcho, maxInc, mdctScale, mdctScalenm1 int
		minRemain                                           int16
	}{
		{40, 1, 2, 5, 3, 0x1000},
		{40, 1, 2, 3, 5, 0x1000},
		{40, 1, 4, 4, 4, 0x4000},
		{51, 0, 2, 6, 2, 0x0800},
		{16, 1, 8, 7, 1, 0x7FFF},
		{1, 1, 2, 2, 2, 0x2000},
	}
	for ci, c := range cases {
		nm1 := make([]int32, c.numPb)
		thr := make([]int32, c.numPb)
		for i := 0; i < c.numPb; i++ {
			// keep magnitudes moderate so the int multiply against maxInc stays meaningful
			nm1[i] = rng.Int31() >> 4
			thr[i] = rng.Int31() >> 4
		}

		cNm1 := append([]int32(nil), nm1...)
		cThr := append([]int32(nil), thr...)
		wantMs := cPreEchoControl(cNm1, c.calcPreEcho, c.numPb, c.maxInc, c.minRemain,
			cThr, c.mdctScale, c.mdctScalenm1)

		gNm1 := append([]int32(nil), nm1...)
		gThr := append([]int32(nil), thr...)
		gotMs := nativeaac.PreEchoControl(gNm1, c.calcPreEcho, c.numPb, c.maxInc, c.minRemain,
			gThr, c.mdctScale, c.mdctScalenm1)

		assert.Equal(t, wantMs, gotMs, "case %d mdctScalenm1", ci)
		assert.Equal(t, cNm1, gNm1, "case %d nm1", ci)
		assert.Equal(t, cThr, gThr, "case %d threshold", ci)
	}
}

func TestCalculateFullTonalityParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-psy-main parity asserts under -tags aac_strict")
	}
	rng := rand.New(rand.NewSource(0xC0DE))
	// Build a plausible long-block sfbOffset table (monotone, last <= 1024).
	for _, sfbCnt := range []int{8, 24, 49} {
		sfbOffset := make([]int32, sfbCnt+1)
		off := 0
		for i := 0; i <= sfbCnt; i++ {
			sfbOffset[i] = int32(off)
			off += 4 + rng.Intn(12)
			if off > 1024 {
				off = 1024
			}
		}
		numLines := int(sfbOffset[sfbCnt])
		spectrum := make([]int32, 1024)
		for i := 0; i < numLines; i++ {
			spectrum[i] = rng.Int31() >> uint(rng.Intn(24))
			if rng.Intn(4) == 0 {
				spectrum[i] = -spectrum[i]
			}
		}
		sfbMaxScaleSpec := make([]int32, sfbCnt)
		sfbEnergyLD64 := make([]int32, sfbCnt)
		for i := 0; i < sfbCnt; i++ {
			sfbMaxScaleSpec[i] = int32(rng.Intn(12))
			sfbEnergyLD64[i] = -(rng.Int31() >> 2) // ldData energies are negative fractions
		}

		// C oracle (it mutates spectrum? No — CalcSfbTonality only reads spectrum;
		// CalculateFullTonality uses a private scratch for chaos). Copy anyway.
		cSpec := append([]int32(nil), spectrum...)
		want := cCalculateFullTonality(cSpec, sfbMaxScaleSpec, sfbEnergyLD64, sfbCnt, sfbOffset, 1)

		gSpec := append([]int32(nil), spectrum...)
		gMax := make([]int, sfbCnt)
		gOff := make([]int, sfbCnt+1)
		for i := range gMax {
			gMax[i] = int(sfbMaxScaleSpec[i])
		}
		for i := range gOff {
			gOff[i] = int(sfbOffset[i])
		}
		got := make([]int16, sfbCnt)
		nativeaac.CalculateFullTonality(gSpec, gMax, sfbEnergyLD64, got, sfbCnt, gOff, 1)

		assert.Equal(t, want, got, "sfbCnt=%d", sfbCnt)
	}
}

// groupings enumerates valid (noOfGroups, groupLen[]) where groupLen sums to 8.
func groupings() [][]int32 {
	return [][]int32{
		{8},
		{4, 4},
		{2, 6},
		{1, 7},
		{2, 2, 4},
		{1, 2, 5},
		{2, 2, 2, 2},
		{1, 1, 1, 5},
		{3, 2, 2, 1},
	}
}

func TestGroupShortDataParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-psy-main parity asserts under -tags aac_strict")
	}
	const transFac = 8
	const sfbCnt = 15 // MAX_SFB_SHORT for short blocks
	const granuleLength = 1024
	const granuleShort = granuleLength / transFac // 128

	rng := rand.New(rand.NewSource(0xD474))

	// short-block sfbOffset within one 128-sample window (sfbCnt+1 entries).
	sfbOffset := make([]int32, sfbCnt+1)
	off := 0
	for i := 0; i <= sfbCnt; i++ {
		sfbOffset[i] = int32(off)
		off += 4 + rng.Intn(8)
		if off > granuleShort {
			off = granuleShort
		}
	}
	sfbOffset[sfbCnt] = granuleShort

	for _, gl := range groupings() {
		for _, sfbActive := range []int{1, 5, 10, sfbCnt} {
			noOfGroups := len(gl)

			// fabricate the four SFB unions (Short[8][15] = 120 cells) with
			// distinct nonzero values so any union-aliasing mismatch shows.
			mkUnion := func() []int32 {
				u := make([]int32, transFac*sfbCnt)
				for k := range u {
					u[k] = rng.Int31() >> 2
				}
				return u
			}
			thr := mkUnion()
			eng := mkUnion()
			ms := mkUnion()
			spr := mkUnion()

			// mdctSpectrum: 1024 cells, some bands zero to exercise maxSfbPerGroup.
			spec := make([]int32, granuleLength)
			for k := range spec {
				if rng.Intn(8) != 0 {
					spec[k] = rng.Int31() >> 4
				}
			}

			sfbMinSnr := make([]int32, sfbCnt)
			for i := range sfbMinSnr {
				sfbMinSnr[i] = -(rng.Int31() >> 2)
			}

			// ---- C oracle ----
			cThr := append([]int32(nil), thr...)
			cEng := append([]int32(nil), eng...)
			cMs := append([]int32(nil), ms...)
			cSpr := append([]int32(nil), spr...)
			cSpec := append([]int32(nil), spec...)
			cGrpOff := make([]int32, 61) // MAX_GROUPED_SFB+1
			cGrpMinSnr := make([]int32, 60)
			cMax := cGroupShortData(cSpec, cThr, cEng, cMs, cSpr, sfbCnt, sfbActive,
				sfbOffset, sfbMinSnr, cGrpOff, cGrpMinSnr, noOfGroups, gl, granuleLength)

			// ---- Go port ----
			toUnion := func(flat []int32) *nativeaac.SfbGrouped {
				u := new(nativeaac.SfbGrouped)
				for w := 0; w < transFac; w++ {
					for b := 0; b < sfbCnt; b++ {
						u.SetShort(w, b, flat[w*sfbCnt+b])
					}
				}
				return u
			}
			gThr := toUnion(thr)
			gEng := toUnion(eng)
			gMs := toUnion(ms)
			gSpr := toUnion(spr)
			gSpec := append([]int32(nil), spec...)
			gGrpOff := make([]int, 61)
			gGrpMinSnr := make([]int32, 60)
			gOff := make([]int, sfbCnt+1)
			for i := range gOff {
				gOff[i] = int(sfbOffset[i])
			}
			gGl := make([]int, noOfGroups)
			for i := range gGl {
				gGl[i] = int(gl[i])
			}
			gMaxVal := nativeaac.GroupShortData(gSpec, gThr, gEng, gMs, gSpr,
				sfbCnt, sfbActive, gOff, sfbMinSnr, gGrpOff, gGrpMinSnr,
				noOfGroups, gGl, granuleLength)

			// compare maxSfbPerGroup
			require.Equal(t, cMax, gMaxVal, "gl=%v sfbActive=%d maxSfbPerGroup", gl, sfbActive)

			// compare the four union Long[] outputs (60 cells)
			for k := 0; k < 60; k++ {
				assert.Equal(t, cThr[k], gThr.Long(k), "gl=%v sfbActive=%d thr.Long[%d]", gl, sfbActive, k)
				assert.Equal(t, cEng[k], gEng.Long(k), "gl=%v sfbActive=%d eng.Long[%d]", gl, sfbActive, k)
				assert.Equal(t, cMs[k], gMs.Long(k), "gl=%v sfbActive=%d ms.Long[%d]", gl, sfbActive, k)
				assert.Equal(t, cSpr[k], gSpr.Long(k), "gl=%v sfbActive=%d spr.Long[%d]", gl, sfbActive, k)
			}

			// Compare regrouped spectrum only where FDKaacEnc_groupShortData
			// deterministically WRITES. Its re-group loop copies the active-band
			// lines into a scratch buffer, jumping the flat write index past the
			// inactive high bands (i += groupLen*(sfbOffset[sfbCnt]-sfbOffset[sfb]))
			// and leaving those gap positions as uninitialised scratch — which it
			// then memcpys back over mdctSpectrum. Those gap cells are genuinely
			// undefined in the reference (and are zero high-frequency lines in the
			// real encoder), so only the written cells are a meaningful parity
			// target. Reconstruct the exact written-index walk (grp_data.cpp:241).
			written := make([]bool, granuleLength)
			{
				idx := 0
				for grp := 0; grp < noOfGroups; grp++ {
					var sfb int
					for sfb = 0; sfb < sfbActive; sfb++ {
						width := int(sfbOffset[sfb+1] - sfbOffset[sfb])
						for j := 0; j < int(gl[grp]); j++ {
							for line := 0; line < width; line++ {
								written[idx] = true
								idx++
							}
						}
					}
					idx += int(gl[grp]) * (int(sfbOffset[sfbCnt]) - int(sfbOffset[sfb]))
				}
			}
			for k := 0; k < granuleLength; k++ {
				if written[k] {
					assert.Equal(t, cSpec[k], gSpec[k], "gl=%v sfbActive=%d regrouped spectrum[%d]", gl, sfbActive, k)
				}
			}
			for k := 0; k < 61; k++ {
				assert.Equal(t, int(cGrpOff[k]), gGrpOff[k], "gl=%v sfbActive=%d grpOff[%d]", gl, sfbActive, k)
			}
			assert.Equal(t, cGrpMinSnr, gGrpMinSnr, "gl=%v sfbActive=%d grpMinSnr", gl, sfbActive)
		}
	}
}
