/* Compiles the vendored libebur128 amalgamation for this parity
 * slice's own cgo build. Same vendored source loudness/libebur128
 * uses (see loudness/libebur128/VERSION for the upstream provenance:
 * v1.2.6, commit 67b33abe1558160ed76ada1322329b0e9e058b02),
 * deliberately compiled again in this package's own translation unit
 * rather than reusing the root package's already-compiled object code
 * — see cgo.go for why this slice does not import package loudness.
 */

#include "ebur128.c"
