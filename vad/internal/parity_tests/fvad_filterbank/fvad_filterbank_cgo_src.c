/* Compiles the vendored libfvad feature-extraction sources for the
 * fvad_filterbank parity slice's own cgo build (see
 * vad/libfvad/VERSION for provenance: dpirch/libfvad master, commit
 * 532ab666c20d3cfda38bca63abbb0f152706c369). Self-contained TU per
 * slice — see fvad_sp/fvad_sp_cgo_src.c for the rationale.
 *
 * vad_filterbank.c needs the energy/scaling-square primitives and the
 * SPL inline table; it does NOT need the resamplers or vad_core.c
 * (vad_core.h is included for the VadInstT type only).
 */

#include "../../../libfvad/src/signal_processing/spl_inl.c"
#include "../../../libfvad/src/signal_processing/get_scaling_square.c"
#include "../../../libfvad/src/signal_processing/energy.c"
#include "../../../libfvad/src/vad/vad_filterbank.c"
