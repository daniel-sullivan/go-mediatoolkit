/* Compiles libFLAC lpc.c (encoder-analysis half) plus the support TUs
 * it links against, so the FLAC__lpc_compute_* symbols are available to
 * this package's cgo wrappers. cpu.c backs the FLAC__cpu_info() the
 * autocorrelation dispatch references; bitmath/format/float round out
 * the references in lpc.c.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/float.c"
#include "src/libFLAC/lpc.c"
