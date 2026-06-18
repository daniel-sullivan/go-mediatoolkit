// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package bitstreamformat

// enc_cgo.go is the cgo bridge to the LAME-encoder half of the bitstream-format
// parity oracle (enc_oracle.c / enc_oracle.h). It drives the genuine vendored
// LAME 3.100 format_bitstream / encodeSideInfo2 / writeMainData /
// drain_into_ancillary / compute_flushbits / flush_bitstream / CRC_* /
// getframebits / get_max_frame_buffer_size_by_constraint / do_copy_buffer and
// the four Resv* reservoir-framing functions, so enc_parity_test.go can pin the
// pure-Go nativemp3 port (bitstream_format.go / reservoir_encode.go) against
// them bit-for-bit.
//
// This file is mp3lame-gated (LGPL fence): bitstream.c / reservoir.c / tables.c
// / version.c are LGPL LAME source. A bare `go test` (cgo, no mp3lame) compiles
// only the decoder-half oracle (cgo.go). Per the parity discipline this package
// compiles its OWN copy of the C reference and never imports libraries/mp3.

/*
#cgo CFLAGS: -DHAVE_CONFIG_H
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../../../liblame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/libmp3lame
#cgo CFLAGS: -I${SRCDIR}/../../../liblame/include
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare -Wno-unused-function -Wno-unused-variable
#cgo CFLAGS: -Wno-shift-negative-value -Wno-absolute-value -Wno-tautological-pointer-compare

#include <stdint.h>
#include <stdlib.h>
#include "enc_oracle.h"
*/
import "C"

import "unsafe"

// cgoEnc wraps the C oracle handle over a heap lame_internal_flags.
type cgoEnc struct {
	e       *C.mp3enc_t
	bufSize int
}

func newCgoEnc(bufSize int) *cgoEnc {
	return &cgoEnc{e: C.mp3enc_new(C.int(bufSize)), bufSize: bufSize}
}

func (c *cgoEnc) free() { C.mp3enc_free(c.e) }

// ---- config / state setters ----

func (c *cgoEnc) setCfg(cf encCfg) {
	C.mp3enc_set_cfg(c.e, C.int(cf.version), C.int(cf.samplerateOut),
		C.int(cf.samplerateIndex), C.int(cf.sideinfoLen), C.int(cf.channelsOut),
		C.int(cf.modeGr), C.int(cf.mode), C.int(cf.errorProtection),
		C.int(cf.extension), C.int(cf.copyright), C.int(cf.original),
		C.int(cf.emphasis), C.int(cf.disableReservoir), C.int(cf.avgBitrate),
		C.int(cf.bufferConstraint))
}

func (c *cgoEnc) setOv(bitrateIndex, padding, modeExt int) {
	C.mp3enc_set_ov(c.e, C.int(bitrateIndex), C.int(padding), C.int(modeExt))
}

func (c *cgoEnc) setSv(hPtr, wPtr, ancillaryFlag, resvSize, resvMax int) {
	C.mp3enc_set_sv(c.e, C.int(hPtr), C.int(wPtr), C.int(ancillaryFlag),
		C.int(resvSize), C.int(resvMax))
}

func (c *cgoEnc) setSubstepShaping(v int) { C.mp3enc_set_substep_shaping(c.e, C.int(v)) }

func (c *cgoEnc) primeHeader(slot, writeTiming, ptr int, buf []byte) {
	var p *C.uchar
	if len(buf) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}
	C.mp3enc_prime_header(c.e, C.int(slot), C.int(writeTiming), C.int(ptr), p, C.int(len(buf)))
}

func (c *cgoEnc) disarmHeaders(sentinel int) { C.mp3enc_disarm_headers(c.e, C.int(sentinel)) }

func (c *cgoEnc) setBs(totbit, bufByteIdx, bufBitIdx int) {
	C.mp3enc_set_bs(c.e, C.int(totbit), C.int(bufByteIdx), C.int(bufBitIdx))
}

func (c *cgoEnc) setSide(mainDataBegin, privateBits, resvDrainPre, resvDrainPost int) {
	C.mp3enc_set_side(c.e, C.int(mainDataBegin), C.int(privateBits),
		C.int(resvDrainPre), C.int(resvDrainPost))
}

