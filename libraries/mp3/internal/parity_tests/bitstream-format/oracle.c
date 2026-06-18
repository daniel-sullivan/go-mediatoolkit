/*
 * oracle.c — compiles the vendored minimp3 single-header implementation and
 * re-exports its file-static bitstream-format routines for the parity tests.
 *
 * This translation unit defines MINIMP3_IMPLEMENTATION and #includes the
 * committed libminimp3/minimp3.h, which brings the static bs_init / get_bits
 * / hdr_* / mp3d_find_frame / mp3d_match_frame / L3_*_reservoir /
 * L3_read_side_info functions into scope. The oracle_* wrappers below sit in
 * the same TU and forward straight through to those statics, so the C side of
 * every parity assertion is the genuine vendored reference, never a hand
 * twin.
 *
 * minimp3.h is a single-header lib; including it with MINIMP3_IMPLEMENTATION
 * in exactly one TU per parity binary keeps each go-test binary's symbol set
 * self-contained (no cross-package static-symbol clash — see the parity
 * discipline in CONTRIBUTING.md).
 *
 * Build flags: only -I/-D/-Wno-* live in the in-source #cgo CFLAGS (cgo.go).
 * The FP-determinism flags (-ffp-contract=off, -fno-vectorize, …) come from
 * the mise task env. This slice is integer-only, so those flags do not change
 * its results, but they are applied uniformly across the suite regardless.
 */

#include <stdlib.h>
#include <string.h>
#include <stddef.h>
#include <stdint.h>

#define MINIMP3_IMPLEMENTATION
#include "minimp3.h"

#include "oracle.h"

/* Layout sanity: the oracle mirror structs must match minimp3's own so the
 * Go cgo views and the reservoir memmoves agree byte-for-byte. */
_Static_assert(sizeof(oracle_bs_t) == sizeof(bs_t), "bs_t layout drift");
_Static_assert(sizeof(oracle_mp3dec_t) == sizeof(mp3dec_t), "mp3dec_t layout drift");
_Static_assert(offsetof(oracle_mp3dec_t, reserv) == offsetof(mp3dec_t, reserv), "reserv offset drift");
_Static_assert(offsetof(oracle_mp3dec_t, reserv_buf) == offsetof(mp3dec_t, reserv_buf), "reserv_buf offset drift");

/* ── bit reader ──────────────────────────────────────────────────────── */

uint8_t *oracle_buf_new(const uint8_t *data, int bytes) {
    uint8_t *p = (uint8_t *)malloc(bytes > 0 ? (size_t)bytes : 1);
    if (bytes > 0) {
        memcpy(p, data, (size_t)bytes);
    }
    return p;
}

void oracle_buf_free(uint8_t *p) { free(p); }

void oracle_bs_init(oracle_bs_t *bs, const uint8_t *data, int bytes) {
    bs_init((bs_t *)bs, data, bytes);
}

uint32_t oracle_get_bits(oracle_bs_t *bs, int n) {
    return get_bits((bs_t *)bs, n);
}

/* ── header accessors ────────────────────────────────────────────────── */

int oracle_hdr_is_mono(const uint8_t *h)         { return HDR_IS_MONO(h) ? 1 : 0; }
int oracle_hdr_is_free_format(const uint8_t *h)  { return HDR_IS_FREE_FORMAT(h) ? 1 : 0; }
int oracle_hdr_is_crc(const uint8_t *h)          { return HDR_IS_CRC(h) ? 1 : 0; }
int oracle_hdr_test_padding(const uint8_t *h)    { return HDR_TEST_PADDING(h); }
int oracle_hdr_test_mpeg1(const uint8_t *h)      { return HDR_TEST_MPEG1(h); }
int oracle_hdr_test_not_mpeg25(const uint8_t *h) { return HDR_TEST_NOT_MPEG25(h); }
int oracle_hdr_get_layer(const uint8_t *h)       { return HDR_GET_LAYER(h); }
int oracle_hdr_get_bitrate(const uint8_t *h)     { return HDR_GET_BITRATE(h); }
int oracle_hdr_get_sample_rate(const uint8_t *h) { return HDR_GET_SAMPLE_RATE(h); }
int oracle_hdr_get_my_sample_rate(const uint8_t *h) { return HDR_GET_MY_SAMPLE_RATE(h); }
int oracle_hdr_is_frame_576(const uint8_t *h)    { return HDR_IS_FRAME_576(h) ? 1 : 0; }
int oracle_hdr_is_layer_1(const uint8_t *h)      { return HDR_IS_LAYER_1(h) ? 1 : 0; }

