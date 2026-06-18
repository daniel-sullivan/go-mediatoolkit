// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU supplying schur_div for the chaos-measure oracle.
 *
 * chaosmeasure.cpp calls schur_div, which on non-x86 targets (arm64, ppc, the
 * generic build) is an out-of-line function defined in
 * libFDK/src/fixpoint_math.cpp — the x86 header inlines it instead, in which
 * case the .cpp body compiles to nothing (it is guarded by
 * `#if !defined(FUNCTION_schur_div)`). Compiling fixpoint_math.cpp as its own
 * translation unit therefore resolves schur_div everywhere without duplicate
 * symbols. fixpoint_math.cpp's only include is fixpoint_math.h, so this drags
 * in no other libfdk module. It also defines the invSqrtTab / ldDataTable
 * lookup tables, which are unused by the chaos path but harmless.
 */

#include "libfdk/libFDK/src/fixpoint_math.cpp"
