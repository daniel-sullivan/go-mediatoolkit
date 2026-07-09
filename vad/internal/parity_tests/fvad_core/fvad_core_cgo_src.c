/* Compiles the complete vendored libfvad core (everything below the
 * fvad.c API wrapper) for the fvad_core parity slice's own cgo build
 * (see vad/libfvad/VERSION for provenance: dpirch/libfvad master,
 * commit 532ab666c20d3cfda38bca63abbb0f152706c369). Self-contained TU
 * per slice — see fvad_sp/fvad_sp_cgo_src.c for the rationale. The
 * vendored files carry no colliding static names, so a single TU
 * including all of them is well-formed.
 */

#include "../../../libfvad/src/signal_processing/spl_inl.c"
#include "../../../libfvad/src/signal_processing/division_operations.c"
#include "../../../libfvad/src/signal_processing/get_scaling_square.c"
#include "../../../libfvad/src/signal_processing/energy.c"
#include "../../../libfvad/src/signal_processing/resample_by_2_internal.c"
#include "../../../libfvad/src/signal_processing/resample_fractional.c"
#include "../../../libfvad/src/signal_processing/resample_48khz.c"
#include "../../../libfvad/src/vad/vad_sp.c"
#include "../../../libfvad/src/vad/vad_gmm.c"
#include "../../../libfvad/src/vad/vad_filterbank.c"
#include "../../../libfvad/src/vad/vad_core.c"
