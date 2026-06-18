package bitreader

import "go-mediatoolkit/libraries/flac/internal/nativeflac"

// nativeBitReader wraps the Go port in the same minimal interface
// the cgo wrapper exposes, so the differential tests can iterate
// uniformly.
type nativeBitReader struct {
	br     *nativeflac.BitReader
	source []byte
	off    int
}

func newNativeBitReader(source []byte) *nativeBitReader {
	n := &nativeBitReader{br: nativeflac.NewBitReader(), source: source}
	n.br.Init(n.read)
	return n
}

func (n *nativeBitReader) free() {
	n.br.Free()
}

func (n *nativeBitReader) read(buf []byte) (uint, bool) {
	avail := len(n.source) - n.off
	if avail <= 0 {
		return 0, false
	}
	want := len(buf)
	if want > avail {
		want = avail
	}
	copy(buf, n.source[n.off:n.off+want])
	n.off += want
	return uint(want), true
}

func (n *nativeBitReader) ReadRawUint32(b uint32) (uint32, bool) { return n.br.ReadRawUint32(b) }
func (n *nativeBitReader) ReadRawInt32(b uint32) (int32, bool)   { return n.br.ReadRawInt32(b) }
func (n *nativeBitReader) ReadRawUint64(b uint32) (uint64, bool) { return n.br.ReadRawUint64(b) }
func (n *nativeBitReader) ReadRawInt64(b uint32) (int64, bool)   { return n.br.ReadRawInt64(b) }
func (n *nativeBitReader) ReadUint32LittleEndian() (uint32, bool) {
	return n.br.ReadUint32LittleEndian()
}
func (n *nativeBitReader) ReadUnaryUnsigned() (uint32, bool) { return n.br.ReadUnaryUnsigned() }
func (n *nativeBitReader) ReadRiceSignedBlock(out []int32, parameter uint32) bool {
	return n.br.ReadRiceSignedBlock(out, parameter)
}
func (n *nativeBitReader) SkipBitsNoCRC(bits uint32) bool { return n.br.SkipBitsNoCRC(bits) }
func (n *nativeBitReader) ReadByteBlockAlignedNoCRC(out []byte) bool {
	return n.br.ReadByteBlockAlignedNoCRC(out)
}
func (n *nativeBitReader) ReadUTF8Uint32() (uint32, int, bool) {
	var raw [7]byte
	v, rl, ok := n.br.ReadUTF8Uint32(raw[:])
	return v, rl, ok
}
func (n *nativeBitReader) ReadUTF8Uint64() (uint64, int, bool) {
	var raw [7]byte
	v, rl, ok := n.br.ReadUTF8Uint64(raw[:])
	return v, rl, ok
}
func (n *nativeBitReader) ResetReadCRC16(seed uint16)       { n.br.ResetReadCRC16(seed) }
func (n *nativeBitReader) GetReadCRC16() uint16             { return n.br.GetReadCRC16() }
func (n *nativeBitReader) IsConsumedByteAligned() bool      { return n.br.IsConsumedByteAligned() }
func (n *nativeBitReader) BitsLeftForByteAlignment() uint32 { return n.br.BitsLeftForByteAlignment() }
