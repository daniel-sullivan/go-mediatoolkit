/* Parity harness for stream_encoder_framing.c. Strategy:
 *
 *   1. The Go test fabricates a metadata block / frame header /
 *      subframe and frames it via the Go port into the Go BitWriter.
 *   2. This TU frames the *same* logical object via libFLAC's real
 *      FLAC__add_metadata_block / FLAC__frame_add_header /
 *      FLAC__subframe_add_* into a libFLAC BitWriter.
 *   3. The test asserts the two byte buffers are identical.
 *
 * libFLAC's framing functions are non-static (declared in
 * private/stream_encoder_framing.h), so we compile the real .c and
 * call them directly — no re-implementation. Each fparity_frame_*
 * helper builds the C struct from the flat arguments Go passes, frames
 * it, byte-aligns, and copies the bitwriter buffer out.
 */

#ifdef HAVE_CONFIG_H
#  include <config.h>
#endif

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/crc.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/bitwriter.c"
#include "src/libFLAC/stream_encoder_framing.c"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

/* Flush a framed bitwriter to out; returns byte length (0 on failure). */
static size_t flush_bw(FLAC__BitWriter *bw, uint8_t *out, size_t out_cap) {
    if (!FLAC__bitwriter_zero_pad_to_byte_boundary(bw))
        return 0;
    const FLAC__byte *buffer;
    size_t bytes = 0;
    if (!FLAC__bitwriter_get_buffer(bw, &buffer, &bytes))
        return 0;
    if (bytes > out_cap) bytes = out_cap;
    memcpy(out, buffer, bytes);
    FLAC__bitwriter_release_buffer(bw);
    return bytes;
}

/* ── Frame header ───────────────────────────────────────────────────── */

size_t fparity_frame_header(uint8_t *out, size_t out_cap,
                            uint32_t blocksize, uint32_t sample_rate,
                            uint32_t channels, uint32_t channel_assignment,
                            uint32_t bits_per_sample, int is_variable,
                            uint64_t number) {
    FLAC__FrameHeader h;
    memset(&h, 0, sizeof(h));
    h.blocksize = blocksize;
    h.sample_rate = sample_rate;
    h.channels = channels;
    h.channel_assignment = (FLAC__ChannelAssignment)channel_assignment;
    h.bits_per_sample = bits_per_sample;
    if (is_variable) {
        h.number_type = FLAC__FRAME_NUMBER_TYPE_SAMPLE_NUMBER;
        h.number.sample_number = number;
    } else {
        h.number_type = FLAC__FRAME_NUMBER_TYPE_FRAME_NUMBER;
        h.number.frame_number = (FLAC__uint32)number;
    }

    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__frame_add_header(&h, bw))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return n;
}

/* ── Subframes ──────────────────────────────────────────────────────── */

size_t fparity_subframe_constant(uint8_t *out, size_t out_cap,
                                 int64_t value, uint32_t subframe_bps,
                                 uint32_t wasted_bits) {
    FLAC__Subframe_Constant sf;
    sf.value = value;
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__subframe_add_constant(&sf, subframe_bps, wasted_bits, bw))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return n;
}

size_t fparity_subframe_verbatim32(uint8_t *out, size_t out_cap,
                                   const int32_t *signal, uint32_t samples,
                                   uint32_t subframe_bps, uint32_t wasted_bits) {
    FLAC__Subframe_Verbatim sf;
    sf.data_type = FLAC__VERBATIM_SUBFRAME_DATA_TYPE_INT32;
    sf.data.int32 = signal;
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__subframe_add_verbatim(&sf, samples, subframe_bps, wasted_bits, bw))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return n;
}

size_t fparity_subframe_verbatim64(uint8_t *out, size_t out_cap,
                                   const int64_t *signal, uint32_t samples,
                                   uint32_t subframe_bps, uint32_t wasted_bits) {
    FLAC__Subframe_Verbatim sf;
    sf.data_type = FLAC__VERBATIM_SUBFRAME_DATA_TYPE_INT64;
    sf.data.int64 = signal;
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__subframe_add_verbatim(&sf, samples, subframe_bps, wasted_bits, bw))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return n;
}

