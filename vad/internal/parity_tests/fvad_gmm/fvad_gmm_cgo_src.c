/* Compiles the vendored libfvad Gaussian-probability source for the
 * fvad_gmm parity slice's own cgo build (see vad/libfvad/VERSION for
 * provenance: dpirch/libfvad master, commit
 * 532ab666c20d3cfda38bca63abbb0f152706c369). Self-contained TU per
 * slice — see fvad_sp/fvad_sp_cgo_src.c for the rationale.
 *
 * vad_gmm.c needs only WebRtcSpl_DivW32W16 from the SPL layer.
 */

#include "../../../libfvad/src/signal_processing/division_operations.c"
#include "../../../libfvad/src/vad/vad_gmm.c"
