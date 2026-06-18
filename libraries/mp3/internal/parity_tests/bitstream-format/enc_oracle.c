// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* enc_oracle.c — compiles a PRIVATE copy of the vendored LAME 3.100 encoder
 * frame-assembler (bitstream.c) + bit-reservoir framing (reservoir.c) and
 * re-exports them through mp3enc_* trampolines for the bitstream-format parity
 * slice's encoder half. See enc_oracle.h for the surface and the LGPL/mp3lame
 * fence rationale.
 *
 * Reference code under test. This TU #includes the committed
 * liblame/libmp3lame bitstream.c + reservoir.c, bringing the genuine vendored
 * format_bitstream / encodeSideInfo2 / writeMainData / drain_into_ancillary /
 * compute_flushbits / flush_bitstream / do_copy_buffer / getframebits /
 * get_max_frame_buffer_size_by_constraint / CRC_* / writeheader and the four
 * Resv* functions into scope (the first set are file-static or module-internal;
 * #include is the only way to reach them). It also #includes tables.c (for the
 * genuine ht[] Huffman codebooks + bitrate_table the writer/getframebits link
 * against) and version.c (for the genuine get_lame_short_version() that
 * drain_into_ancillary emits into ancillary data). The mp3enc_* trampolines
 * forward straight through, so the C side of every assertion is the real
 * reference, never a hand twin.
 *
 * Single-TU self-containment. Per the parity discipline (SKILL.md "parity
 * oracle per slice") each go-test binary is symbol-self-contained; this private
 * LAME copy never collides with libraries/mp3's production copy (this package
 * never imports libraries/mp3) nor with the sibling huffman-encode oracle (a
 * separate package = separate binary). Within THIS binary it coexists with the
 * decoder-half minimp3 oracle (oracle.c): minimp3's symbols are all file-static
 * there, LAME's are the mp3enc_* trampolines + the vendored externs, so the two
 * sets are disjoint.
 *
 * Link-only stubs. A handful of symbols are referenced by bitstream.c functions
 * on code paths this oracle never executes:
 *   - slen1_tab / slen2_tab (takehiro.c) — read by writeMainData's MPEG-1
 *     scalefactor path; provided here with the VERBATIM takehiro.c:961-962
 *     values so even an exercised read is faithful.
 *   - UpdateMusicCRC (VbrTag.c) — only reached by copy_buffer(mp3data=1); the
 *     parity test drives copy_buffer with mp3data=0 (matching the Go port,
 *     which defers the music-CRC to the VbrTag slice), so this stub never runs.
 *   - lame_errorf (util.c) — the ERRORF diagnostics in compute_flushbits /
 *     format_bitstream / encodeSideInfo2 fire only on internal-inconsistency
 *     paths the fabricated inputs never hit; inert stub.
 * do_gain_analysis's hip_decode1_unclipped / AnalyzeSamples are under
 * #ifdef DECODE_ON_THE_FLY (undefined in config.h) so they are not referenced.
 *
 * FP flags. Only -I / -D / -Wno-* live in the in-source #cgo (enc_cgo.go). The
 * FP-determinism flags (-ffp-contract=off, …) come from the mise task env so
 * reservoir.c's two double scalings (ResvMax*0.9, targBits-.1*mean_bits) round
 * separately, matching the FMA-free mp3_strict Go build.
 */

#include <config.h>
#include <stdlib.h>
#include <string.h>

#include "libmp3lame/bitstream.c"
#include "libmp3lame/reservoir.c"
#include "libmp3lame/tables.c"
#include "libmp3lame/version.c"

#include "enc_oracle.h"

/* ---- link-only definitions for unreached bitstream.c code ---- */

const int slen1_tab[16] = { 0, 0, 0, 0, 3, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4 };
const int slen2_tab[16] = { 0, 1, 2, 3, 0, 1, 2, 3, 1, 2, 3, 1, 2, 3, 2, 3 };

void UpdateMusicCRC(uint16_t *crc, const unsigned char *buffer, int size) {
    (void) crc; (void) buffer; (void) size;
}
void lame_errorf(const lame_internal_flags *gfc, const char *format, ...) {
    (void) gfc; (void) format;
}

