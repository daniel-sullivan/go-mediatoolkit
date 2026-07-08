/* Compiles the vendored libebur128 amalgamation for the filter parity
 * slice's own cgo build — the same vendored source the root loudness
 * package uses (see loudness/libebur128/VERSION for provenance: v1.2.6,
 * commit 67b33abe1558160ed76ada1322329b0e9e058b02), compiled again in
 * this package's own translation unit so the slice never imports package
 * loudness and never collides on libebur128's non-static symbols (see
 * cgo.go for the full rationale, and the smoke slice for the original
 * write-up).
 *
 * The parity shims below live here rather than in cgo.go's preamble
 * because they reach into libebur128 internals that are `static` inside
 * ebur128.c — the derived coefficient vectors st->d->b / st->d->a, the
 * opaque struct ebur128_state_internal, and the static function
 * ebur128_filter_double. Only a translation unit that #includes
 * ebur128.c can name them, mirroring the md5 shim pattern in
 * libraries/flac/internal/parity_tests/foundation/foundation_cgo_src.c.
 * They are non-static so cgo.go's separate TU can call them via extern
 * declarations.
 */

#include "ebur128.c"

/* Copy the convolved 4th-order K-weighting coefficients libebur128
 * derives for (channels, rate) into b_out[5]/a_out[5]. Uses
 * EBUR128_MODE_M — the base filtering mode, no sample/true-peak paths —
 * since coefficients depend only on the sample rate. Returns 0 on
 * success, -1 if ebur128_init fails. */
int fparity_kfilter_coeffs(unsigned int channels, unsigned long rate,
                           double b_out[5], double a_out[5]) {
  ebur128_state* st = ebur128_init(channels, rate, EBUR128_MODE_M);
  if (!st) {
    return -1;
  }
  int i;
  for (i = 0; i < 5; ++i) {
    b_out[i] = st->d->b[i];
    a_out[i] = st->d->a[i];
  }
  ebur128_destroy(&st);
  return 0;
}

/* Persistent filter handle so chunked-filtering parity can carry filter
 * state across calls exactly as the Go KFilter does. The handle is just
 * an ebur128_state* returned as void* so cgo.go need not see the
 * ebur128 types. EBUR128_MODE_M keeps only the IIR filter path live (no
 * sample/true-peak loops, which do not touch v[] or audio_data anyway). */
void* fparity_kfilter_new(unsigned int channels, unsigned long rate) {
  return (void*) ebur128_init(channels, rate, EBUR128_MODE_M);
}

void fparity_kfilter_free(void* handle) {
  ebur128_state* st = (ebur128_state*) handle;
  ebur128_destroy(&st);
}

/* Filter one chunk of `frames` interleaved double frames through
 * libebur128's static ebur128_filter_double, then copy the filtered
 * output — audio_data[0 .. frames*channels) — into dst.
 *
 * audio_data_index is deliberately left at 0 across calls, so every
 * chunk is written to the buffer head and copied straight out, while the
 * per-channel filter state st->d->v carries across calls. That matches
 * the Go seam NewKFilter(...) once + FilterInto(dst, src) per chunk. The
 * caller must keep frames <= audio_data_frames (~400 ms at the chosen
 * rate) so a chunk never overruns the ring; the parity test's chunk
 * sizes are all far below that bound. */
void fparity_kfilter_chunk(void* handle, const double* src, size_t frames,
                           double* dst) {
  ebur128_state* st = (ebur128_state*) handle;
  ebur128_filter_double(st, src, frames);
  size_t n = frames * st->channels;
  size_t i;
  for (i = 0; i < n; ++i) {
    dst[i] = st->d->audio_data[i];
  }
}
