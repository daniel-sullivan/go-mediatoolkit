// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* oracle.c — its own TU. Drives a genuine end-to-end -V2 (vbr_default ==
 * vbr_mtrh) encode through the public LAME API and captures the real
 * lame_get_lametag_frame bytes plus the gfc->VBR_seek_table / nMusicCRC / cfg /
 * ov_enc / ov_rpg state the Go VbrTag.c port reads. See oracle.h for the parity
 * rationale and the per-TU split. Build config + include paths come from cgo.go.
 *
 * Includes the LAME internal headers so it can reach gfp->internal_flags (the
 * SessionConfig_t cfg, EncResult_t ov_enc, RpgResult_t ov_rpg, VBR_seek_info_t
 * VBR_seek_table, uint16_t nMusicCRC). These structs are defined in util.h. */

#include <config.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>

#include "lame.h"
#include "machine.h"
#include "encoder.h"
#include "util.h"
#include "lame_global_flags.h"
#include "VbrTag.h"

#include "oracle.h"

#define MAXFRAMESIZE 2880

struct mp3parity_vbrtag_t {
    /* genuine golden tag frame */
    unsigned char frame[MAXFRAMESIZE];
    int           frame_len;

    /* captured cfg */
    SessionConfig_t cfg;
    /* captured ov_enc / ov_rpg / nMusicCRC */
    EncResult_t ov_enc;
    RpgResult_t ov_rpg;
    uint16_t    nMusicCRC;

    /* captured VBR_seek_table (deep-copy of bag) */
    int      seek_sum, seek_seen, seek_want, seek_pos, seek_size;
    int     *seek_bag; /* malloc'd copy of size entries */
    unsigned seek_nframes;
    unsigned long seek_nbytes;
    unsigned seek_totalframesize;

    /* captured gfp bits */
    int gfp_vbr_q, gfp_quality, gfp_nogap_total, gfp_nogap_current;
};

