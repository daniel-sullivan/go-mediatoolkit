//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FIX.h"
#include "tables.h"

static int c_silk_lin2log(int x) { return silk_lin2log(x); }
static int c_silk_log2lin(int x) { return silk_log2lin(x); }
static int c_silk_sigm_Q15(int x) { return silk_sigm_Q15(x); }

static void c_silk_sum_sqr_shift(int *energy, int *shift, const opus_int16 *x, int len) {
    silk_sum_sqr_shift(energy, shift, x, len);
}

static int c_silk_inner_prod_aligned_scale(const opus_int16 *a, const opus_int16 *b,
                                           int scale, int len) {
    return silk_inner_prod_aligned_scale(a, b, scale, len);
}

// silk_inner_prod16_c is only built into the fixed-point libopus; in
// the float build no .c emits it. Reproduce the canonical expansion
// here as the oracle for the Go port.
static long long c_silk_inner_prod16(const opus_int16 *a, const opus_int16 *b, int len) {
    long long sum = 0;
    int i;
    for (i = 0; i < len; i++) {
        sum = silk_SMLALBB(sum, a[i], b[i]);
    }
    return sum;
}

static void c_silk_insertion_sort_increasing(opus_int32 *a, opus_int *idx, int L, int K) {
    silk_insertion_sort_increasing(a, idx, L, K);
}
// silk_insertion_sort_decreasing_int16 is FIXED_POINT-only in sort.c.
// Inline a copy of the same implementation for oracle purposes.
static void c_silk_insertion_sort_decreasing_int16(opus_int16 *a, opus_int *idx, int L, int K) {
    opus_int i, j;
    opus_int value;
    for (i = 0; i < K; i++) idx[i] = i;
    for (i = 1; i < K; i++) {
        value = a[i];
        for (j = i - 1; j >= 0 && value > a[j]; j--) {
            a[j+1] = a[j];
            idx[j+1] = idx[j];
        }
        a[j+1] = (opus_int16)value;
        idx[j+1] = i;
    }
    for (i = K; i < L; i++) {
        value = a[i];
        if (value > a[K-1]) {
            for (j = K - 2; j >= 0 && value > a[j]; j--) {
                a[j+1] = a[j];
                idx[j+1] = idx[j];
            }
            a[j+1] = (opus_int16)value;
            idx[j+1] = i;
        }
    }
}
static void c_silk_insertion_sort_increasing_all_values_int16(opus_int16 *a, int L) {
    silk_insertion_sort_increasing_all_values_int16(a, L);
}

static short c_silk_LSFCosTab_get(int i) { return silk_LSFCosTab_FIX_Q12[i]; }
static int c_silk_LSFCosTab_len(void)    { return 129; }
*/
import "C"
import "unsafe"

func cSilkLin2Log(x int32) int32 { return int32(C.c_silk_lin2log(C.int(x))) }
func cSilkLog2Lin(x int32) int32 { return int32(C.c_silk_log2lin(C.int(x))) }
func cSilkSigmQ15(x int) int     { return int(C.c_silk_sigm_Q15(C.int(x))) }

func cSilkSumSqrShift(x []int16) (energy int32, shift int) {
	var e C.int
	var s C.int
	var p *C.opus_int16
	if len(x) > 0 {
		p = (*C.opus_int16)(unsafe.Pointer(&x[0]))
	}
	C.c_silk_sum_sqr_shift(&e, &s, p, C.int(len(x)))
	return int32(e), int(s)
}

func cSilkInnerProdAlignedScale(a, b []int16, scale int) int32 {
	if len(a) == 0 {
		return 0
	}
	return int32(C.c_silk_inner_prod_aligned_scale(
		(*C.opus_int16)(unsafe.Pointer(&a[0])),
		(*C.opus_int16)(unsafe.Pointer(&b[0])),
		C.int(scale), C.int(len(a))))
}

func cSilkInnerProd16(a, b []int16) int64 {
	if len(a) == 0 {
		return 0
	}
	return int64(C.c_silk_inner_prod16(
		(*C.opus_int16)(unsafe.Pointer(&a[0])),
		(*C.opus_int16)(unsafe.Pointer(&b[0])),
		C.int(len(a))))
}

// cSilkInsertionSortIncreasing returns sorted copy and idx vector.
func cSilkInsertionSortIncreasing(a []int32, K int) ([]int32, []int) {
	ac := append([]int32(nil), a...)
	idx := make([]C.opus_int, K)
	if len(ac) == 0 {
		return ac, nil
	}
	C.c_silk_insertion_sort_increasing(
		(*C.opus_int32)(unsafe.Pointer(&ac[0])),
		(*C.opus_int)(unsafe.Pointer(&idx[0])),
		C.int(len(ac)), C.int(K))
	out := make([]int, K)
	for i := 0; i < K; i++ {
		out[i] = int(idx[i])
	}
	return ac, out
}
func cSilkInsertionSortDecreasingInt16(a []int16, K int) ([]int16, []int) {
	ac := append([]int16(nil), a...)
	idx := make([]C.opus_int, K)
	if len(ac) == 0 {
		return ac, nil
	}
	C.c_silk_insertion_sort_decreasing_int16(
		(*C.opus_int16)(unsafe.Pointer(&ac[0])),
		(*C.opus_int)(unsafe.Pointer(&idx[0])),
		C.int(len(ac)), C.int(K))
	out := make([]int, K)
	for i := 0; i < K; i++ {
		out[i] = int(idx[i])
	}
	return ac, out
}
func cSilkInsertionSortIncreasingAllValuesInt16(a []int16) []int16 {
	ac := append([]int16(nil), a...)
	if len(ac) == 0 {
		return ac
	}
	C.c_silk_insertion_sort_increasing_all_values_int16(
		(*C.opus_int16)(unsafe.Pointer(&ac[0])), C.int(len(ac)))
	return ac
}

func cSilkLSFCosTab() []int16 {
	n := int(C.c_silk_LSFCosTab_len())
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(C.c_silk_LSFCosTab_get(C.int(i)))
	}
	return out
}