func (c *cgoEnc) setScfsi(ch, band, v int) { C.mp3enc_set_scfsi(c.e, C.int(ch), C.int(band), C.int(v)) }
func (c *cgoEnc) setSfbL(i, v int)         { C.mp3enc_set_sfb_l(c.e, C.int(i), C.int(v)) }
func (c *cgoEnc) setSfbS(i, v int)         { C.mp3enc_set_sfb_s(c.e, C.int(i), C.int(v)) }

func (c *cgoEnc) setGr(gr, ch int, g encGr) {
	C.mp3enc_set_gr(c.e, C.int(gr), C.int(ch),
		C.int(g.part23Length), C.int(g.part2Length), C.int(g.bigValues),
		C.int(g.count1), C.int(g.globalGain), C.int(g.scalefacCompress),
		C.int(g.blockType), C.int(g.mixedBlockFlag), C.int(g.region0Count),
		C.int(g.region1Count), C.int(g.preflag), C.int(g.scalefacScale),
		C.int(g.count1tableSelect), C.int(g.sfbdivide), C.int(g.sfbmax))
}

func (c *cgoEnc) setGrTableSelect(gr, ch, idx, v int) {
	C.mp3enc_set_gr_table_select(c.e, C.int(gr), C.int(ch), C.int(idx), C.int(v))
}
func (c *cgoEnc) setGrSubblockGain(gr, ch, idx, v int) {
	C.mp3enc_set_gr_subblock_gain(c.e, C.int(gr), C.int(ch), C.int(idx), C.int(v))
}
func (c *cgoEnc) setGrScalefac(gr, ch, sfb, v int) {
	C.mp3enc_set_gr_scalefac(c.e, C.int(gr), C.int(ch), C.int(sfb), C.int(v))
}
func (c *cgoEnc) setGrPartition(gr, ch int, part4, slen4 [4]int) {
	cp := [4]C.int{C.int(part4[0]), C.int(part4[1]), C.int(part4[2]), C.int(part4[3])}
	cs := [4]C.int{C.int(slen4[0]), C.int(slen4[1]), C.int(slen4[2]), C.int(slen4[3])}
	C.mp3enc_set_gr_partition(c.e, C.int(gr), C.int(ch), &cp[0], &cs[0])
}
func (c *cgoEnc) grTableSelect(gr, ch, idx int) int {
	return int(C.mp3enc_gr_table_select(c.e, C.int(gr), C.int(ch), C.int(idx)))
}

// ---- trampolines ----

func (c *cgoEnc) calcFrameLength(kbps, pad int) int {
	return int(C.mp3enc_calc_frame_length(c.e, C.int(kbps), C.int(pad)))
}
func (c *cgoEnc) getFrameBits() int { return int(C.mp3enc_getframebits(c.e)) }
func (c *cgoEnc) getMaxFrameBufferSizeByConstraint(constraint int) int {
	return int(C.mp3enc_get_max_frame_buffer_size_by_constraint(c.e, C.int(constraint)))
}
func (c *cgoEnc) writeHeader(val, j int) { C.mp3enc_writeheader(c.e, C.int(val), C.int(j)) }
func cgoCRCUpdate(value, crc int) int    { return int(C.mp3enc_crc_update(C.int(value), C.int(crc))) }
func (c *cgoEnc) crcWriteHeader(header []byte) {
	C.mp3enc_crc_writeheader(c.e, (*C.uchar)(unsafe.Pointer(&header[0])))
}
func (c *cgoEnc) drainIntoAncillary(remainingBits int) {
	C.mp3enc_drain_into_ancillary(c.e, C.int(remainingBits))
}
func (c *cgoEnc) encodeSideInfo2(bitsPerFrame int) {
	C.mp3enc_encode_side_info2(c.e, C.int(bitsPerFrame))
}
func (c *cgoEnc) writeMainData() int { return int(C.mp3enc_write_main_data(c.e)) }
func (c *cgoEnc) computeFlushbits() (flushbits, totalBytesOutput int) {
	var tbo C.int
	flushbits = int(C.mp3enc_compute_flushbits(c.e, &tbo))
	return flushbits, int(tbo)
}
func (c *cgoEnc) flushBitstream() { C.mp3enc_flush_bitstream(c.e) }
func (c *cgoEnc) addDummyByte(val byte, n uint) {
	C.mp3enc_add_dummy_byte(c.e, C.uchar(val), C.uint(n))
}
func (c *cgoEnc) doCopyBuffer(buf []byte, size int) int {
	var p *C.uchar
	if len(buf) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}
	return int(C.mp3enc_do_copy_buffer(c.e, p, C.int(size)))
}
func (c *cgoEnc) copyBuffer(buf []byte, size, mp3data int) int {
	var p *C.uchar
	if len(buf) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}
	return int(C.mp3enc_copy_buffer(c.e, p, C.int(size), C.int(mp3data)))
}
func (c *cgoEnc) formatBitstream() int { return int(C.mp3enc_format_bitstream(c.e)) }

