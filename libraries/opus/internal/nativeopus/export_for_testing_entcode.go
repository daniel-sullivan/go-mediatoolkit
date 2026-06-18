package nativeopus

// Range-coder test shims — expose the unexported encoder/decoder API
// to benchcmp's parity tests without promoting the C-named snake_case
// functions to the public API.
//
// An opaque handle (EcCtxHandle) carries the underlying ec_ctx by
// pointer so the tests can thread state through a sequence of
// operations the way the C code does.

type EcCtxHandle struct{ p *ec_ctx }

// --- encoder ------------------------------------------------------

func ExportTestEcEncNew(buf []byte) EcCtxHandle {
	h := EcCtxHandle{p: &ec_ctx{}}
	ec_enc_init(h.p, buf, opus_uint32(len(buf)))
	return h
}
func ExportTestEcEncode(h EcCtxHandle, fl, fh, ft uint32) {
	ec_encode(h.p, opus_uint32(fl), opus_uint32(fh), opus_uint32(ft))
}
func ExportTestEcEncodeBin(h EcCtxHandle, fl, fh uint32, bits int) {
	ec_encode_bin(h.p, opus_uint32(fl), opus_uint32(fh), bits)
}
func ExportTestEcEncBitLogp(h EcCtxHandle, val, logp int) {
	ec_enc_bit_logp(h.p, val, logp)
}
func ExportTestEcEncIcdf(h EcCtxHandle, s int, icdf []byte, ftb int) {
	ec_enc_icdf(h.p, s, icdf, ftb)
}
func ExportTestEcEncIcdf16(h EcCtxHandle, s int, icdf []uint16, ftb int) {
	in := make([]opus_uint16, len(icdf))
	for i, v := range icdf {
		in[i] = opus_uint16(v)
	}
	ec_enc_icdf16(h.p, s, in, ftb)
}
func ExportTestEcEncUint(h EcCtxHandle, fl, ft uint32) {
	ec_enc_uint(h.p, opus_uint32(fl), opus_uint32(ft))
}
func ExportTestEcEncBits(h EcCtxHandle, fl uint32, bits int) {
	ec_enc_bits(h.p, opus_uint32(fl), bits)
}
func ExportTestEcEncPatchInitialBits(h EcCtxHandle, val uint32, nbits int) {
	ec_enc_patch_initial_bits(h.p, opus_uint32(val), nbits)
}
func ExportTestEcEncShrink(h EcCtxHandle, size uint32) {
	ec_enc_shrink(h.p, opus_uint32(size))
}
func ExportTestEcEncDone(h EcCtxHandle) { ec_enc_done(h.p) }

// --- decoder ------------------------------------------------------

func ExportTestEcDecNew(buf []byte) EcCtxHandle {
	h := EcCtxHandle{p: &ec_ctx{}}
	ec_dec_init(h.p, buf, opus_uint32(len(buf)))
	return h
}
func ExportTestEcDecode(h EcCtxHandle, ft uint32) uint32 {
	return uint32(ec_decode(h.p, opus_uint32(ft)))
}
func ExportTestEcDecodeBin(h EcCtxHandle, bits int) uint32 {
	return uint32(ec_decode_bin(h.p, bits))
}
func ExportTestEcDecUpdate(h EcCtxHandle, fl, fh, ft uint32) {
	ec_dec_update(h.p, opus_uint32(fl), opus_uint32(fh), opus_uint32(ft))
}
func ExportTestEcDecBitLogp(h EcCtxHandle, logp int) int {
	return ec_dec_bit_logp(h.p, logp)
}
func ExportTestEcDecIcdf(h EcCtxHandle, icdf []byte, ftb int) int {
	return ec_dec_icdf(h.p, icdf, ftb)
}
func ExportTestEcDecIcdf16(h EcCtxHandle, icdf []uint16, ftb int) int {
	in := make([]opus_uint16, len(icdf))
	for i, v := range icdf {
		in[i] = opus_uint16(v)
	}
	return ec_dec_icdf16(h.p, in, ftb)
}
func ExportTestEcDecUint(h EcCtxHandle, ft uint32) uint32 {
	return uint32(ec_dec_uint(h.p, opus_uint32(ft)))
}
func ExportTestEcDecBits(h EcCtxHandle, bits int) uint32 {
	return uint32(ec_dec_bits(h.p, bits))
}

// --- state inspection ---------------------------------------------

func ExportTestEcTell(h EcCtxHandle) int        { return ec_tell(h.p) }
func ExportTestEcTellFrac(h EcCtxHandle) uint32 { return uint32(ec_tell_frac(h.p)) }
func ExportTestEcRangeBytes(h EcCtxHandle) uint32 {
	return uint32(ec_range_bytes(h.p))
}
func ExportTestEcGetError(h EcCtxHandle) int { return ec_get_error(h.p) }
func ExportTestEcBuf(h EcCtxHandle) []byte   { return h.p.buf }
func ExportTestEcRng(h EcCtxHandle) uint32   { return uint32(h.p.rng) }
func ExportTestEcVal(h EcCtxHandle) uint32   { return uint32(h.p.val) }
