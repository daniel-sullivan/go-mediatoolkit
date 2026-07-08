/* Parity oracle for the e2e slice: the whole meter, every reading, vs the
 * full public libebur128 API. Compiles the vendored amalgamation (v1.2.6,
 * commit 67b33abe1558160ed76ada1322329b0e9e058b02) in this slice's own
 * translation unit so cgo.go can call the public API without importing
 * package loudness (see cgo.go). No shim is needed — every reading exercised
 * here is a public ebur128 entry point. */

#include "ebur128.c"
