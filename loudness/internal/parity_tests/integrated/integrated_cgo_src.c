/* Parity oracle for the integrated-loudness slice.
 *
 * Compiles the vendored libebur128 amalgamation (v1.2.6, commit
 * 67b33abe1558160ed76ada1322329b0e9e058b02) in this slice's own
 * translation unit — see cgo.go for the rationale.
 *
 * The integrated-loudness and relative-threshold comparisons drive the
 * public ebur128 API directly from cgo.go and need no shim. The shims here
 * expose the file-scope static gating constants and histogram tables that
 * libebur128 computes in ebur128_init, so the Go port's copies can be
 * checked against the C oracle (bit-exact for the two constants that feed
 * gating comparisons; within a documented pow() ULP bound for the -70
 * boundary and the histogram tables).
 */

#include "ebur128.c"

/* Ensure the file-scope statics are initialised. In histogram mode
 * ebur128_init fills histogram_energies[] and histogram_energy_boundaries[]
 * (in every mode it fills boundaries[0]); the values are process-wide and
 * persist after destroy, so one init is enough. */
static void parity_ensure_statics(void) {
  static int done = 0;
  if (!done) {
    ebur128_state* st =
        ebur128_init(1, 48000, EBUR128_MODE_I | EBUR128_MODE_HISTOGRAM);
    if (st) {
      ebur128_destroy(&st);
    }
    done = 1;
  }
}

void gate_constants(double* rel_gate_factor, double* minus_twenty,
                    double* boundary0) {
  parity_ensure_statics();
  *rel_gate_factor = relative_gate_factor;
  *minus_twenty = minus_twenty_decibels;
  *boundary0 = histogram_energy_boundaries[0];
}

double hist_energy(int i) {
  parity_ensure_statics();
  return histogram_energies[i];
}

double hist_boundary(int i) {
  parity_ensure_statics();
  return histogram_energy_boundaries[i];
}