/* ---- oracle handle ----
 *
 * lame_internal_flags is large; we calloc one zeroed instance and prime only
 * the fields the frame assembler reads/writes. bs->buf borrows a caller-sized
 * malloc'd buffer. The handle also owns the LSF sfb_partition_table backing
 * (gr_info.sfb_partition_table is a `const int *`, so the pointed-at array must
 * outlive the call). */

struct mp3enc_t {
    lame_internal_flags gfc;
    int part_backing[2][2][4]; /* sfb_partition_table per (gr,ch) */
};

mp3enc_t *mp3enc_new(int buf_size) {
    mp3enc_t *e = (mp3enc_t *) calloc(1, sizeof(*e));
    e->gfc.bs.buf = (unsigned char *) calloc((size_t) buf_size, 1);
    e->gfc.bs.buf_size = buf_size;
    e->gfc.bs.totbit = 0;
    e->gfc.bs.buf_byte_idx = 0;
    e->gfc.bs.buf_bit_idx = 0;
    return e;
}

void mp3enc_free(mp3enc_t *e) {
    if (e == NULL) return;
    free(e->gfc.bs.buf);
    free(e);
}

/* ---- config ---- */

void mp3enc_set_cfg(mp3enc_t *e, int version, int samplerate_out,
                    int samplerate_index, int sideinfo_len, int channels_out,
                    int mode_gr, int mode, int error_protection,
                    int extension, int copyright, int original, int emphasis,
                    int disable_reservoir, int avg_bitrate,
                    int buffer_constraint) {
    SessionConfig_t *cfg = &e->gfc.cfg;
    cfg->version = version;
    cfg->samplerate_out = samplerate_out;
    cfg->samplerate_index = samplerate_index;
    cfg->sideinfo_len = sideinfo_len;
    cfg->channels_out = channels_out;
    cfg->mode_gr = mode_gr;
    cfg->mode = (MPEG_mode) mode;
    cfg->error_protection = error_protection;
    cfg->extension = extension;
    cfg->copyright = copyright;
    cfg->original = original;
    cfg->emphasis = emphasis;
    cfg->disable_reservoir = disable_reservoir;
    cfg->avg_bitrate = avg_bitrate;
    cfg->buffer_constraint = buffer_constraint;
}

void mp3enc_set_ov(mp3enc_t *e, int bitrate_index, int padding, int mode_ext) {
    EncResult_t *ov = &e->gfc.ov_enc;
    ov->bitrate_index = bitrate_index;
    ov->padding = padding;
    ov->mode_ext = mode_ext;
}

void mp3enc_set_sv(mp3enc_t *e, int h_ptr, int w_ptr, int ancillary_flag,
                   int resv_size, int resv_max) {
    EncStateVar_t *sv = &e->gfc.sv_enc;
    sv->h_ptr = h_ptr;
    sv->w_ptr = w_ptr;
    sv->ancillary_flag = ancillary_flag;
    sv->ResvSize = resv_size;
    sv->ResvMax = resv_max;
}

void mp3enc_set_substep_shaping(mp3enc_t *e, int v) {
    e->gfc.sv_qnt.substep_shaping = v;
}

void mp3enc_prime_header(mp3enc_t *e, int slot, int write_timing, int ptr,
                         const unsigned char *buf, int len) {
    EncStateVar_t *sv = &e->gfc.sv_enc;
    sv->header[slot].write_timing = write_timing;
    sv->header[slot].ptr = ptr;
    if (len > MAX_HEADER_LEN) len = MAX_HEADER_LEN;
    if (len > 0) memcpy(sv->header[slot].buf, buf, (size_t) len);
}

void mp3enc_disarm_headers(mp3enc_t *e, int sentinel) {
    int i;
    for (i = 0; i < MAX_HEADER_BUF; i++)
        e->gfc.sv_enc.header[i].write_timing = sentinel;
}

/* ---- bit stream ---- */

void mp3enc_set_bs(mp3enc_t *e, int totbit, int buf_byte_idx, int buf_bit_idx) {
    e->gfc.bs.totbit = totbit;
    e->gfc.bs.buf_byte_idx = buf_byte_idx;
    e->gfc.bs.buf_bit_idx = buf_bit_idx;
}

