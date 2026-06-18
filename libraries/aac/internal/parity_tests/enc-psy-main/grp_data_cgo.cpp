// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored
 * libfdk/libAACenc/src/grp_data.cpp — FDKaacEnc_groupShortData, the short-block
 * regrouping/summing kernel FDKaacEnc_psyMain calls to assemble grouped SFB
 * thresholds/energies (psy_main.cpp:1047). The oracle links this GENUINE
 * symbol (oracle_kind == real_vendored). grp_data.cpp includes psy_const.h and
 * interface.h (-> psy_data.h for the SFB_THRESHOLD/SFB_ENERGY unions). See
 * bridge.cpp for the amalgamation-split rationale. */
#include "libfdk/libAACenc/src/grp_data.cpp"
