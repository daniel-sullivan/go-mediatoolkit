package nativeflac

// 1:1 port of libflac/src/libFLAC/crc.c.
//
// CRC-8: poly 0x07 (= x^8 + x^2 + x + 1), init 0. Used for the FLAC
// frame header self-check (RFC 9639 §11.1).
//
// CRC-16: poly 0x8005 (= x^16 + x^15 + x^2 + 1), init 0, MSB-first.
// Used for the FLAC frame footer (RFC 9639 §11.5).
//
// libFLAC ships precomputed tables (1-bank for CRC-8, 8-bank for
// CRC-16) and a sample-loop generator commented out behind `#if 0`.
// The Go port computes both tables in init() — the result is byte-
// identical to libFLAC's static arrays.

var (
	crc8Table  [256]byte
	crc16Table [8][256]uint16
)

func init() {
	// CRC-8 table.
	for i := 0; i < 256; i++ {
		crc := uint8(i)
		for j := 0; j < 8; j++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0x07
			} else {
				crc <<= 1
			}
		}
		crc8Table[i] = crc
	}

	// CRC-16 8-way table.
	const poly16 = uint16(0x8005)
	for i := 0; i < 256; i++ {
		crc := uint16(i) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ poly16
			} else {
				crc <<= 1
			}
		}
		crc16Table[0][i] = crc
	}
	for i := 0; i < 256; i++ {
		for j := 1; j < 8; j++ {
			crc16Table[j][i] = crc16Table[0][crc16Table[j-1][i]>>8] ^ (crc16Table[j-1][i] << 8)
		}
	}
}

// CRC8 — port of FLAC__crc8 (crc.c:366).
func CRC8(data []byte) uint8 {
	crc := uint8(0)
	for _, b := range data {
		crc = crc8Table[crc^b]
	}
	return crc
}

// CRC8Update folds another byte into a running CRC-8 — used by the
// bitreader/bitwriter when accumulating the header CRC byte-by-byte.
// Equivalent to repeating FLAC__crc8 over a single-element buffer.
func CRC8Update(crc uint8, b byte) uint8 {
	return crc8Table[crc^b]
}

// CRC16 — port of FLAC__crc16 (crc.c:376). Uses the 8-way unrolled
// path on every aligned 8-byte block for parity with libFLAC.
func CRC16(data []byte) uint16 {
	crc := uint16(0)
	i := 0
	for len(data)-i >= 8 {
		crc ^= uint16(data[i])<<8 | uint16(data[i+1])
		crc = crc16Table[7][crc>>8] ^ crc16Table[6][crc&0xFF] ^
			crc16Table[5][data[i+2]] ^ crc16Table[4][data[i+3]] ^
			crc16Table[3][data[i+4]] ^ crc16Table[2][data[i+5]] ^
			crc16Table[1][data[i+6]] ^ crc16Table[0][data[i+7]]
		i += 8
	}
	for ; i < len(data); i++ {
		crc = (crc << 8) ^ crc16Table[0][byte(crc>>8)^data[i]]
	}
	return crc
}

// CRC16UpdateWords32 — port of FLAC__crc16_update_words32
// (crc.c:398). Folds `words` (each containing 4 stream bytes packed
// big-endian) into a running CRC.
func CRC16UpdateWords32(words []uint32, crc uint16) uint16 {
	i := 0
	for len(words)-i >= 2 {
		crc ^= uint16(words[i] >> 16)
		crc = crc16Table[7][crc>>8] ^ crc16Table[6][crc&0xFF] ^
			crc16Table[5][(words[i]>>8)&0xFF] ^ crc16Table[4][words[i]&0xFF] ^
			crc16Table[3][words[i+1]>>24] ^ crc16Table[2][(words[i+1]>>16)&0xFF] ^
			crc16Table[1][(words[i+1]>>8)&0xFF] ^ crc16Table[0][words[i+1]&0xFF]
		i += 2
	}
	if i < len(words) {
		crc ^= uint16(words[i] >> 16)
		crc = crc16Table[3][crc>>8] ^ crc16Table[2][crc&0xFF] ^
			crc16Table[1][(words[i]>>8)&0xFF] ^ crc16Table[0][words[i]&0xFF]
	}
	return crc
}

// CRC16UpdateWords64 — port of FLAC__crc16_update_words64
// (crc.c:422). Each word carries 8 stream bytes packed big-endian.
func CRC16UpdateWords64(words []uint64, crc uint16) uint16 {
	for _, w := range words {
		crc ^= uint16(w >> 48)
		crc = crc16Table[7][crc>>8] ^ crc16Table[6][crc&0xFF] ^
			crc16Table[5][(w>>40)&0xFF] ^ crc16Table[4][(w>>32)&0xFF] ^
			crc16Table[3][(w>>24)&0xFF] ^ crc16Table[2][(w>>16)&0xFF] ^
			crc16Table[1][(w>>8)&0xFF] ^ crc16Table[0][w&0xFF]
	}
	return crc
}
