/*
 * oracle.h — C oracle surface for the vbrquantize-sfalloc parity slice.
 *
 * This slice pins the scalefactor-ALLOCATION tier of LAME 3.100's VBR
 * (vbr_mtrh / -V) quantizer translation unit libmp3lame/vbrquantize.c — the
 * functions that turn the per-band leaf-kernel scalefactor estimates into a
 * granule's final side information and count its bits:
 *
 *   - block_sf            (vbrquantize.c:394, static) — per-band sf survey:
 *                          fills vbrsf / vbrsfmin and the mingain floors, returns
 *                          vbrmax, calling the find dispatch per band.
 *   - quantize_x34        (vbrquantize.c:500, static) — quantize xr34 -> l3_enc
 *                          by the resolved scalefactors (k_34_4 magic-float).
 *   - set_subblock_gain   (vbrquantize.c:595, static) — short-block subblock_gain.
 *   - set_scalefacs       (vbrquantize.c:688, static) — scalefac[] from sf deltas.
 *   - checkScalefactor    (vbrquantize.c:732) — over-amplification predicate.
 *   - short_block_constrain (vbrquantize.c:769, static) — short-block allocator.
 *   - long_block_constrain  (vbrquantize.c:847, static) — long-block allocator.
 *
 * Each is file-static inside vbrquantize.c, so — per the parity discipline in
 * CONTRIBUTING.md — oracle.c #includes the committed
 * libmp3lame/vbrquantize.c directly (bringing the statics into scope) and
 * re-exports them through thin mp3parity_vbrsf_* trampolines. The C side of every
 * assertion is the genuine vendored LAME code, not a reimplementation.
 *
 * HANDLE. The allocation tier operates on a gr_info inside a lame_internal_flags
 * (the algo_t carries gfc + cod_info + the find dispatch). The oracle calloc's a
 * zeroed gfc and a handle exposes setters for the gr_info / cfg fields these
 * routines read (xr, width, energy_above_cutoff, max_nonzero_coeff, psymax,
 * sfbmax, scalefac_scale, preflag, global_gain, subblock_gain, window) and
 * getters for the side-info they write (global_gain, scalefac[], subblock_gain[],
 * scalefac_scale, preflag, l3_enc[]). The Go side mirrors this with a GrInfo in a
 * LameInternalFlags (parityhooks_vbrquantize_sfalloc.go).
 *
 * TABLE FILL. block_sf / quantize_x34 read pow20 / ipow20 / pow43 / adj43asm via
 * the find dispatch and k_34_4; oracle.c fills them with the verbatim
 * iteration_init table-fill loop (TAKEHIRO_IEEE754_HACK branch), as the
 * vbrquantize-leaf oracle does. The Go side runs the identical loop
 * (nativemp3.FillVbrQuantizeTables).
 *
 * FIND DISPATCH. block_sf calls that->find. The handle sets that->find to the
 * genuine find_scalefac_x34 (full) or guess_scalefac_x34 (guess) per a selector,
 * matching VBR_encode_frame's `find = (full_outer_loop < 0) ? guess : find`.
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS (cgo.go).
 * The FP-determinism flags come from the mise task env. This tier is mostly
 * integer (the allocators are bit-identical in any build), but block_sf reads the
 * float leaf results and quantize_x34 does the same float32 product + magic-float
 * quantize, so the slice is FP-bearing and bit-exact only under mp3_strict.
 *
 * LGPL note: vbrquantize.c is LGPL LAME source, so this oracle TU and the Go port
 * it pins are gated by the mp3lame build tag (in addition to cgo). A bare
 * `go test` never compiles them.
 */
#ifndef MP3_VBRQUANTIZE_SFALLOC_ORACLE_H
#define MP3_VBRQUANTIZE_SFALLOC_ORACLE_H

typedef struct mp3parity_vbrsf_t mp3parity_vbrsf_t;

/* oracle_fill_tables fills pow20 / ipow20 / pow43 / adj43asm via the verbatim
 * iteration_init table-fill loop (TAKEHIRO_IEEE754_HACK branch). Idempotent. */
void oracle_fill_tables(void);

/* ---- handle lifecycle ---- */
mp3parity_vbrsf_t *mp3parity_vbrsf_new(void);
void               mp3parity_vbrsf_free(mp3parity_vbrsf_t *h);

/* ---- cfg setters ---- */
void mp3parity_vbrsf_set_cfg(mp3parity_vbrsf_t *h, int mode_gr, int noise_shaping);

