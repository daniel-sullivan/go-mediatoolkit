//go:build cgo

/*
 * oracle.c — compiles the vendored minimp3 single-header implementation and
 * re-exports its file-static Layer III bit-allocation (scalefactor decode)
 * routines for the parity tests.
 *
 * This translation unit defines MINIMP3_IMPLEMENTATION and #includes the
 * committed libminimp3/minimp3.h, which brings the static L3_read_side_info /
 * L3_read_scalefactors / L3_ldexp_q2 / L3_decode_scalefactors functions into
 * scope. The oracle_* wrappers below sit in the same TU and forward straight
 * through to those statics, so the C side of every parity assertion is the
 * genuine vendored reference, never a hand twin.
 *
 * minimp3.h is a single-header lib; including it with MINIMP3_IMPLEMENTATION in
 * exactly one TU per parity binary keeps each go-test binary's symbol set
 * self-contained (no cross-package static-symbol clash — see the parity
 * discipline in CONTRIBUTING.md). MINIMP3_ONLY_MP3
 * matches the production cgo build (Layer III only).
 *
 * Build flags: only -I/-D/-Wno-* live in the in-source #cgo CFLAGS (cgo.go).
 * The FP-determinism flags (-ffp-contract=off, -fno-vectorize,
 * -fno-slp-vectorize, -fno-unroll-loops) come from the mise task env so the
 * single float multiplies in L3_ldexp_q2 round separately, matching the
 * FMA-free mp3_strict Go build. They are NOT placed here because Go's cgo flag
 * allowlist rejects -ffp-contract=off in source.
 */

#include <stdlib.h>
#include <string.h>
#include <stddef.h>
#include <stdint.h>

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"

#include "oracle.h"

/* Layout sanity: the oracle mirror structs must match minimp3's own so the Go
 * cgo views drive the real types byte-for-byte. */
_Static_assert(sizeof(oracle_bs_t) == sizeof(bs_t), "bs_t layout drift");
_Static_assert(sizeof(oracle_gr_t) == sizeof(L3_gr_info_t), "L3_gr_info_t layout drift");

uint8_t *oracle_buf_new(const uint8_t *data, int bytes) {
    uint8_t *p = (uint8_t *)malloc(bytes > 0 ? (size_t)bytes : 1);
    if (bytes > 0) {
        memcpy(p, data, (size_t)bytes);
    }
    return p;
}

void oracle_buf_free(uint8_t *p) { free(p); }

int oracle_read_side_info(const uint8_t *side, int side_bytes,
                          const uint8_t *h, oracle_gr_t *out_gr) {
    bs_t bs;
    bs_init(&bs, side, side_bytes);
    L3_gr_info_t gr[4];
    memset(gr, 0, sizeof(gr));
    int rc = L3_read_side_info(&bs, gr, h);
    /* L3_read_side_info returns -1 on the error path, else main_data_begin
     * (>= 0). We only report success/failure; the populated gr fields are the
     * payload the parity test consumes. */
    if (rc < 0) {
        return 0;
    }
    memcpy(out_gr, gr, sizeof(gr));
    return 1;
}

void oracle_decode_scalefactors(const uint8_t *h,
                                const uint8_t *main, int main_bytes,
                                const oracle_gr_t *gr, int ch,
                                const uint8_t *ist_pos_seed,
                                float *out_scf, uint8_t *out_ist_pos,
                                int *out_bs_pos) {
    bs_t bs;
    bs_init(&bs, main, main_bytes);

    uint8_t ist_pos[39];
    memcpy(ist_pos, ist_pos_seed, sizeof(ist_pos));

    float scf[40];
    memset(scf, 0, sizeof(scf));

    L3_decode_scalefactors(h, ist_pos, &bs, (const L3_gr_info_t *)gr, scf, ch);

    memcpy(out_scf, scf, sizeof(scf));
    memcpy(out_ist_pos, ist_pos, sizeof(ist_pos));
    *out_bs_pos = bs.pos;
}
