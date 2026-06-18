// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/FDK_tools_rom.cpp: the sqrt_tab[49] (sqrtFixp_lookup) and
// invCount[80] (GetInvInt) tables env_calc.cpp's energy/sqrt math reads.
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
