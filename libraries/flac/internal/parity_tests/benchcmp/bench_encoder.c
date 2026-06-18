/* FLAC native-vs-Cgo benchmark — libFLAC stream ENCODER TU.
 *
 * Isolated so stream_encoder.c's file-static helpers do not collide with
 * stream_decoder.c (in bench_decoder.c). fbench_encode drives libFLAC's
 * full stream encoder over an interleaved int32 PCM buffer using NULL
 * seek/tell callbacks (no STREAMINFO rewrite) — the same streaming shape
 * the native nativeflac adapter uses — and collects the .flac bytes into a
 * caller-supplied buffer via a write callback.
 */

#include "src/libFLAC/stream_encoder.c"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    uint8_t *out;
    size_t   cap;
    size_t   pos;
    int      overflow;
} fbench_sink;

static FLAC__StreamEncoderWriteStatus fbench_write_cb(
        const FLAC__StreamEncoder *encoder, const FLAC__byte buffer[],
        size_t bytes, uint32_t samples, uint32_t current_frame, void *client_data) {
    (void)encoder; (void)samples; (void)current_frame;
    fbench_sink *s = (fbench_sink *)client_data;
    if (s->pos + bytes > s->cap) {
        s->overflow = 1;
        return FLAC__STREAM_ENCODER_WRITE_STATUS_FATAL_ERROR;
    }
    memcpy(s->out + s->pos, buffer, bytes);
    s->pos += bytes;
    return FLAC__STREAM_ENCODER_WRITE_STATUS_OK;
}

/* Encode `frames` inter-channel samples of interleaved int32 PCM into a
 * FLAC byte stream using NULL seek/tell (streaming, no STREAMINFO rewrite)
 * — matching the pure-Go native adapter. Returns the encoded byte count
 * (0 on failure). */
size_t fbench_encode(uint8_t *out, size_t out_cap,
                     const int32_t *pcm, uint32_t channels,
                     uint32_t bits_per_sample, uint32_t sample_rate,
                     uint64_t frames, uint32_t compression,
                     uint32_t block_size) {
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

    fbench_sink sink;
    sink.out = out;
    sink.cap = out_cap;
    sink.pos = 0;
    sink.overflow = 0;

    FLAC__StreamEncoderInitStatus st = FLAC__stream_encoder_init_stream(
        enc, fbench_write_cb, NULL /*seek*/, NULL /*tell*/,
        NULL /*metadata callback*/, &sink);
    if (st != FLAC__STREAM_ENCODER_INIT_STATUS_OK) {
        FLAC__stream_encoder_delete(enc);
        return 0;
    }

    FLAC__bool ok = FLAC__stream_encoder_process_interleaved(
        enc, (const FLAC__int32 *)pcm, (uint32_t)frames);

    if (ok) {
        ok = FLAC__stream_encoder_finish(enc);
    } else {
        FLAC__stream_encoder_finish(enc);
    }

    size_t final_len = sink.pos;
    FLAC__stream_encoder_delete(enc);

    if (sink.overflow || !ok) return 0;
    return final_len;
}
