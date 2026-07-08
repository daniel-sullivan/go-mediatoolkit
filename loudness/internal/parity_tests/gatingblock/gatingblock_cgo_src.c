/* Parity oracle for the gating-block slice.
 *
 * Compiles the vendored libebur128 amalgamation (same source and upstream
 * provenance as loudness/libebur128/VERSION: v1.2.6, commit
 * 67b33abe1558160ed76ada1322329b0e9e058b02) in this slice's own
 * translation unit — see cgo.go for why each parity slice compiles its own
 * copy rather than importing package loudness.
 *
 * ebur128_calc_gating_block is `static` inside ebur128.c, so it cannot be
 * linked as an external symbol. Because this file #includes ebur128.c, the
 * shim below lives in the same TU and can call it directly (and read the
 * static samples_in_100ms from the opaque internal struct), exposing the
 * trailing-400ms block energy to cgo.
 */

#include "ebur128.c"

/* Drive a MODE_M meter with the given channel-map override, feed it
 * `frames` interleaved double frames, then return the trailing-400ms
 * gating-block ENERGY (before the log-domain loudness conversion) via the
 * static ebur128_calc_gating_block with a non-NULL optional_output.
 *
 * MODE_M keeps the internal per-block gating list out of the picture (it is
 * only maintained under MODE_I) while still filling audio_data exactly as
 * any mode would — the readout recomputes energy straight from the ring
 * buffer, so the block energy is mode-independent. chmap has `channels`
 * entries; set_channel return values are ignored (the Go side constructs
 * only valid maps, and mirrors this by ignoring its own SetChannel
 * result). */
double gb_block_energy(unsigned int channels, unsigned long rate,
                       const int* chmap, const double* src, size_t frames) {
  ebur128_state* st = ebur128_init(channels, rate, EBUR128_MODE_M);
  if (!st) {
    return -1.0;
  }
  unsigned int c;
  for (c = 0; c < channels; ++c) {
    ebur128_set_channel(st, c, chmap[c]);
  }
  if (frames > 0) {
    ebur128_add_frames_double(st, src, frames);
  }
  double out = 0.0;
  ebur128_calc_gating_block(st, (size_t) st->d->samples_in_100ms * 4, &out);
  ebur128_destroy(&st);
  return out;
}
