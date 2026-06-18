//go:build cgo

package benchcmp

/*
#include "config.h"

// SILK macros live in headers; wrappers give us external linkage.
#include "SigProc_FIX.h"
#include "macros.h"

static int c_silk_SMULWB(int a, int b)       { return silk_SMULWB(a, b); }
static int c_silk_SMLAWB(int a, int b, int c){ return silk_SMLAWB(a, b, c); }
static int c_silk_SMULWT(int a, int b)       { return silk_SMULWT(a, b); }
static int c_silk_SMLAWT(int a, int b, int c){ return silk_SMLAWT(a, b, c); }
static int c_silk_SMULBB(int a, int b)       { return silk_SMULBB(a, b); }
static int c_silk_SMLABB(int a, int b, int c){ return silk_SMLABB(a, b, c); }
static int c_silk_SMULBT(int a, int b)       { return silk_SMULBT(a, b); }
static int c_silk_SMLABT(int a, int b, int c){ return silk_SMLABT(a, b, c); }
static long long c_silk_SMLAL(long long a, int b, int c) { return silk_SMLAL(a, b, c); }
static int c_silk_SMULWW(int a, int b)       { return silk_SMULWW(a, b); }
static int c_silk_SMLAWW(int a, int b, int c){ return silk_SMLAWW(a, b, c); }
static int c_silk_SMMUL(int a, int b)        { return silk_SMMUL(a, b); }
static long long c_silk_SMULL(int a, int b)  { return silk_SMULL(a, b); }

static int c_silk_CLZ16(short v)             { return silk_CLZ16(v); }
static int c_silk_CLZ32(int v)               { return silk_CLZ32(v); }
static int c_silk_CLZ64(long long v)         { return silk_CLZ64(v); }

static int c_silk_ADD_SAT32(int a, int b)    { return silk_ADD_SAT32(a, b); }
static int c_silk_SUB_SAT32(int a, int b)    { return silk_SUB_SAT32(a, b); }
static long long c_silk_ADD_SAT64(long long a, long long b) { return silk_ADD_SAT64(a, b); }
static long long c_silk_SUB_SAT64(long long a, long long b) { return silk_SUB_SAT64(a, b); }

static int c_silk_ROR32(int a, int rot)      { return silk_ROR32(a, rot); }

static int c_silk_RSHIFT_ROUND(int a, int s) { return silk_RSHIFT_ROUND(a, s); }
static long long c_silk_RSHIFT_ROUND64(long long a, int s) { return silk_RSHIFT_ROUND64(a, s); }

static int c_silk_RAND(int seed)             { return silk_RAND(seed); }
static int c_silk_SQRT_APPROX(int x)         { return silk_SQRT_APPROX(x); }

static int c_silk_DIV32_varQ(int a, int b, int Q) { return silk_DIV32_varQ(a, b, Q); }
static int c_silk_INVERSE32_varQ(int b, int Q)    { return silk_INVERSE32_varQ(b, Q); }
*/
import "C"

// Thin Go wrappers for tests below.

func cSilkSMULWB(a, b int32) int32    { return int32(C.c_silk_SMULWB(C.int(a), C.int(b))) }
func cSilkSMLAWB(a, b, c int32) int32 { return int32(C.c_silk_SMLAWB(C.int(a), C.int(b), C.int(c))) }
func cSilkSMULWT(a, b int32) int32    { return int32(C.c_silk_SMULWT(C.int(a), C.int(b))) }
func cSilkSMLAWT(a, b, c int32) int32 { return int32(C.c_silk_SMLAWT(C.int(a), C.int(b), C.int(c))) }
func cSilkSMULBB(a, b int32) int32    { return int32(C.c_silk_SMULBB(C.int(a), C.int(b))) }
func cSilkSMLABB(a, b, c int32) int32 { return int32(C.c_silk_SMLABB(C.int(a), C.int(b), C.int(c))) }
func cSilkSMULBT(a, b int32) int32    { return int32(C.c_silk_SMULBT(C.int(a), C.int(b))) }
func cSilkSMLABT(a, b, c int32) int32 { return int32(C.c_silk_SMLABT(C.int(a), C.int(b), C.int(c))) }
func cSilkSMLAL(a int64, b, c int32) int64 {
	return int64(C.c_silk_SMLAL(C.longlong(a), C.int(b), C.int(c)))
}
func cSilkSMULWW(a, b int32) int32    { return int32(C.c_silk_SMULWW(C.int(a), C.int(b))) }
func cSilkSMLAWW(a, b, c int32) int32 { return int32(C.c_silk_SMLAWW(C.int(a), C.int(b), C.int(c))) }
func cSilkSMMUL(a, b int32) int32     { return int32(C.c_silk_SMMUL(C.int(a), C.int(b))) }
func cSilkSMULL(a, b int32) int64     { return int64(C.c_silk_SMULL(C.int(a), C.int(b))) }

func cSilkCLZ16(v int16) int32 { return int32(C.c_silk_CLZ16(C.short(v))) }
func cSilkCLZ32(v int32) int32 { return int32(C.c_silk_CLZ32(C.int(v))) }
func cSilkCLZ64(v int64) int32 { return int32(C.c_silk_CLZ64(C.longlong(v))) }

func cSilkADDSAT32(a, b int32) int32 { return int32(C.c_silk_ADD_SAT32(C.int(a), C.int(b))) }
func cSilkSUBSAT32(a, b int32) int32 { return int32(C.c_silk_SUB_SAT32(C.int(a), C.int(b))) }
func cSilkADDSAT64(a, b int64) int64 {
	return int64(C.c_silk_ADD_SAT64(C.longlong(a), C.longlong(b)))
}
func cSilkSUBSAT64(a, b int64) int64 {
	return int64(C.c_silk_SUB_SAT64(C.longlong(a), C.longlong(b)))
}

func cSilkROR32(a, rot int32) int32 { return int32(C.c_silk_ROR32(C.int(a), C.int(rot))) }
func cSilkRSHIFTROUND(a, s int32) int32 {
	return int32(C.c_silk_RSHIFT_ROUND(C.int(a), C.int(s)))
}
func cSilkRSHIFTROUND64(a int64, s int32) int64 {
	return int64(C.c_silk_RSHIFT_ROUND64(C.longlong(a), C.int(s)))
}

func cSilkRAND(seed int32) int32    { return int32(C.c_silk_RAND(C.int(seed))) }
func cSilkSQRTAPPROX(x int32) int32 { return int32(C.c_silk_SQRT_APPROX(C.int(x))) }
func cSilkDIV32varQ(a, b, Q int32) int32 {
	return int32(C.c_silk_DIV32_varQ(C.int(a), C.int(b), C.int(Q)))
}
func cSilkINVERSE32varQ(b, Q int32) int32 {
	return int32(C.c_silk_INVERSE32_varQ(C.int(b), C.int(Q)))
}
