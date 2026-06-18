//go:build cgo

package benchcmp

/*
#include "config.h"
#include "opus_types.h"
#include "opus_defines.h"
#include "opus_private.h"
#include <stdlib.h>
#include <string.h>

static opus_int32 c_opus_packet_extensions_generate(
    unsigned char *data, opus_int32 len,
    const opus_extension_data *exts, opus_int32 nb_extensions,
    int nb_frames, int pad) {
    return opus_packet_extensions_generate(data, len, exts, nb_extensions,
        nb_frames, pad);
}

static opus_int32 c_opus_packet_extensions_count(
    const unsigned char *data, opus_int32 len, int nb_frames) {
    return opus_packet_extensions_count(data, len, nb_frames);
}

static opus_int32 c_opus_packet_extensions_count_ext(
    const unsigned char *data, opus_int32 len,
    opus_int32 *nb_frame_exts, int nb_frames) {
    return opus_packet_extensions_count_ext(data, len, nb_frame_exts, nb_frames);
}

static opus_int32 c_opus_packet_extensions_parse(
    const unsigned char *data, opus_int32 len,
    opus_extension_data *exts, opus_int32 *nb_extensions,
    int nb_frames) {
    return opus_packet_extensions_parse(data, len, exts, nb_extensions,
        nb_frames);
}

static opus_int32 c_opus_packet_extensions_parse_ext(
    const unsigned char *data, opus_int32 len,
    opus_extension_data *exts, opus_int32 *nb_extensions,
    const opus_int32 *nb_frame_exts, int nb_frames) {
    return opus_packet_extensions_parse_ext(data, len, exts, nb_extensions,
        nb_frame_exts, nb_frames);
}

// Build an opus_extension_data array from separate scalar arrays, so the
// Go caller doesn't need to allocate a cgo struct array.
static void c_fill_ext(opus_extension_data *arr, int i, int id, int frame,
    const unsigned char *data, opus_int32 len) {
    arr[i].id = id;
    arr[i].frame = frame;
    arr[i].data = data;
    arr[i].len = len;
}

static int c_ext_id(const opus_extension_data *arr, int i) { return arr[i].id; }
static int c_ext_frame(const opus_extension_data *arr, int i) { return arr[i].frame; }
static opus_int32 c_ext_len(const opus_extension_data *arr, int i) { return arr[i].len; }
static const unsigned char *c_ext_data(const opus_extension_data *arr, int i) {
    return arr[i].data;
}
static size_t c_ext_sizeof(void) { return sizeof(opus_extension_data); }
*/
import "C"
import "unsafe"

// cExtensionInput is the Go-friendly description of an opus_extension_data
// value used to drive the C oracle.
type cExtensionInput struct {
	ID    int
	Frame int
	Data  []byte
	Len   int32
}

