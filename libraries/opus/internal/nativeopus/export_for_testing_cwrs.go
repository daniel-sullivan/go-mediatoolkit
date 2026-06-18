package nativeopus

func ExportTestCeltPvqU(n, k int) uint32 {
	return uint32(CELT_PVQ_U(opus_int(n), opus_int(k)))
}
func ExportTestCeltPvqV(n, k int) uint32 {
	return uint32(CELT_PVQ_V(opus_int(n), opus_int(k)))
}
func ExportTestEncodePulses(y []int, n, k int, h EcCtxHandle) {
	yi := make([]opus_int, len(y))
	for i, v := range y {
		yi[i] = opus_int(v)
	}
	encode_pulses(yi, opus_int(n), opus_int(k), h.p)
}
func ExportTestDecodePulses(y []int, n, k int, h EcCtxHandle) float32 {
	yi := make([]opus_int, len(y))
	yy := decode_pulses(yi, opus_int(n), opus_int(k), h.p)
	for i, v := range yi {
		y[i] = int(v)
	}
	return float32(yy)
}
