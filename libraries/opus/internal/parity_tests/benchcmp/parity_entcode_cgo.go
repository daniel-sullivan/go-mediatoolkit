//go:build cgo

package benchcmp

/*
#include "config.h"
#include "entcode.h"
#include "entdec.h"
#include "entenc.h"
#include <stdlib.h>
#include <string.h>

static ec_ctx* c_ec_enc_new(unsigned char *buf, unsigned long size) {
    ec_ctx *e = (ec_ctx*)malloc(sizeof(ec_ctx));
    ec_enc_init(e, buf, (opus_uint32)size);
    return e;
}
static ec_ctx* c_ec_dec_new(unsigned char *buf, unsigned long size) {
    ec_ctx *e = (ec_ctx*)malloc(sizeof(ec_ctx));
    ec_dec_init(e, buf, (opus_uint32)size);
    return e;
}
static void c_ec_free(ec_ctx *e) { free(e); }

static void c_ec_encode(ec_ctx *e, unsigned fl, unsigned fh, unsigned ft) {
    ec_encode(e, fl, fh, ft);
}
static void c_ec_encode_bin(ec_ctx *e, unsigned fl, unsigned fh, unsigned bits) {
    ec_encode_bin(e, fl, fh, bits);
}
static void c_ec_enc_bit_logp(ec_ctx *e, int val, unsigned logp) {
    ec_enc_bit_logp(e, val, logp);
}
static void c_ec_enc_icdf(ec_ctx *e, int s, const unsigned char *icdf, unsigned ftb) {
    ec_enc_icdf(e, s, icdf, ftb);
}
static void c_ec_enc_icdf16(ec_ctx *e, int s, const opus_uint16 *icdf, unsigned ftb) {
    ec_enc_icdf16(e, s, icdf, ftb);
}
static void c_ec_enc_uint(ec_ctx *e, opus_uint32 fl, opus_uint32 ft) {
    ec_enc_uint(e, fl, ft);
}
static void c_ec_enc_bits(ec_ctx *e, opus_uint32 fl, unsigned bits) {
    ec_enc_bits(e, fl, bits);
}
static void c_ec_enc_patch_initial_bits(ec_ctx *e, unsigned v, unsigned n) {
    ec_enc_patch_initial_bits(e, v, n);
}
static void c_ec_enc_shrink(ec_ctx *e, opus_uint32 sz) { ec_enc_shrink(e, sz); }
static void c_ec_enc_done(ec_ctx *e) { ec_enc_done(e); }

static unsigned c_ec_decode(ec_ctx *e, unsigned ft) { return ec_decode(e, ft); }
static unsigned c_ec_decode_bin(ec_ctx *e, unsigned bits) { return ec_decode_bin(e, bits); }
static void c_ec_dec_update(ec_ctx *e, unsigned fl, unsigned fh, unsigned ft) {
    ec_dec_update(e, fl, fh, ft);
}
static int c_ec_dec_bit_logp(ec_ctx *e, unsigned logp) {
    return ec_dec_bit_logp(e, logp);
}
static int c_ec_dec_icdf(ec_ctx *e, const unsigned char *icdf, unsigned ftb) {
    return ec_dec_icdf(e, icdf, ftb);
}
static int c_ec_dec_icdf16(ec_ctx *e, const opus_uint16 *icdf, unsigned ftb) {
    return ec_dec_icdf16(e, icdf, ftb);
}
static opus_uint32 c_ec_dec_uint(ec_ctx *e, opus_uint32 ft) {
    return ec_dec_uint(e, ft);
}
static opus_uint32 c_ec_dec_bits(ec_ctx *e, unsigned bits) {
    return ec_dec_bits(e, bits);
}

static int c_ec_tell(ec_ctx *e) { return ec_tell(e); }
static opus_uint32 c_ec_tell_frac(ec_ctx *e) { return ec_tell_frac(e); }
static opus_uint32 c_ec_range_bytes(ec_ctx *e) { return ec_range_bytes(e); }
static int c_ec_get_error(ec_ctx *e) { return ec_get_error(e); }
static opus_uint32 c_ec_rng(ec_ctx *e) { return e->rng; }
static opus_uint32 c_ec_val(ec_ctx *e) { return e->val; }
*/
import "C"
import "unsafe"