/* Build a PartitionedRiceContents on the heap from flat arrays. The
 * caller supplies one parameter + raw_bits per partition (1<<order). */
static void fill_prc(FLAC__EntropyCodingMethod_PartitionedRiceContents *c,
                     uint32_t partition_order,
                     const uint32_t *params, const uint32_t *raw_bits) {
    uint32_t parts = 1u << partition_order;
    c->parameters = malloc(sizeof(uint32_t) * parts);
    c->raw_bits = malloc(sizeof(uint32_t) * parts);
    c->capacity_by_order = partition_order;
    for (uint32_t i = 0; i < parts; i++) {
        c->parameters[i] = params[i];
        c->raw_bits[i] = raw_bits[i];
    }
}

static void free_prc(FLAC__EntropyCodingMethod_PartitionedRiceContents *c) {
    free(c->parameters);
    free(c->raw_bits);
}

size_t fparity_subframe_fixed(uint8_t *out, size_t out_cap,
                              uint32_t order, uint32_t residual_samples,
                              uint32_t subframe_bps, uint32_t wasted_bits,
                              const int64_t *warmup,
                              uint32_t partition_order, int is_extended,
                              const int32_t *residual,
                              const uint32_t *params, const uint32_t *raw_bits) {
    FLAC__Subframe_Fixed sf;
    memset(&sf, 0, sizeof(sf));
    sf.order = order;
    for (uint32_t i = 0; i < order; i++) sf.warmup[i] = warmup[i];
    sf.residual = residual;
    sf.entropy_coding_method.type = is_extended
        ? FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2
        : FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE;
    sf.entropy_coding_method.data.partitioned_rice.order = partition_order;
    FLAC__EntropyCodingMethod_PartitionedRiceContents prc;
    fill_prc(&prc, partition_order, params, raw_bits);
    sf.entropy_coding_method.data.partitioned_rice.contents = &prc;

    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__subframe_add_fixed(&sf, residual_samples, subframe_bps, wasted_bits, bw))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    free_prc(&prc);
    return n;
}

size_t fparity_subframe_lpc(uint8_t *out, size_t out_cap,
                            uint32_t order, uint32_t residual_samples,
                            uint32_t subframe_bps, uint32_t wasted_bits,
                            const int64_t *warmup,
                            uint32_t qlp_coeff_precision, int quantization_level,
                            const int32_t *qlp_coeff,
                            uint32_t partition_order, int is_extended,
                            const int32_t *residual,
                            const uint32_t *params, const uint32_t *raw_bits) {
    FLAC__Subframe_LPC sf;
    memset(&sf, 0, sizeof(sf));
    sf.order = order;
    sf.qlp_coeff_precision = qlp_coeff_precision;
    sf.quantization_level = quantization_level;
    for (uint32_t i = 0; i < order; i++) {
        sf.warmup[i] = warmup[i];
        sf.qlp_coeff[i] = qlp_coeff[i];
    }
    sf.residual = residual;
    sf.entropy_coding_method.type = is_extended
        ? FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE2
        : FLAC__ENTROPY_CODING_METHOD_PARTITIONED_RICE;
    sf.entropy_coding_method.data.partitioned_rice.order = partition_order;
    FLAC__EntropyCodingMethod_PartitionedRiceContents prc;
    fill_prc(&prc, partition_order, params, raw_bits);
    sf.entropy_coding_method.data.partitioned_rice.contents = &prc;

    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__subframe_add_lpc(&sf, residual_samples, subframe_bps, wasted_bits, bw))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    free_prc(&prc);
    return n;
}

/* ── Metadata blocks ────────────────────────────────────────────────── */

size_t fparity_metadata_streaminfo(uint8_t *out, size_t out_cap,
                                   int is_last, uint32_t length,
                                   uint32_t min_bs, uint32_t max_bs,
                                   uint32_t min_fs, uint32_t max_fs,
                                   uint32_t sample_rate, uint32_t channels,
                                   uint32_t bps, uint64_t total_samples,
                                   const uint8_t *md5) {
    FLAC__StreamMetadata m;
    memset(&m, 0, sizeof(m));
    m.type = FLAC__METADATA_TYPE_STREAMINFO;
    m.is_last = is_last;
    m.length = length;
    m.data.stream_info.min_blocksize = min_bs;
    m.data.stream_info.max_blocksize = max_bs;
    m.data.stream_info.min_framesize = min_fs;
    m.data.stream_info.max_framesize = max_fs;
    m.data.stream_info.sample_rate = sample_rate;
    m.data.stream_info.channels = channels;
    m.data.stream_info.bits_per_sample = bps;
    m.data.stream_info.total_samples = total_samples;
    memcpy(m.data.stream_info.md5sum, md5, 16);

    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__add_metadata_block(&m, bw, false))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return n;
}

