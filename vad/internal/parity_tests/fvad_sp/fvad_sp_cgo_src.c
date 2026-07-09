/* Compiles the vendored libfvad signal-processing sources (plus
 * vad_sp.c, whose Downsampling/FindMinimum this slice also oracles)
 * for the fvad_sp parity slice's own cgo build. Same vendored source
 * tree the other fvad slices use (see vad/libfvad/VERSION for the
 * upstream provenance: dpirch/libfvad master,
 * commit 532ab666c20d3cfda38bca63abbb0f152706c369), compiled in this
 * package's own translation unit so no two slices' non-static WebRtc*
 * symbols can collide — each parity slice package is its own test
 * binary and compiles exactly the C it needs (the loudness-slice
 * pattern; see loudness/internal/parity_tests/smoke/smoke_cgo_src.c
 * for the original write-up).
 *
 * Quoted #include resolves relative to this file, so no -I flags are
 * needed for these; cgo.go's preamble adds the one include path it
 * needs for the headers.
 */

#include "../../../libfvad/src/signal_processing/spl_inl.c"
#include "../../../libfvad/src/signal_processing/division_operations.c"
#include "../../../libfvad/src/signal_processing/get_scaling_square.c"
#include "../../../libfvad/src/signal_processing/energy.c"
#include "../../../libfvad/src/signal_processing/resample_by_2_internal.c"
#include "../../../libfvad/src/signal_processing/resample_fractional.c"
#include "../../../libfvad/src/signal_processing/resample_48khz.c"
#include "../../../libfvad/src/vad/vad_sp.c"
