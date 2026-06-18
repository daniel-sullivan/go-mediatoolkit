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

/* On Windows (incl. mingw) libFLAC's compat.h redirects flac_fprintf/
 * flac_fopen to fprintf_utf8/fopen_utf8 (lpc.c references flac_fprintf
 * in its debug path), which live in share/win_utf8_io.c. That TU is not
 * otherwise compiled into this parity binary, so pull it in here to
 * satisfy the link. No-op on non-Windows, where compat.h maps the
 * flac_* macros straight to the stdio functions. */
#ifdef _WIN32
#include "src/share/win_utf8_io/win_utf8_io.c"
#endif
