//go:build cgo && aec_oracle

// Package smoke is a coarse-grained parity slice proving the fetched
// AEC3 C++ oracle (webrtc-audio-processing v2.1's
// webrtc::EchoCanceller3 — see ../../../oracle/VERSION for
// provenance) links, runs, and produces deterministic,
// substantially-echo-reduced output, as a fast sanity check alongside
// the bit-exact Go-port parity slices (fft, blocking, bandsplit,
// adaptivefir, subtractor, decimator, matchedfilter, delaypipe,
// aecstate, erle, reverb, echoremover, suppressor, and the top-level
// ec3 slice).
//
// Unlike this repo's other cgo parity oracles (libopus, libFLAC,
// libebur128), the AEC3 oracle is fetched into a per-user cache
// directory rather than vendored (~25kLOC of C++ plus an abseil-cpp
// dependency is well past this repo's vendor-small-C convention). Its
// include/library search paths are therefore host-dependent and can't
// be written as static #cgo directives ($ORACLE cache paths aren't
// compile-time constants, and #cgo directives can only expand
// ${SRCDIR}, not arbitrary env vars) — the shared ../run.sh
// resolves them and passes them via CGO_CXXFLAGS/CGO_LDFLAGS, which
// Go's cgo flag-security allowlist requires pairing with
// CGO_CXXFLAGS_ALLOW/CGO_LDFLAGS_ALLOW (mirroring the loudness
// package's CGO_CFLAGS pattern in loudness/mise.toml). The aec_oracle
// build tag gates this file (and shim.h/shim.cc) out of any build
// that hasn't set that up, so a plain `go test ./...` never attempts
// to compile against headers that may not exist on disk — see
// skip_test.go for the fallback behavior in that case.
//
// cgo call sites live in this file rather than parity_test.go: Go's
// cgo does not support `import "C"` inside a _test.go file.
package smoke

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lc++

#include "shim.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// canceller wraps one aec3_canceller lifecycle.
type canceller struct {
	c *C.aec3_canceller
}

// newCanceller creates an EchoCanceller3 instance (the library's
// default EchoCanceller3Config) at sampleRateHz/numChannels.
func newCanceller(sampleRateHz, numChannels int) (*canceller, error) {
	c := C.aec3_create(C.int(sampleRateHz), C.int(numChannels))
	if c == nil {
		return nil, fmt.Errorf("aec3_create failed")
	}
	return &canceller{c: c}, nil
}

// close releases the underlying EchoCanceller3. Safe to call once;
// idempotent no-op after the first call.
func (k *canceller) close() {
	if k.c != nil {
		C.aec3_destroy(k.c)
		k.c = nil
	}
}

// process runs one frame (frameLen samples per channel, interleaved
// float32) through AnalyzeRender(far) + AnalyzeCapture(near)/
// ProcessCapture, writing the echo-cancelled result into out. far,
// near and out must each have length >= numChannels*frameLen.
func (k *canceller) process(far, near, out []float32, frameLen int) error {
	if len(far) == 0 || len(near) == 0 || len(out) == 0 {
		return fmt.Errorf("aec3 process: empty buffer")
	}
	ret := C.aec3_process(
		k.c,
		(*C.float)(unsafe.Pointer(&far[0])),
		(*C.float)(unsafe.Pointer(&near[0])),
		(*C.float)(unsafe.Pointer(&out[0])),
		C.int(frameLen),
	)
	if ret != 0 {
		return fmt.Errorf("aec3_process returned %d", int(ret))
	}
	return nil
}
