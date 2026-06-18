// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Per-TU cgo wrapper for the vbrtag parity oracle: one vendored mpglib source
 * in isolation (LAME reuses per-TU statics). lame.c links the mpglib decoder for
 * its lame_decode path even when decode-on-the-fly is off; the full encoder TU
 * set must resolve them. Build config + include paths come from cgo.go. */
#include "mpglib/tabinit.c"
