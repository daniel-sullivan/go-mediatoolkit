/* FLAC native-vs-Cgo benchmark — libFLAC stream DECODER TU.
 *
 * Isolated so stream_decoder.c's file-static helpers do not collide with
 * stream_encoder.c (in bench_encoder.c). fbench_decode drives libFLAC's
 * full stream decoder over an in-memory .flac byte buffer and collects the
 * decoded interleaved int32 samples — the C side of the decode benchmark.
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
    int      decode_error;
} fbench_dctx;

static FLAC__StreamDecoderReadStatus fbench_read_cb(
        const FLAC__StreamDecoder *decoder, FLAC__byte buffer[],
        size_t *bytes, void *client_data) {
    (void)decoder;
    fbench_dctx *c = (fbench_dctx *)client_data;
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

static FLAC__StreamDecoderWriteStatus fbench_write_cb(
        const FLAC__StreamDecoder *decoder, const FLAC__Frame *frame,
        const FLAC__int32 *const buffer[], void *client_data) {
    (void)decoder;
    fbench_dctx *c = (fbench_dctx *)client_data;
    uint32_t blocksize = frame->header.blocksize;
    uint32_t channels  = frame->header.channels;
    size_t need = (size_t)blocksize * (size_t)channels;
    if (c->out_count + need > c->out_cap) {
        c->overflow = 1;
        return FLAC__STREAM_DECODER_WRITE_STATUS_ABORT;
    }
    for (uint32_t i = 0; i < blocksize; i++) {
        for (uint32_t ch = 0; ch < channels; ch++) {
            c->out[c->out_count + (size_t)i * channels + ch] = buffer[ch][i];
        }
    }
    c->out_count += need;
    c->channels = channels;
    return FLAC__STREAM_DECODER_WRITE_STATUS_CONTINUE;
}

static void fbench_error_cb(const FLAC__StreamDecoder *decoder,
                            FLAC__StreamDecoderErrorStatus status,
                            void *client_data) {
    (void)decoder; (void)status;
    fbench_dctx *c = (fbench_dctx *)client_data;
    c->decode_error = 1;
}

/* Decode a FLAC byte stream with libFLAC's full stream decoder, writing
 * interleaved int32 samples into out (capacity out_cap int32). Returns the
 * number of inter-channel samples decoded, or -1 on failure. MD5 checking
 * is disabled (this is a throughput benchmark, not a parity assertion). */
long fbench_decode(const uint8_t *in, size_t in_len,
                   int32_t *out, size_t out_cap) {
    FLAC__StreamDecoder *dec = FLAC__stream_decoder_new();
    if (!dec) return -1;

    fbench_dctx ctx;
    memset(&ctx, 0, sizeof(ctx));
    ctx.in = in;
    ctx.in_len = in_len;
    ctx.out = out;
    ctx.out_cap = out_cap;

    FLAC__StreamDecoderInitStatus ist = FLAC__stream_decoder_init_stream(
        dec, fbench_read_cb, NULL /*seek*/, NULL /*tell*/, NULL /*length*/,
        NULL /*eof*/, fbench_write_cb, NULL /*metadata*/,
        fbench_error_cb, &ctx);
    if (ist != FLAC__STREAM_DECODER_INIT_STATUS_OK) {
        FLAC__stream_decoder_delete(dec);
        return -1;
    }

    FLAC__bool ok = FLAC__stream_decoder_process_until_end_of_stream(dec);
    FLAC__stream_decoder_finish(dec);
    FLAC__stream_decoder_delete(dec);

    if (!ok || ctx.overflow || ctx.decode_error || ctx.channels == 0) {
        return -1;
    }
    return (long)(ctx.out_count / ctx.channels);
}
