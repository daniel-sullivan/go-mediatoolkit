/* Compiles libFLAC fixed.c + lpc.c so the FLAC__*_restore_signal
 * functions are linkable from this package's cgo wrappers. cpu.c is
 * pulled in for the FLAC__cpu_info() reference at the top of lpc.c.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/fixed.c"
#include "src/libFLAC/float.c"
#include "src/libFLAC/lpc.c"
