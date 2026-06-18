/*
 * oracle.h — C oracle surface for the vbrpsy-multigran parity slice.
 *
 * This slice isolates the multi-granule psychoacoustic-analysis driver
 * (psymodel.c L3psycho_anal_vbr) and pins the pure-Go vbrpsy port against the
 * genuine vendored static function, granule-by-granule, over a SHARED mfbuf.
 *
 * It was built to localize the -V2 byte-identical divergence the vbr-encode-e2e
 * slice flagged: for the first encoded frame the C psymodel sees real audio at
 * granule 1 (pe ~ 3960, tot_ener ~ 2.3e13) while the pure-Go vbrpsy reported
 * near-silence (pe ~ 309, tot_ener = 0). This oracle drives the genuine
 * L3psycho_anal_vbr for gr=0 then gr=1 over a controlled mfbuf and EXPORTS the
 * mfbuf floats plus per-granule energy[4] / pe[2] / pe_MS[2] and the per-band
 * en/thm of masking_ratio[gr][ch] / masking_MS_ratio[gr][ch], so the Go side
 * (native.go) runs nativemp3.L3psychoAnalVbr over the byte-identical mfbuf and
 * the test compares every value bit-exactly.
 *
 * STATE. After lame_init_params both the C gfc and the Go context have an
 * identical, freshly-initialized PsyStateVar / PsyConst / ATH (InitPsyModel is a
 * separately parity-verified port), so a single L3psycho_anal_vbr call per
 * granule over the same mfbuf is a clean apples-to-apples comparison of the
 * energy/threshold propagation.
 *
 * mfbuf CONSTRUCTION. The oracle reproduces the encoder's first-frame mfbuf
 * exactly: mf_size is primed to ENCDELAY-MDCTDELAY = 528 leading zeros, then the
 * synthetic int16 PCM is widened to sample_t through the configured pcm_transform
 * (the same lame_copy_inbuffer / fill_buffer path), filling mfbuf[ch][528..] until
 * the FFT need (BLKSIZE + framesize - FFTOFFSET) is met. bufp[ch] for granule gr
 * is &mfbuf[ch][576 + gr*576 - FFTOFFSET], exactly encoder.c:372.
 *
 * TRANSLATION UNITS (parity discipline — each go-test binary compiles its OWN
 * copy of the reference; see CONTRIBUTING.md). The
 * isolated L3psycho_anal_vbr call still needs the whole vendored libmp3lame init
 * path (lame_init_params + iteration_init + psymodel init), one vendored source
 * per TU (the lame_*.c / mpglib_*.c wrappers), the same per-TU split the public
 * cgo backend and the vbr-encode-e2e slice use. oracle.c is its OWN TU and is the
 * psymodel TU: it #includes libmp3lame/psymodel.c directly so it can call the
 * static L3psycho_anal_vbr (the other slices give psymodel its own lame_*.c
 * wrapper; here psymodel.c lives in oracle.c so the wrapper is omitted). It NEVER
 * imports libraries/mp3 (which would duplicate the LAME symbols at link time);
 * only the pure-Go internal/nativemp3 port is used on the Go side.
 *
 * FP PARITY. The energy/FHT/masking math is FP-bearing, so the bit-exact match
 * holds only under the mp3_strict build (FMA-free Go) vs the -ffp-contract=off
 * oracle. The scalar FP flags come from the mise task env (CGO_CFLAGS), never the
 * in-source #cgo block.
 *
 * LGPL note: the whole vendored psymodel + init path and the Go port it pins are
 * LGPL LAME source, gated by the mp3lame build tag (plus cgo). A bare `go test`
 * never compiles them.
 */
#ifndef MP3_VBRPSY_MULTIGRAN_ORACLE_H
#define MP3_VBRPSY_MULTIGRAN_ORACLE_H

#define MP3PARITY_MG_SBMAXL 22
#define MP3PARITY_MG_SBMAXS 13
#define MP3PARITY_MG_NGR    2  /* MPEG-1 mode_gr */
#define MP3PARITY_MG_NCH    4  /* L,R,M,S (mid/side for JOINT_STEREO) */

typedef struct mp3parity_mg_t mp3parity_mg_t;

