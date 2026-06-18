// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencanalysis

/*
#include <stdint.h>
extern void fparity_run_frame_gen(int allowSpread, int numEnvStatic, int staticFraming,
                                  int timeSlots, const int *freqResFixfix,
                                  unsigned char fResTransIsLow, int ldGrid,
                                  const int *vTuning, int nFrames,
                                  const unsigned char *tranInfos,
                                  const unsigned char *tranInfosPre,
                                  const int *rightBorderFIX, int *out);
*/
import "C"

import "unsafe"

const fiStride = 26

// cFrameInfoGen runs the genuine frame generator over nFrames frames and returns
// the packed per-frame SBR_FRAME_INFO+grid snapshots (each fiStride ints).
func cFrameInfoGen(allowSpread, numEnvStatic, staticFraming, timeSlots int, freqResFixfix []int, fResTransIsLow uint8, ldGrid int, vTuning []int, tranInfos, tranInfosPre []uint8, rightBorderFIX []int, nFrames int) []int32 {
	frf := []C.int{C.int(freqResFixfix[0]), C.int(freqResFixfix[1])}
	vt := make([]C.int, len(vTuning))
	for i := range vTuning {
		vt[i] = C.int(vTuning[i])
	}
	ti := make([]C.uchar, len(tranInfos))
	for i := range tranInfos {
		ti[i] = C.uchar(tranInfos[i])
	}
	tip := make([]C.uchar, len(tranInfosPre))
	for i := range tranInfosPre {
		tip[i] = C.uchar(tranInfosPre[i])
	}
	rbf := make([]C.int, len(rightBorderFIX))
	for i := range rightBorderFIX {
		rbf[i] = C.int(rightBorderFIX[i])
	}
	out := make([]C.int, nFrames*fiStride)
	C.fparity_run_frame_gen(C.int(allowSpread), C.int(numEnvStatic), C.int(staticFraming),
		C.int(timeSlots), &frf[0], C.uchar(fResTransIsLow), C.int(ldGrid),
		&vt[0], C.int(nFrames), &ti[0], &tip[0], &rbf[0],
		(*C.int)(unsafe.Pointer(&out[0])))
	res := make([]int32, len(out))
	for i := range out {
		res[i] = int32(out[i])
	}
	return res
}
