//go:build cgo

/* Compiles a private copy of the vendored minimp3 reference and surfaces the
 * static Layer III stereo-reconstruction helpers (L3_midside_stereo,
 * L3_intensity_stereo_band, L3_stereo_top_band, L3_stereo_process,
 * L3_intensity_stereo) to Go-cgo via mp3parity_* trampolines.
 *
 * minimp3 is a single-header library: all five functions are `static` inside
 * minimp3.h (minimp3.h:879..993), so they can only be reached from a
 * translation unit that #includes the implementation. We therefore define
 * MINIMP3_IMPLEMENTATION here and wrap each behind a stable, non-static
 * linkage name. Each go-test binary is one package wide, so this private
 * minimp3 copy never collides with the one libraries/mp3 compiles for
 * production (and this package never imports libraries/mp3, which would
 * compile minimp3 a second time and clash on its statics).
 *
 * MINIMP3_NO_SIMD keeps the reference on its scalar baseline. The stereo
 * helpers' arithmetic is floating point (the a+b / a-b mid/side sums, the kl /
 * kr intensity weights and their L3_ldexp_q2 / g_pan derivation), so the
 * scalar FP flags (-ffp-contract=off, -fno-vectorize, -fno-slp-vectorize,
 * -fno-unroll-loops) matter here. They come from the mise task env (CGO_CFLAGS
 * + CGO_CFLAGS_ALLOW), never from the in-source #cgo block, because Go's cgo
 * flag allowlist rejects them.
 */

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"

#include <string.h>

/* ---- L3_midside_stereo ----
 *
 * In-place mid/side reconstruction over the first n samples of the granule
 * buffer `left` (left channel at left[0..575], right at left[576..1151]).
 */
void mp3parity_l3_midside_stereo(float *left, int n) {
    L3_midside_stereo(left, n);
}

/* ---- L3_intensity_stereo_band ----
 *
 * Applies one intensity-stereo band: writes the right channel from the
 * original left value scaled by kr, then overwrites the left channel scaled by
 * kl, for the first n samples of `left`.
 */
void mp3parity_l3_intensity_stereo_band(float *left, int n, float kl, float kr) {
    L3_intensity_stereo_band(left, n, kl, kr);
}

/* ---- L3_stereo_top_band ----
 *
 * Locates, per short-block window (i % 3), the highest scalefactor band whose
 * right-channel content is non-zero. `right` points at the right channel
 * (i.e. the caller passes grbuf + 576); sfb is the band-width table; nbands is
 * the band count; max_band[3] receives the per-window result.
 */
void mp3parity_l3_stereo_top_band(const float *right, const uint8_t *sfb, int nbands, int max_band[3]) {
    L3_stereo_top_band(right, sfb, nbands, max_band);
}

/* ---- L3_stereo_process ----
 *
 * Drives the band-by-band stereo reconstruction over the granule buffer
 * `left`: intensity weighting above each window's top non-zero band, else
 * mid/side when MS stereo is active. hdr is the raw 4-byte MPEG frame header
 * (only the HDR_TEST_MPEG1 / HDR_TEST_MS_STEREO bits are consulted); mpeg2_sh
 * is gr[1].scalefac_compress & 1. max_band is taken by value via a copy
 * because the C signature mutates nothing in it.
 */
void mp3parity_l3_stereo_process(float *left, const uint8_t *ist_pos, const uint8_t *sfb,
                                 const uint8_t *hdr, const int max_band[3], int mpeg2_sh) {
    int mb[3] = { max_band[0], max_band[1], max_band[2] };
    L3_stereo_process(left, ist_pos, sfb, hdr, mb, mpeg2_sh);
}

/* ---- L3_intensity_stereo ----
 *
 * The full per-granule stereo driver. Reassembles the discrete L3_gr_info_t
 * fields the Go side mirrors (sfbtab, n_long_sfb, n_short_sfb) into gr[0] and
 * carries gr[1].scalefac_compress so L3_stereo_process's mpeg2_sh derivation
 * matches. ist_pos is mutated in place (the trailing-position fixup), so the
 * caller surfaces the final ist_pos back out for comparison.
 */
void mp3parity_l3_intensity_stereo(float *left, uint8_t *ist_pos,
                                   const uint8_t *sfbtab,
                                   uint8_t n_long_sfb, uint8_t n_short_sfb,
                                   uint16_t gr1_scalefac_compress,
                                   const uint8_t *hdr) {
    L3_gr_info_t gr[2];
    memset(gr, 0, sizeof(gr));
    gr[0].sfbtab       = sfbtab;
    gr[0].n_long_sfb   = n_long_sfb;
    gr[0].n_short_sfb  = n_short_sfb;
    gr[1].scalefac_compress = gr1_scalefac_compress;

    L3_intensity_stereo(left, ist_pos, gr, hdr);
}
