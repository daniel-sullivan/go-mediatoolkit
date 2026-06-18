// SPDX-License-Identifier: LGPL-2.0-or-later

/*
 * enc_oracle.h — C oracle surface for the LAME-encoder half of the
 * bitstream-format parity slice.
 *
 * The decoder half of this package (oracle.h / oracle.c) re-exports minimp3's
 * file-static bitstream/header/reservoir routines. THIS header re-exports the
 * vendored LAME 3.100 encoder's frame-assembly routines from bitstream.c
 * (format_bitstream, encodeSideInfo2, writeMainData, drain_into_ancillary,
 * compute_flushbits, flush_bitstream, add_dummy_byte, do_copy_buffer,
 * copy_buffer, getframebits, get_max_frame_buffer_size_by_constraint, CRC_*,
 * writeheader) plus reservoir.c's bit-reservoir framing (ResvFrameBegin /
 * ResvMaxBits / ResvAdjust / ResvFrameEnd) — the encoder slice nativemp3's
 * bitstream_format.go / reservoir_encode.go port 1:1.
 *
 * enc_oracle.c #includes the committed liblame/libmp3lame/bitstream.c +
 * reservoir.c + tables.c, so the file-static helpers (putbits2, putheader_bits,
 * encodeSideInfo2, writeMainData, drain_into_ancillary, do_copy_buffer, …) are
 * in scope, and re-exports through the mp3enc_* trampolines below — the C side
 * of every assertion is the genuine vendored reference, not a hand reimpl. The
 * mp3enc_* symbols are disjoint from the decoder half's oracle_* symbols, so the
 * two C references coexist in one test binary without clashing.
 *
 * The handle (mp3enc_t) is an opaque pointer over a heap-allocated
 * lame_internal_flags; the Go side never inspects its layout, only priming
 * fields and reading them back through the accessors here. The setters mirror
 * exactly the fields nativemp3 maps onto its unified LameInternalFlags so both
 * sides start from byte-identical state.
 *
 * These compile only under -tags='cgo mp3lame' (LGPL fence): bitstream.c /
 * reservoir.c / tables.c are LGPL LAME source, so the oracle and the Go port it
 * pins are mp3lame-gated. A bare `go test` (cgo, no mp3lame) compiles only the
 * decoder half.
 */
#ifndef MP3_BITSTREAM_FORMAT_ENC_ORACLE_H
#define MP3_BITSTREAM_FORMAT_ENC_ORACLE_H

#include <stdint.h>

typedef struct mp3enc_t mp3enc_t;

/* lifecycle. buf_size is the bit-stream output buffer capacity in bytes. */
mp3enc_t *mp3enc_new(int buf_size);
void      mp3enc_free(mp3enc_t *e);

/* ---- config (cfg) setters ---- */
void mp3enc_set_cfg(mp3enc_t *e, int version, int samplerate_out,
                    int samplerate_index, int sideinfo_len, int channels_out,
                    int mode_gr, int mode, int error_protection,
                    int extension, int copyright, int original, int emphasis,
                    int disable_reservoir, int avg_bitrate,
                    int buffer_constraint);

/* ---- per-frame result (ov_enc) setters ---- */
void mp3enc_set_ov(mp3enc_t *e, int bitrate_index, int padding, int mode_ext);

/* ---- encoder state-var (sv_enc) setters ---- */
void mp3enc_set_sv(mp3enc_t *e, int h_ptr, int w_ptr, int ancillary_flag,
                   int resv_size, int resv_max);
void mp3enc_set_substep_shaping(mp3enc_t *e, int v);

/* Seed header[slot].{write_timing,ptr,buf[0..len)}. */
void mp3enc_prime_header(mp3enc_t *e, int slot, int write_timing, int ptr,
                         const unsigned char *buf, int len);
/* Disarm all UNPRIMED header slots to a sentinel write_timing that putbits2's
 * running totbit can never reach (so no spurious header splice). */
void mp3enc_disarm_headers(mp3enc_t *e, int sentinel);

/* ---- bit-stream (bs) state setters/getters ---- */
void mp3enc_set_bs(mp3enc_t *e, int totbit, int buf_byte_idx, int buf_bit_idx);
int  mp3enc_bs_totbit(const mp3enc_t *e);
int  mp3enc_bs_buf_byte_idx(const mp3enc_t *e);
int  mp3enc_bs_buf_bit_idx(const mp3enc_t *e);
void mp3enc_copy_bs_buf(const mp3enc_t *e, unsigned char *out, int n);

/* ---- side-info (l3_side) setters/getters ---- */
void mp3enc_set_side(mp3enc_t *e, int main_data_begin, int private_bits,
                     int resv_drain_pre, int resv_drain_post);
