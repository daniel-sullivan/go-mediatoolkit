/* Parity harness for read_frame_ + read_subframe_ (stream_decoder.c).
 *
 * Strategy:
 *   1. Fabricate a complete FLAC audio frame (header + per-channel
 *      subframes + footer CRC-16) using libFLAC's OWN framing writers
 *      (FLAC__frame_add_header / FLAC__subframe_add_*) plus the
 *      bitwriter's CRC-16 tracker for the footer. These are the exact
 *      bytes stream_decoder.c's read_frame_ consumes, so the CRC-8
 *      header trailer and CRC-16 footer are computed by libFLAC itself.
 *   2. Decode the buffer with a faithful C re-implementation of
 *      read_frame_ / read_subframe_ built on libFLAC's PUBLIC bitreader
 *      API (the real functions are static), AND with the Go port (in the
 *      test). Both must yield identical interleaved samples + identical
 *      CRC acceptance.
 *
 * The C oracle re-implements read_subframe_'s dispatch + read_frame_'s
 * channel loop / bps adjustment / undo_channel_coding / footer-CRC check
 * line-for-line. The shared bitreader/bitwriter + CRC tables guarantee
 * the low-level bit access and CRC math are bit-exact.
 *
 * Why hand-reimplement: read_frame_ / read_subframe_ are STATIC in
 * stream_decoder.c and not linkable, so per the sanctioned parity
 * convention the oracle mirrors their logic on libFLAC's PUBLIC bitreader
 * API rather than calling them. This unit-level oracle is in turn
 * cross-checked against the full unmodified libFLAC decoder end-to-end by
 * the decode_e2e parity package, which runs the real stream_decoder.c.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/crc.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/float.c"
#include "src/libFLAC/fixed.c"
#include "src/libFLAC/lpc.c"
#include "src/libFLAC/bitreader.c"
#include "src/libFLAC/bitwriter.c"
#include "src/libFLAC/memory.c"
#include "src/libFLAC/stream_encoder_framing.c"

/* On Windows (incl. mingw) libFLAC's compat.h redirects flac_fprintf/
 * flac_fopen to fprintf_utf8/fopen_utf8 (lpc.c references flac_fprintf
 * in its debug path), which live in share/win_utf8_io.c. That TU is not
 * otherwise compiled into this parity binary, so pull it in here to
 * satisfy the link. No-op on non-Windows, where compat.h maps the
 * flac_* macros straight to the stdio functions. */
#ifdef _WIN32
#include "src/share/win_utf8_io/win_utf8_io.c"
#endif

#include "_cgo_export.h"
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

/* read_frame_ status codes mirroring the Go FrameStatus enum. */
#define FR_OK            0
#define FR_READ_ERROR    1
#define FR_LOST_SYNC     2
#define FR_BAD_HEADER    3
#define FR_OUT_OF_BOUNDS 4

FLAC__bool fparity_frame_read_cb(FLAC__byte buf[], size_t *bytes, void *cd) {
    return goFrameRead(buf, bytes, cd) ? 1 : 0;
}

/* ── Frame fabrication ──────────────────────────────────────────────── */

/* The fparity_sf_desc_t struct is defined in the cgo preamble (cgo.go) and
 * reaches this translation unit via "_cgo_export.h"; do not redefine it here
 * or the compiler reports a typedef redefinition. */

/* Assemble a full frame and return its byte length. The header fields are
 * the minimal set read_frame_header_ needs; we always emit fixed-blocking
 * (number_type = SAMPLE_NUMBER chosen by blocking strategy bit) so the
 * sample number round-trips directly. */
