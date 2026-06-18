// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/*
 * oracle.c — compiles the vendored LAME 3.100 analysis-filterbank/MDCT source
 * and re-exports its file-static window_subband / mdct_short / mdct_long for
 * the mdct-analysis parity tests.
 *
 * This translation unit #includes the committed libmp3lame/newmdct.c, which
 * brings the static window_subband (newmdct.c:430), mdct_short (newmdct.c:832)
 * and mdct_long (newmdct.c:869) into scope along with the enwindow / win /
 * order coefficient tables they read. The oracle_* wrappers below sit in the
 * same TU and forward straight through to those statics, so the C side of every
 * parity assertion is the genuine vendored reference, never a hand twin.
 *
 * newmdct.c is compiled in isolation as its own TU (one .c per parity binary)
 * so each go-test binary's symbol set is self-contained — no cross-package
 * static-symbol clash (see the parity discipline in
 * CONTRIBUTING.md). This package never imports
 * libraries/mp3 (which would duplicate the LAME symbols at link time); it only
 * imports the pure-Go internal/nativemp3 port.
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS
 * (cgo.go). The FP-determinism flags (-ffp-contract=off, -fno-vectorize,
 * -fno-slp-vectorize, -fno-unroll-loops) come from the mise task env so the
 * filterbank/MDCT products round separately, matching the FMA-free mp3_strict
 * Go build. They are NOT placed here because Go's cgo flag allowlist rejects
 * -ffp-contract=off in source.
 *
 * newmdct.c needs HAVE_CONFIG_H (set in cgo CFLAGS) so it includes liblame's
 * config.h, which selects FLOAT == float (SIZEOF_FLOAT 4) and the scalar
 * baseline. window_subband / mdct_short / mdct_long touch no gfc state (only
 * the file-static coefficient tables), so no lame_internal_flags instance is
 * needed; mdct_sub48 — which does reach through gfc — is deliberately not
 * wrapped (see oracle.h).
 *
 * LGPL note: newmdct.c is LGPL LAME source, so this oracle TU and the Go hooks
 * it pins are gated by the mp3lame build tag (in addition to cgo), exactly like
 * the encoder-analysis slice they test. A bare `go test` never compiles them.
 */

#include "newmdct.c"

#include "oracle.h"

/* sample_t* / FLOAT* are ABI-identical to float* (config.h: SIZEOF_FLOAT 4),
 * so these forward the buffers straight into the vendored statics. */
void oracle_window_subband(float *x1_base, int base, float *a) {
    window_subband((const sample_t *)(x1_base + base), (FLOAT *)a);
}

void oracle_mdct_short(float *inout) {
    mdct_short((FLOAT *)inout);
}

void oracle_mdct_long(float *out, const float *in) {
    mdct_long((FLOAT *)out, (const FLOAT *)in);
}