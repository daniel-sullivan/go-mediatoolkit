// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling the vendored FDK CRC engine as its own
// translation unit for the ADTS frame-sync-parse parity oracle.
// adtsRead_DecodeHeader links FDKcrcInit/Reset/StartReg/EndReg/GetCRC from here.
// See libfdk/COPYING.
#include "libfdk/libFDK/src/FDK_crc.cpp"
