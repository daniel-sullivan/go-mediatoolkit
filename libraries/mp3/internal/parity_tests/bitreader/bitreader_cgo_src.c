//go:build cgo

/* Compiles a private copy of the vendored minimp3 reference and surfaces
 * its static MSB-first bit reader (bs_t / bs_init / get_bits) to Go-cgo via
 * mp3parity_* trampolines.
 *
 * minimp3 is a single-header library: bs_init and get_bits are `static`, so
 * they can only be called from a translation unit that #includes the
 * implementation. We therefore define MINIMP3_IMPLEMENTATION here and wrap
 * each static behind a stable, non-static linkage name. Each go-test binary
 * is one package wide, so this private minimp3 copy never collides with the
 * one libraries/mp3 compiles for production (and this package never imports
 * libraries/mp3, which would compile minimp3 a second time and clash on its
 * statics).
 *
 * MINIMP3_NO_SIMD keeps the reference on its scalar baseline. The bit reader
 * is integer-only, so the mise scalar-FP flags (-ffp-contract=off, …) do not
 * affect it; they live in the mise task env, never in the in-source #cgo.
 */

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"

/* ---- bit reader (bs_t) trampolines ---- */

void mp3parity_bs_init(bs_t *bs, const uint8_t *data, int bytes) {
    bs_init(bs, data, bytes);
}
uint32_t mp3parity_get_bits(bs_t *bs, int n) { return get_bits(bs, n); }
int      mp3parity_bs_pos(const bs_t *bs)    { return bs->pos; }
int      mp3parity_bs_limit(const bs_t *bs)  { return bs->limit; }
