package wav

import (
	"encoding/binary"
	"errors"
	"io"
)

// Chunk IDs used in RIFF/WAVE files.
var (
	idRIFF = [4]byte{'R', 'I', 'F', 'F'}
	idWAVE = [4]byte{'W', 'A', 'V', 'E'}
	idFMT  = [4]byte{'f', 'm', 't', ' '}
	idDATA = [4]byte{'d', 'a', 't', 'a'}
	idLIST = [4]byte{'L', 'I', 'S', 'T'}
	idINFO = [4]byte{'I', 'N', 'F', 'O'}
	idBEXT = [4]byte{'b', 'e', 'x', 't'}
	idCUE  = [4]byte{'c', 'u', 'e', ' '}
	idFACT = [4]byte{'f', 'a', 'c', 't'}
)

// wFormatTag values we care about.
const (
	formatPCM        uint16 = 0x0001
	formatIEEEFloat  uint16 = 0x0003
	formatExtensible uint16 = 0xFFFE
)

// readChunkHeader reads a 4-byte ID + 4-byte little-endian size.
func readChunkHeader(r io.Reader) (id [4]byte, size uint32, err error) {
	var buf [8]byte
	if _, err = io.ReadFull(r, buf[:]); err != nil {
		return id, 0, err
	}
	copy(id[:], buf[:4])
	size = binary.LittleEndian.Uint32(buf[4:])
	return id, size, nil
}

// writeChunkHeader writes a 4-byte ID + 4-byte little-endian size.
func writeChunkHeader(w io.Writer, id [4]byte, size uint32) error {
	var buf [8]byte
	copy(buf[:4], id[:])
	binary.LittleEndian.PutUint32(buf[4:], size)
	_, err := w.Write(buf[:])
	return err
}

// skip advances an io.Reader by n bytes. Uses a small scratch buffer; for
// large skips this is O(n). The RIFF format requires sequential scanning
// through unknown chunks so we cannot rely on seeking.
func skip(r io.Reader, n int64) error {
	if n == 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, r, n)
	return err
}

// fmtChunk is the decoded WAVE fmt chunk.
type fmtChunk struct {
	FormatTag       uint16
	Channels        uint16
	SampleRate      uint32
	ByteRate        uint32
	BlockAlign      uint16
	BitsPerSample   uint16
	SubFormat       [16]byte // only valid when FormatTag == formatExtensible
	HasSubFormat    bool
	ValidBitsPerSmp uint16 // only valid when FormatTag == formatExtensible
}

func parseFmt(body []byte) (fmtChunk, error) {
	var f fmtChunk
	if len(body) < 16 {
		return f, errors.New("wav: fmt chunk too small")
	}
	f.FormatTag = binary.LittleEndian.Uint16(body[0:2])
	f.Channels = binary.LittleEndian.Uint16(body[2:4])
	f.SampleRate = binary.LittleEndian.Uint32(body[4:8])
	f.ByteRate = binary.LittleEndian.Uint32(body[8:12])
	f.BlockAlign = binary.LittleEndian.Uint16(body[12:14])
	f.BitsPerSample = binary.LittleEndian.Uint16(body[14:16])

	if f.FormatTag == formatExtensible && len(body) >= 40 {
		// cbSize (2) @ 16, wValidBitsPerSample (2) @ 18,
		// dwChannelMask (4) @ 20, SubFormat GUID (16) @ 24.
		f.ValidBitsPerSmp = binary.LittleEndian.Uint16(body[18:20])
		copy(f.SubFormat[:], body[24:40])
		f.HasSubFormat = true
	}
	return f, nil
}

// buildFmt serialises a fmt chunk body (without the chunk header).
// Always emits the 16-byte PCM layout — WAVE_FORMAT_EXTENSIBLE is not
// generated on write.
func buildFmt(f fmtChunk) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint16(buf[0:2], f.FormatTag)
	binary.LittleEndian.PutUint16(buf[2:4], f.Channels)
	binary.LittleEndian.PutUint32(buf[4:8], f.SampleRate)
	binary.LittleEndian.PutUint32(buf[8:12], f.ByteRate)
	binary.LittleEndian.PutUint16(buf[12:14], f.BlockAlign)
	binary.LittleEndian.PutUint16(buf[14:16], f.BitsPerSample)
	return buf
}

// padByte returns 1 if size is odd (RIFF chunks are padded to even length),
// 0 otherwise.
func padByte(size uint32) int {
	return int(size & 1)
}
