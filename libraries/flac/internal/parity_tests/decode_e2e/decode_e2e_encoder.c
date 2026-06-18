/* End-to-end decode parity — libFLAC stream ENCODER TU.
 *
 * Isolated so stream_encoder.c's file-static helpers do not collide with
 * stream_decoder.c (in decode_e2e_decoder.c). The fparity_e2e_encode
 * helper drives libFLAC's full stream encoder over an interleaved int32
 * PCM buffer and collects the .flac bytes into a caller-supplied buffer
 * via a write callback — the same shape the main cgo encoder uses.
 */

#include "src/libFLAC/stream_encoder.c"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

/* Sink for encoded bytes: write into a fixed-capacity caller buffer.
 * pos is the current write cursor (moved by seeks); len is the
 * high-water mark = final file size. The header-rewrite seek at finish
 * moves pos backwards to overwrite STREAMINFO without shrinking len. */
typedef struct {
    uint8_t *out;
    size_t   cap;
    size_t   pos;
    size_t   len;
    int      overflow;
} fparity_e2e_sink;

static FLAC__StreamEncoderWriteStatus fparity_e2e_write_cb(
        const FLAC__StreamEncoder *encoder, const FLAC__byte buffer[],
        size_t bytes, uint32_t samples, uint32_t current_frame, void *client_data) {
    (void)encoder; (void)samples; (void)current_frame;
    fparity_e2e_sink *s = (fparity_e2e_sink *)client_data;
    if (s->pos + bytes > s->cap) {
        s->overflow = 1;
        return FLAC__STREAM_ENCODER_WRITE_STATUS_FATAL_ERROR;
    }
    memcpy(s->out + s->pos, buffer, bytes);
    s->pos += bytes;
    if (s->pos > s->len) s->len = s->pos;
    return FLAC__STREAM_ENCODER_WRITE_STATUS_OK;
}

/* seek/tell callbacks: the encoder rewrites STREAMINFO at finish via a
 * seek back to the header. Moving pos (not len) preserves the file size. */
static FLAC__StreamEncoderSeekStatus fparity_e2e_seek_cb(
        const FLAC__StreamEncoder *encoder, FLAC__uint64 absolute_byte_offset,
        void *client_data) {
    (void)encoder;
    fparity_e2e_sink *s = (fparity_e2e_sink *)client_data;
    if ((size_t)absolute_byte_offset > s->len) {
        return FLAC__STREAM_ENCODER_SEEK_STATUS_ERROR;
    }
    s->pos = (size_t)absolute_byte_offset;
    return FLAC__STREAM_ENCODER_SEEK_STATUS_OK;
}

static FLAC__StreamEncoderTellStatus fparity_e2e_tell_cb(
        const FLAC__StreamEncoder *encoder, FLAC__uint64 *absolute_byte_offset,
        void *client_data) {
    (void)encoder;
    fparity_e2e_sink *s = (fparity_e2e_sink *)client_data;
    *absolute_byte_offset = (FLAC__uint64)s->pos;
    return FLAC__STREAM_ENCODER_TELL_STATUS_OK;
}

size_t fparity_e2e_encode(uint8_t *out, size_t out_cap,
                          const int32_t *pcm, uint32_t channels,
                          uint32_t bits_per_sample, uint32_t sample_rate,
                          uint64_t frames, uint32_t compression,
                          uint32_t block_size, int md5_check) {
    FLAC__StreamEncoder *enc = FLAC__stream_encoder_new();
    if (!enc) return 0;

    FLAC__stream_encoder_set_channels(enc, channels);
    FLAC__stream_encoder_set_bits_per_sample(enc, bits_per_sample);
    FLAC__stream_encoder_set_sample_rate(enc, sample_rate);
    FLAC__stream_encoder_set_compression_level(enc, compression);
    FLAC__stream_encoder_set_total_samples_estimate(enc, frames);
    if (block_size != 0) {
        FLAC__stream_encoder_set_blocksize(enc, block_size);
    }
    (void)md5_check; /* encoder always writes the real MD5 into STREAMINFO */

    fparity_e2e_sink sink;
    sink.out = out;
    sink.cap = out_cap;
    sink.pos = 0;
    sink.len = 0;
    sink.overflow = 0;

    /* Seekable init so libFLAC rewrites the STREAMINFO header on finish
     * (min/max framesize, total samples, MD5). */
    FLAC__StreamEncoderInitStatus st = FLAC__stream_encoder_init_stream(
        enc, fparity_e2e_write_cb, fparity_e2e_seek_cb, fparity_e2e_tell_cb,
        NULL /*metadata callback*/, &sink);
    if (st != FLAC__STREAM_ENCODER_INIT_STATUS_OK) {
        FLAC__stream_encoder_delete(enc);
        return 0;
    }

    /* Feed the whole PCM buffer at once. libFLAC wants per-channel-
     * interleaved int32 with each sample sign-extended in its low bits. */
    FLAC__bool ok = FLAC__stream_encoder_process_interleaved(
        enc, (const FLAC__int32 *)pcm, (uint32_t)frames);

    if (ok) {
        ok = FLAC__stream_encoder_finish(enc);
    } else {
        FLAC__stream_encoder_finish(enc);
    }

    size_t final_len = sink.len;
    FLAC__stream_encoder_delete(enc);

    if (sink.overflow || !ok) return 0;
    return final_len;
}
