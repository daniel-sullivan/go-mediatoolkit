// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* oracle.c — its own TU. Drives a genuine end-to-end -V2 (vbr_default ==
 * vbr_mtrh) encode through the public LAME API and assembles the file-layout
 * output stream: the placeholder Xing/Info tag frame InitVbrTag wrote first, then
 * the audio frames, with the leading placeholder overwritten by the finalized
 * lame_get_lametag_frame — exactly what the LAME CLI does on a seekable file
 * (write placeholder, fseek(0), rewrite the real tag). See oracle.h for the
 * parity rationale and the per-TU split. Build config + include paths come from
 * cgo.go.
 *
 * It also generates and exports the synthetic int16 PCM so the Go side encodes
 * the byte-identical input (no C-sin vs Go-Sin divergence). */

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

struct mp3parity_vbre2e_t {
    short *pcm;       /* interleaved int16 input, len = pcm_len */
    int    pcm_len;

    unsigned char *stream; /* assembled file-layout stream */
    int            stream_len;

    int tag_len; /* finalized tag frame size (the spliced prefix) */
};

mp3parity_vbre2e_t *
mp3parity_vbre2e_run(int samplerate, int channels, int nsamples_per_ch, unsigned seed)
{
    lame_global_flags *gfp;
    lame_internal_flags *gfc;
    mp3parity_vbre2e_t *h;
    short *pcm;
    unsigned char *mp3buf;
    int mp3buf_size;
    int i, n;
    int total;     /* total stream bytes accumulated */
    int tag_len;
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
    /* -V2: vbr_default == vbr_mtrh, VBR_q = 2. The public pure-Go encoder uses
     * VBR_q == cfg.quality; the byte-identical target is a real -V2 stream, so
     * pin VBR_q = 2 explicitly. The Go side mirrors it (native.go). */
    lame_set_VBR(gfp, vbr_default);
    lame_set_VBR_q(gfp, 2);

    if (lame_init_params(gfp) < 0) {
        lame_close(gfp);
        return NULL;
    }

    /* synthetic deterministic PCM — the SAME generator the vbrtag oracle uses:
     * a frequency-swept sine + harmonics so block types / bitrates vary. */
    pcm = (short *) malloc(sizeof(short) * (size_t) nsamples_per_ch * (size_t) channels);
    if (pcm == NULL) {
        lame_close(gfp);
        return NULL;
    }
    phase = 0.0;
    for (i = 0; i < nsamples_per_ch; i++) {
        double f = 220.0 + 660.0 * (0.5 + 0.5 * sin((double) i / 4096.0 + (double) seed));
        double s = 0.6 * sin(phase) + 0.25 * sin(3.0 * phase) + 0.1 * sin(7.0 * phase);
        short v = (short) (s * 26000.0);
        phase += 2.0 * 3.14159265358979323846 * f / (double) samplerate;
        if (channels == 1) {
            pcm[i] = v;
        } else {
            pcm[2 * i] = v;
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

    h = (mp3parity_vbre2e_t *) calloc(1, sizeof(*h));
    if (h == NULL) {
        free(pcm);
        free(mp3buf);
        lame_close(gfp);
        return NULL;
    }

    /* Accumulate the whole stream as LAME produces it (the FIRST bytes are the
     * placeholder Xing/Info frame InitVbrTag wrote). Grow h->stream as we go. */
    total = 0;

    /* Match the pure-Go EncodeBuffer's input handling: for stereo the public
     * encoder takes interleaved L/R (lame_encode_buffer_interleaved); for mono it
     * takes a single contiguous channel (lame_encode_buffer with pcm_l == pcm_r,
     * as the Go side does pcmR = pcmL). Using the interleaved entry point for mono
     * would (wrongly) read the buffer stride-2 and downmix, feeding a different
     * signal than the Go side. */
    if (channels == 1) {
        n = lame_encode_buffer(gfp, pcm, pcm, nsamples_per_ch, mp3buf, mp3buf_size);
    } else {
        n = lame_encode_buffer_interleaved(gfp, pcm, nsamples_per_ch, mp3buf, mp3buf_size);
    }
    if (n < 0) {
        goto fail;
    }
    if (n > 0) {
        unsigned char *p = (unsigned char *) realloc(h->stream, (size_t) (total + n));
        if (p == NULL) goto fail;
        h->stream = p;
        memcpy(h->stream + total, mp3buf, (size_t) n);
        total += n;
    }

    n = lame_encode_flush(gfp, mp3buf, mp3buf_size);
    if (n < 0) {
        goto fail;
    }
    if (n > 0) {
        unsigned char *p = (unsigned char *) realloc(h->stream, (size_t) (total + n));
        if (p == NULL) goto fail;
        h->stream = p;
        memcpy(h->stream + total, mp3buf, (size_t) n);
        total += n;
    }

    /* Build the finalized tag frame and splice it over the leading placeholder.
     * lame_get_lametag_frame's size == the placeholder frame length (TotalFrameSize),
     * so the overwrite is in place and the stream length is unchanged. */
    gfc = gfp->internal_flags;
    (void) gfc;
    {
        unsigned char tagbuf[2880]; /* MAXFRAMESIZE */
        size_t got = lame_get_lametag_frame(gfp, tagbuf, sizeof(tagbuf));
        tag_len = (int) got;
        if (got > 0 && (int) got <= total) {
            memcpy(h->stream, tagbuf, got);
        }
    }

    h->pcm = pcm;
    h->pcm_len = nsamples_per_ch * channels;
    h->stream_len = total;
    h->tag_len = tag_len;

    free(mp3buf);
    lame_close(gfp);
    return h;

fail:
    free(pcm);
    free(mp3buf);
    free(h->stream);
    free(h);
    lame_close(gfp);
    return NULL;
}

void
mp3parity_vbre2e_free(mp3parity_vbre2e_t *h)
{
    if (h == NULL)
        return;
    free(h->pcm);
    free(h->stream);
    free(h);
}

int          mp3parity_vbre2e_pcm_len(const mp3parity_vbre2e_t *h) { return h->pcm_len; }
const short *mp3parity_vbre2e_pcm_ptr(const mp3parity_vbre2e_t *h) { return h->pcm; }

int                  mp3parity_vbre2e_stream_len(const mp3parity_vbre2e_t *h) { return h->stream_len; }
const unsigned char *mp3parity_vbre2e_stream_ptr(const mp3parity_vbre2e_t *h) { return h->stream; }

int mp3parity_vbre2e_tag_len(const mp3parity_vbre2e_t *h) { return h->tag_len; }
