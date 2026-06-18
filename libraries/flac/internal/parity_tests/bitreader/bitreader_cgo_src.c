/* Compiles libFLAC bitreader.c plus its dependencies (bitmath.c is
 * already pulled in by the Go-side ports — but its TU is recompiled
 * here because cgo packages have isolated symbol tables). The test
 * binary is one go-test process per package so duplicate symbols
 * across packages do not clash.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/crc.c"
#include "src/libFLAC/bitreader.c"

#include "_cgo_export.h"

/* Read-callback trampoline. The libFLAC bitreader needs a function
 * pointer with this exact prototype; cgo's //export gives us
 * goBitreaderRead returning C.int, which we coerce to FLAC__bool here.
 */
FLAC__bool fparity_br_read_cb(FLAC__byte buf[], size_t *bytes, void *cd) {
    return goBitreaderRead(buf, bytes, cd) ? 1 : 0;
}