size_t fparity_metadata_padding(uint8_t *out, size_t out_cap,
                                int is_last, uint32_t length) {
    FLAC__StreamMetadata m;
    memset(&m, 0, sizeof(m));
    m.type = FLAC__METADATA_TYPE_PADDING;
    m.is_last = is_last;
    m.length = length;
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__add_metadata_block(&m, bw, false))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return n;
}

size_t fparity_metadata_application(uint8_t *out, size_t out_cap,
                                    int is_last, uint32_t length,
                                    const uint8_t *id, const uint8_t *data) {
    FLAC__StreamMetadata m;
    memset(&m, 0, sizeof(m));
    m.type = FLAC__METADATA_TYPE_APPLICATION;
    m.is_last = is_last;
    m.length = length;
    memcpy(m.data.application.id, id, 4);
    m.data.application.data = (FLAC__byte *)data; /* length-4 bytes */
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__add_metadata_block(&m, bw, false))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return n;
}

size_t fparity_metadata_seektable(uint8_t *out, size_t out_cap,
                                  int is_last, uint32_t length,
                                  uint32_t num_points,
                                  const uint64_t *sample_numbers,
                                  const uint64_t *stream_offsets,
                                  const uint32_t *frame_samples) {
    FLAC__StreamMetadata m;
    memset(&m, 0, sizeof(m));
    m.type = FLAC__METADATA_TYPE_SEEKTABLE;
    m.is_last = is_last;
    m.length = length;
    m.data.seek_table.num_points = num_points;
    FLAC__StreamMetadata_SeekPoint *pts =
        malloc(sizeof(FLAC__StreamMetadata_SeekPoint) * (num_points ? num_points : 1));
    for (uint32_t i = 0; i < num_points; i++) {
        pts[i].sample_number = sample_numbers[i];
        pts[i].stream_offset = stream_offsets[i];
        pts[i].frame_samples = frame_samples[i];
    }
    m.data.seek_table.points = pts;
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__add_metadata_block(&m, bw, false))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    free(pts);
    return n;
}

/* VORBIS_COMMENT. comments are packed as length-prefixed entries in a
 * single flat buffer: for each comment, a 4-byte LE length is NOT used;
 * instead Go passes parallel arrays. To keep the ABI simple we accept
 * the vendor entry + a count and two parallel arrays describing each
 * comment (offset into a flat data buffer, plus its length). */
size_t fparity_metadata_vorbiscomment(uint8_t *out, size_t out_cap,
                                      int is_last, uint32_t length,
                                      int update_vendor,
                                      const uint8_t *vendor, uint32_t vendor_len,
                                      uint32_t num_comments,
                                      const uint8_t *flat, const uint32_t *offsets,
                                      const uint32_t *lengths) {
    FLAC__StreamMetadata m;
    memset(&m, 0, sizeof(m));
    m.type = FLAC__METADATA_TYPE_VORBIS_COMMENT;
    m.is_last = is_last;
    m.length = length;
    m.data.vorbis_comment.vendor_string.length = vendor_len;
    m.data.vorbis_comment.vendor_string.entry = (FLAC__byte *)vendor;
    m.data.vorbis_comment.num_comments = num_comments;
    FLAC__StreamMetadata_VorbisComment_Entry *entries =
        malloc(sizeof(FLAC__StreamMetadata_VorbisComment_Entry) * (num_comments ? num_comments : 1));
    for (uint32_t i = 0; i < num_comments; i++) {
        entries[i].length = lengths[i];
        entries[i].entry = (FLAC__byte *)(flat + offsets[i]);
    }
    m.data.vorbis_comment.comments = entries;
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__add_metadata_block(&m, bw, update_vendor ? true : false))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    free(entries);
    return n;
}

