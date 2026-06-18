/*
 * oracle.h — C oracle surface for the vbrtag parity slice (LAME 3.100
 * libmp3lame/VbrTag.c — the Xing/Info/LAME VBR tag).
 *
 * The Go port lives in nativemp3/vbrtag.go: AddVbrFrame / InitVbrTag /
 * CRC_update_lookup / UpdateMusicCRC / Xing_seek_table / setLameTagFrameHeader /
 * PutLameVBR / lame_get_lametag_frame, plus the crc16_lookup[256] table. The
 * emitted tag bytes depend on the REAL encoded -V2 frames (frame count, the
 * per-frame bitrate bag, the music CRC over the actual audio, the encoder
 * delay/padding), so this oracle drives a genuine end-to-end -V2 (vbr_default ==
 * vbr_mtrh) encode through the public LAME API:
 *
 *   lame_init -> lame_set_* (-V2) -> lame_init_params ->
 *   lame_encode_buffer_interleaved* (real audio) -> lame_encode_flush ->
 *   lame_get_lametag_frame (the genuine golden tag bytes).
 *
 * It then exports the gfc->VBR_seek_table / nMusicCRC / cfg / ov_enc / ov_rpg
 * state the Go lame_get_lametag_frame reads, so the Go side can reconstruct an
 * identical LameInternalFlags and produce its own tag frame to compare
 * byte-for-byte. Sub-function getters (the CRC table, addVbr/Xing_seek_table via
 * the populated bag, PutLameVBR via the whole-frame compare) localize any
 * divergence.
 *
 * TRANSLATION UNITS (parity discipline — each go-test binary compiles its OWN
 * copy of the reference; see CONTRIBUTING.md). This
 * slice needs a full real encode, so it compiles the whole vendored libmp3lame
 * encoder, one vendored source per TU (lame_*.c wrappers), exactly the per-TU
 * split the public cgo backend uses (LAME reuses Min/Max macros + per-TU file
 * statics — fi_union/MAGIC in takehiro.c/vbrquantize.c — so they must not share a
 * TU). oracle.c is its own TU: it includes the LAME internal headers and exports
 * the genuine VbrTag.c surface + the gfc state. It NEVER imports libraries/mp3
 * (which would duplicate the LAME symbols at link time); only the pure-Go
 * internal/nativemp3 port is imported on the Go side.
 *
 * FP PARITY. VbrTag.c is mostly integer (the bag arithmetic, the CRC, the bit
 * packing). The few FP expressions — Xing_seek_table's 256.*act/sum and j*pos
 * floor, PutLameVBR's nLowpass and the (gated-off) peak amplitude — are computed
 * once for the tag; the genuine -V2 encode that FEEDS the bag/CRC is itself
 * FP-bearing, so the byte-identical tag is asserted under the mp3_strict build
 * (FMA-free Go) vs the -ffp-contract=off oracle (flags from the mise task env,
 * never the in-source #cgo block).
 *
 * LGPL note: every lame_*.c here and the Go port it pins are LGPL LAME source,
 * gated by the mp3lame build tag (plus cgo). A bare `go test` never compiles them.
 */
#ifndef MP3_VBRTAG_ORACLE_H
#define MP3_VBRTAG_ORACLE_H

#include <stddef.h>

typedef struct mp3parity_vbrtag_t mp3parity_vbrtag_t;

/* ---- handle lifecycle: run a full -V2 encode over `nframes_pcm` granules of
 * synthetic PCM (a deterministic sine the caller seeds via `seed`), at the given
 * sample rate / channels, then capture the genuine lame_get_lametag_frame bytes
 * and the gfc state. Returns NULL on any LAME setup/encode failure. ---- */
mp3parity_vbrtag_t *mp3parity_vbrtag_run(int samplerate, int channels,
                                         int nsamples_per_ch, unsigned seed);
void                mp3parity_vbrtag_free(mp3parity_vbrtag_t *h);

/* ---- golden tag frame (the genuine lame_get_lametag_frame output) ---- */
int            mp3parity_vbrtag_frame_len(const mp3parity_vbrtag_t *h);
const unsigned char *mp3parity_vbrtag_frame_ptr(const mp3parity_vbrtag_t *h);

