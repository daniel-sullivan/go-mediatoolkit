/* Benchmark oracle: compiles the vendored libebur128 amalgamation (v1.2.6)
 * in this slice's own translation unit so the Go-vs-C comparison benchmarks
 * can drive the C meter without importing package loudness (see cgo.go).
 *
 * The //loudness:bench mise task builds this with the same scalar, FMA-free
 * flags as the parity oracle (-ffp-contract=off -fno-vectorize
 * -fno-slp-vectorize -fno-unroll-loops), so the C column is an
 * apples-to-apples scalar reference, NOT a production libebur128 build. */

#include "ebur128.c"

/* One full meter pass: init, feed all frames, read the integrated loudness
 * and (if enabled) channel-0 true peak, destroy. Returns a value derived
 * from both readings so the compiler cannot elide the work. */
double bench_run(unsigned int channels, unsigned long rate, int mode,
                 const double* data, size_t frames) {
  ebur128_state* st = ebur128_init(channels, rate, mode);
  if (!st) {
    return 0.0;
  }
  ebur128_add_frames_double(st, data, frames);
  double out = 0.0;
  if ((mode & EBUR128_MODE_I) == EBUR128_MODE_I) {
    ebur128_loudness_global(st, &out);
  }
  double tp = 0.0;
  if ((mode & EBUR128_MODE_TRUE_PEAK) == EBUR128_MODE_TRUE_PEAK) {
    ebur128_true_peak(st, 0, &tp);
  }
  ebur128_destroy(&st);
  return out + tp;
}
