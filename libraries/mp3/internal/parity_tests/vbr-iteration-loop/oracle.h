/*
 * oracle.h — C oracle surface for the vbr-iteration-loop parity slice.
 *
 * This slice pins the per-frame VBR iteration DRIVERS in LAME 3.100's
 * libmp3lame/quantize.c — the loops that turn a frame's MDCT lines + masking
 * thresholds into resolved side info and a chosen bitrate:
 *
 *   - VBR_old_prepare        (quantize.c:1389, static)
 *   - bitpressure_strategy   (quantize.c:1453, static)
 *   - VBR_old_iteration_loop (quantize.c:1490, EXTERN — vbr_rh entry)
 *   - VBR_encode_granule     (quantize.c:1244, static)
 *   - VBR_new_prepare        (quantize.c:1582, static)
 *   - VBR_new_iteration_loop (quantize.c:1645, EXTERN — vbr_mtrh / -V entry)
 *
 * The Go port lives in nativemp3/quantize_encode_vbr.go; the loops are driven
 * via FrameEncodeStages (stages.go). This oracle drives the GENUINE vendored
 * VBR_{new,old}_iteration_loop over a heap lame_internal_flags whose cfg /
 * sv_qnt / ATH / reservoir / scalefac_band / l3_side geometry + the per-(gr,ch)
 * MDCT lines (gr_info.xr) and psy ratio are set from the same Go-test values the
 * Go side sets, then reads back the resolved per-(gr,ch) side info, the chosen
 * eov->bitrate_index and the reservoir state (sv_enc.ResvSize) — the exact frame
 * output the byte-identical -V2 bitstream depends on.
 *
 * TRANSLATION UNITS (parity discipline — each go-test binary compiles its OWN
 * copy of the reference; see CONTRIBUTING.md):
 *
 *   oracle.c            — VBR_*_iteration_loop and its callees, by #including
 *                         libmp3lame/quantize.c + quantize_pvt.c + reservoir.c
 *                         (none carry the takehiro fi_union/MAGIC statics, so
 *                         they share one TU). quantize_pvt.c DEFINES the
 *                         precompute tables (pow43/adj43asm/pow20/ipow20/pretab/
 *                         nr_of_sfb_block); the other TUs reference them extern.
 *                         getframebits / calcFrameLength (bitstream.c) are tiny
 *                         and pulling bitstream.c in would drag the whole huffman
 *                         emitter + util.c tree, so they are reproduced VERBATIM
 *                         here as documented hand-twins (their only job for the
 *                         loop is frame-length arithmetic over the bitrate_table,
 *                         which the Go getframebits/calcFrameLength port mirrors).
 *                         ATHformula (util.c) is referenced only by the unreached
 *                         compute_ath/ATHmdct in quantize_pvt.c (the loop never
 *                         calls them); it is stubbed so the TU links.
 *   oracle_vbrquantize.c — libmp3lame/vbrquantize.c (VBR_encode_frame + its
 *                         file-static fi_union/MAGIC bit-search drivers).
 *   oracle_takehiro.c   — libmp3lame/takehiro.c + tables.c (the bit-counter +
 *                         huffman_init + bitrate_table, with their own
 *                         fi_union/MAGIC, isolated in their own TU).
 *
 * The three non-static surfaces are disjoint, so one binary links cleanly, the
 * same per-TU split the vbrquantize-frame slice uses.
 *
 * FP PARITY. The loops are mostly integer (the bisection, the bitrate scan, the
 * reservoir arithmetic). The FP-bearing parts — VBR_*_prepare's masking adjust /
 * pow(10,.) masking_lower, bitpressure's xmin inflation, and the whole
 * quantization the loop drives (outer_loop / VBR_encode_frame) — round float32
 * products separately, so the frame output is bit-exact only under the mp3_strict
 * build (FMA-free Go) vs the -ffp-contract=off oracle. The scalar FP flags come
 * from the mise task env, never the in-source #cgo block.
 *
 * LGPL note: quantize.c / quantize_pvt.c / reservoir.c / vbrquantize.c /
 * takehiro.c / tables.c are LGPL LAME source, so this oracle TU and the Go port
 * it pins are gated by the mp3lame build tag (in addition to cgo). A bare
 * `go test` never compiles them.
 */
#ifndef MP3_VBR_ITERATION_LOOP_ORACLE_H
#define MP3_VBR_ITERATION_LOOP_ORACLE_H

typedef struct mp3parity_vbrit_t mp3parity_vbrit_t;

/* oracle_fill_tables fills pow43 / adj43asm / pow20 / ipow20 via the verbatim
 * iteration_init table-fill loop (TAKEHIRO_IEEE754_HACK branch). Idempotent. */
void oracle_fill_tables(void);

/* ---- handle lifecycle ---- */
mp3parity_vbrit_t *mp3parity_vbrit_new(void);
void               mp3parity_vbrit_free(mp3parity_vbrit_t *h);

/* ---- cfg + reservoir setters (the bitrate/reservoir machinery the loop +
 * ResvFrameBegin/getframebits read) ---- */
