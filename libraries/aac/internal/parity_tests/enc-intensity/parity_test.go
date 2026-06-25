// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package enc_intensity

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// This slice asserts the pure-Go encode-side intensity-stereo port
// (nativeaac.IntensityStereoProcessing) is bit-for-bit identical to the genuine
// vendored libfdk FDKaacEnc_IntensityStereoProcessing kernel. fdk-aac encode is
// fixed-point, so equality is EXACT int32/int — no tolerance.

const maxGroupedSfb = 60

// pseudo is a tiny deterministic LCG used to fabricate spectra / energies that
// exercise the IS decision + collapse branches without the encoder front end.
type pseudo struct{ s uint32 }

func (p *pseudo) next() uint32 {
	p.s = p.s*1664525 + 1013904223
	return p.s
}

// buildSfbOffset builds a uniform-width sfb offset layout of sfbCnt bands plus
// the terminating offset.
func buildSfbOffset(sfbCnt, sfbWidth int) []int32 {
	off := make([]int32, sfbCnt+1)
	for i := 0; i <= sfbCnt; i++ {
		off[i] = int32(i * sfbWidth)
	}
	return off
}

// isCase is one fabricated IntensityStereoProcessing input configuration.
type isCase struct {
	name           string
	seed           uint32
	sfbCnt         int
	sfbWidth       int
	sfbPerGroup    int // == sfbCnt for a single long-block group
	maxSfbPerGroup int
	allowIS        int
	pnsPresent     int
	// corr controls how correlated R is with L: R[j] = (L[j]*corrNum)>>corrShift
	// + noiseAmt*noise. Higher corr / lower noise -> IS more likely to fire.
	corrNum   int32
	corrShift uint
	noiseAmt  int32
	// pan scales the right energy relative to left so the L/R ratio gate passes.
	panNum   int32
	panShift uint
}

var isCases = []isCase{
	// Highly-correlated, panned pairs across several layouts (IS should fire).
	{"long49_corr_pan", 0x1234, 49, 16, 49, 49, 1, 0, 3, 2, 1 << 8, 5, 3},
	{"long44_corr_pan", 0xBEEF, 44, 16, 44, 44, 1, 0, 7, 3, 1 << 9, 3, 2},
	{"long40_corr_pan_pns", 0xC0DE, 40, 12, 40, 40, 1, 1, 5, 2, 1 << 7, 7, 3},
	{"long49_outofphase", 0x55AA, 49, 16, 49, 49, 1, 0, -3, 2, 1 << 8, 5, 3},
	{"long30_anticorr_pns", 0x0F0F, 30, 16, 30, 30, 1, 1, -7, 3, 1 << 9, 11, 4},
	// Grouped short-block style layout (sfbPerGroup < sfbCnt).
	{"grp_short", 0xABCD, 32, 12, 8, 8, 1, 0, 3, 2, 1 << 8, 5, 3},
	// Near-middle pan (ratio gate should mostly block IS) + low correlation.
	{"long49_middle", 0x9999, 49, 16, 49, 49, 1, 0, 1, 0, 1 << 12, 1, 0},
	// allowIS == 0: kernel must only zero the reset arrays and return.
	{"disabled", 0x2222, 49, 16, 49, 49, 0, 0, 3, 2, 1 << 8, 5, 3},
}

// makeISInputs fabricates a full input set for one case. Left spectrum/energies
// are random-but-bounded; the right channel is a correlated, panned, optionally
// out-of-phase version of the left so the IS decision branches are exercised.
func makeISInputs(tc isCase) (
	enL, enR, mdL, mdR, thrL, thrR, thrLdR, sprL, sprR, enLdL, enLdR, msMask []int32,
	pnsL, pnsR []int32, sfbOffset []int32) {

	p := pseudo{s: tc.seed}
	sfbOffset = buildSfbOffset(tc.sfbCnt, tc.sfbWidth)
	specLen := int(sfbOffset[tc.sfbCnt])

	mdL = make([]int32, specLen)
	mdR = make([]int32, specLen)
	for j := 0; j < specLen; j++ {
		// keep magnitudes modest so the <<sL/<<sR headroom shifts in
		// calcSfbMaxScale don't saturate, matching the encoder's block-float regime.
		l := int32(p.next()) >> 12
		mdL[j] = l
		noise := (int32(p.next()) >> 12) / tc.noiseAmtOrOne()
		r := (l*tc.corrNum)>>tc.corrShift + noise
		mdR[j] = r
	}

	enL = make([]int32, maxGroupedSfb)
	enR = make([]int32, maxGroupedSfb)
	thrL = make([]int32, maxGroupedSfb)
	thrR = make([]int32, maxGroupedSfb)
	thrLdR = make([]int32, maxGroupedSfb)
	sprL = make([]int32, maxGroupedSfb)
	sprR = make([]int32, maxGroupedSfb)
	enLdL = make([]int32, maxGroupedSfb)
	enLdR = make([]int32, maxGroupedSfb)
	msMask = make([]int32, maxGroupedSfb)
	pnsL = make([]int32, maxGroupedSfb)
	pnsR = make([]int32, maxGroupedSfb)

	for i := 0; i < tc.sfbCnt; i++ {
		// positive FIXP_DBL energies in the upper range; pan the right channel.
		base := int32(p.next()>>2) | (1 << 28)
		enL[i] = base
		enR[i] = (base * tc.panNum) >> tc.panShift
		if enR[i] <= 0 {
			enR[i] = 1
		}
		// thresholds below energy so the threshold gate (intensity.cpp:671) passes.
		thrL[i] = enL[i] >> 3
		thrR[i] = enR[i] >> 3
		// ld-data: small negative-ish values; the L-R difference must land inside
		// the ±60/(1<<7) realScale clamp to vary realIsScale/isScale.
		enLdL[i] = -int32(p.next()%(1<<24)) - (1 << 20)
		enLdR[i] = enLdL[i] + (int32(p.next()%(1<<22)) - (1 << 21))
		thrLdR[i] = -int32(p.next() % (1 << 24))
		sprL[i] = int32(p.next() >> 4)
		sprR[i] = int32(p.next() >> 4)
		// some PNS flags on so the PNS-switchoff branch is reachable.
		if tc.pnsPresent != 0 {
			pnsL[i] = int32(p.next() & 1)
			pnsR[i] = int32(p.next() & 1)
		}
	}
	return
}