// Raw pointer wrappers. Each test maintains paired C + Go handles and
// issues the same operation in lock-step.

type cEc struct{ p *C.ec_ctx }

func cEcEncNew(buf []byte) cEc {
	var p *C.uchar
	if len(buf) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}
	return cEc{p: C.c_ec_enc_new(p, C.ulong(len(buf)))}
}
func cEcDecNew(buf []byte) cEc {
	var p *C.uchar
	if len(buf) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}
	return cEc{p: C.c_ec_dec_new(p, C.ulong(len(buf)))}
}
func (e cEc) Free() { C.c_ec_free(e.p) }

func (e cEc) Encode(fl, fh, ft uint32) {
	C.c_ec_encode(e.p, C.uint(fl), C.uint(fh), C.uint(ft))
}
func (e cEc) EncodeBin(fl, fh uint32, bits int) {
	C.c_ec_encode_bin(e.p, C.uint(fl), C.uint(fh), C.uint(bits))
}
func (e cEc) EncBitLogp(val, logp int) {
	C.c_ec_enc_bit_logp(e.p, C.int(val), C.uint(logp))
}
func (e cEc) EncIcdf(s int, icdf []byte, ftb int) {
	C.c_ec_enc_icdf(e.p, C.int(s), (*C.uchar)(unsafe.Pointer(&icdf[0])), C.uint(ftb))
}
func (e cEc) EncIcdf16(s int, icdf []uint16, ftb int) {
	C.c_ec_enc_icdf16(e.p, C.int(s), (*C.opus_uint16)(unsafe.Pointer(&icdf[0])), C.uint(ftb))
}
func (e cEc) EncUint(fl, ft uint32) {
	C.c_ec_enc_uint(e.p, C.opus_uint32(fl), C.opus_uint32(ft))
}
func (e cEc) EncBits(fl uint32, bits int) {
	C.c_ec_enc_bits(e.p, C.opus_uint32(fl), C.uint(bits))
}
func (e cEc) EncPatchInitialBits(v uint32, n int) {
	C.c_ec_enc_patch_initial_bits(e.p, C.uint(v), C.uint(n))
}
func (e cEc) EncShrink(sz uint32) { C.c_ec_enc_shrink(e.p, C.opus_uint32(sz)) }
func (e cEc) EncDone()            { C.c_ec_enc_done(e.p) }

func (e cEc) Decode(ft uint32) uint32 {
	return uint32(C.c_ec_decode(e.p, C.uint(ft)))
}
func (e cEc) DecodeBin(bits int) uint32 {
	return uint32(C.c_ec_decode_bin(e.p, C.uint(bits)))
}
func (e cEc) DecUpdate(fl, fh, ft uint32) {
	C.c_ec_dec_update(e.p, C.uint(fl), C.uint(fh), C.uint(ft))
}
func (e cEc) DecBitLogp(logp int) int {
	return int(C.c_ec_dec_bit_logp(e.p, C.uint(logp)))
}
func (e cEc) DecIcdf(icdf []byte, ftb int) int {
	return int(C.c_ec_dec_icdf(e.p, (*C.uchar)(unsafe.Pointer(&icdf[0])), C.uint(ftb)))
}
func (e cEc) DecIcdf16(icdf []uint16, ftb int) int {
	return int(C.c_ec_dec_icdf16(e.p, (*C.opus_uint16)(unsafe.Pointer(&icdf[0])), C.uint(ftb)))
}
func (e cEc) DecUint(ft uint32) uint32 {
	return uint32(C.c_ec_dec_uint(e.p, C.opus_uint32(ft)))
}
func (e cEc) DecBits(bits int) uint32 {
	return uint32(C.c_ec_dec_bits(e.p, C.uint(bits)))
}

func (e cEc) Tell() int          { return int(C.c_ec_tell(e.p)) }
func (e cEc) TellFrac() uint32   { return uint32(C.c_ec_tell_frac(e.p)) }
func (e cEc) RangeBytes() uint32 { return uint32(C.c_ec_range_bytes(e.p)) }
func (e cEc) Error() int         { return int(C.c_ec_get_error(e.p)) }
func (e cEc) Rng() uint32        { return uint32(C.c_ec_rng(e.p)) }
func (e cEc) Val() uint32        { return uint32(C.c_ec_val(e.p)) }
