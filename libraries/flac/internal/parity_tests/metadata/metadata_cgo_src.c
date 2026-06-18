/* Parity harness for the decoder metadata path: find_metadata_,
 * skip_id3v2_tag_, read_metadata_ dispatch, and
 * read_metadata_streaminfo_ (plus the length-skip taken by every other
 * block type during a plain decode).
 *
 * These libFLAC functions are static in stream_decoder.c, so the
 * strategy mirrors the subframe parity harness:
 *
 *   1. Fabricate metadata-block bytes with libFLAC's own bitwriter
 *      (fparity_encode_*), producing exactly the layout
 *      stream_decoder.c expects.
 *   2. Parse the bytes with libFLAC's bitreader, re-implementing the
 *      static find/read logic line-for-line on the PUBLIC bitreader
 *      API (fparity_*). The Go port is driven over the same bytes in
 *      the _test.go.
 *   3. Compare parsed fields AND the post-read stream position
 *      (bytes consumed) between the C oracle and the Go port.
 *
 * Why hand-reimplement: find_metadata_ / read_metadata_ /
 * read_metadata_streaminfo_ are STATIC in stream_decoder.c and not
 * linkable, so per the sanctioned parity convention the oracle mirrors
 * their logic on libFLAC's PUBLIC bitreader API rather than calling them.
 * This unit-level oracle is in turn cross-checked against the full
 * unmodified libFLAC decoder end-to-end by the decode_e2e parity package,
 * which runs the real stream_decoder.c.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/crc.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/bitreader.c"
#include "src/libFLAC/bitwriter.c"
#include "src/libFLAC/memory.c"

#include "_cgo_export.h"
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

/* find-metadata outcomes, mirroring the Go FindMetadataStatus enum. */
#define FM_READ_METADATA 0
#define FM_READ_FRAME    1
#define FM_READ_ERROR    2

/* read-metadata outcomes, mirroring the Go ReadMetadataStatus enum. */
#define RM_OK         0
#define RM_READ_ERROR 1
#define RM_BAD        2

static const FLAC__byte ID3V2_TAG_[3] = { 'I', 'D', '3' };

FLAC__bool fparity_md_read_cb(FLAC__byte buf[], size_t *bytes, void *cd) {
    return goMetadataRead(buf, bytes, cd) ? 1 : 0;
}

/* ── Input fabrication via libFLAC's bitwriter ─────────────────────── */

/* Encode a STREAMINFO metadata block: the 32-bit (is_last|type|length)
 * header followed by the 34-byte body. extra_pad pads the declared
 * length beyond the 34-byte fixed layout to exercise the trailing-skip
 * branch in read_metadata_streaminfo_. */
size_t fparity_encode_streaminfo(uint8_t *out, size_t out_cap,
                                 int is_last, uint32_t type,
                                 uint32_t min_blocksize, uint32_t max_blocksize,
                                 uint32_t min_framesize, uint32_t max_framesize,
                                 uint32_t sample_rate, uint32_t channels,
                                 uint32_t bits_per_sample, uint64_t total_samples,
                                 const uint8_t md5[16], uint32_t extra_pad) {
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);

    uint32_t length = FLAC__STREAM_METADATA_STREAMINFO_LENGTH + extra_pad;
    FLAC__bitwriter_write_raw_uint32(bw, is_last ? 1u : 0u, FLAC__STREAM_METADATA_IS_LAST_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, type, FLAC__STREAM_METADATA_TYPE_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, length, FLAC__STREAM_METADATA_LENGTH_LEN);

    FLAC__bitwriter_write_raw_uint32(bw, min_blocksize, FLAC__STREAM_METADATA_STREAMINFO_MIN_BLOCK_SIZE_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, max_blocksize, FLAC__STREAM_METADATA_STREAMINFO_MAX_BLOCK_SIZE_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, min_framesize, FLAC__STREAM_METADATA_STREAMINFO_MIN_FRAME_SIZE_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, max_framesize, FLAC__STREAM_METADATA_STREAMINFO_MAX_FRAME_SIZE_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, sample_rate, FLAC__STREAM_METADATA_STREAMINFO_SAMPLE_RATE_LEN);
    /* channels and bits_per_sample are stored as value-1 on disk */
    FLAC__bitwriter_write_raw_uint32(bw, channels - 1, FLAC__STREAM_METADATA_STREAMINFO_CHANNELS_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, bits_per_sample - 1, FLAC__STREAM_METADATA_STREAMINFO_BITS_PER_SAMPLE_LEN);
    FLAC__bitwriter_write_raw_uint64(bw, total_samples, FLAC__STREAM_METADATA_STREAMINFO_TOTAL_SAMPLES_LEN);
    for (int i = 0; i < 16; i++)
        FLAC__bitwriter_write_raw_uint32(bw, md5[i], 8);
    for (uint32_t i = 0; i < extra_pad; i++)
        FLAC__bitwriter_write_raw_uint32(bw, 0, 8);

    FLAC__bitwriter_zero_pad_to_byte_boundary(bw);
    const FLAC__byte *buffer; size_t bytes = 0;
    FLAC__bitwriter_get_buffer(bw, &buffer, &bytes);
    if (bytes > out_cap) bytes = out_cap;
    memcpy(out, buffer, bytes);
    FLAC__bitwriter_release_buffer(bw);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return bytes;
}

