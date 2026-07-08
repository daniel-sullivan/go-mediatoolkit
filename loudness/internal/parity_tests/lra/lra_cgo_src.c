/* Parity oracle for the LRA (loudness range) slice. Compiles the vendored
 * libebur128 amalgamation (v1.2.6, commit
 * 67b33abe1558160ed76ada1322329b0e9e058b02) in this slice's own translation
 * unit so cgo.go can call the public ebur128 API without importing package
 * loudness (see cgo.go).
 *
 * The one shim, lra_st_count, walks the static short-term block list (or
 * sums the short-term histogram) to expose how many short-term blocks the
 * oracle has retained — a structural check on the 1 s LRA hop and
 * set_max_history trimming, alongside the public ebur128_loudness_range
 * value comparison. */

#include "ebur128.c"

int lra_st_count(ebur128_state* st) {
  if (st->mode & EBUR128_MODE_HISTOGRAM) {
    int n = 0;
    int i;
    for (i = 0; i < 1000; ++i) {
      n += (int) st->d->short_term_block_energy_histogram[i];
    }
    return n;
  }
  struct ebur128_dq_entry* it;
  int n = 0;
  STAILQ_FOREACH(it, &st->d->short_term_block_list, entries) { n++; }
  return n;
}
