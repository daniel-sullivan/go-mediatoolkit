//go:build cgo

package benchcmp

/*
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../libogg
#cgo CFLAGS: -I${SRCDIR}/../libogg/include
#cgo CFLAGS: -O2

#include <ogg/ogg.h>
#include <string.h>
#include <stdlib.h>
*/
import "C"
import "unsafe"

// CEncoder wraps the C ogg_stream_state for encoding.
type CEncoder struct {
	state C.ogg_stream_state
}

func NewCEncoder(serialNo int) *CEncoder {
	e := &CEncoder{}
	C.ogg_stream_init(&e.state, C.int(serialNo))
	return e
}

func (e *CEncoder) Destroy() {
	C.ogg_stream_clear(&e.state)
}

func (e *CEncoder) PacketIn(data []byte, bos, eos bool, granulepos int64, packetno int64) int {
	var pkt C.ogg_packet
	if len(data) > 0 {
		pkt.packet = (*C.uchar)(C.malloc(C.size_t(len(data))))
		C.memcpy(unsafe.Pointer(pkt.packet), unsafe.Pointer(&data[0]), C.size_t(len(data)))
	}
	pkt.bytes = C.long(len(data))
	if bos {
		pkt.b_o_s = 1
	}
	if eos {
		pkt.e_o_s = 1
	}
	pkt.granulepos = C.ogg_int64_t(granulepos)
	pkt.packetno = C.ogg_int64_t(packetno)

	ret := int(C.ogg_stream_packetin(&e.state, &pkt))
	if pkt.packet != nil {
		C.free(unsafe.Pointer(pkt.packet))
	}
	return ret
}

func (e *CEncoder) Flush() (header, body []byte, ok bool) {
	var page C.ogg_page
	if C.ogg_stream_flush(&e.state, &page) == 0 {
		return nil, nil, false
	}
	h := C.GoBytes(unsafe.Pointer(page.header), C.int(page.header_len))
	b := C.GoBytes(unsafe.Pointer(page.body), C.int(page.body_len))
	return h, b, true
}

func (e *CEncoder) PageOut() (header, body []byte, ok bool) {
	var page C.ogg_page
	if C.ogg_stream_pageout(&e.state, &page) == 0 {
		return nil, nil, false
	}
	h := C.GoBytes(unsafe.Pointer(page.header), C.int(page.header_len))
	b := C.GoBytes(unsafe.Pointer(page.body), C.int(page.body_len))
	return h, b, true
}

// CSync wraps the C ogg_sync_state.
type CSync struct {
	state C.ogg_sync_state
}

func NewCSync() *CSync {
	s := &CSync{}
	C.ogg_sync_init(&s.state)
	return s
}

func (s *CSync) Destroy() {
	C.ogg_sync_clear(&s.state)
}

func (s *CSync) Write(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	buf := C.ogg_sync_buffer(&s.state, C.long(len(data)))
	if buf == nil {
		return -1
	}
	C.memcpy(unsafe.Pointer(buf), unsafe.Pointer(&data[0]), C.size_t(len(data)))
	C.ogg_sync_wrote(&s.state, C.long(len(data)))
	return len(data)
}

func (s *CSync) PageOut() (header, body []byte, ret int) {
	var page C.ogg_page
	r := int(C.ogg_sync_pageout(&s.state, &page))
	if r <= 0 {
		return nil, nil, r
	}
	h := C.GoBytes(unsafe.Pointer(page.header), C.int(page.header_len))
	b := C.GoBytes(unsafe.Pointer(page.body), C.int(page.body_len))
	return h, b, 1
}

// CDecoder wraps the C ogg_stream_state for decoding.
type CDecoder struct {
	state C.ogg_stream_state
}

func NewCDecoder(serialNo int) *CDecoder {
	d := &CDecoder{}
	C.ogg_stream_init(&d.state, C.int(serialNo))
	return d
}

func (d *CDecoder) Destroy() {
	C.ogg_stream_clear(&d.state)
}

func (d *CDecoder) PageIn(header, body []byte) int {
	var page C.ogg_page
	hBuf := C.CBytes(header)
	bBuf := C.CBytes(body)
	page.header = (*C.uchar)(hBuf)
	page.header_len = C.long(len(header))
	page.body = (*C.uchar)(bBuf)
	page.body_len = C.long(len(body))

	ret := int(C.ogg_stream_pagein(&d.state, &page))
	C.free(hBuf)
	C.free(bBuf)
	return ret
}

func (d *CDecoder) PacketOut() (data []byte, granulepos int64, packetno int64, bos, eos bool, ret int) {
	var pkt C.ogg_packet
	r := int(C.ogg_stream_packetout(&d.state, &pkt))
	if r <= 0 {
		return nil, 0, 0, false, false, r
	}
	out := make([]byte, int(pkt.bytes))
	if pkt.bytes > 0 {
		C.memcpy(unsafe.Pointer(&out[0]), unsafe.Pointer(pkt.packet), C.size_t(pkt.bytes))
	}
	return out, int64(pkt.granulepos), int64(pkt.packetno), pkt.b_o_s != 0, pkt.e_o_s != 0, 1
}
