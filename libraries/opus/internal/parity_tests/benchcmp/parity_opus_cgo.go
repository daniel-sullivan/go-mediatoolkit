//go:build cgo

package benchcmp

/*
#include "config.h"
#include "opus.h"
#include "opus_private.h"
#include <string.h>
#include <stdlib.h>

static int c_opus_packet_get_bandwidth(const unsigned char *data) {
    return opus_packet_get_bandwidth(data);
}
static int c_opus_packet_get_samples_per_frame(const unsigned char *data, opus_int32 Fs) {
    return opus_packet_get_samples_per_frame(data, Fs);
}
static int c_opus_packet_get_nb_channels(const unsigned char *data) {
    return opus_packet_get_nb_channels(data);
}
static int c_opus_packet_get_nb_frames(const unsigned char *data, opus_int32 len) {
    return opus_packet_get_nb_frames(data, len);
}
static int c_opus_packet_get_nb_samples(const unsigned char *data, opus_int32 len, opus_int32 Fs) {
    return opus_packet_get_nb_samples(data, len, Fs);
}
static int c_encode_size(int size, unsigned char *data) {
    return encode_size(size, data);
}

// Parse a packet and return the flattened result through the provided
// out buffers. The Go side allocates fixed 48-slot arrays for frames/
// sizes and a single int for payload_offset.
static int c_opus_packet_parse(const unsigned char *data, opus_int32 len,
    unsigned char *out_toc,
    int *frame_offsets, short *sizes,
    int *payload_offset) {
    const unsigned char *frames[48];
    opus_int16 size[48];
    int po = 0;
    int ret = opus_packet_parse(data, len, out_toc, frames, size, &po);
    if (ret > 0) {
        for (int i = 0; i < ret; i++) {
            frame_offsets[i] = (int)(frames[i] - data);
            sizes[i] = size[i];
        }
    }
    if (payload_offset) *payload_offset = po;
    return ret;
}

// Cat/out helpers. The Go test hands in a single flat byte buffer
// carrying concatenated packets, with per-packet offsets + lengths.
static int c_repacketizer_cat_out(const unsigned char *packets_flat,
    const int *offsets, const int *lens, int n_packets,
    unsigned char *out, int maxlen) {
    OpusRepacketizer rp;
    opus_repacketizer_init(&rp);
    for (int i = 0; i < n_packets; i++) {
        int r = opus_repacketizer_cat(&rp, packets_flat + offsets[i], lens[i]);
        if (r != OPUS_OK) return r;
    }
    return opus_repacketizer_out(&rp, out, maxlen);
}

static int c_opus_packet_pad(unsigned char *data, opus_int32 len, opus_int32 new_len) {
    return opus_packet_pad(data, len, new_len);
}

static opus_int32 c_opus_packet_unpad(unsigned char *data, opus_int32 len) {
    return opus_packet_unpad(data, len);
}
*/
import "C"
import "unsafe"

func cOpusPacketGetBandwidth(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	return int(C.c_opus_packet_get_bandwidth((*C.uchar)(unsafe.Pointer(&data[0]))))
}

func cOpusPacketGetSamplesPerFrame(data []byte, Fs int32) int {
	return int(C.c_opus_packet_get_samples_per_frame(
		(*C.uchar)(unsafe.Pointer(&data[0])), C.opus_int32(Fs)))
}

func cOpusPacketGetNbChannels(data []byte) int {
	return int(C.c_opus_packet_get_nb_channels((*C.uchar)(unsafe.Pointer(&data[0]))))
}

func cOpusPacketGetNbFrames(data []byte, length int32) int {
	var p *C.uchar
	if len(data) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	return int(C.c_opus_packet_get_nb_frames(p, C.opus_int32(length)))
}

func cOpusPacketGetNbSamples(data []byte, length, Fs int32) int {
	var p *C.uchar
	if len(data) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	return int(C.c_opus_packet_get_nb_samples(p, C.opus_int32(length), C.opus_int32(Fs)))
}

func cEncodeSize(size int, data []byte) int {
	return int(C.c_encode_size(C.int(size), (*C.uchar)(unsafe.Pointer(&data[0]))))
}

// COpusPacketParseResult mirrors the Go ExportTestOpusPacketParse result.
type COpusPacketParseResult struct {
	Ret           int
	Toc           byte
	FrameOffsets  []int
	Sizes         []int16
	PayloadOffset int
}

func cOpusPacketParse(data []byte) COpusPacketParseResult {
	var toc C.uchar
	var frameOffsets [48]C.int
	var sizes [48]C.short
	var payloadOffset C.int
	var p *C.uchar
	if len(data) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	ret := int(C.c_opus_packet_parse(p, C.opus_int32(len(data)),
		&toc,
		&frameOffsets[0],
		&sizes[0],
		&payloadOffset))
	res := COpusPacketParseResult{
		Ret:           ret,
		Toc:           byte(toc),
		PayloadOffset: int(payloadOffset),
	}
	if ret > 0 {
		res.FrameOffsets = make([]int, ret)
		res.Sizes = make([]int16, ret)
		for i := 0; i < ret; i++ {
			res.FrameOffsets[i] = int(frameOffsets[i])
			res.Sizes[i] = int16(sizes[i])
		}
	}
	return res
}

// cRepacketizerCatOut runs init → cat (each packet) → out on the C
// side and returns the output buffer + return code.
func cRepacketizerCatOut(packets [][]byte, maxlen int) ([]byte, int32) {
	// Flatten.
	total := 0
	for _, p := range packets {
		total += len(p)
	}
	flat := make([]byte, total)
	offsets := make([]C.int, len(packets))
	lens := make([]C.int, len(packets))
	pos := 0
	for i, p := range packets {
		offsets[i] = C.int(pos)
		lens[i] = C.int(len(p))
		copy(flat[pos:], p)
		pos += len(p)
	}
	out := make([]byte, maxlen)
	var flatPtr *C.uchar
	if total > 0 {
		flatPtr = (*C.uchar)(unsafe.Pointer(&flat[0]))
	}
	var offPtr, lenPtr *C.int
	if len(packets) > 0 {
		offPtr = &offsets[0]
		lenPtr = &lens[0]
	}
	n := int(C.c_repacketizer_cat_out(flatPtr, offPtr, lenPtr,
		C.int(len(packets)),
		(*C.uchar)(unsafe.Pointer(&out[0])),
		C.int(maxlen)))
	if n < 0 {
		return nil, int32(n)
	}
	return out[:n], int32(n)
}

func cOpusPacketPad(data []byte, newLen int) ([]byte, int) {
	buf := make([]byte, newLen)
	copy(buf, data)
	ret := int(C.c_opus_packet_pad(
		(*C.uchar)(unsafe.Pointer(&buf[0])),
		C.opus_int32(len(data)),
		C.opus_int32(newLen)))
	return buf, ret
}

func cOpusPacketUnpad(data []byte) ([]byte, int32) {
	buf := make([]byte, len(data))
	copy(buf, data)
	ret := int32(C.c_opus_packet_unpad(
		(*C.uchar)(unsafe.Pointer(&buf[0])),
		C.opus_int32(len(data))))
	if ret < 0 {
		return nil, ret
	}
	return buf[:ret], ret
}