func (tc isCase) noiseAmtOrOne() int32 {
	if tc.noiseAmt <= 0 {
		return 1
	}
	return tc.noiseAmt
}

func TestIntensityStereoProcessingParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-intensity parity asserts under -tags aac_strict")
	}

	for _, tc := range isCases {
		t.Run(tc.name, func(t *testing.T) {
			enL, enR, mdL, mdR, thrL, thrR, thrLdR, sprL, sprR, enLdL, enLdR, msMask,
				pnsL, pnsR, sfbOffset := makeISInputs(tc)

			// --- C oracle ---
			cRes := cIntensityStereoProcessing(
				enL, enR, mdL, mdR, thrL, thrR, thrLdR, sprL, sprR, enLdL, enLdR,
				nativeaac.MsNone, msMask, tc.sfbCnt, tc.sfbPerGroup, tc.maxSfbPerGroup,
				sfbOffset, tc.allowIS, tc.pnsPresent, pnsL, pnsR)

			// --- Go port (operates on its own copies of the mutated arrays) ---
			gMdL := append([]int32(nil), mdL...)
			gMdR := append([]int32(nil), mdR...)
			gEnR := append([]int32(nil), enR...)
			gThrR := append([]int32(nil), thrR...)
			gThrLdR := append([]int32(nil), thrLdR...)
			gSprR := append([]int32(nil), sprR...)
			gMsMask := make([]int, maxGroupedSfb)
			for i := range gMsMask {
				gMsMask[i] = int(msMask[i])
			}
			gIsBook := make([]int, maxGroupedSfb)
			gIsScale := make([]int, maxGroupedSfb)
			gDigest := nativeaac.MsNone

			var pnsData [2]*nativeaac.PNSData
			if tc.pnsPresent != 0 {
				var l, r nativeaac.PNSData
				for i := 0; i < maxGroupedSfb; i++ {
					l.PnsFlag[i] = int(pnsL[i])
					r.PnsFlag[i] = int(pnsR[i])
				}
				pnsData[0] = &l
				pnsData[1] = &r
			}

			nativeaac.IntensityStereoProcessing(
				enL, gEnR, gMdL, gMdR, thrL, gThrR, gThrLdR, sprL, gSprR,
				enLdL, enLdR, &gDigest, gMsMask, tc.sfbCnt, tc.sfbPerGroup,
				tc.maxSfbPerGroup, sfbOffset, tc.allowIS, gIsBook, gIsScale, pnsData)

			// --- compare ---
			assert.Equal(t, cRes.mdctLeft, gMdL, "mdctSpectrumLeft")
			assert.Equal(t, cRes.mdctRight, gMdR, "mdctSpectrumRight")
			assert.Equal(t, cRes.sfbEnergyRight, gEnR, "sfbEnergyRight")
			assert.Equal(t, cRes.sfbThrRight, gThrR, "sfbThresholdRight")
			assert.Equal(t, cRes.sfbThrLdRight, gThrLdR, "sfbThresholdLdDataRight")
			assert.Equal(t, cRes.sfbSpreadRight, gSprR, "sfbSpreadEnRight")
			assert.Equal(t, cRes.isBook, toI32(gIsBook), "isBook")
			assert.Equal(t, cRes.isScale, toI32(gIsScale), "isScale")
			assert.Equal(t, cRes.msMask, toI32(gMsMask), "msMask")
			assert.Equal(t, cRes.msDigest, gDigest, "msDigest")
			if tc.pnsPresent != 0 {
				goPnsL := make([]int32, maxGroupedSfb)
				goPnsR := make([]int32, maxGroupedSfb)
				for i := 0; i < maxGroupedSfb; i++ {
					goPnsL[i] = int32(pnsData[0].PnsFlag[i])
					goPnsR[i] = int32(pnsData[1].PnsFlag[i])
				}
				assert.Equal(t, cRes.pnsFlagL, goPnsL, "pnsFlagL")
				assert.Equal(t, cRes.pnsFlagR, goPnsR, "pnsFlagR")
			}
		})
	}
}

func toI32(s []int) []int32 {
	out := make([]int32, len(s))
	for i, v := range s {
		out[i] = int32(v)
	}
	return out
}
