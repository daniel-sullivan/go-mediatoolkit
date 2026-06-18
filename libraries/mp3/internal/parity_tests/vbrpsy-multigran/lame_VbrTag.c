// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

/* Per-TU cgo wrapper for the vbr-encode-e2e parity oracle: compiles one vendored
 * LAME source in isolation (LAME reuses Min/Max macros + per-TU statics, so
 * each source needs its own translation unit). The oracle drives a real -V2
 * encode end-to-end through the genuine public LAME API to populate
 * gfc->VBR_seek_table / nMusicCRC / cfg / ov_enc, then reads the genuine
 * lame_get_lametag_frame output as the golden bytes. Build config + include
 * paths come from cgo.go. */
#include "libmp3lame/VbrTag.c"
