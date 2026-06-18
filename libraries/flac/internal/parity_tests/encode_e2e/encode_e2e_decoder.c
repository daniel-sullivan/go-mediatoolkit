/* End-to-end ENCODE parity — libFLAC reference DECODER TU.
 *
 * Isolated so stream_decoder.c's file-static helpers do not collide with
 * stream_encoder.c (in encode_e2e_encoder.c). fparity_ee_decode runs
 * libFLAC's full stream decoder over a FLAC byte stream produced by the
 * pure-Go native encoder and reconstructs the interleaved int32 samples,
 * so the test can assert lossless round-trip against the original PCM.
 */

#ifdef HAVE_CONFIG_H
#  include <config.h>
#endif

#include "src/libFLAC/stream_decoder.c"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

/* Decode state: a memory source (the .flac bytes) plus an int32 sink for
 * the interleaved decoded samples. */
typedef struct {
    const uint8_t *in;
    size_t         in_len;
    size_t         in_pos;

    int32_t *out;
    size_t   out_cap;   /* in int32 units */
    size_t   out_pos;   /* in int32 units */
    int      overflow;

    uint32_t channels;
    uint32_t bits_per_sample;
    uint32_t sample_rate;
    uint32_t min_blocksize;
    uint32_t max_blocksize;
    uint32_t min_framesize;
    uint32_t max_framesize;
    uint64_t total_samples;
    uint8_t  md5sum[16];

    int error;
} fparity_ee_decsink;

static FLAC__StreamDecoderReadStatus fparity_ee_read_cb(
        const FLAC__StreamDecoder *decoder, FLAC__byte buffer[], size_t *bytes,
        void *client_data) {
    (void)decoder;
    fparity_ee_decsink *s = (fparity_ee_decsink *)client_data;
    size_t want = *bytes;
    size_t avail = s->in_len - s->in_pos;
    if (avail == 0) {
        *bytes = 0;
        return FLAC__STREAM_DECODER_READ_STATUS_END_OF_STREAM;
    }
    if (want > avail) want = avail;
    memcpy(buffer, s->in + s->in_pos, want);
    s->in_pos += want;
    *bytes = want;
    return FLAC__STREAM_DECODER_READ_STATUS_CONTINUE;
}

static FLAC__StreamDecoderWriteStatus fparity_ee_write_cb(
        const FLAC__StreamDecoder *decoder, const FLAC__Frame *frame,
        const FLAC__int32 *const buffer[], void *client_data) {
    (void)decoder;
    fparity_ee_decsink *s = (fparity_ee_decsink *)client_data;
    uint32_t ch = frame->header.channels;
    uint32_t bs = frame->header.blocksize;
    for (uint32_t i = 0; i < bs; i++) {
        for (uint32_t c = 0; c < ch; c++) {
            if (s->out_pos >= s->out_cap) {
                s->overflow = 1;
                return FLAC__STREAM_DECODER_WRITE_STATUS_ABORT;
            }
            s->out[s->out_pos++] = buffer[c][i];
        }
    }
    return FLAC__STREAM_DECODER_WRITE_STATUS_CONTINUE;
}

static void fparity_ee_meta_cb(
        const FLAC__StreamDecoder *decoder, const FLAC__StreamMetadata *metadata,
        void *client_data) {
    (void)decoder;
    fparity_ee_decsink *s = (fparity_ee_decsink *)client_data;
    if (metadata->type == FLAC__METADATA_TYPE_STREAMINFO) {
        const FLAC__StreamMetadata_StreamInfo *si = &metadata->data.stream_info;
        s->channels = si->channels;
        s->bits_per_sample = si->bits_per_sample;
        s->sample_rate = si->sample_rate;
        s->min_blocksize = si->min_blocksize;
        s->max_blocksize = si->max_blocksize;
        s->min_framesize = si->min_framesize;
        s->max_framesize = si->max_framesize;
        s->total_samples = si->total_samples;
        memcpy(s->md5sum, si->md5sum, 16);
    }
}

static void fparity_ee_error_cb(
        const FLAC__StreamDecoder *decoder, FLAC__StreamDecoderErrorStatus status,
        void *client_data) {
    (void)decoder; (void)status;
    fparity_ee_decsink *s = (fparity_ee_decsink *)client_data;
    s->error = 1;
}

/* Decode a FLAC byte stream. The decoded interleaved int32 samples are
 * written into out (capacity out_cap int32). Returns the number of
 * inter-channel samples decoded, or -1 on failure. STREAMINFO fields are
 * reported through the out-params. */
long fparity_ee_decode(const uint8_t *in, size_t in_len,
                       int32_t *out, size_t out_cap,
                       uint32_t *channels, uint32_t *bits_per_sample,
                       uint32_t *sample_rate,
                       uint32_t *min_blocksize, uint32_t *max_blocksize,
                       uint32_t *min_framesize, uint32_t *max_framesize,
                       uint64_t *total_samples, uint8_t *md5sum) {
    FLAC__StreamDecoder *dec = FLAC__stream_decoder_new();
    if (!dec) return -1;

    fparity_ee_decsink s;
    memset(&s, 0, sizeof(s));
    s.in = in;
    s.in_len = in_len;
    s.out = out;
    s.out_cap = out_cap;

    if (FLAC__stream_decoder_init_stream(
            dec, fparity_ee_read_cb, NULL, NULL, NULL, NULL,
            fparity_ee_write_cb, fparity_ee_meta_cb, fparity_ee_error_cb,
            &s) != FLAC__STREAM_DECODER_INIT_STATUS_OK) {
        FLAC__stream_decoder_delete(dec);
        return -1;
    }

    FLAC__bool ok = FLAC__stream_decoder_process_until_end_of_stream(dec);
    FLAC__stream_decoder_finish(dec);
    FLAC__stream_decoder_delete(dec);

    if (!ok || s.error || s.overflow) return -1;

    *channels = s.channels;
    *bits_per_sample = s.bits_per_sample;
    *sample_rate = s.sample_rate;
    *min_blocksize = s.min_blocksize;
    *max_blocksize = s.max_blocksize;
    *min_framesize = s.min_framesize;
    *max_framesize = s.max_framesize;
    *total_samples = s.total_samples;
    memcpy(md5sum, s.md5sum, 16);

    if (s.channels == 0) return 0;
    return (long)(s.out_pos / s.channels);
}