mp3parity_vbrtag_t *
mp3parity_vbrtag_run(int samplerate, int channels, int nsamples_per_ch, unsigned seed)
{
    lame_global_flags *gfp;
    lame_internal_flags *gfc;
    mp3parity_vbrtag_t *h;
    short *pcm;
    unsigned char *mp3buf;
    int mp3buf_size;
    int i, n;
    double phase;

    gfp = lame_init();
    if (gfp == NULL)
        return NULL;
    lame_set_in_samplerate(gfp, samplerate);
    lame_set_num_channels(gfp, channels);
    lame_set_quality(gfp, 5); /* lameNew default quality (encoder_cgo.go) */
    if (channels == 1)
        lame_set_mode(gfp, MONO);
    else
        lame_set_mode(gfp, JOINT_STEREO);
    /* -V2: lame_set_VBR(vbr_default) == vbr_mtrh, lame_set_VBR_q(2). The public
     * pure-Go encoder uses VBR_q == cfg.quality, but the byte-identical target is
     * a real -V2 stream, so pin VBR_q = 2 explicitly. The Go test mirrors it. */
    lame_set_VBR(gfp, vbr_default);
    lame_set_VBR_q(gfp, 2);

    if (lame_init_params(gfp) < 0) {
        lame_close(gfp);
        return NULL;
    }

    /* synthetic deterministic PCM: a sine whose frequency varies with `seed`,
     * so the encoder produces genuinely variable bitrates frame to frame. */
    pcm = (short *) malloc(sizeof(short) * (size_t) nsamples_per_ch * (size_t) channels);
    if (pcm == NULL) {
        lame_close(gfp);
        return NULL;
    }
    phase = 0.0;
    for (i = 0; i < nsamples_per_ch; i++) {
        /* frequency-swept sine + a little harmonic content so block types vary */
        double f = 220.0 + 660.0 * (0.5 + 0.5 * sin((double) i / 4096.0 + (double) seed));
        double s = 0.6 * sin(phase) + 0.25 * sin(3.0 * phase) + 0.1 * sin(7.0 * phase);
        short v = (short) (s * 26000.0);
        phase += 2.0 * 3.14159265358979323846 * f / (double) samplerate;
        if (channels == 1) {
            pcm[i] = v;
        } else {
            pcm[2 * i] = v;
            /* a slightly different right channel so M/S has work to do */
            pcm[2 * i + 1] = (short) (v * 0.85);
        }
    }

    mp3buf_size = nsamples_per_ch + nsamples_per_ch / 4 + 7200;
    mp3buf = (unsigned char *) malloc((size_t) mp3buf_size);
    if (mp3buf == NULL) {
        free(pcm);
        lame_close(gfp);
        return NULL;
    }

    n = lame_encode_buffer_interleaved(gfp, pcm, nsamples_per_ch, mp3buf, mp3buf_size);
    if (n < 0) {
        free(pcm);
        free(mp3buf);
        lame_close(gfp);
        return NULL;
    }
    n = lame_encode_flush(gfp, mp3buf, mp3buf_size);
    if (n < 0) {
        free(pcm);
        free(mp3buf);
        lame_close(gfp);
        return NULL;
    }

    h = (mp3parity_vbrtag_t *) calloc(1, sizeof(*h));
    if (h == NULL) {
        free(pcm);
        free(mp3buf);
        lame_close(gfp);
        return NULL;
    }

    /* genuine golden tag frame */
    h->frame_len = (int) lame_get_lametag_frame(gfp, h->frame, sizeof(h->frame));

    /* capture gfc state */
    gfc = gfp->internal_flags;
    h->cfg = gfc->cfg;
    h->ov_enc = gfc->ov_enc;
    h->ov_rpg = gfc->ov_rpg;
    h->nMusicCRC = gfc->nMusicCRC;

    h->seek_sum = gfc->VBR_seek_table.sum;
    h->seek_seen = gfc->VBR_seek_table.seen;
    h->seek_want = gfc->VBR_seek_table.want;
    h->seek_pos = gfc->VBR_seek_table.pos;
    h->seek_size = gfc->VBR_seek_table.size;
    h->seek_nframes = gfc->VBR_seek_table.nVbrNumFrames;
    h->seek_nbytes = gfc->VBR_seek_table.nBytesWritten;
    h->seek_totalframesize = gfc->VBR_seek_table.TotalFrameSize;
    if (gfc->VBR_seek_table.bag != NULL && h->seek_size > 0) {
        h->seek_bag = (int *) malloc(sizeof(int) * (size_t) h->seek_size);
        if (h->seek_bag != NULL)
            memcpy(h->seek_bag, gfc->VBR_seek_table.bag,
                   sizeof(int) * (size_t) h->seek_size);
    }

    h->gfp_vbr_q = lame_get_VBR_q(gfp);
    h->gfp_quality = lame_get_quality(gfp);
    h->gfp_nogap_total = gfp->nogap_total;
    h->gfp_nogap_current = gfp->nogap_current;

    free(pcm);
    free(mp3buf);
    lame_close(gfp);
    return h;
}

void
mp3parity_vbrtag_free(mp3parity_vbrtag_t *h)
{
    if (h == NULL)
        return;
    free(h->seek_bag);
    free(h);
}

int mp3parity_vbrtag_frame_len(const mp3parity_vbrtag_t *h) { return h->frame_len; }
const unsigned char *mp3parity_vbrtag_frame_ptr(const mp3parity_vbrtag_t *h) { return h->frame; }

int
mp3parity_vbrtag_cfg(const mp3parity_vbrtag_t *h, int which)
{
    const SessionConfig_t *c = &h->cfg;
    switch (which) {
    case VT_CFG_write_lame_tag:        return c->write_lame_tag;
    case VT_CFG_sideinfo_len:          return c->sideinfo_len;
    case VT_CFG_error_protection:      return c->error_protection;
    case VT_CFG_vbr:                   return (int) c->vbr;
    case VT_CFG_version:               return c->version;
    case VT_CFG_samplerate_out:        return c->samplerate_out;
    case VT_CFG_samplerate_index:      return c->samplerate_index;
    case VT_CFG_extension:             return c->extension;
    case VT_CFG_mode:                  return (int) c->mode;
    case VT_CFG_copyright:             return c->copyright;
    case VT_CFG_original:              return c->original;
    case VT_CFG_emphasis:              return c->emphasis;
    case VT_CFG_avg_bitrate:           return c->avg_bitrate;
    case VT_CFG_free_format:           return c->free_format;
    case VT_CFG_noise_shaping:         return c->noise_shaping;
    case VT_CFG_ATHtype:               return c->ATHtype;
    case VT_CFG_use_safe_joint_stereo: return c->use_safe_joint_stereo;
    case VT_CFG_force_ms:              return c->force_ms;
    case VT_CFG_samplerate_in:         return c->samplerate_in;
    case VT_CFG_short_blocks:          return (int) c->short_blocks;
    case VT_CFG_lowpassfreq:           return c->lowpassfreq;
    case VT_CFG_highpassfreq:          return c->highpassfreq;
    case VT_CFG_disable_reservoir:     return c->disable_reservoir;
    case VT_CFG_findReplayGain:        return c->findReplayGain;
    case VT_CFG_findPeakSample:        return c->findPeakSample;
    case VT_CFG_vbr_avg_bitrate_kbps:  return c->vbr_avg_bitrate_kbps;
    case VT_CFG_vbr_min_bitrate_index: return c->vbr_min_bitrate_index;
    case VT_CFG_preset:                return c->preset;
    case VT_CFG_ATHonly:               return c->ATHonly;
    case VT_CFG_noATH:                 return c->noATH;
    default:                           return 0;
    }
}