/* ---- exported cfg (SessionConfig_t) ---- */
int mp3parity_vbrtag_cfg(const mp3parity_vbrtag_t *h, int which);
/* which selectors for mp3parity_vbrtag_cfg: */
enum {
    VT_CFG_write_lame_tag = 0,
    VT_CFG_sideinfo_len,
    VT_CFG_error_protection,
    VT_CFG_vbr,
    VT_CFG_version,
    VT_CFG_samplerate_out,
    VT_CFG_samplerate_index,
    VT_CFG_extension,
    VT_CFG_mode,
    VT_CFG_copyright,
    VT_CFG_original,
    VT_CFG_emphasis,
    VT_CFG_avg_bitrate,
    VT_CFG_free_format,
    VT_CFG_noise_shaping,
    VT_CFG_ATHtype,
    VT_CFG_use_safe_joint_stereo,
    VT_CFG_force_ms,
    VT_CFG_samplerate_in,
    VT_CFG_short_blocks,
    VT_CFG_lowpassfreq,
    VT_CFG_highpassfreq,
    VT_CFG_disable_reservoir,
    VT_CFG_findReplayGain,
    VT_CFG_findPeakSample,
    VT_CFG_vbr_avg_bitrate_kbps,
    VT_CFG_vbr_min_bitrate_index,
    VT_CFG_preset,
    VT_CFG_ATHonly,
    VT_CFG_noATH
};

/* ---- exported ov_enc (EncResult_t) ---- */
int mp3parity_vbrtag_ovenc(const mp3parity_vbrtag_t *h, int which);
enum {
    VT_OV_bitrate_index = 0,
    VT_OV_mode_ext,
    VT_OV_encoder_delay,
    VT_OV_encoder_padding
};

/* ---- exported ov_rpg (RpgResult_t) ---- */
int   mp3parity_vbrtag_radio_gain(const mp3parity_vbrtag_t *h);
float mp3parity_vbrtag_peak_sample(const mp3parity_vbrtag_t *h);

/* ---- exported nMusicCRC ---- */
unsigned mp3parity_vbrtag_music_crc(const mp3parity_vbrtag_t *h);

/* ---- exported VBR_seek_table (VBR_seek_info_t) ---- */
int      mp3parity_vbrtag_seek_sum(const mp3parity_vbrtag_t *h);
int      mp3parity_vbrtag_seek_seen(const mp3parity_vbrtag_t *h);
int      mp3parity_vbrtag_seek_want(const mp3parity_vbrtag_t *h);
int      mp3parity_vbrtag_seek_pos(const mp3parity_vbrtag_t *h);
int      mp3parity_vbrtag_seek_size(const mp3parity_vbrtag_t *h);
int      mp3parity_vbrtag_seek_bag(const mp3parity_vbrtag_t *h, int i);
unsigned mp3parity_vbrtag_seek_nframes(const mp3parity_vbrtag_t *h);
unsigned long mp3parity_vbrtag_seek_nbytes(const mp3parity_vbrtag_t *h);
unsigned mp3parity_vbrtag_seek_totalframesize(const mp3parity_vbrtag_t *h);

/* ---- exported gfp (LameGlobalFlags) bits PutLameVBR reads ---- */
int mp3parity_vbrtag_gfp_vbr_q(const mp3parity_vbrtag_t *h);
int mp3parity_vbrtag_gfp_quality(const mp3parity_vbrtag_t *h);
int mp3parity_vbrtag_gfp_nogap_total(const mp3parity_vbrtag_t *h);
int mp3parity_vbrtag_gfp_nogap_current(const mp3parity_vbrtag_t *h);

/* ---- direct genuine-function probes (localize divergence) ---- */
/* CRC_update_lookup is file-static in VbrTag.c; reproduced VERBATIM here as a
 * documented hand-twin (a single table lookup over crc16_lookup, which IS the
 * vendored table — exposed via the genuine UpdateMusicCRC over a 1-byte buffer to
 * avoid duplicating the static). */
unsigned mp3parity_vbrtag_crc_step(unsigned value, unsigned crc);

#endif /* MP3_VBRTAG_ORACLE_H */
