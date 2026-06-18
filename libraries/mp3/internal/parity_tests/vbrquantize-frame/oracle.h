/*
 * oracle.h — C oracle surface for the vbrquantize-frame parity slice.
 *
 * This slice pins the TOP tier of LAME 3.100's VBR (vbr_mtrh / -V) quantizer
 * translation unit libmp3lame/vbrquantize.c — the global-stepsize / distribution
 * bit-search orchestration and the whole-frame driver VBR_encode_frame:
 *
 *   - tryGlobalStepsize       (vbrquantize.c:1011, static)
 *   - searchGlobalStepsizeMax (vbrquantize.c:1040, static)
 *   - sfDepth                 (vbrquantize.c:1074, static)
 *   - cutDistribution         (vbrquantize.c:1093, static)
 *   - flattenDistribution     (vbrquantize.c:1104, static)
 *   - tryThatOne              (vbrquantize.c:1140, static)
 *   - outOfBitsStrategy       (vbrquantize.c:1154, static)
 *   - reduce_bit_usage        (vbrquantize.c:1231, static)
 *   - VBR_encode_frame        (vbrquantize.c:1254, EXTERN — the entry point)
 *
 * Unlike the vbrquantize-sfalloc slice (which stubbed out the bit-counting and
 * the best_* finishers because it pinned only the allocation outputs), this
 * slice DOES drive the full pipeline: VBR_encode_frame runs block_sf + the
 * allocator, quantizeAndCountBits (-> quantize_x34 + noquant_count_bits),
 * bitcount (-> scale_bitcount) and reduce_bit_usage (-> best_scalefac_store +
 * best_huffman_divide), and its per-granule scalefactors / quantized spectrum /
 * bit usage are the parity target. So oracle.c #includes BOTH the genuine
 * libmp3lame/vbrquantize.c (the VBR drivers) AND libmp3lame/takehiro.c +
 * libmp3lame/tables.c (the genuine bit-counting machinery the drivers call) into
 * one translation unit — every assertion is against vendored LAME code, not a
 * reimplementation. The two .c files define disjoint non-static symbols, so a
 * single TU links cleanly; the shared precompute tables (pretab / pow* / adj43*)
 * are defined once here and filled with the verbatim iteration_init loop, and
 * huffman_init populates sv_qnt.bv_scf + choose_table the bit-counter reads.
 *
 * HANDLE. VBR_encode_frame operates over a whole lame_internal_flags (cfg + the
 * 2x2 l3_side.tt gr_info grid + scalefac_band + sv_qnt). The oracle calloc's a
 * zeroed gfc and exposes setters for the cfg fields (mode_gr, channels_out,
 * noise_shaping, full_outer_loop, use_best_huffman) and per-[gr][ch] gr_info
 * geometry (block_type, width, window, sfbmax, sfbdivide, psymax,
 * max_nonzero_coeff, energy_above_cutoff, xr, xrpow_max), plus the scalefac_band
 * tables and a huffman_init call. VBR_encode_frame is then driven with the
 * per-[gr][ch] xr34orig / l3_xmin and the per-[gr][ch] max_bits budget. The
 * resolved side info (global_gain / scalefac[] / subblock_gain[] / scalefac_scale
 * / preflag / l3_enc[] / part2_3_length / part2_length / scalefac_compress /
 * table_select / region counts) and the function's bit-usage return value are
 * read back for the assertion. The Go side mirrors this with a
 * LameInternalFlags + nativemp3.VBRencodeFrame (parityhooks_vbrquantize_frame.go).
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS (cgo.go).
 * The FP-determinism flags come from the mise task env. This tier is mostly
 * integer, but the 'as is' quantize + the out-of-budget bit redistribution carry
 * the same float32 products as the allocation tier, so the slice is FP-bearing
 * and bit-exact only under mp3_strict (FMA-free Go vs the -ffp-contract=off
 * oracle).
 *
 * LGPL note: vbrquantize.c / takehiro.c / tables.c are LGPL LAME source, so this
 * oracle TU and the Go port it pins are gated by the mp3lame build tag (in
 * addition to cgo). A bare `go test` never compiles them.
 */
#ifndef MP3_VBRQUANTIZE_FRAME_ORACLE_H
#define MP3_VBRQUANTIZE_FRAME_ORACLE_H