void mp3parity_vbrit_set_cfg(mp3parity_vbrit_t *h, int mode_gr, int channels_out,
                             int version, int samplerate_out, int avg_bitrate,
                             int sideinfo_len, int buffer_constraint,
                             int vbr_min_bitrate_index, int vbr_max_bitrate_index,
                             int disable_reservoir, int free_format,
                             int enforce_min_bitrate, int mode_ext);
void mp3parity_vbrit_set_cfg_quant(mp3parity_vbrit_t *h, int noise_shaping,
                                   int full_outer_loop, int use_best_huffman,
                                   float ATHfixpoint, float athcurve, int athtype);
void mp3parity_vbrit_set_resv(mp3parity_vbrit_t *h, int resv_size, int resv_max);
void mp3parity_vbrit_set_binsearch(mp3parity_vbrit_t *h, int old_value, int current_step);
void mp3parity_vbrit_set_svqnt(mp3parity_vbrit_t *h, float mask_adjust,
                               float mask_adjust_short, int substep_shaping,
                               int sfb21_extra);
void mp3parity_vbrit_set_longfact(mp3parity_vbrit_t *h, const float *lf, int n);
void mp3parity_vbrit_set_shortfact(mp3parity_vbrit_t *h, const float *sf, int n);

/* ---- ATH (the calc_xmin masking floor) ---- */
void mp3parity_vbrit_set_ath(mp3parity_vbrit_t *h, float adjust_factor, float floor,
                             const float *l, int nl, const float *s, int ns);

/* ---- scalefac_band tables + huffman_init ---- */
void mp3parity_vbrit_set_sfb_long(mp3parity_vbrit_t *h, const int *l, int n);
void mp3parity_vbrit_set_sfb_short(mp3parity_vbrit_t *h, const int *s, int n);
void mp3parity_vbrit_huffman_init(mp3parity_vbrit_t *h);

/* ---- per-[gr][ch] gr_info input setters ---- */
void mp3parity_vbrit_set_xr(mp3parity_vbrit_t *h, int gr, int ch, const float *xr, int n);
void mp3parity_vbrit_set_geom(mp3parity_vbrit_t *h, int gr, int ch, int block_type,
                              int mixed_block_flag);
/* per-[gr][ch] psy ratio (en/thm, long + short bands) */
void mp3parity_vbrit_set_ratio_l(mp3parity_vbrit_t *h, int gr, int ch,
                                 const float *en_l, const float *thm_l, int n);
void mp3parity_vbrit_set_ratio_s(mp3parity_vbrit_t *h, int gr, int ch,
                                 const float *en_s /*n*3*/, const float *thm_s /*n*3*/, int n);

/* ---- the drivers ---- */
void mp3parity_vbrit_run_new(mp3parity_vbrit_t *h, const float *pe /*2*2*/,
                             const float *ms_ener_ratio /*2*/);
void mp3parity_vbrit_run_old(mp3parity_vbrit_t *h, const float *pe /*2*2*/,
                             const float *ms_ener_ratio /*2*/);

/* ---- frame-level outputs ---- */
int mp3parity_vbrit_bitrate_index(const mp3parity_vbrit_t *h);
int mp3parity_vbrit_resv_size(const mp3parity_vbrit_t *h);
int mp3parity_vbrit_mode_ext(const mp3parity_vbrit_t *h);

/* ---- per-[gr][ch] gr_info output getters ---- */
int mp3parity_vbrit_global_gain(const mp3parity_vbrit_t *h, int gr, int ch);
int mp3parity_vbrit_scalefac_scale(const mp3parity_vbrit_t *h, int gr, int ch);
int mp3parity_vbrit_preflag(const mp3parity_vbrit_t *h, int gr, int ch);
int mp3parity_vbrit_scalefac(const mp3parity_vbrit_t *h, int gr, int ch, int sfb);
int mp3parity_vbrit_subblock_gain(const mp3parity_vbrit_t *h, int gr, int ch, int i);
int mp3parity_vbrit_l3enc(const mp3parity_vbrit_t *h, int gr, int ch, int i);
int mp3parity_vbrit_part2_3_length(const mp3parity_vbrit_t *h, int gr, int ch);
int mp3parity_vbrit_part2_length(const mp3parity_vbrit_t *h, int gr, int ch);
int mp3parity_vbrit_scalefac_compress(const mp3parity_vbrit_t *h, int gr, int ch);
int mp3parity_vbrit_big_values(const mp3parity_vbrit_t *h, int gr, int ch);
int mp3parity_vbrit_table_select(const mp3parity_vbrit_t *h, int gr, int ch, int i);
int mp3parity_vbrit_region0_count(const mp3parity_vbrit_t *h, int gr, int ch);
int mp3parity_vbrit_region1_count(const mp3parity_vbrit_t *h, int gr, int ch);
int mp3parity_vbrit_block_type(const mp3parity_vbrit_t *h, int gr, int ch);

#endif /* MP3_VBR_ITERATION_LOOP_ORACLE_H */
