//go:build cgo

/* Compiles a private copy of the vendored minimp3 reference and surfaces the
 * static Layer III IMDCT and polyphase-synthesis-filterbank routines
 * (L3_imdct_gr, L3_change_sign, mp3d_DCT_II, mp3d_synth_granule and the
 * helpers they call) to Go-cgo via mp3parity_* trampolines.
 *
 * minimp3 is a single-header library: every function in the
 * imdct-synthesis-filterbank slice (L3_dct3_9, L3_imdct36, L3_idct3,
 * L3_imdct12, L3_imdct_short, L3_change_sign, L3_imdct_gr, mp3d_DCT_II,
 * mp3d_scale_pcm, mp3d_synth_pair, mp3d_synth, mp3d_synth_granule) is `static`
 * inside minimp3.h, so they can only be reached from a translation unit that
 * #includes the implementation. We therefore define MINIMP3_IMPLEMENTATION
 * here and wrap the two slice driver functions (L3_imdct_gr + L3_change_sign,
 * and mp3d_synth_granule) plus mp3d_DCT_II behind stable, non-static linkage
 * names. Driving the drivers exercises every helper transitively, so the
 * parity suite pins the whole slice end-to-end rather than each static in
 * isolation. Each go-test binary is one package wide, so this private minimp3
 * copy never collides with the one libraries/mp3 compiles for production (and
 * this package never imports libraries/mp3, which would compile minimp3 a
 * second time and clash on its statics).
 *
 * MINIMP3_NO_SIMD keeps the reference on its scalar baseline so the C side
 * runs the same scalar branches of mp3d_DCT_II / mp3d_synth the Go port
 * translates (minimp3's SIMD fast paths are guarded by HAVE_SIMD). This is a
 * floating-point slice, so the scalar FP flags (-ffp-contract=off,
 * -fno-vectorize, -fno-slp-vectorize, -fno-unroll-loops) matter: they come
 * from the mise task env (CGO_CFLAGS + CGO_CFLAGS_ALLOW), never from the
 * in-source #cgo block, because Go's cgo flag allowlist rejects them.
 *
 * mp3d_sample_t is the default int16_t (no MINIMP3_FLOAT_OUTPUT), matching the
 * Go port's int16 PCM output.
 */

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"

/* ---- L3_imdct_gr + L3_change_sign ----
 *
 * mp3parity_l3_imdct_gr runs the per-granule inverse MDCT in place over a
 * channel's 32x18 grbuf, reading and updating the 9*32 overlap history, then
 * applies L3_change_sign (the frequency inversion the decode loop performs
 * immediately after, minimp3.h:1269-1270). Surfacing both behind one
 * trampoline pins the slice exactly as the real decoder drives it.
 *
 * grbuf aliases a 576-float channel buffer; overlap aliases the 9*32-float
 * mdct_overlap[ch] history. Both are mutated in place; the caller owns them.
 */
void mp3parity_l3_imdct_gr(float *grbuf, float *overlap, unsigned block_type, unsigned n_long_bands) {
    L3_imdct_gr(grbuf, overlap, block_type, n_long_bands);
    L3_change_sign(grbuf);
}

/* mp3parity_l3_change_sign exposes L3_change_sign alone (the frequency
 * inversion of every odd line of every other subband) so the suite can pin it
 * in isolation as well as folded into the imdct driver above. */
void mp3parity_l3_change_sign(float *grbuf) {
    L3_change_sign(grbuf);
}

/* ---- mp3d_DCT_II ----
 *
 * The 32-point DCT-II run per row of the polyphase synthesis. Pinned standalone
 * so its scalar reference branch is asserted directly over crafted columns,
 * independently of the synthesis windowing that consumes its output.
 *
 * grbuf aliases an n-wide x 32-deep (stride-18) column block; mutated in place.
 */
void mp3parity_mp3d_dct_ii(float *grbuf, int n) {
    mp3d_DCT_II(grbuf, n);
}

/* ---- mp3d_synth_granule ----
 *
 * The full per-granule synthesis filterbank: per-channel DCT-II, seed the lins
 * scratch from the carried qmf_state, run the windowed overlap-add synthesis
 * for each subband pair (emitting interleaved int16 PCM), then save the lins
 * tail back into qmf_state. Driving this pins mp3d_DCT_II, mp3d_synth,
 * mp3d_synth_pair and mp3d_scale_pcm together, exactly as mp3dec_decode_frame
 * calls it (minimp3.h:1774 / :1793).
 *
 * qmf_state aliases the 15*2*32-float cross-granule history (mutated);
 * grbuf aliases nch channels of 576 floats; pcm receives 32*nbands*nch
 * interleaved int16 samples; lins aliases the (18+15)*64-float syn scratch.
 * The caller owns and sizes all four.
 */
void mp3parity_mp3d_synth_granule(float *qmf_state, float *grbuf, int nbands, int nch,
                                  int16_t *pcm, float *lins) {
    mp3d_synth_granule(qmf_state, grbuf, nbands, nch, pcm, lins);
}
