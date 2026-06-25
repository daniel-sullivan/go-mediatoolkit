package bitwriter

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"

// nativeBitWriter wraps the Go port in the same minimal interface the
// cgo wrapper exposes, so the differential tests can drive both
// uniformly.
type nativeBitWriter struct {
	bw *nativeflac.BitWriter
}

func newNativeBitWriter() *nativeBitWriter {
	n := &nativeBitWriter{bw: nativeflac.NewBitWriter()}
	n.bw.Init()
	return n
}

func (n *nativeBitWriter) free()  { n.bw.Free() }
func (n *nativeBitWriter) clear() { n.bw.Clear() }

func (n *nativeBitWriter) WriteZeroes(bits uint32) bool { return n.bw.WriteZeroes(bits) }
func (n *nativeBitWriter) WriteRawUint32(val, bits uint32) bool {
	return n.bw.WriteRawUint32(val, bits)
}
func (n *nativeBitWriter) WriteRawInt32(val int32, bits uint32) bool {
	return n.bw.WriteRawInt32(val, bits)
}
func (n *nativeBitWriter) WriteRawUint64(val uint64, bits uint32) bool {
	return n.bw.WriteRawUint64(val, bits)
}
func (n *nativeBitWriter) WriteRawInt64(val int64, bits uint32) bool {
	return n.bw.WriteRawInt64(val, bits)
}
func (n *nativeBitWriter) WriteRawUint32LittleEndian(val uint32) bool {
	return n.bw.WriteRawUint32LittleEndian(val)
}
func (n *nativeBitWriter) WriteByteBlock(vals []byte) bool { return n.bw.WriteByteBlock(vals) }
func (n *nativeBitWriter) WriteUnaryUnsigned(val uint32) bool {
	return n.bw.WriteUnaryUnsigned(val)
}
func (n *nativeBitWriter) WriteRiceSignedBlock(vals []int32, parameter uint32) bool {
	return n.bw.WriteRiceSignedBlock(vals, parameter)
}
func (n *nativeBitWriter) WriteUTF8Uint32(val uint32) bool { return n.bw.WriteUTF8Uint32(val) }
func (n *nativeBitWriter) WriteUTF8Uint64(val uint64) bool { return n.bw.WriteUTF8Uint64(val) }
func (n *nativeBitWriter) ZeroPadToByteBoundary() bool     { return n.bw.ZeroPadToByteBoundary() }
func (n *nativeBitWriter) IsByteAligned() bool             { return n.bw.IsByteAligned() }
func (n *nativeBitWriter) GetInputBitsUnconsumed() uint32  { return n.bw.GetInputBitsUnconsumed() }
func (n *nativeBitWriter) GetBuffer() ([]byte, bool)       { return n.bw.GetBuffer() }
func (n *nativeBitWriter) GetWriteCRC16() (uint16, bool)   { return n.bw.GetWriteCRC16() }
func (n *nativeBitWriter) GetWriteCRC8() (byte, bool)      { return n.bw.GetWriteCRC8() }
