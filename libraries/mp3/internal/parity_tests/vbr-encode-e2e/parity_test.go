// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbre2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVBRStreamByteIdentical is the top-level byte-identical gate for the pure-Go
// LAME VBR (-V2 / vbr_mtrh) encoder. For each case the C oracle drives a genuine
// end-to-end -V2 encode through the vendored libmp3lame, generates the synthetic
// int16 PCM, and assembles the file-layout stream (placeholder Xing/Info tag
// frame overwritten by the finalized lame_get_lametag_frame). The Go side encodes
// the SAME int16 PCM through the pure-Go nativemp3 encoder and assembles the
// identical splice. The two FULL streams — finalized tag frame plus every audio
// frame — must match byte-for-byte.
//
// Because the -V2 encode is heavily FP-bearing, the assertion holds only under
// the mp3_strict build (FMA-free Go) vs the -ffp-contract=off oracle (flags from
// the mise task env). A bare `go test` does not target bit-exactness.
//
// PRESET TUNING NOW CONVERGES — the apply_vbr_preset port (nativemp3/presets.go,
// installed through FrameEncodeStages.ApplyPreset) plus the lame_init_params_ppflt
// port (nativemp3/ppflt.go, installed through FrameEncodeStages.InitParamsPpflt)
// make the pure-Go SessionConfig byte-for-byte identical to the -V2 oracle's:
// quant_comp/quant_comp_short = 9, ATHtype = 5, ATHcurve = 2, minval = 5,
// ATHfixpoint = 97, msfix = 1.288, ATH_offset_db = -3.7, mask_adjust = -2.6,
// use_safe_joint = 2, adjust_sfb21_db = 5.75, attackthre = 4.2/25, AND the
// band-quantized lowpass1/lowpass2 (0.846774 / 0.870968) + amp_filter[32] the
// polyphase filter design installs. Verified equal against an instrumented oracle.
//
// PSYMODEL MULTI-GRANULE BUG — FIXED. The granule-1 "near-silence" divergence this
// comment used to describe (gr0 pe=281/tot_ener=0, gr1 pe≈309/tot_ener=0 vs the
// oracle's gr1 pe≈3960/tot_ener≈2.3e13) was a missing psymodel init: NewEncoderContext
// pre-allocated gfc.cd_psy, which tripped psymodel_init's `if (cd_psy != 0) return 0`
// idempotency guard and skipped the ENTIRE psymodel init (the 1e20 en/thm/nb seed, the
// numline/bval/mld/s3 tables, the attack thresholds and ATH curve), leaving sv_psy zero
// so every granule read silence. Fixed by NOT pre-allocating cd_psy (encode_driver.go),
// plus a cascade of FP-precision fixes in the now-running psymodel init/masking that the
// freshly-exercised path exposed (freq2bark / s3_func / SNR-norm / minval / ATH-cb double
// vs float, the bo_weight FMA barrier, the pecalc double accumulation, fastLog10X). The
// new vbrpsy-multigran parity slice drives the genuine L3psycho_anal_vbr and the pure-Go
// vbrpsy over a SHARED mfbuf and is now BIT-EXACT for 44.1k/48k (mono + stereo), and the
// real streaming encoder's first-frame mfbuf is byte-identical to the oracle's — so the
// first-frame psymodel in the real encode is bit-exact.
//
// REMAINING DIVERGENCES — RESOLVED. The byte-identical -V2 gate is now GREEN for
// mono + stereo at 44.1k / 48k / 32k. The three residuals this comment used to list
// were each a 1:1-port bug, now fixed:
//
//  1. adj43asm uninitialized in the real encode path. iteration_init filled
//     pow43/adj43/ipow20/pow20 (InitQuantizePvtTables) but NOT adj43asm — the
//     TAKEHIRO_IEEE754_HACK rounding table the VBR k_34_4 (vqHackQuantize) reads.
//     The C fills it in the SAME init block (quantize_pvt.c:355-358); the port only
//     filled it via a parity hook, so the real VBR scalefactor noise search quantized
//     against a zero adj43asm and every -V granule's global_gain / scalefactors drifted.
//     Fixed by calling InitVbrQuantizeTables from iterationInit (iteration_init.go).
//
//  2. buffer_constraint mis-sized. The native LameGlobalFlags seeded strict_ISO = 511
//     as "MDB_MAXIMUM", but MDB_MAXIMUM == 2 (lame.h:141). 511 matched no case in
//     get_max_frame_buffer_size_by_constraint, so it fell to MDB_DEFAULT (8*1440)
//     instead of 7680*(version+1); that shrank ResvMax and skewed the per-frame VBR
//     bitrate choice. Fixed by seeding strict_ISO = 2 (native_encoder.go + the slice
//     configs), matching lame_init_old (lame.c:2398, strict_ISO = MDB_MAXIMUM).
//
//  3. the <44kHz preset config (ATHcurve 1.76 / msfix 1.2216 at 32k). The lame.c:677
//     "WORK IN PROGRESS" q-map that remaps VBR_q/VBR_q_frac per input sample rate (and
//     the optimum_samplefreq output-rate derivation it feeds) was stubbed; the configs
//     also pre-set samplerate_out, skipping the q-map entirely. Fixed by porting the
//     q-map + optimum_samplefreq (init.go / stages.go) and leaving samplerate_out unset
//     for the VBR-new modes so lame_init_params derives it, as the genuine -V2 path does.
//
// The byte-identical assertion below was never loosened.
func TestVBRStreamByteIdentical(t *testing.T) {
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
			require.NotNil(t, c, "cgo -V2 encode failed")
			defer c.free()

			pcm := c.pcm()
			require.NotEmpty(t, pcm, "oracle produced no PCM")
			require.Len(t, pcm, tc.nsamplesCh*tc.channels)

			golden := c.goldenStream()
			require.NotEmpty(t, golden, "oracle produced no stream")
			cTagLen := c.tagLen()

			got, goTagLen := nativeStream(tc.samplerate, tc.channels, pcm)
			require.NotEmpty(t, got, "native produced no stream")

			// Same tag frame size first — a mismatch here means the placeholder /
			// finalized tag sizing diverged before the audio is even compared.
			assert.Equal(t, cTagLen, goTagLen, "tag frame length")

			// Same total length next — a length mismatch localizes a frame
			// count / reservoir / padding divergence.
			require.Equal(t, len(golden), len(got), "stream length")

			if assert.Equal(t, golden, got, "VBR -V2 stream not byte-identical") {
				return
			}

			// Localize the first differing byte and which region it falls in.
			n := len(golden)
			if len(got) < n {
				n = len(got)
			}
			for i := 0; i < n; i++ {
				if golden[i] != got[i] {
					region := "audio"
					if i < cTagLen {
						region = "tag"
					}
					t.Fatalf("first byte divergence at offset %d (region=%s, tagLen=%d): golden=0x%02x got=0x%02x",
						i, region, cTagLen, golden[i], got[i])
				}
			}
		})
	}
}
