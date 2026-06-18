package nativeopus

// Laplace test shims — pass-through to the unexported port.

func ExportTestEcLaplaceEncode(h EcCtxHandle, value *int, fs uint32, decay int) {
	var v int = *value
	ec_laplace_encode(h.p, &v, opus_uint32(fs), decay)
	*value = v
}
func ExportTestEcLaplaceDecode(h EcCtxHandle, fs uint32, decay int) int {
	return ec_laplace_decode(h.p, opus_uint32(fs), decay)
}
func ExportTestEcLaplaceEncodeP0(h EcCtxHandle, value int, p0, decay uint16) {
	ec_laplace_encode_p0(h.p, value, opus_uint16(p0), opus_uint16(decay))
}
func ExportTestEcLaplaceDecodeP0(h EcCtxHandle, p0, decay uint16) int {
	return ec_laplace_decode_p0(h.p, opus_uint16(p0), opus_uint16(decay))
}
func ExportTestEcLaplaceGetFreq1(fs0 uint32, decay int) uint32 {
	return uint32(ec_laplace_get_freq1(opus_uint32(fs0), decay))
}
