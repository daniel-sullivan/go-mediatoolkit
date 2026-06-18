//go:build cgo

package ogg

/*
#include <ogg/ogg.h>
*/
import "C"
import (
	"runtime"
	"unsafe"
)

type cgoEncoder struct {
	state    C.ogg_stream_state
	serialNo int32
	closed   bool
}

// NewCgoEncoder creates an Encoder that uses the C libogg implementation.
// Only available when built with Cgo enabled.
//
// The returned Encoder owns libogg-allocated C memory. Call Close when
// finished to release it; otherwise the stream buffers leak until process
// exit.
func NewCgoEncoder(serialNo int32) (Encoder, error) {
	e := &cgoEncoder{serialNo: serialNo}
	if C.ogg_stream_init(&e.state, C.int(serialNo)) != 0 {
		return nil, ErrInternalError
	}
	return e, nil
}

// Close releases the libogg C state backing this Encoder. It is
// idempotent; after Close the Encoder must not be used again. The C
// struct itself is a Go-allocated field, so only ogg_stream_clear is
// required (no C.free).
func (e *cgoEncoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	C.ogg_stream_clear(&e.state)
	return nil
}

func (e *cgoEncoder) PacketIn(pkt *Packet) error {
	var cp C.ogg_packet
	var pinner runtime.Pinner
	if len(pkt.Data) > 0 {
		pinner.Pin(&pkt.Data[0])
		cp.packet = (*C.uchar)(unsafe.Pointer(&pkt.Data[0]))
	}
	cp.bytes = C.long(len(pkt.Data))
	if pkt.BOS {
		cp.b_o_s = 1
	}
	if pkt.EOS {
		cp.e_o_s = 1
	}
	cp.granulepos = C.ogg_int64_t(pkt.GranulePos)
	cp.packetno = C.ogg_int64_t(pkt.PacketNo)

	ret := C.ogg_stream_packetin(&e.state, &cp)
	pinner.Unpin()
	if ret != 0 {
		return ErrInternalError
	}
	return nil
}

func (e *cgoEncoder) PageOut() (Page, bool) {
	var cp C.ogg_page
	if C.ogg_stream_pageout(&e.state, &cp) == 0 {
		return Page{}, false
	}
	hdr := C.GoBytes(unsafe.Pointer(cp.header), C.int(cp.header_len))
	body := C.GoBytes(unsafe.Pointer(cp.body), C.int(cp.body_len))
	return Page{Header: hdr, Body: body}, true
}

func (e *cgoEncoder) Flush() (Page, bool) {
	var cp C.ogg_page
	if C.ogg_stream_flush(&e.state, &cp) == 0 {
		return Page{}, false
	}
	hdr := C.GoBytes(unsafe.Pointer(cp.header), C.int(cp.header_len))
	body := C.GoBytes(unsafe.Pointer(cp.body), C.int(cp.body_len))
	return Page{Header: hdr, Body: body}, true
}

func (e *cgoEncoder) SerialNo() int32 { return e.serialNo }

func (e *cgoEncoder) EOS() bool {
	return C.ogg_stream_eos(&e.state) != 0
}

func (e *cgoEncoder) Reset() {
	C.ogg_stream_reset(&e.state)
}

func (e *cgoEncoder) ResetSerialNo(serialNo int32) {
	C.ogg_stream_reset_serialno(&e.state, C.int(serialNo))
	e.serialNo = serialNo
}

func (e *cgoEncoder) GranulePos() int64 {
	return int64(e.state.granulepos)
}
