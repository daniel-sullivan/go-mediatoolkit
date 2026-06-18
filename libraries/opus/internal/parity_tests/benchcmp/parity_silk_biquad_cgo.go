//go:build cgo

package benchcmp

/*
#include "config.h"
#include "SigProc_FIX.h"
#include "structs.h"

static void c_biquad1(const opus_int16 *in, const opus_int32 *B, const opus_int32 *A,
                      opus_int32 *S, opus_int16 *out, int len) {
    silk_biquad_alt_stride1(in, B, A, S, out, len);
}
static void c_biquad2(const opus_int16 *in, const opus_int32 *B, const opus_int32 *A,
                      opus_int32 *S, opus_int16 *out, int len) {
    silk_biquad_alt_stride2_c(in, B, A, S, out, len);
}
static void c_anafb1(const opus_int16 *in, opus_int32 *S,
                     opus_int16 *outL, opus_int16 *outH, int N) {
    silk_ana_filt_bank_1(in, S, outL, outH, N);
}

// silk_LP_variable_cutoff lives in main.h (encoder-only header chain);
// take the one in the amalgamated build by linking to it directly.
extern void silk_LP_variable_cutoff(silk_LP_state *psLP, opus_int16 *frame, int frame_length);

static void c_lp_cutoff(int mode, int trans_fn, opus_int32 s0, opus_int32 s1,
                        opus_int16 *frame, int frame_length,
                        int *out_trans_fn, opus_int32 *out_s0, opus_int32 *out_s1) {
    silk_LP_state st;
    st.mode = mode;
    st.transition_frame_no = trans_fn;
    st.In_LP_State[0] = s0;
    st.In_LP_State[1] = s1;
    st.saved_fs_kHz = 0;
    silk_LP_variable_cutoff(&st, frame, frame_length);
    *out_trans_fn = st.transition_frame_no;
    *out_s0 = st.In_LP_State[0];
    *out_s1 = st.In_LP_State[1];
}
*/
import "C"
import "unsafe"

func cSilkBiquadAltStride1(in_ []int16, B, A, S []int32) (out []int16, Sout []int32) {
	out = make([]int16, len(in_))
	sCopy := append([]int32(nil), S...)
	C.c_biquad1(
		(*C.opus_int16)(unsafe.Pointer(&in_[0])),
		(*C.opus_int32)(unsafe.Pointer(&B[0])),
		(*C.opus_int32)(unsafe.Pointer(&A[0])),
		(*C.opus_int32)(unsafe.Pointer(&sCopy[0])),
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		C.int(len(in_)))
	return out, sCopy
}
func cSilkBiquadAltStride2(in_ []int16, B, A, S []int32) (out []int16, Sout []int32) {
	out = make([]int16, len(in_))
	sCopy := append([]int32(nil), S...)
	C.c_biquad2(
		(*C.opus_int16)(unsafe.Pointer(&in_[0])),
		(*C.opus_int32)(unsafe.Pointer(&B[0])),
		(*C.opus_int32)(unsafe.Pointer(&A[0])),
		(*C.opus_int32)(unsafe.Pointer(&sCopy[0])),
		(*C.opus_int16)(unsafe.Pointer(&out[0])),
		C.int(len(in_)/2))
	return out, sCopy
}
func cSilkAnaFiltBank1(in_ []int16, S []int32) ([]int16, []int16, []int32) {
	n := len(in_) / 2
	outL := make([]int16, n)
	outH := make([]int16, n)
	sCopy := append([]int32(nil), S...)
	C.c_anafb1(
		(*C.opus_int16)(unsafe.Pointer(&in_[0])),
		(*C.opus_int32)(unsafe.Pointer(&sCopy[0])),
		(*C.opus_int16)(unsafe.Pointer(&outL[0])),
		(*C.opus_int16)(unsafe.Pointer(&outH[0])),
		C.int(len(in_)))
	return outL, outH, sCopy
}

func cSilkLPVariableCutoff(frame []int16, mode int, transFrameNo int32, InLPState []int32) (
	[]int16, []int32, int32) {
	out := append([]int16(nil), frame...)
	var outTrans C.int
	var outS0, outS1 C.opus_int32
	C.c_lp_cutoff(C.int(mode), C.int(transFrameNo),
		C.opus_int32(InLPState[0]), C.opus_int32(InLPState[1]),
		(*C.opus_int16)(unsafe.Pointer(&out[0])), C.int(len(out)),
		&outTrans, &outS0, &outS1)
	return out, []int32{int32(outS0), int32(outS1)}, int32(outTrans)
}
