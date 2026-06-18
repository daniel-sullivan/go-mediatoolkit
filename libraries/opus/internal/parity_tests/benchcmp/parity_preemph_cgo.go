//go:build cgo

package benchcmp

/*
#include "config.h"
#include "celt.h"
#include "modes.h"

static void c_preemph(const float *pcm, float *inp, int N, int CC, int upsample,
    const float *coef, float *mem, int clip) {
    celt_preemphasis(pcm, inp, N, CC, upsample, coef, mem, clip);
}

// transient_analysis is static in celt_encoder.c; we reach it via
// celt_encode_with_ec's side effects in the main test. For isolated
// parity testing we replicate the public behavior using the encoder
// fields. Instead, expose it through a fresh encode of a 1-frame
// buffer and read back intermediate state via encoder dumps.

// No direct invocation of tf_analysis / transient_analysis / tone_detect
// from C is possible since they are file-static. We infer their
// outputs indirectly via the full-encode byte comparison.
*/
import "C"
import "unsafe"

func cCeltPreemphasis(pcm, inp []float32, N, CC, upsample int, coef [4]float32,
	mem *float32, clip int) {
	m := C.float(*mem)
	C.c_preemph(
		(*C.float)(unsafe.Pointer(&pcm[0])),
		(*C.float)(unsafe.Pointer(&inp[0])),
		C.int(N), C.int(CC), C.int(upsample),
		(*C.float)(unsafe.Pointer(&coef[0])),
		&m, C.int(clip))
	*mem = float32(m)
}
