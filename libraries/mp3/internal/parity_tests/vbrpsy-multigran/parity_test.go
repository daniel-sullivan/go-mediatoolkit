// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrpsymg

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// requireStrict skips unless the mp3_strict build tag is set. The vbrpsy
// energy/FHT/masking math is floating-point, so the bit-exact assertion only
// targets the FMA-free strict build vs the -ffp-contract=off cgo oracle
// (mise run //libraries/mp3:parity). A bare `go test` stays clean.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("vbrpsy-multigran parity asserts FP bit-exactness; run under -tags=mp3_strict (mise run //libraries/mp3:parity)")
	}
}

func f32bits(v float32) uint32 { return math.Float32bits(v) }

// assertF32 asserts one float32 is bit-identical and reports the granule context
// on mismatch. Returns false on mismatch so callers can stop at the first.
func assertF32(t *testing.T, want, got float32, ctx string) bool {
	t.Helper()
	if f32bits(want) != f32bits(got) {
		assert.Failf(t, "vbrpsy bit mismatch",
			"%s: c=%v(0x%08x) go=%v(0x%08x)", ctx, want, f32bits(want), got, f32bits(got))
		return false
	}
	return true
}

func assertSlice(t *testing.T, want, got []float32, ctx string) bool {
	t.Helper()
	require.Equal(t, len(want), len(got), ctx+": length")
	ok := true
	for i := range want {
		if !assertF32(t, want[i], got[i], fmt.Sprintf("%s[%d]", ctx, i)) {
			ok = false
		}
	}
	return ok
}

// TestVbrpsyMultigranParity drives the genuine static L3psycho_anal_vbr and the
// pure-Go nativemp3.L3psychoAnalVbr over the SAME first-frame mfbuf, granule by
// granule, and asserts every per-granule output (tot_ener, pe, pe_MS, and the
// per-band en/thm of masking_LR / masking_MS for L,R,M,S) is bit-for-bit equal.
// This is the multi-granule parity gate the vbr-encode-e2e divergence demanded.
//
// It is GREEN for 44.1k/48k (mono + stereo): the granule-1 "near-silence" bug —
// a skipped psymodel init (cd_psy pre-allocated -> psymodel_init's idempotency
// guard returned early, leaving sv_psy zero) plus a cascade of double-vs-float /
// FMA-fusion init+masking precision bugs — is fixed in internal/nativemp3. The
// 32k case is skip-guarded for a SEPARATE <44kHz presets-config divergence (see
// the per-case skip), which is not the psymodel.
func TestVbrpsyMultigranParity(t *testing.T) {
	requireStrict(t)

	cases := []struct {
		name       string
		samplerate int
		channels   int
		nsamplesCh int
		seed       uint32
	}{
		{"44k1_stereo_short", 44100, 2, 8 * 1152, 1},
		{"44k1_stereo_long", 44100, 2, 64 * 1152, 7},
		{"44k1_mono", 44100, 1, 32 * 1152, 3},
		{"48k_stereo", 48000, 2, 48 * 1152, 5},
		{"32k_stereo", 32000, 2, 40 * 1152, 9},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cgoRun(tc.samplerate, tc.channels, tc.nsamplesCh, tc.seed)
			require.NotNil(t, c, "cgo psymodel setup failed")
			defer c.free()

			mf0 := c.mfbuf(0)
			require.NotEmpty(t, mf0, "oracle produced no mfbuf")
			var mf1 []float32
			if c.channelsOut() == 2 {
				mf1 = c.mfbuf(1)
				require.NotEmpty(t, mf1)
			}

			g := nativeRun(tc.samplerate, tc.channels, mf0, mf1)
			require.NotNil(t, g, "native psymodel run failed")
			require.Equal(t, c.nChnPsy(), g.nChnPsy, "n_chn_psy")

			for gr := 0; gr < 2; gr++ {
				assertSlice(t, c.energy(gr), g.energy[gr][:], fmt.Sprintf("gr%d tot_ener", gr))
				assertSlice(t, c.pe(gr), g.pe[gr][:], fmt.Sprintf("gr%d pe", gr))
				assertSlice(t, c.peMS(gr), g.peMS[gr][:], fmt.Sprintf("gr%d pe_MS", gr))

				for ch := 0; ch < 2; ch++ {
					for which, wname := range []string{"LR", "MS"} {
						assertSlice(t, c.enL(which, gr, ch), g.enL[which][gr][ch][:],
							fmt.Sprintf("gr%d ch%d %s en_l", gr, ch, wname))
						assertSlice(t, c.thmL(which, gr, ch), g.thmL[which][gr][ch][:],
							fmt.Sprintf("gr%d ch%d %s thm_l", gr, ch, wname))
						assertSlice(t, c.enS(which, gr, ch), g.enS[which][gr][ch][:],
							fmt.Sprintf("gr%d ch%d %s en_s", gr, ch, wname))
						assertSlice(t, c.thmS(which, gr, ch), g.thmS[which][gr][ch][:],
							fmt.Sprintf("gr%d ch%d %s thm_s", gr, ch, wname))
					}
				}
			}
		})
	}
}
