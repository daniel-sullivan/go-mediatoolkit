//go:build cgo

package ogg

/*
#include <ogg/ogg.h>
#include <string.h>
*/
import "C"
import (
	"runtime"
	"unsafe"
)

type cgoDecoder struct {
	state    C.ogg_stream_state
	serialNo int32
}

// NewCgoDecoder creates a Decoder that uses the C libogg implementation.
// Only available when built with Cgo enabled.
func NewCgoDecoder(serialNo int32) (Decoder, error) {
	d := &cgoDecoder{serialNo: serialNo}
	if C.ogg_stream_init(&d.state, C.int(serialNo)) != 0 {
		return nil, ErrInternalError
	}
	return d, nil
}

func (d *cgoDecoder) PageIn(page *Page) error {
	var cp C.ogg_page
	var pinner runtime.Pinner
	if len(page.Header) > 0 {
		pinner.Pin(&page.Header[0])
		cp.header = (*C.uchar)(unsafe.Pointer(&page.Header[0]))
	}
	cp.header_len = C.long(len(page.Header))
	if len(page.Body) > 0 {
		pinner.Pin(&page.Body[0])
		cp.body = (*C.uchar)(unsafe.Pointer(&page.Body[0]))
	}
	cp.body_len = C.long(len(page.Body))

	ret := C.ogg_stream_pagein(&d.state, &cp)
	pinner.Unpin()
	if ret != 0 {
		return ErrStreamMismatch
	}
	return nil
}

func (d *cgoDecoder) PacketOut() (Packet, int, error) {
	var cp C.ogg_packet
	ret := int(C.ogg_stream_packetout(&d.state, &cp))
	if ret <= 0 {
		return Packet{}, ret, nil
	}

	data := make([]byte, int(cp.bytes))
	if cp.bytes > 0 {
		C.memcpy(unsafe.Pointer(&data[0]), unsafe.Pointer(cp.packet), C.size_t(cp.bytes))
	}

	return Packet{
		Data:       data,
		BOS:        cp.b_o_s != 0,
		EOS:        cp.e_o_s != 0,
		GranulePos: int64(cp.granulepos),
		PacketNo:   int64(cp.packetno),
	}, 1, nil
}

func (d *cgoDecoder) PacketPeek() (Packet, int, error) {
	var cp C.ogg_packet
	ret := int(C.ogg_stream_packetpeek(&d.state, &cp))
	if ret <= 0 {
		return Packet{}, ret, nil
	}

	data := make([]byte, int(cp.bytes))
	if cp.bytes > 0 {
		C.memcpy(unsafe.Pointer(&data[0]), unsafe.Pointer(cp.packet), C.size_t(cp.bytes))
	}

	return Packet{
		Data:       data,
		BOS:        cp.b_o_s != 0,
		EOS:        cp.e_o_s != 0,
		GranulePos: int64(cp.granulepos),
		PacketNo:   int64(cp.packetno),
	}, 1, nil
}

func (d *cgoDecoder) SerialNo() int32 { return d.serialNo }

func (d *cgoDecoder) EOS() bool {
	return C.ogg_stream_eos(&d.state) != 0
}

func (d *cgoDecoder) Reset() {
	C.ogg_stream_reset(&d.state)
}
