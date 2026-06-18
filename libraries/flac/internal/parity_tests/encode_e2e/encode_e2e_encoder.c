/* End-to-end ENCODE parity — libFLAC reference ENCODER TU.
 *
 * Isolated so stream_encoder.c's file-static helpers do not collide with
 * stream_decoder.c (in encode_e2e_decoder.c). fparity_ee_encode_noseek
 * drives libFLAC's full stream encoder over an interleaved int32 PCM
 * buffer with NULL seek/tell callbacks — exactly matching the pure-Go
 * native adapter (libraries/flac.newNativeStreamEncoder), which also
 * passes nil seek/tell. With no seek callback the encoder leaves
 * STREAMINFO at its streaming placeholder (min_framesize = 0x7fffff-style
 * max, max_framesize = 0, total_samples = estimate, md5 = all-zero) and
 * never rewrites it, so the two byte streams can be compared bit-for-bit.
 */

#ifdef HAVE_CONFIG_H
#  include <config.h>
#endif

#include "src/libFLAC/stream_encoder.c"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

/* Append-only sink: writes are sequential because there is no seek. */
typedef struct {
    uint8_t *out;
    size_t   cap;
    size_t   len;
    int      overflow;
} fparity_ee_sink;

static FLAC__StreamEncoderWriteStatus fparity_ee_write_cb(
        const FLAC__StreamEncoder *encoder, const FLAC__byte buffer[],
        size_t bytes, uint32_t samples, uint32_t current_frame, void *client_data) {
    (void)encoder; (void)samples; (void)current_frame;
    fparity_ee_sink *s = (fparity_ee_sink *)client_data;
    if (s->len + bytes > s->cap) {
        s->overflow = 1;
        return FLAC__STREAM_ENCODER_WRITE_STATUS_FATAL_ERROR;
    }
    memcpy(s->out + s->len, buffer, bytes);
    s->len += bytes;
    return FLAC__STREAM_ENCODER_WRITE_STATUS_OK;
}

/* Encode `frames` inter-channel samples of interleaved int32 PCM into a
 * FLAC byte stream using NULL seek/tell (no STREAMINFO rewrite), matching
 * the native Go encoder. Returns the encoded byte count (0 on failure). */
size_t fparity_ee_encode_noseek(uint8_t *out, size_t out_cap,
                                const int32_t *pcm, uint32_t channels,
                                uint32_t bits_per_sample, uint32_t sample_rate,
                                uint64_t frames, uint32_t compression,
                                uint32_t block_size, uint64_t total_estimate,
                                int do_verify) {
    FLAC__StreamEncoder *enc = FLAC__stream_encoder_new();
    if (!enc) return 0;

    FLAC__stream_encoder_set_channels(enc, channels);
    FLAC__stream_encoder_set_bits_per_sample(enc, bits_per_sample);
    FLAC__stream_encoder_set_sample_rate(enc, sample_rate);
    FLAC__stream_encoder_set_compression_level(enc, compression);
    if (do_verify)
        FLAC__stream_encoder_set_verify(enc, true);
    if (block_size != 0)
        FLAC__stream_encoder_set_blocksize(enc, block_size);
    if (total_estimate != 0)
        FLAC__stream_encoder_set_total_samples_estimate(enc, total_estimate);

    fparity_ee_sink sink;
    sink.out = out;
    sink.cap = out_cap;
    sink.len = 0;
    sink.overflow = 0;

    FLAC__StreamEncoderInitStatus st = FLAC__stream_encoder_init_stream(
        enc, fparity_ee_write_cb,
        NULL /* seek  */, NULL /* tell */,
        NULL /* metadata callback */, &sink);
    if (st != FLAC__STREAM_ENCODER_INIT_STATUS_OK) {
        FLAC__stream_encoder_delete(enc);
        return 0;
    }

    FLAC__bool ok = FLAC__stream_encoder_process_interleaved(
        enc, (const FLAC__int32 *)pcm, (uint32_t)frames);
    if (ok)
        ok = FLAC__stream_encoder_finish(enc);
    else
        FLAC__stream_encoder_finish(enc);

    FLAC__stream_encoder_delete(enc);

    if (sink.overflow || !ok) return 0;
    return sink.len;
}
