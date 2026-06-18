//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FIX.h"
#include "resampler_structs.h"
#include "resampler_private.h"

static void c_AR2(opus_int32 *S, opus_int32 *outQ8, const opus_int16 *in_,
                  const opus_int16 *A, int len) {
    silk_resampler_private_AR2(S, outQ8, in_, A, len);
}
static void c_down2(opus_int32 *S, opus_int16 *out, const opus_int16 *in_, int inLen) {
    silk_resampler_down2(S, out, in_, inLen);
}
static void c_down2_3(opus_int32 *S, opus_int16 *out, const opus_int16 *in_, int inLen) {
    silk_resampler_down2_3(S, out, in_, inLen);
}
static void c_up2_hq(opus_int32 *S, opus_int16 *out, const opus_int16 *in_, int len) {
    silk_resampler_private_up2_HQ(S, out, in_, len);
}

static int c_resample(int Fs_in, int Fs_out, const opus_int16 *in_, int inLen,
                      opus_int16 *out, int forEnc) {
    silk_resampler_state_struct st;
    int r = silk_resampler_init(&st, Fs_in, Fs_out, forEnc);
    if (r != 0) return r;
    silk_resampler(&st, out, in_, inLen);
    return 0;
}
*/
import "C"
import "unsafe"

func cSilkResamplerAR2(S []int32, in_ []int16, A []int16) ([]int32, []int32) {
	outQ8 := make([]int32, len(in_))
	sCopy := append([]int32(nil), S...)
	C.c_AR2(
		(*C.opus_int32)(unsafe.Pointer(&sCopy[0])),
		(*C.opus_int32)(unsafe.Pointer(&outQ8[0])),
		(*C.opus_int16)(unsafe.Pointer(&in_[0])),
		(*C.opus_int16)(unsafe.Pointer(&A[0])),
		C.int(len(in_)))
	return outQ8, sCopy
}
func cSilkResamplerDown2(S []int32, in_ []int16) ([]int16, []int32) {
	sCopy := append([]int32(nil), S...)
	out := make([]int16, len(in_)/2)
	C.c_down2(
		(*C.opus_int32)(unsafe.Pointer(&sCopy[0])),
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		(*C.opus_int16)(unsafe.Pointer(&in_[0])),
		C.int(len(in_)))
	return out, sCopy
}
func cSilkResamplerDown23(S []int32, in_ []int16) ([]int16, []int32) {
	sCopy := append([]int32(nil), S...)
	out := make([]int16, 2*len(in_)/3)
	C.c_down2_3(
		(*C.opus_int32)(unsafe.Pointer(&sCopy[0])),
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		(*C.opus_int16)(unsafe.Pointer(&in_[0])),
		C.int(len(in_)))
	return out, sCopy
}
func cSilkResamplerUp2HQ(S []int32, in_ []int16) ([]int16, []int32) {
	sCopy := append([]int32(nil), S...)
	out := make([]int16, 2*len(in_))
	C.c_up2_hq(
		(*C.opus_int32)(unsafe.Pointer(&sCopy[0])),
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		(*C.opus_int16)(unsafe.Pointer(&in_[0])),
		C.int(len(in_)))
	return out, sCopy
}
func cSilkResampler(FsIn, FsOut int, in_ []int16, forEnc int) ([]int16, int) {
	outLen := len(in_) * FsOut / FsIn
	out := make([]int16, outLen)
	r := C.c_resample(C.int(FsIn), C.int(FsOut),
		(*C.opus_int16)(unsafe.Pointer(&in_[0])),
		C.int(len(in_)),
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		C.int(forEnc))
	return out, int(r)
}