/* mp3parity_mg_run builds the first-frame mfbuf for the given samplerate/channels
 * (JOINT_STEREO for 2ch, MONO for 1ch) from nsamples_per_ch synthetic samples,
 * then drives the genuine L3psycho_anal_vbr for gr=0 and gr=1, capturing the
 * mfbuf and the per-granule outputs. Returns NULL on a LAME setup failure. */
mp3parity_mg_t *mp3parity_mg_run(int samplerate, int channels, int nsamples_per_ch,
                                 unsigned seed);
void            mp3parity_mg_free(mp3parity_mg_t *h);

/* mf_size used (== the FFT need, the mfbuf fill length) and channels_out. */
int mp3parity_mg_mf_size(const mp3parity_mg_t *h);
int mp3parity_mg_channels_out(const mp3parity_mg_t *h);
int mp3parity_mg_n_chn_psy(const mp3parity_mg_t *h);

/* The shared mfbuf the Go side must reuse byte-identically: ch in {0,1}, length
 * == mp3parity_mg_mf_size. */
const float *mp3parity_mg_mfbuf_ptr(const mp3parity_mg_t *h, int ch);
int          mp3parity_mg_mfbuf_len(const mp3parity_mg_t *h);

/* Per-granule scalar outputs. gr in {0,1}. energy is [4] (L,R,M,S tot_ener);
 * pe is [2] (percep_entropy L,R); pe_MS is [2] (percep_MS_entropy M,S). */
const float *mp3parity_mg_energy(const mp3parity_mg_t *h, int gr); /* [4] */
const float *mp3parity_mg_pe(const mp3parity_mg_t *h, int gr);     /* [2] */
const float *mp3parity_mg_pe_ms(const mp3parity_mg_t *h, int gr);  /* [2] */

/* Per-granule, per-channel masking en/thm. which selects LR (0) or MS (1);
 * gr in {0,1}; ch in {0,1}. en_l / thm_l are [SBMAX_l], en_s / thm_s [SBMAX_s*3]
 * (flattened sb-major, sub-block-minor). */
const float *mp3parity_mg_en_l(const mp3parity_mg_t *h, int which, int gr, int ch);  /* [22] */
const float *mp3parity_mg_thm_l(const mp3parity_mg_t *h, int which, int gr, int ch); /* [22] */
const float *mp3parity_mg_en_s(const mp3parity_mg_t *h, int which, int gr, int ch);  /* [13*3] */
const float *mp3parity_mg_thm_s(const mp3parity_mg_t *h, int which, int gr, int ch); /* [13*3] */

/* ---- the long-block PsyConst_CB2SB_t init tables (gdl), to localize an
 * init-time vs per-frame divergence. ---- */
int          mp3parity_mg_gdl_npart(const mp3parity_mg_t *h);
int          mp3parity_mg_gdl_s3_len(const mp3parity_mg_t *h);
const float *mp3parity_mg_gdl_s3(const mp3parity_mg_t *h);            /* [s3_len] */
const float *mp3parity_mg_gdl_masking_lower(const mp3parity_mg_t *h); /* [64] */
const float *mp3parity_mg_gdl_minval(const mp3parity_mg_t *h);        /* [64] */
const float *mp3parity_mg_gdl_rnumlines(const mp3parity_mg_t *h);     /* [64] */
const float *mp3parity_mg_gdl_bo_weight(const mp3parity_mg_t *h);     /* [22] */
const float *mp3parity_mg_gdl_mld(const mp3parity_mg_t *h);          /* [22] */

int          mp3parity_mg_gds_s3_len(const mp3parity_mg_t *h);
const float *mp3parity_mg_gds_s3(const mp3parity_mg_t *h);
const float *mp3parity_mg_gds_masking_lower(const mp3parity_mg_t *h);
const float *mp3parity_mg_gds_minval(const mp3parity_mg_t *h);
const float *mp3parity_mg_gds_rnumlines(const mp3parity_mg_t *h);
const float *mp3parity_mg_gds_mld(const mp3parity_mg_t *h);          /* [13] */
const float *mp3parity_mg_gds_mld_cb(const mp3parity_mg_t *h);
const float *mp3parity_mg_ath_cb_s(const mp3parity_mg_t *h);
const float *mp3parity_mg_ath_cb_l(const mp3parity_mg_t *h);


#endif /* MP3_VBRPSY_MULTIGRAN_ORACLE_H */