int oracle_hdr_valid(const uint8_t *h)                       { return hdr_valid(h); }
int oracle_hdr_compare(const uint8_t *h1, const uint8_t *h2) { return hdr_compare(h1, h2); }
unsigned oracle_hdr_bitrate_kbps(const uint8_t *h)          { return hdr_bitrate_kbps(h); }
unsigned oracle_hdr_sample_rate_hz(const uint8_t *h)        { return hdr_sample_rate_hz(h); }
unsigned oracle_hdr_frame_samples(const uint8_t *h)         { return hdr_frame_samples(h); }
int oracle_hdr_frame_bytes(const uint8_t *h, int ff)        { return hdr_frame_bytes(h, ff); }
int oracle_hdr_padding(const uint8_t *h)                    { return hdr_padding(h); }

/* ── frame sync ──────────────────────────────────────────────────────── */

int oracle_mp3d_match_frame(const uint8_t *hdr, int mp3_bytes, int frame_bytes) {
    return mp3d_match_frame(hdr, mp3_bytes, frame_bytes);
}

int oracle_mp3d_find_frame(const uint8_t *mp3, int mp3_bytes,
                           int *free_format_bytes, int *ptr_frame_bytes) {
    return mp3d_find_frame(mp3, mp3_bytes, free_format_bytes, ptr_frame_bytes);
}

/* ── reservoir reassembly ────────────────────────────────────────────── */

struct oracle_scratch {
    mp3dec_scratch_t s;
};

oracle_scratch_t *oracle_scratch_new(void) {
    oracle_scratch_t *s = (oracle_scratch_t *)calloc(1, sizeof(oracle_scratch_t));
    return s;
}

void oracle_scratch_free(oracle_scratch_t *s) { free(s); }

oracle_mp3dec_t *oracle_dec_new(void) {
    return (oracle_mp3dec_t *)calloc(1, sizeof(oracle_mp3dec_t));
}

void oracle_dec_free(oracle_mp3dec_t *h) { free(h); }

void oracle_scratch_set_maindata(oracle_scratch_t *s, const uint8_t *data, int n) {
    memcpy(s->s.maindata, data, (size_t)n);
}

void oracle_scratch_set_bs(oracle_scratch_t *s, int pos, int limit) {
    s->s.bs.pos = pos;
    s->s.bs.limit = limit;
}

void oracle_scratch_get_maindata(oracle_scratch_t *s, uint8_t *out, int n) {
    memcpy(out, s->s.maindata, (size_t)n);
}

int oracle_scratch_bs_pos(oracle_scratch_t *s)   { return s->s.bs.pos; }
int oracle_scratch_bs_limit(oracle_scratch_t *s) { return s->s.bs.limit; }

void oracle_L3_save_reservoir(oracle_mp3dec_t *h, oracle_scratch_t *s) {
    L3_save_reservoir((mp3dec_t *)h, &s->s);
}

int oracle_L3_restore_reservoir(oracle_mp3dec_t *h, oracle_bs_t *bs,
                                oracle_scratch_t *s, int main_data_begin) {
    return L3_restore_reservoir((mp3dec_t *)h, (bs_t *)bs, &s->s, main_data_begin);
}

/* L3_read_side_info is intentionally not re-exported — see the note in
 * oracle.h. The committed minimp3 and the Go port disagree on the success
 * return value (raw main_data_begin vs. bs.Pos/8), so a side-info parity
 * oracle is deferred until that is reconciled. */
