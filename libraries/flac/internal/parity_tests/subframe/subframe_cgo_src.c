/* Parity harness for read_residual_partitioned_rice_ +
 * read_subframe_constant_/verbatim_. These libFLAC functions are
 * static in stream_decoder.c, so the parity strategy is:
 *
 *   1. Use libFLAC's bitwriter to encode a known residual / value /
 *      verbatim block to a byte buffer.
 *   2. Parse the buffer with both libFLAC's bitreader (in this TU,
 *      via fparity_*_oracle) and the Go port (in the test).
 *   3. Compare decoded outputs sample-for-sample.
 *
 * The C-side parity readers re-implement the static stream_decoder
 * logic line-for-line on top of libFLAC's PUBLIC bitreader API; any
 * divergence in parse choice between Go port and the C harness
 * would indicate a port bug. The shared bitreader + CRC tables
 * guarantee the LOW-LEVEL bit access stays bit-exact.
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

#define SF_OK           0
#define SF_READ_ERROR   1
#define SF_BAD_FRAME    2
#define SF_UNPARSEABLE  3
#define SF_ALLOC_FAIL   4

FLAC__bool fparity_sf_read_cb(FLAC__byte buf[], size_t *bytes, void *cd) {
    return goSubframeRead(buf, bytes, cd) ? 1 : 0;
}

/* ── BitWriter helpers used to fabricate test inputs ───────────────── */

/* Encode a residual block with the same encoding read_residual_partitioned_rice_
 * expects: per-partition rice parameter (4 or 5 bits), then a Rice-coded
 * unary+binary residual value per sample. We always emit the non-escape
 * path (rice_parameter < pesc) — sufficient for parity coverage of the
 * common case. */
size_t fparity_encode_residual(uint8_t *out, size_t out_cap,
                               uint32_t predictor_order,
                               uint32_t partition_order,
                               uint32_t blocksize,
                               int is_extended,
                               const int32_t *residual,
                               const uint32_t *rice_params) {
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);

    uint32_t partitions = 1u << partition_order;
    uint32_t partition_samples = blocksize >> partition_order;
    uint32_t plen = is_extended ? FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2_PARAMETER_LEN
                                : FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_PARAMETER_LEN;

    uint32_t sample = 0;
    for (uint32_t p = 0; p < partitions; p++) {
        uint32_t rp = rice_params[p];
        FLAC__bitwriter_write_raw_uint32(bw, rp, plen);
        uint32_t u = (p == 0) ? partition_samples - predictor_order : partition_samples;
        FLAC__bitwriter_write_rice_signed_block(bw, residual + sample, u, rp);
        sample += u;
    }

    /* Pad to a byte boundary so the bitreader can consume the buffer
     * cleanly. */
    FLAC__bitwriter_zero_pad_to_byte_boundary(bw);

    const FLAC__byte *buffer;
    size_t bytes = 0;
    FLAC__bitwriter_get_buffer(bw, &buffer, &bytes);
    if (bytes > out_cap) bytes = out_cap;
    memcpy(out, buffer, bytes);
    FLAC__bitwriter_release_buffer(bw);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return bytes;
}

/* Encode a CONSTANT subframe body: a single bps-bit signed value. */
size_t fparity_encode_constant(uint8_t *out, size_t out_cap, int64_t value, uint32_t bps) {
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    FLAC__bitwriter_write_raw_int64(bw, value, bps);
    FLAC__bitwriter_zero_pad_to_byte_boundary(bw);
    const FLAC__byte *buffer;
    size_t bytes = 0;
    FLAC__bitwriter_get_buffer(bw, &buffer, &bytes);
    if (bytes > out_cap) bytes = out_cap;
    memcpy(out, buffer, bytes);
    FLAC__bitwriter_release_buffer(bw);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return bytes;
}

/* Encode a VERBATIM subframe body: blocksize back-to-back bps-bit
 * signed samples. */
size_t fparity_encode_verbatim(uint8_t *out, size_t out_cap, const int32_t *samples,
                               uint32_t blocksize, uint32_t bps) {
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    for (uint32_t i = 0; i < blocksize; i++) {
        FLAC__bitwriter_write_raw_int32(bw, samples[i], bps);
    }
    FLAC__bitwriter_zero_pad_to_byte_boundary(bw);
    const FLAC__byte *buffer;
    size_t bytes = 0;
    FLAC__bitwriter_get_buffer(bw, &buffer, &bytes);
    if (bytes > out_cap) bytes = out_cap;
    memcpy(out, buffer, bytes);
    FLAC__bitwriter_release_buffer(bw);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return bytes;
}

/* ── Oracle parsers ─────────────────────────────────────────────────── */

int fparity_decode_constant(FLAC__BitReader *br, uint32_t bps, int64_t *out_value) {
    if (!FLAC__bitreader_read_raw_int64(br, out_value, bps)) return SF_READ_ERROR;
    return SF_OK;
}

int fparity_decode_verbatim_int32(FLAC__BitReader *br, uint32_t blocksize, uint32_t bps, int32_t *out) {
    for (uint32_t i = 0; i < blocksize; i++) {
        FLAC__int32 x;
        if (!FLAC__bitreader_read_raw_int32(br, &x, bps)) return SF_READ_ERROR;
        out[i] = x;
    }
    return SF_OK;
}

int fparity_decode_verbatim_int64(FLAC__BitReader *br, uint32_t blocksize, uint32_t bps, int64_t *out) {
    for (uint32_t i = 0; i < blocksize; i++) {
        FLAC__int64 x;
        if (!FLAC__bitreader_read_raw_int64(br, &x, bps)) return SF_READ_ERROR;
        out[i] = x;
    }
    return SF_OK;
}

