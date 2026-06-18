/* Compiles libFLAC bitreader.c + the dependencies needed to call
 * read_frame_header_ in isolation. Since read_frame_header_ is a
 * static function inside stream_decoder.c, we cannot link it as an
 * external symbol; instead we re-implement the same parse pattern in
 * pure C using the bitreader's PUBLIC API and compare both against
 * each other. The duplicated logic is small (≈100 lines of C) and
 * structurally tracks libFLAC's read_frame_header_ word-for-word —
 * it's the parse table that matters for parity.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/crc.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/bitreader.c"

#include "_cgo_export.h"
#include <stdlib.h>
#include <string.h>

/* Read callback: pulls bytes from a Go-side []byte slab via the
 * exported goFrameHdrRead trampoline. */
FLAC__bool fparity_fh_read_cb(FLAC__byte buf[], size_t *bytes, void *cd) {
    return goFrameHdrRead(buf, bytes, cd) ? 1 : 0;
}

/* fparity_fh_result_t is declared in cgo.go's preamble; this file
 * uses that typedef via the cgo-generated _cgo_export.h above. */

#define FH_OK           0
#define FH_READ_ERROR   1
#define FH_BAD_HEADER   2
#define FH_UNPARSEABLE  3

#define BAD_HEADER_RETURN do { out->status = FH_BAD_HEADER; return; } while(0)
#define READ_ERROR_RETURN do { out->status = FH_READ_ERROR; return; } while(0)

