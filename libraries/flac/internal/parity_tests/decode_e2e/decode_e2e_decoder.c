/* End-to-end decode parity — libFLAC stream DECODER TU.
 *
 * Isolated so stream_decoder.c's file-static helpers do not collide with
 * stream_encoder.c (in decode_e2e_encoder.c). The fparity_e2e_decode
 * helper drives libFLAC's full stream decoder over an in-memory .flac
 * byte buffer and collects the decoded interleaved int32 samples,
 * STREAMINFO fields, and the MD5-check result — the oracle the Go
 * nativeflac decoder is compared against.
 */

#ifdef HAVE_CONFIG_H
#  include <config.h>
#endif

#include "src/libFLAC/stream_decoder.c"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    const uint8_t *in;
    size_t         in_len;
    size_t         in_pos;

    int32_t       *out;
    size_t         out_cap;   /* in int32 elements */
    size_t         out_count; /* int32 elements written */
    int            overflow;

    uint32_t channels;
    uint32_t bits_per_sample;
    uint32_t sample_rate;
    uint32_t min_blocksize, max_blocksize;
    uint32_t min_framesize, max_framesize;
    uint64_t total_samples;
    uint8_t  md5sum[16];
    int      have_streaminfo;
    int      decode_error;
} fparity_e2e_dctx;

static FLAC__StreamDecoderReadStatus fparity_e2e_read_cb(
        const FLAC__StreamDecoder *decoder, FLAC__byte buffer[],
        size_t *bytes, void *client_data) {
    (void)decoder;
    fparity_e2e_dctx *c = (fparity_e2e_dctx *)client_data;
    size_t want = *bytes;
    size_t avail = c->in_len - c->in_pos;
    if (want == 0) {
        return FLAC__STREAM_DECODER_READ_STATUS_ABORT;
    }
    if (avail == 0) {
        *bytes = 0;
        return FLAC__STREAM_DECODER_READ_STATUS_END_OF_STREAM;
    }
    size_t n = want < avail ? want : avail;
    memcpy(buffer, c->in + c->in_pos, n);
    c->in_pos += n;
    *bytes = n;
    return FLAC__STREAM_DECODER_READ_STATUS_CONTINUE;
}

static FLAC__StreamDecoderWriteStatus fparity_e2e_write_cb(
        const FLAC__StreamDecoder *decoder, const FLAC__Frame *frame,
        const FLAC__int32 *const buffer[], void *client_data) {
    (void)decoder;
    fparity_e2e_dctx *c = (fparity_e2e_dctx *)client_data;
    uint32_t blocksize = frame->header.blocksize;
    uint32_t channels  = frame->header.channels;
    size_t need = (size_t)blocksize * (size_t)channels;
    if (c->out_count + need > c->out_cap) {
        c->overflow = 1;
        return FLAC__STREAM_DECODER_WRITE_STATUS_ABORT;
    }
    /* Interleave to [s0c0, s0c1, …, s1c0, …] matching the toolkit
     * convention the native adapter produces. */
    for (uint32_t i = 0; i < blocksize; i++) {
        for (uint32_t ch = 0; ch < channels; ch++) {
            c->out[c->out_count + (size_t)i * channels + ch] = buffer[ch][i];
        }
    }
    c->out_count += need;
    return FLAC__STREAM_DECODER_WRITE_STATUS_CONTINUE;
}

static void fparity_e2e_meta_cb(const FLAC__StreamDecoder *decoder,
                                const FLAC__StreamMetadata *metadata,
                                void *client_data) {
    (void)decoder;
    fparity_e2e_dctx *c = (fparity_e2e_dctx *)client_data;
    if (metadata->type == FLAC__METADATA_TYPE_STREAMINFO) {
        const FLAC__StreamMetadata_StreamInfo *si = &metadata->data.stream_info;
        c->channels        = si->channels;
        c->bits_per_sample = si->bits_per_sample;
        c->sample_rate     = si->sample_rate;
        c->min_blocksize   = si->min_blocksize;
        c->max_blocksize   = si->max_blocksize;
        c->min_framesize   = si->min_framesize;
        c->max_framesize   = si->max_framesize;
        c->total_samples   = si->total_samples;
        memcpy(c->md5sum, si->md5sum, 16);
        c->have_streaminfo = 1;
    }
}

static void fparity_e2e_error_cb(const FLAC__StreamDecoder *decoder,
                                 FLAC__StreamDecoderErrorStatus status,
                                 void *client_data) {
    (void)decoder; (void)status;
    fparity_e2e_dctx *c = (fparity_e2e_dctx *)client_data;
    c->decode_error = 1;
}

long fparity_e2e_decode(const uint8_t *in, size_t in_len,
                        int32_t *out, size_t out_cap,
                        uint32_t *channels, uint32_t *bits_per_sample,
                        uint32_t *sample_rate,
                        uint32_t *min_blocksize, uint32_t *max_blocksize,
                        uint32_t *min_framesize, uint32_t *max_framesize,
                        uint64_t *total_samples, uint8_t *md5sum,
                        int md5_check, int *md5_ok) {
    FLAC__StreamDecoder *dec = FLAC__stream_decoder_new();
    if (!dec) return -1;

    fparity_e2e_dctx ctx;
    memset(&ctx, 0, sizeof(ctx));
    ctx.in = in;
    ctx.in_len = in_len;
    ctx.out = out;
    ctx.out_cap = out_cap;

    if (md5_check) {
        FLAC__stream_decoder_set_md5_checking(dec, true);
    }

    FLAC__StreamDecoderInitStatus ist = FLAC__stream_decoder_init_stream(
        dec, fparity_e2e_read_cb, NULL /*seek*/, NULL /*tell*/, NULL /*length*/,
        NULL /*eof*/, fparity_e2e_write_cb, fparity_e2e_meta_cb,
        fparity_e2e_error_cb, &ctx);
    if (ist != FLAC__STREAM_DECODER_INIT_STATUS_OK) {
        FLAC__stream_decoder_delete(dec);
        return -1;
    }

    FLAC__bool ok = FLAC__stream_decoder_process_until_end_of_stream(dec);

    /* FLAC__stream_decoder_finish runs the deferred MD5 comparison when
     * md5 checking is enabled; it returns false on a mismatch. */
    FLAC__bool finish_ok = FLAC__stream_decoder_finish(dec);
    *md5_ok = finish_ok ? 1 : 0;

    FLAC__stream_decoder_delete(dec);

    if (!ok || ctx.overflow || ctx.decode_error || !ctx.have_streaminfo) {
        return -1;
    }

    *channels        = ctx.channels;
    *bits_per_sample = ctx.bits_per_sample;
    *sample_rate     = ctx.sample_rate;
    *min_blocksize   = ctx.min_blocksize;
    *max_blocksize   = ctx.max_blocksize;
    *min_framesize   = ctx.min_framesize;
    *max_framesize   = ctx.max_framesize;
    *total_samples   = ctx.total_samples;
    memcpy(md5sum, ctx.md5sum, 16);

    if (ctx.channels == 0) return -1;
    return (long)(ctx.out_count / ctx.channels);
}
