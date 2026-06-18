// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrencfreqsca pins the Go port of the SBR-encoder frequency
// band-table construction (internal/nativeaac/sbr/enc_freq_sca.go) against the
// vendored Fraunhofer FDK-AAC C (sbrenc_freq_sca.cpp) via cgo. Drives
// FindStartAndStopBand, UpdateFreqScale, UpdateHiRes/UpdateLoRes and the RAW
// start/stop-freq helpers across the legal sample-rate / start-freq / stop-freq
// / freq-scale grid and compares the band tables bit-for-bit. fixed-point =>
// EXACT int equality.
package sbrencfreqsca

/*
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo LDFLAGS: -lm

#include <stdint.h>

extern int freqsca_startstop(int srSbr, int srCore, int noChannels, int startFreq,
                             int stopFreq, int *k0Out, int *k2Out);
extern int freqsca_updatefreqscale(int k0, int k2, int freqScale, int alterScale,
                                   unsigned char *vkOut);
extern int freqsca_hires_lores(const unsigned char *vk, int numMaster, int xoverBand,
                               unsigned char *hiresOut, unsigned char *loresOut,
                               int *numLoresOut, int *xoverOut);
extern int freqsca_startfreq_raw(int startFreq, int fsCore);
extern int freqsca_stopfreq_raw(int stopFreq, int fsCore);
*/
import "C"

import "unsafe"

func cStartStop(srSbr, srCore, noChannels, startFreq, stopFreq int) (k0, k2, err int) {
	var ck0, ck2 C.int
	e := C.freqsca_startstop(C.int(srSbr), C.int(srCore), C.int(noChannels),
		C.int(startFreq), C.int(stopFreq), &ck0, &ck2)
	return int(ck0), int(ck2), int(e)
}

func cUpdateFreqScale(k0, k2, freqScale, alterScale int) (vk []uint8, numBands int) {
	buf := make([]byte, 64)
	n := C.freqsca_updatefreqscale(C.int(k0), C.int(k2), C.int(freqScale),
		C.int(alterScale), (*C.uchar)(unsafe.Pointer(&buf[0])))
	if int(n) < 0 {
		return nil, -1
	}
	out := make([]uint8, int(n)+1)
	copy(out, buf[:int(n)+1])
	return out, int(n)
}

func cHiResLoRes(vk []uint8, numMaster, xoverBand int) (hires, lores []uint8, numHires, numLores, xover int) {
	hbuf := make([]byte, 64)
	lbuf := make([]byte, 64)
	var nLores, xOut C.int
	nHires := C.freqsca_hires_lores((*C.uchar)(unsafe.Pointer(&vk[0])), C.int(numMaster),
		C.int(xoverBand), (*C.uchar)(unsafe.Pointer(&hbuf[0])), (*C.uchar)(unsafe.Pointer(&lbuf[0])),
		&nLores, &xOut)
	hires = make([]uint8, int(nHires)+1)
	copy(hires, hbuf[:int(nHires)+1])
	lores = make([]uint8, int(nLores)+1)
	copy(lores, lbuf[:int(nLores)+1])
	return hires, lores, int(nHires), int(nLores), int(xOut)
}

func cStartFreqRAW(startFreq, fsCore int) int {
	return int(C.freqsca_startfreq_raw(C.int(startFreq), C.int(fsCore)))
}

func cStopFreqRAW(stopFreq, fsCore int) int {
	return int(C.freqsca_stopfreq_raw(C.int(stopFreq), C.int(fsCore)))
}
