// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Link stubs for the HCR (Huffman Codeword Reordering) state functions.
 *
 * aac_rom.cpp (compiled as a sibling TU for the spectral Huffman ROM tables)
 * also defines the file-scope table `aStateConstant2State`, whose initializers
 * take the ADDRESSES of the seven Hcr_State_* functions. That single table
 * makes the linker demand those symbols even though this parity area (the plain
 * non-HCR Huffman path) never touches HCR. Pulling the real aacdec_hcrs.cpp
 * would cascade in the entire HCR/arith/MDCT decoder — exactly the cross-module
 * drag the per-package oracle discipline avoids.
 *
 * These stubs are NEVER called: only their addresses sit in the unused table.
 * If the plain-Huffman path ever reached HCR (it cannot — flags & AC_ER_HCR is
 * never set here) the abort() makes that a hard failure rather than silent
 * wrong output. Defining them here is purely a link-time concern and changes no
 * observable behaviour of the spectral decode under test.
 */

#include <stdlib.h>

#include "aacdec_hcrs.h"

UINT Hcr_State_BODY_ONLY(HANDLE_FDK_BITSTREAM, void *) { abort(); }
UINT Hcr_State_BODY_SIGN__BODY(HANDLE_FDK_BITSTREAM, void *) { abort(); }
UINT Hcr_State_BODY_SIGN__SIGN(HANDLE_FDK_BITSTREAM, void *) { abort(); }
UINT Hcr_State_BODY_SIGN_ESC__BODY(HANDLE_FDK_BITSTREAM, void *) { abort(); }
UINT Hcr_State_BODY_SIGN_ESC__SIGN(HANDLE_FDK_BITSTREAM, void *) { abort(); }
UINT Hcr_State_BODY_SIGN_ESC__ESC_PREFIX(HANDLE_FDK_BITSTREAM, void *) {
  abort();
}
UINT Hcr_State_BODY_SIGN_ESC__ESC_WORD(HANDLE_FDK_BITSTREAM, void *) { abort(); }
