//go:build cgo

/* Compiles a private copy of the vendored minimp3 reference and surfaces
 * the static "main-bits" helpers to Go-cgo via mp3parity_* trampolines.
 *
 * minimp3 is a single-header library: every function this slice tests
 * (bs_init, get_bits, hdr_valid, hdr_compare, the HDR_* accessors,
 * mp3d_match_frame, mp3d_find_frame, L3_save_reservoir,
 * L3_restore_reservoir) is `static`, so it can only be called from a TU
 * that #includes the implementation. We therefore define
 * MINIMP3_IMPLEMENTATION here and wrap each static. Each go-test binary is
 * one package wide, so this private minimp3 copy never collides with the
 * one libraries/mp3 compiles for production.
 *
 * The trampolines are verbatim calls into minimp3 — they add nothing but a
 * stable, non-static linkage name and (for the reservoir helpers) the
 * scratch plumbing the C decode loop normally owns.
 *
 * Scalar-baseline FP flags (-ffp-contract=off, …) come from the mise task
 * env, not from here; this slice is integer-only so they do not affect it.
 */

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"

#include <string.h>

/* ---- bit reader (bs_t) ---- */

void mp3parity_bs_init(bs_t *bs, const uint8_t *data, int bytes) {
    bs_init(bs, data, bytes);
}
uint32_t mp3parity_get_bits(bs_t *bs, int n) { return get_bits(bs, n); }
int      mp3parity_bs_pos(const bs_t *bs)    { return bs->pos; }
int      mp3parity_bs_limit(const bs_t *bs)  { return bs->limit; }

/* ---- frame-header accessors ---- */

int      mp3parity_hdr_valid(const uint8_t *h)        { return hdr_valid(h) ? 1 : 0; }
int      mp3parity_hdr_compare(const uint8_t *h1, const uint8_t *h2) { return hdr_compare(h1, h2) ? 1 : 0; }
unsigned mp3parity_hdr_bitrate_kbps(const uint8_t *h) { return hdr_bitrate_kbps(h); }
unsigned mp3parity_hdr_sample_rate_hz(const uint8_t *h){ return hdr_sample_rate_hz(h); }
unsigned mp3parity_hdr_frame_samples(const uint8_t *h){ return hdr_frame_samples(h); }
int      mp3parity_hdr_frame_bytes(const uint8_t *h, int ff) { return hdr_frame_bytes(h, ff); }
int      mp3parity_hdr_padding(const uint8_t *h)      { return hdr_padding(h); }

/* ---- frame sync ---- */

int mp3parity_match_frame(const uint8_t *hdr, int mp3_bytes, int frame_bytes) {
    return mp3d_match_frame(hdr, mp3_bytes, frame_bytes);
}
int mp3parity_find_frame(const uint8_t *mp3, int mp3_bytes, int *free_format_bytes, int *ptr_frame_bytes) {
    return mp3d_find_frame(mp3, mp3_bytes, free_format_bytes, ptr_frame_bytes);
}

/* ---- bit reservoir ----
 *
 * L3_save_reservoir reads s->bs.{pos,limit} and s->maindata; L3_restore_
 * reservoir reads h->reserv / h->reserv_buf and a frame bs, writing
 * s->maindata + s->bs. We materialise a scratch on the stack, prime the
 * fields the helper consumes, call it, and copy the bytes the parity test
 * inspects back out.
 */

void mp3parity_save_reservoir(mp3dec_t *h, uint8_t *scratch_maindata, int bs_pos, int bs_limit) {
    mp3dec_scratch_t s;
    memset(&s, 0, sizeof(s));
    memcpy(s.maindata, scratch_maindata, (size_t)(MAX_BITRESERVOIR_BYTES + MAX_L3_FRAME_PAYLOAD_BYTES));
    s.bs.buf   = s.maindata;
    s.bs.pos   = bs_pos;
    s.bs.limit = bs_limit;
    L3_save_reservoir(h, &s);
}

int mp3parity_restore_reservoir(mp3dec_t *h, const uint8_t *payload, int payload_bytes,
                                int main_data_begin, uint8_t *out_maindata, int *out_limit) {
    mp3dec_scratch_t s;
    memset(&s, 0, sizeof(s));
    bs_t frame_bs;
    bs_init(&frame_bs, payload, payload_bytes);
    int ok = L3_restore_reservoir(h, &frame_bs, &s, main_data_begin);
    *out_limit = s.bs.limit;
    memcpy(out_maindata, s.maindata, (size_t)(MAX_BITRESERVOIR_BYTES + MAX_L3_FRAME_PAYLOAD_BYTES));
    return ok;
}
