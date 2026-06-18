//go:build cgo

package benchcmp

/*
#include "config.h"
#include <string.h>
#include "entcode.h"
#include "entenc.h"
#include "entdec.h"
#include "main.h"

static int c_shell_roundtrip(const int *pulses, unsigned char *buf, int bufSize,
                             short *out, int sum) {
    ec_enc enc;
    ec_enc_init(&enc, buf, bufSize);
    silk_shell_encoder(&enc, pulses);
    ec_enc_done(&enc);
    ec_dec dec;
    ec_dec_init(&dec, buf, bufSize);
    silk_shell_decoder(out, &dec, sum);
    return 0;
}

static void c_silk_decode_pulses(const unsigned char *buf, int bufSize,
                            short *out, int signalType, int quantOffsetType,
                            int frame_length) {
    ec_dec dec;
    ec_dec_init(&dec, (unsigned char *)buf, bufSize);
    silk_decode_pulses(&dec, out, signalType, quantOffsetType, frame_length);
}

static int c_encode_signs(const signed char *pulses, int length, int signalType,
                          int quantOffsetType, const int *sum_pulses,
                          unsigned char *buf, int bufSize) {
    ec_enc enc;
    ec_enc_init(&enc, buf, bufSize);
    silk_encode_signs(&enc, pulses, length, signalType, quantOffsetType, sum_pulses);
    ec_enc_done(&enc);
    return 0;
}

static void c_decode_signs(const unsigned char *buf, int bufSize,
                           short *pulses, int length, int signalType,
                           int quantOffsetType, const int *sum_pulses) {
    ec_dec dec;
    ec_dec_init(&dec, (unsigned char *)buf, bufSize);
    silk_decode_signs(&dec, pulses, length, signalType, quantOffsetType, sum_pulses);
}
*/
import "C"
import "unsafe"

func cSilkShellRoundtrip(pulses []int, bufSize int) ([]byte, []int16) {
	cp := make([]C.int, len(pulses))
	sum := 0
	for i, p := range pulses {
		cp[i] = C.int(p)
		sum += p
	}
	buf := make([]byte, bufSize)
	out := make([]int16, 16)
	C.c_shell_roundtrip(
		(*C.int)(unsafe.Pointer(&cp[0])),
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufSize),
		(*C.short)(unsafe.Pointer(&out[0])), C.int(sum))
	return buf, out
}

func cSilkDecodePulses(pkt []byte, signalType, quantOffsetType, frame_length int) []int16 {
	out := make([]int16, frame_length)
	C.c_silk_decode_pulses(
		(*C.uchar)(unsafe.Pointer(&pkt[0])), C.int(len(pkt)),
		(*C.short)(unsafe.Pointer(&out[0])),
		C.int(signalType), C.int(quantOffsetType), C.int(frame_length))
	return out
}

func cSilkEncodeSigns(pulses []int8, length, signalType, quantOffsetType int, sum_pulses []int, bufSize int) []byte {
	buf := make([]byte, bufSize)
	var pPtr *C.schar
	if len(pulses) > 0 {
		pPtr = (*C.schar)(unsafe.Pointer(&pulses[0]))
	}
	sp := make([]C.int, len(sum_pulses))
	for i, v := range sum_pulses {
		sp[i] = C.int(v)
	}
	var spPtr *C.int
	if len(sp) > 0 {
		spPtr = (*C.int)(unsafe.Pointer(&sp[0]))
	}
	C.c_encode_signs(pPtr, C.int(length), C.int(signalType),
		C.int(quantOffsetType), spPtr,
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufSize))
	return buf
}

func cSilkDecodeSigns(pkt []byte, pulses []int16, length, signalType, quantOffsetType int, sum_pulses []int) []int16 {
	p16 := append([]int16(nil), pulses...)
	sp := make([]C.int, len(sum_pulses))
	for i, v := range sum_pulses {
		sp[i] = C.int(v)
	}
	var spPtr *C.int
	if len(sp) > 0 {
		spPtr = (*C.int)(unsafe.Pointer(&sp[0]))
	}
	C.c_decode_signs(
		(*C.uchar)(unsafe.Pointer(&pkt[0])), C.int(len(pkt)),
		(*C.short)(unsafe.Pointer(&p16[0])), C.int(length),
		C.int(signalType), C.int(quantOffsetType), spPtr)
	return p16
}
