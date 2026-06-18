// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/*
 * oracle.c — compiles the vendored LAME 3.100 FFT/FHT source and re-exports its
 * file-static fht() for the psychoacoustic-model parity tests.
 *
 * This translation unit #includes the committed libmp3lame/fft.c, which brings
 * the static fht() (Ron Mayer's fast Hartley transform, fft.c:62) into scope
 * along with its costab trig-seed table. The oracle_fht wrapper below sits in
 * the same TU and forwards straight through to that static, so the C side of
 * every parity assertion is the genuine vendored reference, never a hand twin.
 *
 * fft.c is compiled in isolation as its own TU (one .c per parity binary) so
 * each go-test binary's symbol set is self-contained — no cross-package
 * static-symbol clash (see the parity discipline in
 * CONTRIBUTING.md). This package never imports
 * libraries/mp3 (which would duplicate the LAME symbols at link time); it only
 * imports the pure-Go internal/nativemp3 port.
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS
 * (cgo.go). The FP-determinism flags (-ffp-contract=off, -fno-vectorize,
 * -fno-slp-vectorize, -fno-unroll-loops) come from the mise task env so the
 * butterfly products in fht round separately, matching the FMA-free mp3_strict
 * Go build. They are NOT placed here because Go's cgo flag allowlist rejects
 * -ffp-contract=off in source.
 *
 * fft.c needs HAVE_CONFIG_H (set in cgo CFLAGS) so it includes liblame's
 * config.h, which selects FLOAT == float (SIZEOF_FLOAT 4) and the scalar
 * baseline (no MMX/SSE/3DNow paths). fft.c references gfc->fft_fht only inside
 * fft_long/fft_short, which the oracle never calls — only fht itself — so no
 * lame_internal_flags instance is needed.
 */

#include "libmp3lame/fft.c"

#include "oracle.h"

/* float* is ABI-identical to FLOAT* (config.h: SIZEOF_FLOAT 4), so this
 * forwards the buffer straight into the vendored static fht. */
void oracle_fht(float *fz, int n) {
    fht((FLOAT *)fz, n);
}