size_t fparity_assemble_frame(uint8_t *out, size_t out_cap,
                              uint32_t blocksize, uint32_t sample_rate,
                              uint32_t channels, uint32_t bits_per_sample,
                              uint32_t channel_assignment,
                              uint64_t sample_number,
                              const fparity_sf_desc_t *subframes) {
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);

    FLAC__FrameHeader header;
    memset(&header, 0, sizeof(header));
    header.blocksize = blocksize;
    header.sample_rate = sample_rate;
    header.channels = channels;
    header.channel_assignment = (FLAC__ChannelAssignment)channel_assignment;
    header.bits_per_sample = bits_per_sample;
    header.number_type = FLAC__FRAME_NUMBER_TYPE_SAMPLE_NUMBER;
    header.number.sample_number = sample_number;

    if (!FLAC__frame_add_header(&header, bw)) { goto fail; }

    for (uint32_t ch = 0; ch < channels; ch++) {
        const fparity_sf_desc_t *d = &subframes[ch];
        uint32_t residual_samples = blocksize - d->order;
        /* The FLAC__subframe_add_* writers expect the EFFECTIVE bps already
         * reduced by wasted bits (stream_encoder.c:158/3837 store
         * subframe_bps = bits_per_sample - wasted). The descriptor carries
         * the full subframe_bps + separate wasted_bits, so reduce it here;
         * the decoder reads the warmup/value/residual at this same width. */
        uint32_t eff_bps = d->subframe_bps - d->wasted_bits;
        switch (d->type) {
        case 0: { /* CONSTANT */
            FLAC__Subframe_Constant c;
            c.value = d->constant_value;
            if (!FLAC__subframe_add_constant(&c, eff_bps, d->wasted_bits, bw)) goto fail;
            break;
        }
        case 1: { /* VERBATIM (int32) */
            FLAC__Subframe_Verbatim v;
            memset(&v, 0, sizeof(v));
            v.data_type = FLAC__VERBATIM_SUBFRAME_DATA_TYPE_INT32;
            v.data.int32 = d->verbatim;
            if (!FLAC__subframe_add_verbatim(&v, blocksize, eff_bps, d->wasted_bits, bw)) goto fail;
            break;
        }
        case 2: { /* FIXED */
            FLAC__Subframe_Fixed f;
            FLAC__EntropyCodingMethod_PartitionedRiceContents contents;
            memset(&f, 0, sizeof(f));
            memset(&contents, 0, sizeof(contents));
            f.order = d->order;
            f.residual = (FLAC__int32 *)d->residual;
            for (uint32_t i = 0; i < d->order; i++) f.warmup[i] = d->warmup[i];
            f.entropy_coding_method.type = d->is_extended ?
                FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2 :
                FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE;
            f.entropy_coding_method.data.partitioned_rice.order = d->partition_order;
            contents.parameters = (uint32_t *)d->rice_params;
            /* add_residual_partitioned_rice_ reads raw_bits[i] for EVERY
             * partition to choose escape-vs-rice; a NULL here segfaults. We
             * never escape, so supply a zeroed array sized to the partitions.
             * FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ORDER_LEN is an
             * extern const (== 4), not a macro, so it is not a constant
             * expression in C: sizing an array with it yields a VLA, and GCC
             * rejects an initializer on a VLA. Use the literal 1u << 4 so the
             * array is fixed-size and memset it instead of an initializer. */
            uint32_t f_raw_bits[1u << 4]; /* 1u << FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ORDER_LEN */
            memset(f_raw_bits, 0, sizeof(f_raw_bits));
            contents.raw_bits = f_raw_bits;
            f.entropy_coding_method.data.partitioned_rice.contents = &contents;
            if (!FLAC__subframe_add_fixed(&f, residual_samples, eff_bps, d->wasted_bits, bw)) goto fail;
            break;
        }
        case 3: { /* LPC */
            FLAC__Subframe_LPC l;
            FLAC__EntropyCodingMethod_PartitionedRiceContents contents;
            memset(&l, 0, sizeof(l));
            memset(&contents, 0, sizeof(contents));
            l.order = d->order;
            l.qlp_coeff_precision = d->qlp_coeff_precision;
            l.quantization_level = d->quantization_level;
            l.residual = (FLAC__int32 *)d->residual;
            for (uint32_t i = 0; i < d->order; i++) { l.warmup[i] = d->warmup[i]; l.qlp_coeff[i] = d->qlp_coeff[i]; }
            l.entropy_coding_method.type = d->is_extended ?
                FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2 :
                FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE;
            l.entropy_coding_method.data.partitioned_rice.order = d->partition_order;
            contents.parameters = (uint32_t *)d->rice_params;
            /* See FIXED case: raw_bits[i] is read for every partition, and the
             * size macro is actually an extern const, so use the literal 1u<<4
             * (VLA + initializer is rejected by GCC). */
            uint32_t l_raw_bits[1u << 4]; /* 1u << FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ORDER_LEN */
            memset(l_raw_bits, 0, sizeof(l_raw_bits));
            contents.raw_bits = l_raw_bits;
            l.entropy_coding_method.data.partitioned_rice.contents = &contents;
            if (!FLAC__subframe_add_lpc(&l, residual_samples, eff_bps, d->wasted_bits, bw)) goto fail;
            break;
        }
        default: goto fail;
        }
    }

    /* zero-pad to byte boundary (read_zero_padding_ consumes these) */
    if (!FLAC__bitwriter_zero_pad_to_byte_boundary(bw)) goto fail;

    /* footer CRC-16 over everything written so far */
    FLAC__uint16 crc16;
    if (!FLAC__bitwriter_get_write_crc16(bw, &crc16)) goto fail;
    if (!FLAC__bitwriter_write_raw_uint32(bw, crc16, FLAC__FRAME_FOOTER_CRC_LEN)) goto fail;

    const FLAC__byte *buffer; size_t bytes = 0;
    if (!FLAC__bitwriter_get_buffer(bw, &buffer, &bytes)) goto fail;
    if (bytes > out_cap) bytes = out_cap;
    memcpy(out, buffer, bytes);
    FLAC__bitwriter_release_buffer(bw);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return bytes;

