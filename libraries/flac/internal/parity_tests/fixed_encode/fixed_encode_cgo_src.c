/* Compiles libFLAC fixed.c (plus its dependencies) so the encoder-side
 * FLAC__fixed_compute_best_predictor / _wide functions are linkable from
 * this package's cgo wrappers. This TU has its own private copy of the
 * .c files so it does not clash with any other parity package's libFLAC
 * symbols at link time. bitmath.c is needed by fixed.c's integer-only
 * path declarations; cpu.c/float.c/format.c are pulled in to satisfy the
 * remaining references compiled in the non-integer-only build.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/float.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/fixed.c"