/* ---- gr_info input setters ---- */
void mp3parity_vbrsf_set_xr(mp3parity_vbrsf_t *h, const float *xr, int n);
void mp3parity_vbrsf_set_width(mp3parity_vbrsf_t *h, const int *w, int n);
void mp3parity_vbrsf_set_window(mp3parity_vbrsf_t *h, const int *win, int n);
void mp3parity_vbrsf_set_eac(mp3parity_vbrsf_t *h, const char *eac, int n);
void mp3parity_vbrsf_set_scalefac(mp3parity_vbrsf_t *h, const int *sf, int n);
void mp3parity_vbrsf_set_subblock_gain(mp3parity_vbrsf_t *h, const int *sbg, int n);
void mp3parity_vbrsf_set_geom(mp3parity_vbrsf_t *h, int block_type, int global_gain,
                              int scalefac_scale, int preflag, int sfbmax, int psymax,
                              int max_nonzero_coeff);

/* ---- gr_info output getters ---- */
int mp3parity_vbrsf_global_gain(const mp3parity_vbrsf_t *h);
int mp3parity_vbrsf_scalefac_scale(const mp3parity_vbrsf_t *h);
int mp3parity_vbrsf_preflag(const mp3parity_vbrsf_t *h);
int mp3parity_vbrsf_scalefac(const mp3parity_vbrsf_t *h, int sfb);
int mp3parity_vbrsf_subblock_gain(const mp3parity_vbrsf_t *h, int i);
int mp3parity_vbrsf_l3enc(const mp3parity_vbrsf_t *h, int i);

/* ---- trampolines ----
 *
 * block_sf: fills vbrsf[SFBMAX] / vbrsfmin[SFBMAX], returns vbrmax, and exports
 * the mingain floors via out-params. find_sel: 0 = guess_scalefac_x34, 1 =
 * find_scalefac_x34. l3_xmin is the SFBMAX per-band allowed distortion. xr34orig
 * is the granule's |xr|^(3/4) (576). */
int mp3parity_vbrsf_block_sf(mp3parity_vbrsf_t *h, const float *xr34orig,
                             const float *l3_xmin, int find_sel,
                             int *vbrsf /*SFBMAX*/, int *vbrsfmin /*SFBMAX*/,
                             int *mingain_l, int *mingain_s /*3*/);

/* quantize_x34: quantizes xr34orig into l3_enc by the gr_info's resolved
 * scalefactors. Read l3_enc back with mp3parity_vbrsf_l3enc. */
void mp3parity_vbrsf_quantize_x34(mp3parity_vbrsf_t *h, const float *xr34orig);

/* set_subblock_gain (the kernel): sf[SFBMAX] updated in place; subblock_gain /
 * global_gain mutated. Named *_run_* to avoid colliding with the gr_info input
 * setter mp3parity_vbrsf_set_subblock_gain above. */
void mp3parity_vbrsf_run_set_subblock_gain(mp3parity_vbrsf_t *h, const int *mingain_s /*3*/,
                                           int *sf /*SFBMAX*/);

/* set_scalefacs: max_range_sel 0=short 1=long 2=long_lsf_pretab. sf[SFBMAX]
 * updated in place; scalefac filled. */
void mp3parity_vbrsf_set_scalefacs(mp3parity_vbrsf_t *h, const int *vbrsfmin /*SFBMAX*/,
                                   int *sf /*SFBMAX*/, int max_range_sel);

/* checkScalefactor: 1 iff no band over-amplified vs vbrsfmin. */
int mp3parity_vbrsf_check_scalefactor(mp3parity_vbrsf_t *h, const int *vbrsfmin /*SFBMAX*/);

/* short_block_constrain / long_block_constrain: resolve the gr_info side info
 * from the block_sf survey. mingain_l / mingain_s seed the algo_t floors. */
void mp3parity_vbrsf_short_constrain(mp3parity_vbrsf_t *h, const int *vbrsf /*SFBMAX*/,
                                     const int *vbrsfmin /*SFBMAX*/, int vbrmax,
                                     int mingain_l, const int *mingain_s /*3*/);
void mp3parity_vbrsf_long_constrain(mp3parity_vbrsf_t *h, const int *vbrsf /*SFBMAX*/,
                                    const int *vbrsfmin /*SFBMAX*/, int vbrmax,
                                    int mingain_l, const int *mingain_s /*3*/);

#endif /* MP3_VBRQUANTIZE_SFALLOC_ORACLE_H */