fail:
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return 0;
}

/* ── Faithful read_frame_ oracle ────────────────────────────────────── */

/* read_residual_partitioned_rice_ re-implementation (non-escape +
 * escape), identical to the one in the subframe parity harness. */
static int oracle_read_residual(FLAC__BitReader *br, uint32_t predictor_order,
                                uint32_t partition_order, uint32_t blocksize,
                                int is_extended, int32_t *residual) {
    uint32_t partitions = 1u << partition_order;
    uint32_t partition_samples = blocksize >> partition_order;
    uint32_t plen = is_extended ? FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2_PARAMETER_LEN
                                : FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_PARAMETER_LEN;
    uint32_t pesc = is_extended ? FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2_ESCAPE_PARAMETER
                                : FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ESCAPE_PARAMETER;
    uint32_t sample = 0;
    for (uint32_t p = 0; p < partitions; p++) {
        FLAC__uint32 rp;
        if (!FLAC__bitreader_read_raw_uint32(br, &rp, plen)) return FR_READ_ERROR;
        if (rp < pesc) {
            uint32_t u = (p == 0) ? partition_samples - predictor_order : partition_samples;
            if (!FLAC__bitreader_read_rice_signed_block(br, residual + sample, u, rp)) return FR_LOST_SYNC;
            sample += u;
        } else {
            FLAC__uint32 rb;
            if (!FLAC__bitreader_read_raw_uint32(br, &rb, FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_RAW_LEN)) return FR_READ_ERROR;
            uint32_t start = (p == 0) ? predictor_order : 0;
            for (uint32_t u = start; u < partition_samples; u++, sample++) {
                if (rb == 0) { residual[sample] = 0; }
                else {
                    FLAC__int32 x;
                    if (!FLAC__bitreader_read_raw_int32(br, &x, rb)) return FR_READ_ERROR;
                    residual[sample] = x;
                }
            }
        }
    }
    return FR_OK;
}

/* read_subframe_ oracle. output[] are the int32 channel buffers; side is
 * the int64 side buffer; *side_in_use mirrors decoder->private_. */
