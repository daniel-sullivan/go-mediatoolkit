//go:build cgo

// Cgo path: exposes the vendored libsamplerate (resample/libsamplerate/)
// as a resample.Converter via NewLibsamplerate. Built whenever the
// toolchain has cgo — no extra build tag needed (matches the
// libraries/opus and libraries/ogg pattern). CFLAGS for the vendored
// build live in resample_cgo.go.
//
// Use case: applications that prefer the C reference implementation
// (e.g. for byte-for-byte parity with audio software written against
// libsamplerate) over the pure-Go port. Most callers should stick with
// the default Go path — it's faster on Go-managed buffers, doesn't
// require cgo, and is parity-tested against this same C library.

package resample

// #include "libsamplerate/include/samplerate.h"
// #include <stdlib.h>
import "C"

import (
	"errors"
	"runtime"
	"unsafe"
)

// cgoConverter implements Converter on top of libsamplerate's SRC_STATE.
// One state per converter; not safe for concurrent Process calls (same
// contract as the pure-Go converters).
type cgoConverter struct {
	state    *C.SRC_STATE
	channels int
	ctype    C.int
	closed   bool
}

// NewLibsamplerate returns a Converter backed by the vendored libsamplerate.
// channels and ct map onto src_new's parameters; see the type-level
// docs for the lifetime rules (single-Process-at-a-time, Close
// releases the SRC_STATE).
func NewLibsamplerate(ct ConverterType, channels int) (Converter, error) {
	if channels < 1 {
		return nil, ErrBadChannelCount
	}
	cct := cgoConverterTypeFor(ct)
	if cct == -1 {
		return nil, ErrBadConverterType
	}
	var errCode C.int
	state := C.src_new(cct, C.int(channels), &errCode)
	if state == nil {
		return nil, errors.New(C.GoString(C.src_strerror(errCode)))
	}
	c := &cgoConverter{state: state, channels: channels, ctype: cct}
	// Best-effort cleanup if the caller forgets Close. Explicit Close
	// is still preferred — finalizers don't run promptly.
	runtime.SetFinalizer(c, (*cgoConverter).Close)
	return c, nil
}

func (c *cgoConverter) Process(d *Data) error {
	if d == nil {
		return ErrBadData
	}
	inN := len(d.DataIn)
	outN := len(d.DataOut)

	// libsamplerate's SRC_DATA wants float32 buffers. Convert in/out
	// per call — Go float64 → C float32 → C float32 → Go float64.
	// The precision drop is libsamplerate's contract, not ours.
	var inF []C.float
	if inN > 0 {
		inF = make([]C.float, inN)
		for i, v := range d.DataIn {
			inF[i] = C.float(v)
		}
	}
	var outF []C.float
	if outN > 0 {
		outF = make([]C.float, outN)
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	var sd C.SRC_DATA
	if inN > 0 {
		pinner.Pin(&inF[0])
		sd.data_in = (*C.float)(unsafe.Pointer(&inF[0]))
	}
	if outN > 0 {
		pinner.Pin(&outF[0])
		sd.data_out = (*C.float)(unsafe.Pointer(&outF[0]))
	}
	sd.input_frames = C.long(inN / c.channels)
	sd.output_frames = C.long(outN / c.channels)
	sd.src_ratio = C.double(d.Ratio.Float64())
	if d.EndOfInput {
		sd.end_of_input = 1
	}

	if rc := C.src_process(c.state, &sd); rc != 0 {
		return errors.New(C.GoString(C.src_strerror(rc)))
	}

	gen := int(sd.output_frames_gen) * c.channels
	for i := 0; i < gen; i++ {
		d.DataOut[i] = float64(outF[i])
	}
	d.InputFramesUsed = int(sd.input_frames_used)
	d.OutputFramesGen = int(sd.output_frames_gen)
	return nil
}

func (c *cgoConverter) Reset() {
	if !c.closed {
		C.src_reset(c.state)
	}
}

func (c *cgoConverter) Clone() Converter {
	if c.closed {
		return nil
	}
	var errCode C.int
	state := C.src_clone(c.state, &errCode)
	if state == nil {
		return nil
	}
	clone := &cgoConverter{state: state, channels: c.channels, ctype: c.ctype}
	runtime.SetFinalizer(clone, (*cgoConverter).Close)
	return clone
}

func (c *cgoConverter) Close() {
	if c.closed {
		return
	}
	C.src_delete(c.state)
	c.state = nil
	c.closed = true
	runtime.SetFinalizer(c, nil)
}

func (c *cgoConverter) Channels() int {
	return c.channels
}

func (c *cgoConverter) SetRatio(ratio Ratio) error {
	if c.closed {
		return ErrBadInternalState
	}
	if rc := C.src_set_ratio(c.state, C.double(ratio.Float64())); rc != 0 {
		return errors.New(C.GoString(C.src_strerror(rc)))
	}
	return nil
}

// cgoConverterTypeFor maps our Go ConverterType to libsamplerate's
// SRC_* constant. -1 signals "unknown" so the caller can return
// ErrBadConverterType. Kept aligned with libsamplerate/include/samplerate.h.
func cgoConverterTypeFor(ct ConverterType) C.int {
	switch ct {
	case SincBestQuality:
		return 0 // SRC_SINC_BEST_QUALITY
	case SincMediumQuality:
		return 1 // SRC_SINC_MEDIUM_QUALITY
	case SincFastest:
		return 2 // SRC_SINC_FASTEST
	case ZeroOrderHold:
		return 3 // SRC_ZERO_ORDER_HOLD
	case Linear:
		return 4 // SRC_LINEAR
	}
	return -1
}
