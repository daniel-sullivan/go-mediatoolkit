//go:build cgo

/* Compiles the vendored minimp3 decoder implementation as a single cgo
 * translation unit. minimp3 is a single-header library; MINIMP3_IMPLEMENTATION
 * must be defined in exactly one TU. MINIMP3_NO_SIMD keeps it scalar (the
 * pure-Go port is the bit-exact target; this backend is the reference oracle).
 * Include paths come from mp3_cgo.go. */

#define MINIMP3_IMPLEMENTATION
#define MINIMP3_NO_SIMD
#include "minimp3.h"
