// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored
 * libfdk/libAACenc/src/pre_echo_control.cpp — FDKaacEnc_InitPreEchoControl and
 * FDKaacEnc_PreEchoControl, the pre-echo threshold limiter FDKaacEnc_psyMain
 * calls per window (psy_main.cpp:987). The oracle links these GENUINE symbols
 * (oracle_kind == real_vendored). pre_echo_control.cpp includes
 * psy_configuration.h (for PCM_QUANT_THR_SCALE); no other libfdk module is
 * dragged in. See bridge.cpp for the amalgamation-split rationale. */
#include "libfdk/libAACenc/src/pre_echo_control.cpp"