size_t fparity_metadata_picture(uint8_t *out, size_t out_cap,
                                int is_last, uint32_t length,
                                uint32_t type,
                                const char *mime, const uint8_t *desc,
                                uint32_t width, uint32_t height, uint32_t depth,
                                uint32_t colors, uint32_t data_length,
                                const uint8_t *data) {
    FLAC__StreamMetadata m;
    memset(&m, 0, sizeof(m));
    m.type = FLAC__METADATA_TYPE_PICTURE;
    m.is_last = is_last;
    m.length = length;
    m.data.picture.type = (FLAC__StreamMetadata_Picture_Type)type;
    m.data.picture.mime_type = (char *)mime;
    m.data.picture.description = (FLAC__byte *)desc;
    m.data.picture.width = width;
    m.data.picture.height = height;
    m.data.picture.depth = depth;
    m.data.picture.colors = colors;
    m.data.picture.data_length = data_length;
    m.data.picture.data = (FLAC__byte *)data;
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__add_metadata_block(&m, bw, false))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    return n;
}

/* CUESHEET. tracks flattened: parallel arrays for the per-track fixed
 * fields plus a flat indices array addressed by index_offset[i]. */
size_t fparity_metadata_cuesheet(uint8_t *out, size_t out_cap,
                                 int is_last, uint32_t length,
                                 const char *mcn, uint64_t lead_in, int is_cd,
                                 uint32_t num_tracks,
                                 const uint64_t *t_offset, const uint8_t *t_number,
                                 const char *t_isrc /* num_tracks*12 */,
                                 const uint32_t *t_type, const uint32_t *t_preemph,
                                 const uint8_t *t_num_indices,
                                 const uint32_t *t_index_base,
                                 const uint64_t *idx_offset, const uint8_t *idx_number) {
    FLAC__StreamMetadata m;
    memset(&m, 0, sizeof(m));
    m.type = FLAC__METADATA_TYPE_CUESHEET;
    m.is_last = is_last;
    m.length = length;
    memcpy(m.data.cue_sheet.media_catalog_number, mcn, 129);
    m.data.cue_sheet.lead_in = lead_in;
    m.data.cue_sheet.is_cd = is_cd ? true : false;
    m.data.cue_sheet.num_tracks = num_tracks;
    FLAC__StreamMetadata_CueSheet_Track *tracks =
        malloc(sizeof(FLAC__StreamMetadata_CueSheet_Track) * (num_tracks ? num_tracks : 1));
    memset(tracks, 0, sizeof(FLAC__StreamMetadata_CueSheet_Track) * (num_tracks ? num_tracks : 1));
    for (uint32_t i = 0; i < num_tracks; i++) {
        tracks[i].offset = t_offset[i];
        tracks[i].number = t_number[i];
        memcpy(tracks[i].isrc, t_isrc + i * 13, 13);
        tracks[i].type = t_type[i] & 1;
        tracks[i].pre_emphasis = t_preemph[i] & 1;
        tracks[i].num_indices = t_num_indices[i];
        uint32_t ni = t_num_indices[i];
        if (ni > 0) {
            FLAC__StreamMetadata_CueSheet_Index *idx =
                malloc(sizeof(FLAC__StreamMetadata_CueSheet_Index) * ni);
            uint32_t base = t_index_base[i];
            for (uint32_t j = 0; j < ni; j++) {
                idx[j].offset = idx_offset[base + j];
                idx[j].number = idx_number[base + j];
            }
            tracks[i].indices = idx;
        } else {
            tracks[i].indices = NULL;
        }
    }
    m.data.cue_sheet.tracks = tracks;
    FLAC__BitWriter *bw = FLAC__bitwriter_new();
    FLAC__bitwriter_init(bw);
    size_t n = 0;
    if (FLAC__add_metadata_block(&m, bw, false))
        n = flush_bw(bw, out, out_cap);
    FLAC__bitwriter_free(bw);
    FLAC__bitwriter_delete(bw);
    for (uint32_t i = 0; i < num_tracks; i++) free(tracks[i].indices);
    free(tracks);
    return n;
}
