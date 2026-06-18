//go:build cgo

package ogg

/*
#include <ogg/ogg.h>
#include <string.h>
*/
import "C"
import "unsafe"

type cgoSync struct {
	state  C.ogg_sync_state
	closed bool
}

// NewCgoSync creates a Sync that uses the C libogg implementation.
// Only available when built with Cgo enabled.
//
// The returned Sync owns libogg-allocated C memory. Call Close (via the
// CgoSync type assertion) when finished to release it; otherwise the
// internal buffer leaks until process exit.
func NewCgoSync() Sync {
	s := &cgoSync{}
	C.ogg_sync_init(&s.state)
	return s
}

// Close releases the libogg C state backing this Sync. It is idempotent;
// after Close the Sync must not be used again. The C struct itself is a
// Go-allocated field, so only ogg_sync_clear is required (no C.free).
func (s *cgoSync) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	C.ogg_sync_clear(&s.state)
	return nil
}

func (s *cgoSync) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	buf := C.ogg_sync_buffer(&s.state, C.long(len(data)))
	if buf == nil {
		return 0, ErrInternalError
	}
	C.memcpy(unsafe.Pointer(buf), unsafe.Pointer(&data[0]), C.size_t(len(data)))
	if C.ogg_sync_wrote(&s.state, C.long(len(data))) != 0 {
		return 0, ErrInternalError
	}
	return len(data), nil
}

func (s *cgoSync) PageOut() (Page, int, error) {
	var cp C.ogg_page
	ret := int(C.ogg_sync_pageout(&s.state, &cp))
	if ret <= 0 {
		return Page{}, ret, nil
	}

	// Copy page data out of C memory into Go-owned slices.
	hdr := C.GoBytes(unsafe.Pointer(cp.header), C.int(cp.header_len))
	body := C.GoBytes(unsafe.Pointer(cp.body), C.int(cp.body_len))

	return Page{Header: hdr, Body: body}, 1, nil
}

func (s *cgoSync) Reset() {
	C.ogg_sync_reset(&s.state)
}
