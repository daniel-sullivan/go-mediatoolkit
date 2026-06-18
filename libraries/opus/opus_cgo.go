//go:build cgo

package opus

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DOPUS_BUILD
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/libopus
#cgo CFLAGS: -I${SRCDIR}/libopus/include
#cgo CFLAGS: -I${SRCDIR}/libopus/celt
#cgo CFLAGS: -I${SRCDIR}/libopus/silk
#cgo CFLAGS: -I${SRCDIR}/libopus/silk/float
#cgo CFLAGS: -I${SRCDIR}/libopus/src
// -O2 intentionally omitted: the env-provided CGO_CFLAGS carries
// `-O2 -ffp-contract=off -fno-vectorize -fno-slp-vectorize
// -fno-unroll-loops` in that order. Clang processes flags left-to-
// right; a trailing -O2 from the package directive would re-enable
// vectorization and undo the careful ordering, breaking bit-exact
// parity with the Go port. See benchcmp/cgo.go's matching comment.
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare

#include <opus.h>
*/
import "C"
