// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Per-TU cgo wrapper: compiles one vendored LAME source in isolation.
 * LAME reuses Min/Max macros and a per-TU struct hip_global_struct, so
 * each source must be its own translation unit (cgo compiles one .c per
 * file). Build config + include paths come from mp3_cgo.go. */
#include "liblame/libmp3lame/mpglib_interface.c"
