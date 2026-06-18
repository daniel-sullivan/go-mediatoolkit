/* Compiles libFLAC bitwriter.c plus its dependencies. Each parity
 * package is its own go-test process with an isolated symbol table, so
 * recompiling these TUs here does not clash with other packages.
 */

#include "src/libFLAC/bitmath.c"
#include "src/libFLAC/cpu.c"
#include "src/libFLAC/crc.c"
#include "src/libFLAC/format.c"
#include "src/libFLAC/bitwriter.c"