void mp3enc_set_scfsi(mp3enc_t *e, int ch, int band, int v);
int  mp3enc_main_data_begin(const mp3enc_t *e);
int  mp3enc_resv_drain_pre(const mp3enc_t *e);
int  mp3enc_resv_drain_post(const mp3enc_t *e);

/* ---- scalefac_band (scalefac_band.l[] / .s[]) ---- */
void mp3enc_set_sfb_l(mp3enc_t *e, int i, int v);
void mp3enc_set_sfb_s(mp3enc_t *e, int i, int v);

/* ---- gr_info (l3_side.tt[gr][ch]) setters ----
 * Sets the header/main-data fields encodeSideInfo2 / writeMainData read. With
 * big_values == count1 == 0 and table_select all 0, the Huffman emitters touch
 * no ht[] code words (Go's ht[].Table is unpopulated), so the emitted main data
 * is the scalefactor stream only — bit-exact on both sides. */
void mp3enc_set_gr(mp3enc_t *e, int gr, int ch,
                   int part2_3_length, int part2_length, int big_values,
                   int count1, int global_gain, int scalefac_compress,
                   int block_type, int mixed_block_flag,
                   int region0_count, int region1_count, int preflag,
                   int scalefac_scale, int count1table_select,
                   int sfbdivide, int sfbmax);
void mp3enc_set_gr_table_select(mp3enc_t *e, int gr, int ch, int idx, int v);
void mp3enc_set_gr_subblock_gain(mp3enc_t *e, int gr, int ch, int idx, int v);
void mp3enc_set_gr_scalefac(mp3enc_t *e, int gr, int ch, int sfb, int v);
/* LSF partition table: the gr_info.sfb_partition_table pointer + slen[4]. The
 * partition table must persist; the handle owns a per-(gr,ch) backing array. */
void mp3enc_set_gr_partition(mp3enc_t *e, int gr, int ch, const int *part4,
                             const int *slen4);
/* Read back gr_info.table_select (encodeSideInfo2 remaps 14 -> 16 in place). */
int  mp3enc_gr_table_select(const mp3enc_t *e, int gr, int ch, int idx);

/* ---- trampolines into the genuine vendored statics/functions ---- */
int  mp3enc_calc_frame_length(mp3enc_t *e, int kbps, int pad);
int  mp3enc_getframebits(mp3enc_t *e);
int  mp3enc_get_max_frame_buffer_size_by_constraint(mp3enc_t *e, int constraint);
void mp3enc_writeheader(mp3enc_t *e, int val, int j);
int  mp3enc_crc_update(int value, int crc);
void mp3enc_crc_writeheader(mp3enc_t *e, unsigned char *header);
void mp3enc_drain_into_ancillary(mp3enc_t *e, int remaining_bits);
void mp3enc_encode_side_info2(mp3enc_t *e, int bits_per_frame);
int  mp3enc_write_main_data(mp3enc_t *e);
int  mp3enc_compute_flushbits(mp3enc_t *e, int *total_bytes_output);
void mp3enc_flush_bitstream(mp3enc_t *e);
void mp3enc_add_dummy_byte(mp3enc_t *e, unsigned char val, unsigned int n);
int  mp3enc_do_copy_buffer(mp3enc_t *e, unsigned char *buffer, int size);
int  mp3enc_copy_buffer(mp3enc_t *e, unsigned char *buffer, int size, int mp3data);
int  mp3enc_format_bitstream(mp3enc_t *e);

/* reservoir.c (non-static) */
int  mp3enc_resv_frame_begin(mp3enc_t *e, int *mean_bits);
void mp3enc_resv_max_bits(mp3enc_t *e, int mean_bits, int *targ_bits, int *extra_bits, int cbr);
void mp3enc_resv_adjust(mp3enc_t *e, int gr, int ch);
void mp3enc_resv_frame_end(mp3enc_t *e, int mean_bits);

/* read back sv_enc / header-ring state mutated by encodeSideInfo2 / putbits */
int  mp3enc_h_ptr(const mp3enc_t *e);
int  mp3enc_w_ptr(const mp3enc_t *e);
int  mp3enc_ancillary_flag(const mp3enc_t *e);
int  mp3enc_resv_size(const mp3enc_t *e);
int  mp3enc_resv_max(const mp3enc_t *e);
int  mp3enc_substep_shaping(const mp3enc_t *e);
int  mp3enc_header_write_timing(const mp3enc_t *e, int slot);
int  mp3enc_header_ptr(const mp3enc_t *e, int slot);
void mp3enc_copy_header_buf(const mp3enc_t *e, int slot, unsigned char *out, int n);

#endif /* MP3_BITSTREAM_FORMAT_ENC_ORACLE_H */
