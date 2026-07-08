/* Parity oracle for the windows slice: momentary / short-term /
 * loudness-window readings. It compiles the vendored libebur128
 * amalgamation (v1.2.6, commit 67b33abe1558160ed76ada1322329b0e9e058b02)
 * in this slice's own translation unit so cgo.go can call the public
 * ebur128 API without importing package loudness (see cgo.go).
 *
 * The one shim, win_energy, exposes the static ebur128_calc_gating_block so
 * the trailing-window ENERGY (before the log-domain loudness conversion)
 * can be compared bit-for-bit — the conversion itself, 10*log(e)/log(10) -
 * 0.691, loses precision to catastrophic cancellation when the loudness is
 * near 0 LUFS, so the LUFS readings are checked to an absolute tolerance
 * while the energies carry the exact guarantee. */

#include "ebur128.c"

/* Trailing-window energy over interval_frames frames of the ring buffer,
 * via the static ebur128_calc_gating_block — identical to the Go side's
 * r128.State.CalcGatingBlockEnergy. The caller must keep interval_frames <=
 * audio_data_frames (the public readers guarantee this via their own
 * interval check). */
double win_energy(ebur128_state* st, size_t interval_frames) {
  double out = 0.0;
  ebur128_calc_gating_block(st, interval_frames, &out);
  return out;
}