// cPacketExtensionsGenerate invokes the C oracle.
// `data` may be nil (pass a zero-length buffer) for length probes.
func cPacketExtensionsGenerate(data []byte, maxlen int32, exts []cExtensionInput,
	nbFrames int, pad int) int32 {
	n := len(exts)
	arr := C.malloc(C.size_t(n+1) * C.c_ext_sizeof())
	defer C.free(arr)

	// Pin the per-extension payload buffers in C-visible memory.
	pins := make([]unsafe.Pointer, n)
	defer func() {
		for _, p := range pins {
			if p != nil {
				C.free(p)
			}
		}
	}()
	for i, e := range exts {
		var pdata *C.uchar
		if e.Len > 0 && len(e.Data) > 0 {
			buf := C.malloc(C.size_t(e.Len))
			C.memcpy(buf, unsafe.Pointer(&e.Data[0]), C.size_t(e.Len))
			pins[i] = buf
			pdata = (*C.uchar)(buf)
		}
		C.c_fill_ext((*C.opus_extension_data)(arr), C.int(i),
			C.int(e.ID), C.int(e.Frame), pdata, C.opus_int32(e.Len))
	}

	var dp *C.uchar
	if len(data) > 0 {
		dp = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	return int32(C.c_opus_packet_extensions_generate(dp, C.opus_int32(maxlen),
		(*C.opus_extension_data)(arr), C.opus_int32(n), C.int(nbFrames),
		C.int(pad)))
}

// cPacketExtensionsCount invokes the C oracle.
func cPacketExtensionsCount(data []byte, len_ int32, nbFrames int) int32 {
	var dp *C.uchar
	if len(data) > 0 {
		dp = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	return int32(C.c_opus_packet_extensions_count(dp, C.opus_int32(len_),
		C.int(nbFrames)))
}

// cPacketExtensionsCountExt invokes the C oracle.
func cPacketExtensionsCountExt(data []byte, len_ int32, nbFrameExts []int32,
	nbFrames int) int32 {
	var dp *C.uchar
	if len(data) > 0 {
		dp = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	tmp := make([]C.opus_int32, len(nbFrameExts))
	var tp *C.opus_int32
	if len(tmp) > 0 {
		tp = (*C.opus_int32)(unsafe.Pointer(&tmp[0]))
	}
	ret := int32(C.c_opus_packet_extensions_count_ext(dp, C.opus_int32(len_),
		tp, C.int(nbFrames)))
	for i := range nbFrameExts {
		nbFrameExts[i] = int32(tmp[i])
	}
	return ret
}

// cPacketExtensionsParse invokes the C oracle.
// nbExtensions is in/out (cap -> count).
func cPacketExtensionsParse(data []byte, len_ int32, cap_ int,
	nbExtensions *int32, nbFrames int) (int32, []cExtensionInput) {
	var dp *C.uchar
	if len(data) > 0 {
		dp = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	arr := C.malloc(C.size_t(cap_+1) * C.c_ext_sizeof())
	defer C.free(arr)
	var cn C.opus_int32 = C.opus_int32(*nbExtensions)
	ret := int32(C.c_opus_packet_extensions_parse(dp, C.opus_int32(len_),
		(*C.opus_extension_data)(arr), &cn, C.int(nbFrames)))
	*nbExtensions = int32(cn)
	if ret < 0 {
		return ret, nil
	}
	out := make([]cExtensionInput, int(cn))
	for i := range out {
		id := int(C.c_ext_id((*C.opus_extension_data)(arr), C.int(i)))
		fr := int(C.c_ext_frame((*C.opus_extension_data)(arr), C.int(i)))
		ln := int32(C.c_ext_len((*C.opus_extension_data)(arr), C.int(i)))
		dp2 := C.c_ext_data((*C.opus_extension_data)(arr), C.int(i))
		out[i] = cExtensionInput{ID: id, Frame: fr, Len: ln}
		if ln > 0 && dp2 != nil {
			out[i].Data = C.GoBytes(unsafe.Pointer(dp2), C.int(ln))
		}
	}
	return ret, out
}

// cPacketExtensionsParseExt invokes the C oracle.
func cPacketExtensionsParseExt(data []byte, len_ int32, cap_ int,
	nbExtensions *int32, nbFrameExts []int32, nbFrames int) (int32, []cExtensionInput) {
	var dp *C.uchar
	if len(data) > 0 {
		dp = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	arr := C.malloc(C.size_t(cap_+1) * C.c_ext_sizeof())
	defer C.free(arr)
	nfe := make([]C.opus_int32, len(nbFrameExts))
	for i, v := range nbFrameExts {
		nfe[i] = C.opus_int32(v)
	}
	var np *C.opus_int32
	if len(nfe) > 0 {
		np = (*C.opus_int32)(unsafe.Pointer(&nfe[0]))
	}
	var cn C.opus_int32 = C.opus_int32(*nbExtensions)
	ret := int32(C.c_opus_packet_extensions_parse_ext(dp, C.opus_int32(len_),
		(*C.opus_extension_data)(arr), &cn, np, C.int(nbFrames)))
	*nbExtensions = int32(cn)
	if ret < 0 {
		return ret, nil
	}
	out := make([]cExtensionInput, int(cn))
	for i := range out {
		id := int(C.c_ext_id((*C.opus_extension_data)(arr), C.int(i)))
		fr := int(C.c_ext_frame((*C.opus_extension_data)(arr), C.int(i)))
		ln := int32(C.c_ext_len((*C.opus_extension_data)(arr), C.int(i)))
		dp2 := C.c_ext_data((*C.opus_extension_data)(arr), C.int(i))
		out[i] = cExtensionInput{ID: id, Frame: fr, Len: ln}
		if ln > 0 && dp2 != nil {
			out[i].Data = C.GoBytes(unsafe.Pointer(dp2), C.int(ln))
		}
	}
	return ret, out
}
