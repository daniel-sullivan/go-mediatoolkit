/* Parity harness for undo_channel_coding + the read_frame_ frame-footer
 * CRC-16 verification. Both are static (or inline in the read_frame_
 * body) inside stream_decoder.c, so:
 *
 *   - fparity_undo_channel_coding re-implements undo_channel_coding's
 *     body line-for-line (stream_decoder.c:3477) on plain C arrays. The
 *     Go port (nativeflac.UndoChannelCoding) is compared sample-for-
 *     sample against it across all four channel assignments and the
 *     33-bit side path.
 *
 *   - fparity_build_footer / fparity_verify_footer drive libFLAC's REAL
 *     bitreader CRC plumbing (FLAC__bitreader_reset_read_crc16 /
 *     _get_read_crc16) the same way read_frame_ does
 *     (stream_decoder.c:2384–2452): seed the CRC with the two header-
 *     warmup bytes folded through FLAC__CRC16_UPDATE, consume the
 *     payload, snapshot the running CRC, then read + compare the 16-bit
 *     footer. The Go port (nativeflac.ReadFrameFooterCRC) must agree.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/crc.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/bitreader.c"
#include "src/libFLAC/bitwriter.c"
#include "src/libFLAC/memory.c"

#include "private/crc.h"

#include "_cgo_export.h"
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

/* Channel assignment codes, matching FLAC__ChannelAssignment. */
#define CA_INDEPENDENT 0
#define CA_LEFT_SIDE   1
#define CA_RIGHT_SIDE  2
#define CA_MID_SIDE    3

FLAC__bool fparity_ch_read_cb(FLAC__byte buf[], size_t *bytes, void *cd) {
    return goChannelRead(buf, bytes, cd) ? 1 : 0;
}

/* Line-for-line re-implementation of undo_channel_coding
 * (stream_decoder.c:3477). output0/output1 are the FLAC__int32 *output[]
 * channels; side is the FLAC__int64 *side_subframe buffer used when
 * side_in_use (bps == 32). Mutates output0/output1 in place. */
void fparity_undo_channel_coding(int assignment,
                                 int32_t *output0,
                                 int32_t *output1,
                                 const int64_t *side,
                                 int side_in_use,
                                 uint32_t blocksize) {
    uint32_t i;
    switch (assignment) {
    case CA_INDEPENDENT:
        break;
    case CA_LEFT_SIDE:
        for (i = 0; i < blocksize; i++)
            if (side_in_use)
                output1[i] = output0[i] - side[i];
            else
                output1[i] = output0[i] - output1[i];
        break;
    case CA_RIGHT_SIDE:
        for (i = 0; i < blocksize; i++)
            if (side_in_use)
                output0[i] = output1[i] + side[i];
            else
                output0[i] += output1[i];
        break;
    case CA_MID_SIDE:
        for (i = 0; i < blocksize; i++) {
            if (!side_in_use) {
                FLAC__int32 mid, sd;
                mid = output0[i];
                sd = output1[i];
                mid = ((uint32_t) mid) << 1;
                mid |= (sd & 1);
                output0[i] = (mid + sd) >> 1;
                output1[i] = (mid - sd) >> 1;
            } else { /* bps == 32 */
                FLAC__int64 mid;
                mid = ((uint64_t) output0[i]) << 1;
                mid |= (side[i] & 1);
                output0[i] = (mid + side[i]) >> 1;
                output1[i] = (mid - side[i]) >> 1;
            }
        }
        break;
    default:
        break;
    }
}

/* Build a frame-body buffer: `payload` raw bytes followed by a 16-bit
 * footer CRC computed over (warmup0, warmup1, payload) exactly as
 * read_frame_ accumulates it. We emit via the bitwriter so the byte
 * layout (and the CRC arithmetic) matches libFLAC. When corrupt!=0 the
 * stored footer is XOR'd with 0xFFFF so verification fails. */
size_t fparity_build_footer(uint8_t *out, size_t out_cap,
                            const uint8_t *payload, size_t payload_len,
                            uint8_t warmup0, uint8_t warmup1,
                            int corrupt) {
    /* Compute the expected CRC the same way read_frame_ does. */
    FLAC__uint16 crc = 0;
    crc = FLAC__CRC16_UPDATE(warmup0, crc);
    crc = FLAC__CRC16_UPDATE(warmup1, crc);
    {
        size_t k;
        for (k = 0; k < payload_len; k++)
            crc = FLAC__CRC16_UPDATE(payload[k], crc);
    }
    if (corrupt)
        crc ^= 0xFFFF;

    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    if (payload_len > 0)
        FLAC__bitwriter_write_byte_block(bw, payload, (uint32_t)payload_len);
    FLAC__bitwriter_write_raw_uint32(bw, crc, FLAC__FRAME_FOOTER_CRC_LEN);

    const FLAC__byte *buf = NULL;
    size_t bytes = 0;
    FLAC__bitwriter_get_buffer(bw, &buf, &bytes);
    if (bytes > out_cap) bytes = out_cap;
    memcpy(out, buf, bytes);
    FLAC__bitwriter_release_buffer(bw);
    FLAC__bitwriter_delete(bw);
    return bytes;
}

/* Verify the footer the way read_frame_ does: seed the bitreader CRC
 * with the two warmup bytes, consume payload_len payload bytes (which
 * the reader folds into the running CRC), snapshot the CRC, read the
 * 16-bit footer, and compare. *out_match receives the comparison.
 * Returns 0 on success, 1 if a read failed. */
int fparity_verify_footer(FLAC__BitReader *br,
                          uint8_t warmup0, uint8_t warmup1,
                          size_t payload_len,
                          int *out_match) {
    FLAC__uint32 frame_crc = 0;
    frame_crc = FLAC__CRC16_UPDATE(warmup0, frame_crc);
    frame_crc = FLAC__CRC16_UPDATE(warmup1, frame_crc);
    FLAC__bitreader_reset_read_crc16(br, (FLAC__uint16)frame_crc);

    /* Consume the payload bytes so they are folded into the CRC. */
    {
        size_t k;
        FLAC__uint32 b;
        for (k = 0; k < payload_len; k++) {
            if (!FLAC__bitreader_read_raw_uint32(br, &b, 8))
                return 1;
        }
    }

    FLAC__uint16 expected = FLAC__bitreader_get_read_crc16(br);
    FLAC__uint32 x = 0;
    if (!FLAC__bitreader_read_raw_uint32(br, &x, FLAC__FRAME_FOOTER_CRC_LEN))
        return 1;
    *out_match = ((FLAC__uint16)x == expected) ? 1 : 0;
    return 0;
}
