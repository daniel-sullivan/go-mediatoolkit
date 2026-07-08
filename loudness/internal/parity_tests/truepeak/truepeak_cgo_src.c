/* Parity oracle for the true-peak interpolator slice.
 *
 * Compiles the vendored libebur128 amalgamation (same source and
 * upstream provenance as loudness/libebur128/VERSION: v1.2.6, commit
 * 67b33abe1558160ed76ada1322329b0e9e058b02) in this slice's own
 * translation unit — see cgo.go for why each parity slice compiles its
 * own copy rather than importing package loudness.
 *
 * interp_create / interp_process / interp_destroy are `static` inside
 * ebur128.c, so they cannot be linked as external symbols. Because this
 * file #includes ebur128.c, the thin extern shims below live in the same
 * TU and can call those statics directly, exposing them to cgo through
 * opaque void* handles. This mirrors the static-internal trampoline
 * pattern in libraries/flac/internal/parity_tests/frameheader.
 *
 * The full-path true-peak comparison needs no shim: it drives the public
 * ebur128_init / ebur128_add_frames_double / ebur128_true_peak API
 * directly from cgo.go.
 */

#include "ebur128.c"

/* Opaque-handle wrappers around the static polyphase interpolator so the
 * Go side can create one interpolator, push several chunks through it
 * (persistent delay-line state), then destroy it. */

void* tp_interp_create(unsigned int taps, unsigned int factor,
                       unsigned int channels) {
  return (void*) interp_create(taps, factor, channels);
}

size_t tp_interp_process(void* h, size_t frames, float* in, float* out) {
  return interp_process((interpolator*) h, frames, in, out);
}

void tp_interp_destroy(void* h) { interp_destroy((interpolator*) h); }