/* Encode a generic (non-STREAMINFO) metadata block: the 32-bit header
 * plus `length` body bytes filled from `body` (or zero-filled if body
 * is NULL). Used for PADDING / APPLICATION / SEEKTABLE /
 * VORBIS_COMMENT / CUESHEET / PICTURE / unknown skip-path coverage. */
size_t fparity_encode_generic(uint8_t *out, size_t out_cap,
                              int is_last, uint32_t type, uint32_t length,
                              const uint8_t *body) {
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    FLAC__bitwriter_write_raw_uint32(bw, is_last ? 1u : 0u, FLAC__STREAM_METADATA_IS_LAST_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, type, FLAC__STREAM_METADATA_TYPE_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, length, FLAC__STREAM_METADATA_LENGTH_LEN);
    for (uint32_t i = 0; i < length; i++)
        FLAC__bitwriter_write_raw_uint32(bw, body ? body[i] : 0, 8);
    FLAC__bitwriter_zero_pad_to_byte_boundary(bw);
    const FLAC__byte *buffer; size_t bytes = 0;
    FLAC__bitwriter_get_buffer(bw, &buffer, &bytes);
    if (bytes > out_cap) bytes = out_cap;
    memcpy(out, buffer, bytes);
    FLAC__bitwriter_release_buffer(bw);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return bytes;
}

/* Encode a raw byte prefix (e.g. "fLaC", an ID3v2 tag, leading frame
 * sync) directly. Just copies in. */
size_t fparity_make_prefix(uint8_t *out, size_t out_cap, const uint8_t *bytes, size_t n) {
    if (n > out_cap) n = out_cap;
    memcpy(out, bytes, n);
    return n;
}

/* ── Oracle parsers (static stream_decoder.c logic on public API) ──── */

/* Re-implements find_metadata_ (stream_decoder.c:1654). On return,
 * *cached / *lookahead / warmup[0..1] / *lost_sync reflect the decoder
 * state find_metadata_ would have left. */
int fparity_find_metadata(FLAC__BitReader *br,
                          int *cached, uint8_t *lookahead,
                          uint8_t warmup[2], int *lost_sync) {
    FLAC__uint32 x;
    uint32_t i, id;
    FLAC__bool first = 1;
    *lost_sync = 0;

    for (i = id = 0; i < 4; ) {
        if (*cached) {
            x = (FLAC__uint32)*lookahead;
            *cached = 0;
        } else {
            if (!FLAC__bitreader_read_raw_uint32(br, &x, 8))
                return FM_READ_ERROR;
        }
        if (x == FLAC__STREAM_SYNC_STRING[i]) {
            first = 1; i++; id = 0; continue;
        }
        if (id >= 3) return FM_READ_ERROR;
        if (x == ID3V2_TAG_[id]) {
            id++; i = 0;
            if (id == 3) {
                /* inline skip_id3v2_tag_ */
                FLAC__uint32 y; uint32_t k, skip = 0;
                if (!FLAC__bitreader_read_raw_uint32(br, &y, 24)) return FM_READ_ERROR;
                for (k = 0; k < 4; k++) {
                    if (!FLAC__bitreader_read_raw_uint32(br, &y, 8)) return FM_READ_ERROR;
                    skip <<= 7; skip |= (y & 0x7f);
                }
                if (!FLAC__bitreader_skip_byte_block_aligned_no_crc(br, skip)) return FM_READ_ERROR;
            }
            continue;
        }
        id = 0;
        if (x == 0xff) {
            warmup[0] = (FLAC__byte)x;
            if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) return FM_READ_ERROR;
            if (x == 0xff) {
                *lookahead = (FLAC__byte)x;
                *cached = 1;
            } else if (x >> 1 == 0x7c) {
                warmup[1] = (FLAC__byte)x;
                return FM_READ_FRAME;
            }
        }
        i = 0;
        if (first) { *lost_sync = 1; first = 0; }
    }
    return FM_READ_METADATA;
}

/* Re-implements read_metadata_ (stream_decoder.c:1719) for the
 * decode-only path: parse the header, fully parse STREAMINFO, otherwise
 * length-skip. Outputs parsed STREAMINFO fields + header. */
