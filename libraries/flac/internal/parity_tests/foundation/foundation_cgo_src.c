/* Compiles the libFLAC TUs whose helpers the parity tests in this
 * package call into. Same vendored sources libraries/flac uses; same
 * scalar-baseline config.h. Each cgo package gets its own libFLAC
 * compilation, which is fine for tests because each go-test binary is
 * one package wide — there's no cross-binary symbol collision.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/crc.c"
#include "src/libFLAC/md5.c"
#include "src/libFLAC/format.c"

/* MD5 update is static inside md5.c, so its trampoline must live in
 * the same translation unit. The other helpers (init/final/accumulate)
 * have public declarations and could go in cgo.go's preamble — they
 * are co-located here for symmetry. */
#include <stdlib.h>

FLAC__MD5Context *fparity_md5_new_impl(void) {
    FLAC__MD5Context *c = (FLAC__MD5Context*)calloc(1, sizeof(*c));
    FLAC__MD5Init(c);
    return c;
}

void fparity_md5_free_impl(FLAC__MD5Context *c) {
    if (!c) return;
    free(c->internal_buf.p8);
    free(c);
}

void fparity_md5_update_impl(FLAC__MD5Context *c, const uint8_t *d, uint32_t n) {
    FLAC__MD5Update(c, d, n);
}

void fparity_md5_final_impl(FLAC__MD5Context *c, uint8_t out[16]) {
    FLAC__MD5Final(out, c);
}

int fparity_md5_accumulate_impl(FLAC__MD5Context *c, const int32_t * const *signal, uint32_t channels, uint32_t samples, uint32_t bytes_per_sample) {
    return FLAC__MD5Accumulate(c, signal, channels, samples, bytes_per_sample) ? 1 : 0;
}