int  mp3enc_bs_totbit(const mp3enc_t *e)       { return e->gfc.bs.totbit; }
int  mp3enc_bs_buf_byte_idx(const mp3enc_t *e) { return e->gfc.bs.buf_byte_idx; }
int  mp3enc_bs_buf_bit_idx(const mp3enc_t *e)  { return e->gfc.bs.buf_bit_idx; }

void mp3enc_copy_bs_buf(const mp3enc_t *e, unsigned char *out, int n) {
    if (n > 0) memcpy(out, e->gfc.bs.buf, (size_t) n);
}

/* ---- side info ---- */

void mp3enc_set_side(mp3enc_t *e, int main_data_begin, int private_bits,
                     int resv_drain_pre, int resv_drain_post) {
    III_side_info_t *s = &e->gfc.l3_side;
    s->main_data_begin = main_data_begin;
    s->private_bits = private_bits;
    s->resvDrain_pre = resv_drain_pre;
    s->resvDrain_post = resv_drain_post;
}

void mp3enc_set_scfsi(mp3enc_t *e, int ch, int band, int v) {
    e->gfc.l3_side.scfsi[ch][band] = v;
}

int mp3enc_main_data_begin(const mp3enc_t *e) { return e->gfc.l3_side.main_data_begin; }
int mp3enc_resv_drain_pre(const mp3enc_t *e)  { return e->gfc.l3_side.resvDrain_pre; }
int mp3enc_resv_drain_post(const mp3enc_t *e) { return e->gfc.l3_side.resvDrain_post; }

/* ---- scalefac_band ---- */

void mp3enc_set_sfb_l(mp3enc_t *e, int i, int v) { e->gfc.scalefac_band.l[i] = v; }
void mp3enc_set_sfb_s(mp3enc_t *e, int i, int v) { e->gfc.scalefac_band.s[i] = v; }

/* ---- gr_info ---- */

void mp3enc_set_gr(mp3enc_t *e, int gr, int ch,
                   int part2_3_length, int part2_length, int big_values,
                   int count1, int global_gain, int scalefac_compress,
                   int block_type, int mixed_block_flag,
                   int region0_count, int region1_count, int preflag,
                   int scalefac_scale, int count1table_select,
                   int sfbdivide, int sfbmax) {
    gr_info *gi = &e->gfc.l3_side.tt[gr][ch];
    gi->part2_3_length = part2_3_length;
    gi->part2_length = part2_length;
    gi->big_values = big_values;
    gi->count1 = count1;
    gi->global_gain = global_gain;
    gi->scalefac_compress = scalefac_compress;
    gi->block_type = block_type;
    gi->mixed_block_flag = mixed_block_flag;
    gi->region0_count = region0_count;
    gi->region1_count = region1_count;
    gi->preflag = preflag;
    gi->scalefac_scale = scalefac_scale;
    gi->count1table_select = count1table_select;
    gi->sfbdivide = sfbdivide;
    gi->sfbmax = sfbmax;
}

void mp3enc_set_gr_table_select(mp3enc_t *e, int gr, int ch, int idx, int v) {
    e->gfc.l3_side.tt[gr][ch].table_select[idx] = v;
}
void mp3enc_set_gr_subblock_gain(mp3enc_t *e, int gr, int ch, int idx, int v) {
    e->gfc.l3_side.tt[gr][ch].subblock_gain[idx] = v;
}
void mp3enc_set_gr_scalefac(mp3enc_t *e, int gr, int ch, int sfb, int v) {
    e->gfc.l3_side.tt[gr][ch].scalefac[sfb] = v;
}
void mp3enc_set_gr_partition(mp3enc_t *e, int gr, int ch, const int *part4,
                             const int *slen4) {
    gr_info *gi = &e->gfc.l3_side.tt[gr][ch];
    int i;
    for (i = 0; i < 4; i++) {
        e->part_backing[gr][ch][i] = part4[i];
        gi->slen[i] = slen4[i];
    }
    gi->sfb_partition_table = e->part_backing[gr][ch];
}
int mp3enc_gr_table_select(const mp3enc_t *e, int gr, int ch, int idx) {
    return e->gfc.l3_side.tt[gr][ch].table_select[idx];
}

/* ---- trampolines ---- */