typedef struct mp3parity_vbrfr_t mp3parity_vbrfr_t;

/* oracle_fill_tables fills pow20 / ipow20 / pow43 / adj43asm via the verbatim
 * iteration_init table-fill loop (TAKEHIRO_IEEE754_HACK branch). Idempotent. */
void oracle_fill_tables(void);

/* ---- handle lifecycle ---- */
mp3parity_vbrfr_t *mp3parity_vbrfr_new(void);
void               mp3parity_vbrfr_free(mp3parity_vbrfr_t *h);

/* ---- cfg setters ---- */
void mp3parity_vbrfr_set_cfg(mp3parity_vbrfr_t *h, int mode_gr, int channels_out,
                             int noise_shaping, int full_outer_loop, int use_best_huffman);

/* ---- scalefac_band tables + huffman_init ---- */
void mp3parity_vbrfr_set_sfb_long(mp3parity_vbrfr_t *h, const int *l, int n);
void mp3parity_vbrfr_set_sfb_short(mp3parity_vbrfr_t *h, const int *s, int n);
void mp3parity_vbrfr_huffman_init(mp3parity_vbrfr_t *h);

/* ---- per-[gr][ch] gr_info input setters ---- */
void mp3parity_vbrfr_set_xr(mp3parity_vbrfr_t *h, int gr, int ch, const float *xr, int n);
void mp3parity_vbrfr_set_width(mp3parity_vbrfr_t *h, int gr, int ch, const int *w, int n);
void mp3parity_vbrfr_set_window(mp3parity_vbrfr_t *h, int gr, int ch, const int *win, int n);
void mp3parity_vbrfr_set_eac(mp3parity_vbrfr_t *h, int gr, int ch, const char *eac, int n);
void mp3parity_vbrfr_set_geom(mp3parity_vbrfr_t *h, int gr, int ch, int block_type,
                              int mixed_block_flag, int sfbmax, int sfbdivide, int psymax,
                              int max_nonzero_coeff, float xrpow_max);

/* ---- the driver ----
 *
 * VBR_encode_frame over the handle. xr34orig is the 2*2*576 |xr|^(3/4) grid,
 * l3_xmin the 2*2*SFBMAX per-band allowed distortion, max_bits the 2*2 budget.
 * Returns the function's bit-usage result. */
int mp3parity_vbrfr_encode(mp3parity_vbrfr_t *h, const float *xr34orig /*2*2*576*/,
                           const float *l3_xmin /*2*2*SFBMAX*/, const int *max_bits /*2*2*/);

/* ---- per-[gr][ch] gr_info output getters ---- */
int mp3parity_vbrfr_global_gain(const mp3parity_vbrfr_t *h, int gr, int ch);
int mp3parity_vbrfr_scalefac_scale(const mp3parity_vbrfr_t *h, int gr, int ch);
int mp3parity_vbrfr_preflag(const mp3parity_vbrfr_t *h, int gr, int ch);
int mp3parity_vbrfr_scalefac(const mp3parity_vbrfr_t *h, int gr, int ch, int sfb);
int mp3parity_vbrfr_subblock_gain(const mp3parity_vbrfr_t *h, int gr, int ch, int i);
int mp3parity_vbrfr_l3enc(const mp3parity_vbrfr_t *h, int gr, int ch, int i);
int mp3parity_vbrfr_part2_3_length(const mp3parity_vbrfr_t *h, int gr, int ch);
int mp3parity_vbrfr_part2_length(const mp3parity_vbrfr_t *h, int gr, int ch);
int mp3parity_vbrfr_scalefac_compress(const mp3parity_vbrfr_t *h, int gr, int ch);
int mp3parity_vbrfr_big_values(const mp3parity_vbrfr_t *h, int gr, int ch);
int mp3parity_vbrfr_table_select(const mp3parity_vbrfr_t *h, int gr, int ch, int i);
int mp3parity_vbrfr_region0_count(const mp3parity_vbrfr_t *h, int gr, int ch);
int mp3parity_vbrfr_region1_count(const mp3parity_vbrfr_t *h, int gr, int ch);
int mp3parity_vbrfr_scfsi(const mp3parity_vbrfr_t *h, int ch, int band);

#endif /* MP3_VBRQUANTIZE_FRAME_ORACLE_H */