static int oracle_read_subframe(FLAC__BitReader *br, uint32_t channel,
                                uint32_t bps, uint32_t blocksize,
                                int32_t **output, int64_t *side, int *side_in_use,
                                int32_t *residual_scratch) {
    FLAC__uint32 x;
    FLAC__bool wasted_bits;
    uint32_t wasted = 0;
    if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) return FR_READ_ERROR;
    wasted_bits = (x & 1);
    x &= 0xfe;
    if (wasted_bits) {
        uint32_t u;
        if (!FLAC__bitreader_read_unary_unsigned(br, &u)) return FR_READ_ERROR;
        wasted = u + 1;
        if (wasted >= bps) return FR_LOST_SYNC;
        bps -= wasted;
    }

    if (x & 0x80) return FR_LOST_SYNC;
    else if (x == 0) { /* CONSTANT */
        FLAC__int64 v;
        if (!FLAC__bitreader_read_raw_int64(br, &v, bps)) return FR_READ_ERROR;
        if (bps <= 32) { for (uint32_t i = 0; i < blocksize; i++) output[channel][i] = (FLAC__int32)v; }
        else { *side_in_use = 1; for (uint32_t i = 0; i < blocksize; i++) side[i] = v; }
    }
    else if (x == 2) { /* VERBATIM */
        if (bps < 33) {
            for (uint32_t i = 0; i < blocksize; i++) {
                FLAC__int32 s; if (!FLAC__bitreader_read_raw_int32(br, &s, bps)) return FR_READ_ERROR;
                output[channel][i] = s;
            }
        } else {
            *side_in_use = 1;
            for (uint32_t i = 0; i < blocksize; i++) {
                FLAC__int64 s; if (!FLAC__bitreader_read_raw_int64(br, &s, bps)) return FR_READ_ERROR;
                side[i] = s;
            }
        }
    }
    else if (x < 16) return FR_LOST_SYNC;
    else if (x <= 24) { /* FIXED */
        uint32_t order = (x >> 1) & 7;
        if (blocksize <= order) return FR_LOST_SYNC;
        FLAC__int64 warmup[4];
        for (uint32_t u = 0; u < order; u++) {
            FLAC__int64 v; if (!FLAC__bitreader_read_raw_int64(br, &v, bps)) return FR_READ_ERROR;
            warmup[u] = v;
        }
        FLAC__uint32 ec;
        if (!FLAC__bitreader_read_raw_uint32(br, &ec, FLAC__ENTROPY_CODING_METHOD_TYPE_LEN)) return FR_READ_ERROR;
        if (ec != FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE && ec != FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2) return FR_LOST_SYNC;
        FLAC__uint32 po;
        if (!FLAC__bitreader_read_raw_uint32(br, &po, FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ORDER_LEN)) return FR_READ_ERROR;
        if ((blocksize >> po < order) || (blocksize % (1u << po) > 0)) return FR_LOST_SYNC;
        int st = oracle_read_residual(br, order, po, blocksize, ec == FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2, residual_scratch);
        if (st != FR_OK) return st;
        if (bps < 33) {
            for (uint32_t i = 0; i < order; i++) output[channel][i] = (FLAC__int32)warmup[i];
            if (bps + order <= 32) FLAC__fixed_restore_signal(residual_scratch, blocksize - order, order, output[channel] + order);
            else FLAC__fixed_restore_signal_wide(residual_scratch, blocksize - order, order, output[channel] + order);
        } else {
            *side_in_use = 1;
            for (uint32_t i = 0; i < order; i++) side[i] = warmup[i];
            FLAC__fixed_restore_signal_wide_33bit(residual_scratch, blocksize - order, order, side + order);
        }
    }
    else if (x < 64) return FR_LOST_SYNC;
    else { /* LPC */
        uint32_t order = ((x >> 1) & 31) + 1;
        if (blocksize <= order) return FR_LOST_SYNC;
        FLAC__int64 warmup[32];
        for (uint32_t u = 0; u < order; u++) {
            FLAC__int64 v; if (!FLAC__bitreader_read_raw_int64(br, &v, bps)) return FR_READ_ERROR;
            warmup[u] = v;
        }
        FLAC__uint32 prec;
        if (!FLAC__bitreader_read_raw_uint32(br, &prec, FLAC__SUBFRAME_LPC_QLP_COEFF_PRECISION_LEN)) return FR_READ_ERROR;
        if (prec == (1u << FLAC__SUBFRAME_LPC_QLP_COEFF_PRECISION_LEN) - 1) return FR_LOST_SYNC;
        prec += 1;
        FLAC__int32 shift;
        if (!FLAC__bitreader_read_raw_int32(br, &shift, FLAC__SUBFRAME_LPC_QLP_SHIFT_LEN)) return FR_READ_ERROR;
        if (shift < 0) return FR_LOST_SYNC;
        FLAC__int32 qlp[32];
        for (uint32_t u = 0; u < order; u++) {
            FLAC__int32 c; if (!FLAC__bitreader_read_raw_int32(br, &c, prec)) return FR_READ_ERROR;
            qlp[u] = c;
        }
        FLAC__uint32 ec;
        if (!FLAC__bitreader_read_raw_uint32(br, &ec, FLAC__ENTROPY_CODING_METHOD_TYPE_LEN)) return FR_READ_ERROR;
        if (ec != FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE && ec != FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2) return FR_LOST_SYNC;
        FLAC__uint32 po;
        if (!FLAC__bitreader_read_raw_uint32(br, &po, FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ORDER_LEN)) return FR_READ_ERROR;
        if ((blocksize >> po < order) || (blocksize % (1u << po) > 0)) return FR_LOST_SYNC;
        int st = oracle_read_residual(br, order, po, blocksize, ec == FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2, residual_scratch);
        if (st != FR_OK) return st;
        if (bps <= 32) {
            for (uint32_t i = 0; i < order; i++) output[channel][i] = (FLAC__int32)warmup[i];
            if (FLAC__lpc_max_residual_bps(bps, qlp, order, shift) <= 32 &&
                FLAC__lpc_max_prediction_before_shift_bps(bps, qlp, order) <= 32)
                FLAC__lpc_restore_signal(residual_scratch, blocksize - order, qlp, order, shift, output[channel] + order);
            else
                FLAC__lpc_restore_signal_wide(residual_scratch, blocksize - order, qlp, order, shift, output[channel] + order);
        } else {
            *side_in_use = 1;
            for (uint32_t i = 0; i < order; i++) side[i] = warmup[i];
            FLAC__lpc_restore_signal_wide_33bit(residual_scratch, blocksize - order, qlp, order, shift, side + order);
        }
    }

    /* wasted-bits left shift */
    if (wasted) {
        if (bps + wasted < 33) {
            for (uint32_t i = 0; i < blocksize; i++) {
                uint32_t val = output[channel][i];
                output[channel][i] = (val << wasted);
            }
        } else {
            *side_in_use = 1;
            for (uint32_t i = 0; i < blocksize; i++) {
                uint64_t val = output[channel][i];
                side[i] = (val << wasted);
            }
        }
    }
    return FR_OK;
}