int mp3enc_calc_frame_length(mp3enc_t *e, int kbps, int pad) {
    return calcFrameLength(&e->gfc.cfg, kbps, pad);
}
int mp3enc_getframebits(mp3enc_t *e) {
    return getframebits(&e->gfc);
}
int mp3enc_get_max_frame_buffer_size_by_constraint(mp3enc_t *e, int constraint) {
    return get_max_frame_buffer_size_by_constraint(&e->gfc.cfg, constraint);
}
void mp3enc_writeheader(mp3enc_t *e, int val, int j) {
    writeheader(&e->gfc, val, j);
}
int mp3enc_crc_update(int value, int crc) {
    return CRC_update(value, crc);
}
void mp3enc_crc_writeheader(mp3enc_t *e, unsigned char *header) {
    CRC_writeheader(&e->gfc, (char *) header);
}
void mp3enc_drain_into_ancillary(mp3enc_t *e, int remaining_bits) {
    drain_into_ancillary(&e->gfc, remaining_bits);
}
void mp3enc_encode_side_info2(mp3enc_t *e, int bits_per_frame) {
    encodeSideInfo2(&e->gfc, bits_per_frame);
}
int mp3enc_write_main_data(mp3enc_t *e) {
    return writeMainData(&e->gfc);
}
int mp3enc_compute_flushbits(mp3enc_t *e, int *total_bytes_output) {
    return compute_flushbits(&e->gfc, total_bytes_output);
}
void mp3enc_flush_bitstream(mp3enc_t *e) {
    flush_bitstream(&e->gfc);
}
void mp3enc_add_dummy_byte(mp3enc_t *e, unsigned char val, unsigned int n) {
    add_dummy_byte(&e->gfc, val, n);
}
int mp3enc_do_copy_buffer(mp3enc_t *e, unsigned char *buffer, int size) {
    return do_copy_buffer(&e->gfc, buffer, size);
}
int mp3enc_copy_buffer(mp3enc_t *e, unsigned char *buffer, int size, int mp3data) {
    return copy_buffer(&e->gfc, buffer, size, mp3data);
}
int mp3enc_format_bitstream(mp3enc_t *e) {
    return format_bitstream(&e->gfc);
}

int mp3enc_resv_frame_begin(mp3enc_t *e, int *mean_bits) {
    return ResvFrameBegin(&e->gfc, mean_bits);
}
void mp3enc_resv_max_bits(mp3enc_t *e, int mean_bits, int *targ_bits, int *extra_bits, int cbr) {
    ResvMaxBits(&e->gfc, mean_bits, targ_bits, extra_bits, cbr);
}
void mp3enc_resv_adjust(mp3enc_t *e, int gr, int ch) {
    ResvAdjust(&e->gfc, &e->gfc.l3_side.tt[gr][ch]);
}
void mp3enc_resv_frame_end(mp3enc_t *e, int mean_bits) {
    ResvFrameEnd(&e->gfc, mean_bits);
}

/* ---- state read-back ---- */

int mp3enc_h_ptr(const mp3enc_t *e)           { return e->gfc.sv_enc.h_ptr; }
int mp3enc_w_ptr(const mp3enc_t *e)           { return e->gfc.sv_enc.w_ptr; }
int mp3enc_ancillary_flag(const mp3enc_t *e)  { return e->gfc.sv_enc.ancillary_flag; }
int mp3enc_resv_size(const mp3enc_t *e)       { return e->gfc.sv_enc.ResvSize; }
int mp3enc_resv_max(const mp3enc_t *e)        { return e->gfc.sv_enc.ResvMax; }
int mp3enc_substep_shaping(const mp3enc_t *e) { return e->gfc.sv_qnt.substep_shaping; }

int mp3enc_header_write_timing(const mp3enc_t *e, int slot) {
    return e->gfc.sv_enc.header[slot].write_timing;
}
int mp3enc_header_ptr(const mp3enc_t *e, int slot) {
    return e->gfc.sv_enc.header[slot].ptr;
}
void mp3enc_copy_header_buf(const mp3enc_t *e, int slot, unsigned char *out, int n) {
    if (n > 0) memcpy(out, e->gfc.sv_enc.header[slot].buf, (size_t) n);
}