func (c *cgoEnc) resvFrameBegin() (fullFrameBits, meanBits int) {
	var mb C.int
	fullFrameBits = int(C.mp3enc_resv_frame_begin(c.e, &mb))
	return fullFrameBits, int(mb)
}
func (c *cgoEnc) resvMaxBits(meanBits, cbr int) (targBits, extraBits int) {
	var tb, eb C.int
	C.mp3enc_resv_max_bits(c.e, C.int(meanBits), &tb, &eb, C.int(cbr))
	return int(tb), int(eb)
}
func (c *cgoEnc) resvAdjust(gr, ch int) { C.mp3enc_resv_adjust(c.e, C.int(gr), C.int(ch)) }
func (c *cgoEnc) resvFrameEnd(meanBits int) {
	C.mp3enc_resv_frame_end(c.e, C.int(meanBits))
}

// ---- read-back ----

func (c *cgoEnc) bsTotbit() int     { return int(C.mp3enc_bs_totbit(c.e)) }
func (c *cgoEnc) bsBufByteIdx() int { return int(C.mp3enc_bs_buf_byte_idx(c.e)) }
func (c *cgoEnc) bsBufBitIdx() int  { return int(C.mp3enc_bs_buf_bit_idx(c.e)) }
func (c *cgoEnc) bsBuf(n int) []byte {
	out := make([]byte, n)
	if n > 0 {
		C.mp3enc_copy_bs_buf(c.e, (*C.uchar)(unsafe.Pointer(&out[0])), C.int(n))
	}
	return out
}
func (c *cgoEnc) mainDataBegin() int { return int(C.mp3enc_main_data_begin(c.e)) }
func (c *cgoEnc) resvDrainPre() int  { return int(C.mp3enc_resv_drain_pre(c.e)) }
func (c *cgoEnc) resvDrainPost() int { return int(C.mp3enc_resv_drain_post(c.e)) }
func (c *cgoEnc) hPtr() int          { return int(C.mp3enc_h_ptr(c.e)) }
func (c *cgoEnc) wPtr() int          { return int(C.mp3enc_w_ptr(c.e)) }
func (c *cgoEnc) ancillaryFlag() int { return int(C.mp3enc_ancillary_flag(c.e)) }
func (c *cgoEnc) resvSize() int      { return int(C.mp3enc_resv_size(c.e)) }
func (c *cgoEnc) resvMax() int       { return int(C.mp3enc_resv_max(c.e)) }
func (c *cgoEnc) substepShaping() int {
	return int(C.mp3enc_substep_shaping(c.e))
}
func (c *cgoEnc) headerWriteTiming(slot int) int {
	return int(C.mp3enc_header_write_timing(c.e, C.int(slot)))
}
func (c *cgoEnc) headerPtr(slot int) int { return int(C.mp3enc_header_ptr(c.e, C.int(slot))) }
func (c *cgoEnc) headerBuf(slot, n int) []byte {
	out := make([]byte, n)
	if n > 0 {
		C.mp3enc_copy_header_buf(c.e, C.int(slot), (*C.uchar)(unsafe.Pointer(&out[0])), C.int(n))
	}
	return out
}
