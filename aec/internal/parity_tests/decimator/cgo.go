//go:build cgo && aec_oracle

// Package decimator is the bit-exact parity slice for
// aec/internal/aec3's decimator.go (Decimator: cascaded-biquad
// anti-aliasing + noise-reduction filters, then stride downsampling)
// against the fetched C++ oracle. Env/link setup is shared with the
// other slices via ../run.sh -- see the bandsplit slice's cgo.go for
// the full rationale.
//
// cgo call sites live here rather than parity_test.go: Go's cgo does
// not support `import "C"` inside a _test.go file.
package decimator

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
*/
import "C"

// decimatorC wraps the oracle's webrtc::Decimator.
type decimatorC struct {
	h *C.aec3_decimator
}

func newDecimatorC(downSamplingFactor int) *decimatorC {
	return &decimatorC{h: C.aec3_decimator_create(C.int(downSamplingFactor))}
}

func (d *decimatorC) close() {
	C.aec3_decimator_destroy(d.h)
	d.h = nil
}

// decimate downsamples in (kBlockSize=64 floats) into outLen floats.
func (d *decimatorC) decimate(in []float32, outLen int) []float32 {
	out := make([]float32, outLen)
	C.aec3_decimator_decimate(d.h, (*C.float)(&in[0]), (*C.float)(&out[0]), C.int(outLen))
	return out
}
