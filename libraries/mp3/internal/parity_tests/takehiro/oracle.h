/*
 * oracle.h — C API the takehiro parity tests call into.
 *
 * Declares an opaque handle over a heap lame_internal_flags and trampolines
 * that drive the genuine vendored takehiro.c bit-counting routines
 * (compiled in oracle.c via #include "libmp3lame/takehiro.c"): huffman_init,
 * gfc->choose_table (== choose_table_nonMMX), noquant_count_bits,
 * scale_bitcount (mpeg1 / mpeg2-LSF), best_huffman_divide and
 * best_scalefac_store. The Go side fabricates a gr_info + scalefac_band.l on
 * both sides, runs one routine through the genuine C, then reads back the
 * gr_info side-information the routine fills.
 *
 * The handle layout is opaque to Go; cgo.go only passes the pointer back.
 */

#ifndef MP3PARITY_TAKEHIRO_ORACLE_H
#define MP3PARITY_TAKEHIRO_ORACLE_H

typedef struct mp3parity_tk_t mp3parity_tk_t;

mp3parity_tk_t *mp3parity_tk_new(void);
void mp3parity_tk_free(mp3parity_tk_t *h);

/* ---- session configuration ---- */
void mp3parity_tk_set_cfg(mp3parity_tk_t *h, int mode_gr, int use_best_huffman);
/* scalefac_band.l[0..22] (SBMAX_l+1 entries). */
void mp3parity_tk_set_sfb_long(mp3parity_tk_t *h, const int *l, int n);
/* scalefac_band.s[0..13] (SBMAX_s+1 entries). */
void mp3parity_tk_set_sfb_short(mp3parity_tk_t *h, const int *s, int n);

/* ---- gr_info (granule gr, channel ch) input ---- */
void mp3parity_tk_set_l3enc(mp3parity_tk_t *h, int gr, int ch, const int *ix, int n);
void mp3parity_tk_set_scalefac(mp3parity_tk_t *h, int gr, int ch, const int *sf, int n);
void mp3parity_tk_set_width(mp3parity_tk_t *h, int gr, int ch, const int *w, int n);
void mp3parity_tk_set_window(mp3parity_tk_t *h, int gr, int ch, const int *win, int n);
void mp3parity_tk_set_geom(mp3parity_tk_t *h, int gr, int ch,
                           int block_type, int mixed_block_flag, int global_gain,
                           int scalefac_scale, int preflag, int sfbmax, int sfbdivide,
                           int max_nonzero_coeff, int part2_3_length);

/* ---- runners ---- */
/* huffman_init: fills sv_qnt.bv_scf[576] from scalefac_band.l. */
void mp3parity_tk_huffman_init(mp3parity_tk_t *h);
/* gfc->choose_table over ix[begin..end); returns table index, *bits accumulates. */
int  mp3parity_tk_choose_table(mp3parity_tk_t *h, int gr, int ch, int begin, int end, int *bits);
/* noquant_count_bits: returns big-value+count1 bits. */
int  mp3parity_tk_noquant_count_bits(mp3parity_tk_t *h, int gr, int ch);
/* scale_bitcount: returns the over/fail flag. */
int  mp3parity_tk_scale_bitcount(mp3parity_tk_t *h, int gr, int ch);
/* best_huffman_divide: re-optimizes the region split in place. */
void mp3parity_tk_best_huffman_divide(mp3parity_tk_t *h, int gr, int ch);
/* best_scalefac_store(gr, ch): rewrites scalefac storage + scfsi. */
void mp3parity_tk_best_scalefac_store(mp3parity_tk_t *h, int gr, int ch);

/* ---- gr_info readback ---- */
int mp3parity_tk_bv_scf(const mp3parity_tk_t *h, int i);
int mp3parity_tk_big_values(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_count1(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_count1bits(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_count1table_select(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_region0_count(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_region1_count(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_table_select(const mp3parity_tk_t *h, int gr, int ch, int i);
int mp3parity_tk_part2_3_length(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_part2_length(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_scalefac_compress(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_scalefac_scale(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_preflag(const mp3parity_tk_t *h, int gr, int ch);
int mp3parity_tk_scalefac(const mp3parity_tk_t *h, int gr, int ch, int sfb);
int mp3parity_tk_slen(const mp3parity_tk_t *h, int gr, int ch, int i);
int mp3parity_tk_scfsi(const mp3parity_tk_t *h, int ch, int i);

#endif /* MP3PARITY_TAKEHIRO_ORACLE_H */