/* read_frame_ oracle: parses the whole frame, applies undo_channel_coding,
 * verifies the footer CRC, and writes interleaved samples into out (length
 * blocksize*channels). header fields are passed back through the out_*
 * pointers. Returns FR_OK / FR_LOST_SYNC / FR_READ_ERROR / FR_BAD_HEADER. */
int fparity_decode_frame(FLAC__BitReader *br,
                         uint8_t hdr0, uint8_t hdr1,
                         uint32_t si_sr, uint32_t si_bps,
                         uint32_t si_minbs, uint32_t si_maxbs,
                         int32_t *interleaved,
                         uint32_t *out_blocksize, uint32_t *out_channels,
                         uint32_t *out_bps, uint32_t *out_channel_assignment,
                         uint64_t *out_sample_number) {
    /* seed CRC-16 from the two header-warmup bytes */
    uint32_t frame_crc = 0;
    frame_crc = FLAC__CRC16_UPDATE(hdr0, frame_crc);
    frame_crc = FLAC__CRC16_UPDATE(hdr1, frame_crc);
    FLAC__bitreader_reset_read_crc16(br, (FLAC__uint16)frame_crc);

    /* parse the frame header (faithful to read_frame_header_) */
    uint8_t raw[16] = {hdr0, hdr1};
    int rawlen = 2;
    int unparseable = 0;
    if (raw[1] & 0x02) unparseable = 1;
    FLAC__uint32 x;
    for (int i = 0; i < 2; i++) {
        if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) return FR_READ_ERROR;
        if (x == 0xFF) return FR_BAD_HEADER;
        raw[rawlen++] = (uint8_t)x;
    }
    uint32_t blocksize = 0, sample_rate = 0, channels = 0, bps = 0, channel_assignment = 0;
    uint32_t blocksize_hint = 0;
    switch ((x = raw[2] >> 4)) {
    case 0: unparseable = 1; break;
    case 1: blocksize = 192; break;
    case 2: case 3: case 4: case 5: blocksize = 576u << (x-2); break;
    case 6: case 7: blocksize_hint = x; break;
    default: blocksize = 256u << (x-8);
    }
    uint32_t sample_rate_hint = 0;
    switch ((x = raw[2] & 0x0F)) {
    case 0: if (si_sr) sample_rate = si_sr; else unparseable = 1; break;
    case 1: sample_rate = 88200; break;
    case 2: sample_rate = 176400; break;
    case 3: sample_rate = 192000; break;
    case 4: sample_rate = 8000; break;
    case 5: sample_rate = 16000; break;
    case 6: sample_rate = 22050; break;
    case 7: sample_rate = 24000; break;
    case 8: sample_rate = 32000; break;
    case 9: sample_rate = 44100; break;
    case 10: sample_rate = 48000; break;
    case 11: sample_rate = 96000; break;
    case 12: case 13: case 14: sample_rate_hint = x; break;
    case 15: return FR_BAD_HEADER;
    }
    x = (raw[3] >> 4);
    if (x & 8) {
        channels = 2;
        switch (x & 7) {
        case 0: channel_assignment = 1; break;
        case 1: channel_assignment = 2; break;
        case 2: channel_assignment = 3; break;
        default: unparseable = 1;
        }
    } else { channels = x + 1; channel_assignment = 0; }
    switch ((x = (raw[3] & 0x0E) >> 1)) {
    case 0: if (si_bps) bps = si_bps; else unparseable = 1; break;
    case 1: bps = 8; break; case 2: bps = 12; break; case 3: unparseable = 1; break;
    case 4: bps = 16; break; case 5: bps = 20; break; case 6: bps = 24; break; case 7: bps = 32; break;
    }
    if (raw[3] & 0x01) unparseable = 1;
    int variable = (raw[1] & 0x01) || (si_minbs != si_maxbs && si_sr);
    uint64_t sample_number = 0;
    if (variable) {
        FLAC__uint64 xx; FLAC__uint32 rl = rawlen;
        if (!FLAC__bitreader_read_utf8_uint64(br, &xx, raw, &rl)) return FR_READ_ERROR;
        rawlen = rl;
        if (xx == 0xFFFFFFFFFFFFFFFFull) return FR_BAD_HEADER;
        sample_number = xx;
    } else {
        FLAC__uint32 fr; FLAC__uint32 rl = rawlen;
        if (!FLAC__bitreader_read_utf8_uint32(br, &fr, raw, &rl)) return FR_READ_ERROR;
        rawlen = rl;
        if (fr == 0xFFFFFFFFu) return FR_BAD_HEADER;
        sample_number = fr; /* frame number; converted below */
    }
    if (blocksize_hint) {
        if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) return FR_READ_ERROR;
        raw[rawlen++] = (uint8_t)x;
        if (blocksize_hint == 7) {
            FLAC__uint32 lo; if (!FLAC__bitreader_read_raw_uint32(br, &lo, 8)) return FR_READ_ERROR;
            raw[rawlen++] = (uint8_t)lo; x = (x << 8) | lo;
        }
        blocksize = x + 1;
        if (blocksize > 65535) return FR_BAD_HEADER;
    }
    if (sample_rate_hint) {
        if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) return FR_READ_ERROR;
        raw[rawlen++] = (uint8_t)x;
        if (sample_rate_hint != 12) {
            FLAC__uint32 lo; if (!FLAC__bitreader_read_raw_uint32(br, &lo, 8)) return FR_READ_ERROR;
            raw[rawlen++] = (uint8_t)lo; x = (x << 8) | lo;
        }
        if (sample_rate_hint == 12) sample_rate = x * 1000;
        else if (sample_rate_hint == 13) sample_rate = x;
        else sample_rate = x * 10;
    }
    FLAC__byte crc8;
    if (!FLAC__bitreader_read_raw_uint32(br, &x, 8)) return FR_READ_ERROR;
    crc8 = (FLAC__byte)x;
    if (FLAC__crc8(raw, rawlen) != crc8) return FR_BAD_HEADER;
    if (unparseable) return FR_LOST_SYNC;

    *out_blocksize = blocksize;
    *out_channels = channels;
    *out_bps = bps;
    *out_channel_assignment = channel_assignment;
    *out_sample_number = sample_number;

    /* allocate per-channel buffers + side buffer + residual scratch */
    int32_t *output[8];
    for (uint32_t c = 0; c < channels; c++) output[c] = (int32_t *)calloc(blocksize, sizeof(int32_t));
    int64_t *side = (int64_t *)calloc(blocksize, sizeof(int64_t));
    int32_t *residual_scratch = (int32_t *)calloc(blocksize, sizeof(int32_t));
    int side_in_use = 0;
    int status = FR_OK;

    for (uint32_t channel = 0; channel < channels; channel++) {
        uint32_t cbps = bps;
        switch (channel_assignment) {
        case 0: break;
        case 1: if (channel == 1) cbps++; break; /* LEFT_SIDE */
        case 2: if (channel == 0) cbps++; break; /* RIGHT_SIDE */
        case 3: if (channel == 1) cbps++; break; /* MID_SIDE */
        }
        status = oracle_read_subframe(br, channel, cbps, blocksize, output, side, &side_in_use, residual_scratch);
        if (status != FR_OK) goto done;
    }

    /* read_zero_padding_ */
    if (!FLAC__bitreader_is_consumed_byte_aligned(br)) {
        if (!FLAC__bitreader_read_raw_uint32(br, &x, FLAC__bitreader_bits_left_for_byte_alignment(br))) { status = FR_READ_ERROR; goto done; }
        if (x != 0) { status = FR_LOST_SYNC; goto done; }
    }

    /* footer CRC-16 */
    FLAC__uint16 expected = FLAC__bitreader_get_read_crc16(br);
    FLAC__uint32 footer;
    if (!FLAC__bitreader_read_raw_uint32(br, &footer, FLAC__FRAME_FOOTER_CRC_LEN)) { status = FR_READ_ERROR; goto done; }
    if ((FLAC__uint16)footer != expected) { status = FR_LOST_SYNC; goto done; }

    /* undo_channel_coding */
    switch (channel_assignment) {
    case 0: break;
    case 1: /* LEFT_SIDE */
        for (uint32_t i = 0; i < blocksize; i++)
            if (side_in_use) output[1][i] = output[0][i] - side[i];
            else output[1][i] = output[0][i] - output[1][i];
        break;
    case 2: /* RIGHT_SIDE */
        for (uint32_t i = 0; i < blocksize; i++)
            if (side_in_use) output[0][i] = output[1][i] + side[i];
            else output[0][i] += output[1][i];
        break;
    case 3: /* MID_SIDE */
        for (uint32_t i = 0; i < blocksize; i++) {
            if (!side_in_use) {
                FLAC__int32 mid = output[0][i], sd = output[1][i];
                mid = ((uint32_t)mid) << 1;
                mid |= (sd & 1);
                output[0][i] = (mid + sd) >> 1;
                output[1][i] = (mid - sd) >> 1;
            } else {
                FLAC__int64 mid = ((uint64_t)output[0][i]) << 1;
                mid |= (side[i] & 1);
                output[0][i] = (mid + side[i]) >> 1;
                output[1][i] = (mid - side[i]) >> 1;
            }
        }
        break;
    }

    /* Check whether decoded data actually fits bps (stream_decoder.c:2457-2473).
     * shift_bits = 32 - bps; when bps==32 shift_bits==0 and the range is the
     * full int32 (INT32_MIN/INT32_MAX) — no rejection. Mirror libFLAC exactly:
     * on any out-of-range sample, reject the whole frame. */
    for (uint32_t channel = 0; channel < channels; channel++) {
        int shift_bits = 32 - (int)bps;
        int32_t lower_limit = INT32_MIN >> shift_bits;
        int32_t upper_limit = INT32_MAX >> shift_bits;
        for (uint32_t i = 0; i < blocksize; i++) {
            if (output[channel][i] < lower_limit || output[channel][i] > upper_limit) {
                status = FR_OUT_OF_BOUNDS;
                goto done;
            }
        }
    }

    /* interleave */
    for (uint32_t i = 0; i < blocksize; i++)
        for (uint32_t c = 0; c < channels; c++)
            interleaved[i * channels + c] = output[c][i];

done:
    for (uint32_t c = 0; c < channels; c++) free(output[c]);
    free(side);
    free(residual_scratch);
    return status;
}
