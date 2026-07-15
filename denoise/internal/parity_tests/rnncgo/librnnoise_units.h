/* Shared oracle translation-unit body for the RNNoise parity slices.
 *
 * Each slice's oracle_src.c #includes this once, then defines its own
 * fparity_* accessor(s). It compiles the vendored RNNoise 0.2 C sources
 * needed to link the whole non-neural + neural-generic path into the
 * test binary, forced onto vec.h's generic scalar branch (-DDISABLE_NEON,
 * from each slice's cgo.go) and -ffp-contract=off (from CGO_CFLAGS,
 * libraries/rnnoise/mise.toml).
 *
 * By default the 28 MB float+int8 weight TU (rnnoise_data.c) is NOT
 * compiled — the non-neural slices never load weights, so its two
 * external symbols are stubbed. A slice that DOES need the real weights
 * defines RNNCGO_WITH_WEIGHTS before including this header, which pulls
 * in rnnoise_data.c instead of the stubs. */

#ifndef RNNCGO_UNITS_H
#define RNNCGO_UNITS_H

#include "nnet.h"
#include "rnnoise_data.h"

#ifdef RNNCGO_WITH_WEIGHTS
#include "rnnoise_data.c"
#else
/* Weight-path stubs: the non-neural slices never load weights. */
const WeightArray rnnoise_arrays[] = {{NULL, 0, 0, NULL}};
int init_rnnoise(RNNoise *m, const WeightArray *a) { (void)m; (void)a; return 0; }
#endif

#include "denoise.c"
#include "kiss_fft.c"
#include "pitch.c"
#include "celt_lpc.c"
#include "rnn.c"
#include "nnet.c"
#include "nnet_default.c"
#include "parse_lpcnet_weights.c"
#include "rnnoise_tables.c"

#endif /* RNNCGO_UNITS_H */