int fparity_read_metadata(FLAC__BitReader *br,
                          int *out_is_last, uint32_t *out_type, uint32_t *out_length,
                          int *has_stream_info,
                          uint32_t *min_bs, uint32_t *max_bs,
                          uint32_t *min_fs, uint32_t *max_fs,
                          uint32_t *sr, uint32_t *ch, uint32_t *bps,
                          uint64_t *total, uint8_t md5[16], int *md5_is_zero) {
    FLAC__bool is_last;
    FLAC__uint32 x, type, length;

    if (!FLAC__bitreader_read_raw_uint32(br, &x, FLAC__STREAM_METADATA_IS_LAST_LEN)) return RM_READ_ERROR;
    is_last = x ? 1 : 0;
    if (!FLAC__bitreader_read_raw_uint32(br, &type, FLAC__STREAM_METADATA_TYPE_LEN)) return RM_READ_ERROR;
    if (!FLAC__bitreader_read_raw_uint32(br, &length, FLAC__STREAM_METADATA_LENGTH_LEN)) return RM_READ_ERROR;

    *out_is_last = is_last; *out_type = type; *out_length = length;
    *has_stream_info = 0;

    if (type == FLAC__METADATA_TYPE_STREAMINFO) {
        FLAC__uint32 y; uint32_t used_bits = 0; FLAC__uint64 yy;
        if (!FLAC__bitreader_read_raw_uint32(br, &y, FLAC__STREAM_METADATA_STREAMINFO_MIN_BLOCK_SIZE_LEN)) return RM_READ_ERROR;
        *min_bs = y; used_bits += FLAC__STREAM_METADATA_STREAMINFO_MIN_BLOCK_SIZE_LEN;
        if (!FLAC__bitreader_read_raw_uint32(br, &y, FLAC__STREAM_METADATA_STREAMINFO_MAX_BLOCK_SIZE_LEN)) return RM_READ_ERROR;
        *max_bs = y; used_bits += FLAC__STREAM_METADATA_STREAMINFO_MAX_BLOCK_SIZE_LEN;
        if (!FLAC__bitreader_read_raw_uint32(br, &y, FLAC__STREAM_METADATA_STREAMINFO_MIN_FRAME_SIZE_LEN)) return RM_READ_ERROR;
        *min_fs = y; used_bits += FLAC__STREAM_METADATA_STREAMINFO_MIN_FRAME_SIZE_LEN;
        if (!FLAC__bitreader_read_raw_uint32(br, &y, FLAC__STREAM_METADATA_STREAMINFO_MAX_FRAME_SIZE_LEN)) return RM_READ_ERROR;
        *max_fs = y; used_bits += FLAC__STREAM_METADATA_STREAMINFO_MAX_FRAME_SIZE_LEN;
        if (!FLAC__bitreader_read_raw_uint32(br, &y, FLAC__STREAM_METADATA_STREAMINFO_SAMPLE_RATE_LEN)) return RM_READ_ERROR;
        *sr = y; used_bits += FLAC__STREAM_METADATA_STREAMINFO_SAMPLE_RATE_LEN;
        if (!FLAC__bitreader_read_raw_uint32(br, &y, FLAC__STREAM_METADATA_STREAMINFO_CHANNELS_LEN)) return RM_READ_ERROR;
        *ch = y + 1; used_bits += FLAC__STREAM_METADATA_STREAMINFO_CHANNELS_LEN;
        if (!FLAC__bitreader_read_raw_uint32(br, &y, FLAC__STREAM_METADATA_STREAMINFO_BITS_PER_SAMPLE_LEN)) return RM_READ_ERROR;
        *bps = y + 1; used_bits += FLAC__STREAM_METADATA_STREAMINFO_BITS_PER_SAMPLE_LEN;
        if (!FLAC__bitreader_read_raw_uint64(br, &yy, FLAC__STREAM_METADATA_STREAMINFO_TOTAL_SAMPLES_LEN)) return RM_READ_ERROR;
        *total = yy; used_bits += FLAC__STREAM_METADATA_STREAMINFO_TOTAL_SAMPLES_LEN;
        if (!FLAC__bitreader_read_byte_block_aligned_no_crc(br, md5, 16)) return RM_READ_ERROR;
        used_bits += 16 * 8;

        if (length < used_bits / 8) return RM_READ_ERROR;
        length -= used_bits / 8;
        if (!FLAC__bitreader_skip_byte_block_aligned_no_crc(br, length)) return RM_READ_ERROR;

        *has_stream_info = 1;
        *md5_is_zero = (memcmp(md5, "\0\0\0\0\0\0\0\0\0\0\0\0\0\0\0\0", 16) == 0) ? 1 : 0;
        return RM_OK;
    }

    /* skip path */
    if (type == FLAC__METADATA_TYPE_APPLICATION) {
        FLAC__byte id[4];
        if (!FLAC__bitreader_read_byte_block_aligned_no_crc(br, id, FLAC__STREAM_METADATA_APPLICATION_ID_LEN/8)) return RM_READ_ERROR;
        if (length < FLAC__STREAM_METADATA_APPLICATION_ID_LEN/8) return RM_READ_ERROR;
        length -= FLAC__STREAM_METADATA_APPLICATION_ID_LEN/8;
    }
    if (!FLAC__bitreader_skip_byte_block_aligned_no_crc(br, length)) return RM_READ_ERROR;
    return RM_OK;
}

/* Bytes consumed by the reader so far, relative to a known total. The
 * caller passes the full source length; we subtract the unconsumed
 * remainder. Both sides are byte aligned at every observation point. */
uint32_t fparity_bytes_consumed(FLAC__BitReader *br, uint32_t total_bytes) {
    return total_bytes - (FLAC__bitreader_get_input_bits_unconsumed(br) / 8);
}