/* ── Subframe-FIXED + LPC encoders (test-input fabrication) ─────────── */

/* Encode a complete FIXED subframe BODY: order×bps signed warmup
 * samples, the 2-bit method type, the 4-bit partition order, then a
 * partitioned-rice residual. Returns the byte length. */
size_t fparity_encode_subframe_fixed(uint8_t *out, size_t out_cap,
                                     uint32_t blocksize, uint32_t bps, uint32_t order,
                                     const int64_t *warmup,
                                     uint32_t partition_order,
                                     int is_extended,
                                     const int32_t *residual,
                                     const uint32_t *rice_params) {
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    for (uint32_t u = 0; u < order; u++)
        FLAC__bitwriter_write_raw_int64(bw, warmup[u], bps);
    FLAC__bitwriter_write_raw_uint32(bw, is_extended ? 1u : 0u, FLAC__ENTROPY_CODING_METHOD_TYPE_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, partition_order, FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ORDER_LEN);

    uint32_t partitions = 1u << partition_order;
    uint32_t partition_samples = blocksize >> partition_order;
    uint32_t plen = is_extended ? FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2_PARAMETER_LEN
                                : FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_PARAMETER_LEN;
    uint32_t sample = 0;
    for (uint32_t p = 0; p < partitions; p++) {
        FLAC__bitwriter_write_raw_uint32(bw, rice_params[p], plen);
        uint32_t u = (p == 0) ? partition_samples - order : partition_samples;
        FLAC__bitwriter_write_rice_signed_block(bw, residual + sample, u, rice_params[p]);
        sample += u;
    }
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

/* Encode an LPC subframe body. */
size_t fparity_encode_subframe_lpc(uint8_t *out, size_t out_cap,
                                   uint32_t blocksize, uint32_t bps, uint32_t order,
                                   const int64_t *warmup,
                                   uint32_t qlp_coeff_precision, /* the field value, NOT precision-1 */
                                   int qlp_shift,
                                   const int32_t *qlp_coeff,
                                   uint32_t partition_order,
                                   int is_extended,
                                   const int32_t *residual,
                                   const uint32_t *rice_params) {
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    for (uint32_t u = 0; u < order; u++)
        FLAC__bitwriter_write_raw_int64(bw, warmup[u], bps);
    FLAC__bitwriter_write_raw_uint32(bw, qlp_coeff_precision - 1, FLAC__SUBFRAME_LPC_QLP_COEFF_PRECISION_LEN);
    FLAC__bitwriter_write_raw_int32(bw, qlp_shift, FLAC__SUBFRAME_LPC_QLP_SHIFT_LEN);
    for (uint32_t u = 0; u < order; u++)
        FLAC__bitwriter_write_raw_int32(bw, qlp_coeff[u], qlp_coeff_precision);
    FLAC__bitwriter_write_raw_uint32(bw, is_extended ? 1u : 0u, FLAC__ENTROPY_CODING_METHOD_TYPE_LEN);
    FLAC__bitwriter_write_raw_uint32(bw, partition_order, FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ORDER_LEN);
    uint32_t partitions = 1u << partition_order;
    uint32_t partition_samples = blocksize >> partition_order;
    uint32_t plen = is_extended ? FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2_PARAMETER_LEN
                                : FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_PARAMETER_LEN;
    uint32_t sample = 0;
    for (uint32_t p = 0; p < partitions; p++) {
        FLAC__bitwriter_write_raw_uint32(bw, rice_params[p], plen);
        uint32_t u = (p == 0) ? partition_samples - order : partition_samples;
        FLAC__bitwriter_write_rice_signed_block(bw, residual + sample, u, rice_params[p]);
        sample += u;
    }
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

int fparity_decode_residual(FLAC__BitReader *br,
                            uint32_t predictor_order,
                            uint32_t partition_order,
                            uint32_t blocksize,
                            int is_extended,
                            int32_t *residual,
                            uint32_t *parameters,
                            uint32_t *raw_bits) {
    uint32_t partitions = 1u << partition_order;
    uint32_t partition_samples = blocksize >> partition_order;
    uint32_t plen = is_extended ? FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2_PARAMETER_LEN
                                : FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_PARAMETER_LEN;
    uint32_t pesc = is_extended ? FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2_ESCAPE_PARAMETER
                                : FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_ESCAPE_PARAMETER;

    uint32_t sample = 0;
    for (uint32_t p = 0; p < partitions; p++) {
        FLAC__uint32 rp;
        if (!FLAC__bitreader_read_raw_uint32(br, &rp, plen)) return SF_READ_ERROR;
        parameters[p] = rp;
        if (rp < pesc) {
            raw_bits[p] = 0;
            uint32_t u = (p == 0) ? partition_samples - predictor_order : partition_samples;
            if (!FLAC__bitreader_read_rice_signed_block(br, residual + sample, u, rp))
                return SF_BAD_FRAME;
            sample += u;
        } else {
            FLAC__uint32 rb;
            if (!FLAC__bitreader_read_raw_uint32(br, &rb, FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE_RAW_LEN))
                return SF_READ_ERROR;
            raw_bits[p] = rb;
            uint32_t start = (p == 0) ? predictor_order : 0;
            if (rb == 0) {
                for (uint32_t u = start; u < partition_samples; u++, sample++)
                    residual[sample] = 0;
            } else {
                for (uint32_t u = start; u < partition_samples; u++, sample++) {
                    FLAC__int32 x;
                    if (!FLAC__bitreader_read_raw_int32(br, &x, rb)) return SF_READ_ERROR;
                    residual[sample] = x;
                }
            }
        }
    }
    return SF_OK;
}