void fparity_read_frame_header(FLAC__BitReader *br,
                               uint8_t hdr0, uint8_t hdr1,
                               int has_streaminfo,
                               uint32_t si_sr, uint32_t si_bps,
                               uint32_t si_minbs, uint32_t si_maxbs,
                               uint32_t fixed_block_size,
                               fparity_fh_result_t *out) {
    memset(out, 0, sizeof(*out));
    uint8_t raw[16] = {hdr0, hdr1};
    int rawlen = 2;
    int unparseable = 0;
    if (raw[1] & 0x02) unparseable = 1;

    FLAC__uint32 x;
    for (int i = 0; i < 2; i++) {
        if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) READ_ERROR_RETURN;
        if (x == 0xFF) BAD_HEADER_RETURN;
        raw[rawlen++] = (uint8_t)x;
    }

    uint32_t blocksize_hint = 0;
    switch ((x = raw[2] >> 4)) {
    case 0:  unparseable = 1; break;
    case 1:  out->blocksize = 192; break;
    case 2: case 3: case 4: case 5:
             out->blocksize = 576u << (x-2); break;
    case 6: case 7: blocksize_hint = x; break;
    default: out->blocksize = 256u << (x-8);
    }

    uint32_t sample_rate_hint = 0;
    switch ((x = raw[2] & 0x0F)) {
    case 0: if (has_streaminfo) out->sample_rate = si_sr; else unparseable = 1; break;
    case 1: out->sample_rate = 88200; break;
    case 2: out->sample_rate = 176400; break;
    case 3: out->sample_rate = 192000; break;
    case 4: out->sample_rate = 8000; break;
    case 5: out->sample_rate = 16000; break;
    case 6: out->sample_rate = 22050; break;
    case 7: out->sample_rate = 24000; break;
    case 8: out->sample_rate = 32000; break;
    case 9: out->sample_rate = 44100; break;
    case 10: out->sample_rate = 48000; break;
    case 11: out->sample_rate = 96000; break;
    case 12: case 13: case 14: sample_rate_hint = x; break;
    case 15: BAD_HEADER_RETURN;
    }

    x = (raw[3] >> 4);
    if (x & 8) {
        out->channels = 2;
        switch (x & 7) {
        case 0: out->channel_assignment = 1; break; /* LEFT_SIDE */
        case 1: out->channel_assignment = 2; break; /* RIGHT_SIDE */
        case 2: out->channel_assignment = 3; break; /* MID_SIDE */
        default: unparseable = 1;
        }
    } else {
        out->channels = x + 1;
        out->channel_assignment = 0;
    }

    switch ((x = (raw[3] & 0x0E) >> 1)) {
    case 0: if (has_streaminfo) out->bits_per_sample = si_bps; else unparseable = 1; break;
    case 1: out->bits_per_sample = 8; break;
    case 2: out->bits_per_sample = 12; break;
    case 3: unparseable = 1; break;
    case 4: out->bits_per_sample = 16; break;
    case 5: out->bits_per_sample = 20; break;
    case 6: out->bits_per_sample = 24; break;
    case 7: out->bits_per_sample = 32; break;
    }

    if (raw[3] & 0x01) unparseable = 1;

    int variable = (raw[1] & 0x01) ||
                   (has_streaminfo && si_minbs != si_maxbs);
    if (variable) {
        FLAC__uint64 xx;
        FLAC__uint32 rl = rawlen;
        if (!FLAC__bitreader_read_utf8_uint64(br, &xx, raw, &rl)) READ_ERROR_RETURN;
        rawlen = rl;
        if (xx == 0xFFFFFFFFFFFFFFFFull) BAD_HEADER_RETURN;
        out->number_type = 1;
        out->number = xx;
    } else {
        FLAC__uint32 fr;
        FLAC__uint32 rl = rawlen;
        if (!FLAC__bitreader_read_utf8_uint32(br, &fr, raw, &rl)) READ_ERROR_RETURN;
        rawlen = rl;
        if (fr == 0xFFFFFFFFu) BAD_HEADER_RETURN;
        out->number_type = 0;
        out->number = fr;
    }

    if (blocksize_hint) {
        if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) READ_ERROR_RETURN;
        raw[rawlen++] = (uint8_t)x;
        if (blocksize_hint == 7) {
            FLAC__uint32 lo;
            if (!FLAC__bitreader_read_raw_uint32(br, &lo, 8)) READ_ERROR_RETURN;
            raw[rawlen++] = (uint8_t)lo;
            x = (x << 8) | lo;
        }
        out->blocksize = x + 1;
        if (out->blocksize > 65535) BAD_HEADER_RETURN;
    }

    if (sample_rate_hint) {
        if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) READ_ERROR_RETURN;
        raw[rawlen++] = (uint8_t)x;
        if (sample_rate_hint != 12) {
            FLAC__uint32 lo;
            if (!FLAC__bitreader_read_raw_uint32(br, &lo, 8)) READ_ERROR_RETURN;
            raw[rawlen++] = (uint8_t)lo;
            x = (x << 8) | lo;
        }
        if      (sample_rate_hint == 12) out->sample_rate = x * 1000;
        else if (sample_rate_hint == 13) out->sample_rate = x;
        else                             out->sample_rate = x * 10;
    }

    if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) READ_ERROR_RETURN;
    out->crc = (uint8_t)x;
    if (FLAC__crc8(raw, rawlen) != out->crc) BAD_HEADER_RETURN;

    if (out->number_type == 0) { /* FRAME_NUMBER → SAMPLE_NUMBER */
        uint64_t fn = out->number;
        out->number_type = 1;
        if (fixed_block_size != 0) {
            out->number = (uint64_t)fixed_block_size * fn;
        } else if (has_streaminfo) {
            if (si_minbs == si_maxbs) {
                out->number = (uint64_t)si_minbs * fn;
                out->next_fixed_block_size = si_maxbs;
            } else {
                unparseable = 1;
            }
        } else if (fn == 0) {
            out->number = 0;
            out->next_fixed_block_size = out->blocksize;
        } else {
            out->number = (uint64_t)out->blocksize * fn;
        }
    }

    if (unparseable) {
        out->status = FH_UNPARSEABLE;
        return;
    }
    out->status = FH_OK;
}