int
mp3parity_vbrtag_ovenc(const mp3parity_vbrtag_t *h, int which)
{
    const EncResult_t *e = &h->ov_enc;
    switch (which) {
    case VT_OV_bitrate_index:    return e->bitrate_index;
    case VT_OV_mode_ext:         return e->mode_ext;
    case VT_OV_encoder_delay:    return e->encoder_delay;
    case VT_OV_encoder_padding:  return e->encoder_padding;
    default:                     return 0;
    }
}

int   mp3parity_vbrtag_radio_gain(const mp3parity_vbrtag_t *h) { return h->ov_rpg.RadioGain; }
float mp3parity_vbrtag_peak_sample(const mp3parity_vbrtag_t *h) { return (float) h->ov_rpg.PeakSample; }

unsigned mp3parity_vbrtag_music_crc(const mp3parity_vbrtag_t *h) { return h->nMusicCRC; }

int      mp3parity_vbrtag_seek_sum(const mp3parity_vbrtag_t *h)  { return h->seek_sum; }
int      mp3parity_vbrtag_seek_seen(const mp3parity_vbrtag_t *h) { return h->seek_seen; }
int      mp3parity_vbrtag_seek_want(const mp3parity_vbrtag_t *h) { return h->seek_want; }
int      mp3parity_vbrtag_seek_pos(const mp3parity_vbrtag_t *h)  { return h->seek_pos; }
int      mp3parity_vbrtag_seek_size(const mp3parity_vbrtag_t *h) { return h->seek_size; }
int      mp3parity_vbrtag_seek_bag(const mp3parity_vbrtag_t *h, int i)
{
    if (h->seek_bag == NULL || i < 0 || i >= h->seek_size)
        return 0;
    return h->seek_bag[i];
}
unsigned      mp3parity_vbrtag_seek_nframes(const mp3parity_vbrtag_t *h) { return h->seek_nframes; }
unsigned long mp3parity_vbrtag_seek_nbytes(const mp3parity_vbrtag_t *h)  { return h->seek_nbytes; }
unsigned      mp3parity_vbrtag_seek_totalframesize(const mp3parity_vbrtag_t *h) { return h->seek_totalframesize; }

int mp3parity_vbrtag_gfp_vbr_q(const mp3parity_vbrtag_t *h)        { return h->gfp_vbr_q; }
int mp3parity_vbrtag_gfp_quality(const mp3parity_vbrtag_t *h)      { return h->gfp_quality; }
int mp3parity_vbrtag_gfp_nogap_total(const mp3parity_vbrtag_t *h)  { return h->gfp_nogap_total; }
int mp3parity_vbrtag_gfp_nogap_current(const mp3parity_vbrtag_t *h){ return h->gfp_nogap_current; }

/* CRC_update_lookup hand-twin probe: VbrTag.c's CRC_update_lookup is file-static.
 * The genuine UpdateMusicCRC (extern) folds one byte through it over the genuine
 * crc16_lookup table, so probe the real static via a 1-byte UpdateMusicCRC. */
unsigned
mp3parity_vbrtag_crc_step(unsigned value, unsigned crc)
{
    uint16_t c = (uint16_t) crc;
    unsigned char b = (unsigned char) value;
    UpdateMusicCRC(&c, &b, 1);
    return c;
}
