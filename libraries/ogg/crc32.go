package ogg

import "encoding/binary"

// oggCRC32 computes the Ogg CRC-32 checksum over buf, starting from crc.
// The Ogg CRC uses the same polynomial as Ethernet (0x04c11db7) but with
// an unreflected algorithm and init/final of 0 (not 0xffffffff).
func oggCRC32(crc uint32, buf []byte) uint32 {
	for len(buf) >= 8 {
		_ = buf[7] // BCE hint
		crc ^= binary.BigEndian.Uint32(buf)

		crc = crcTable[7][crc>>24] ^
			crcTable[6][(crc>>16)&0xFF] ^
			crcTable[5][(crc>>8)&0xFF] ^
			crcTable[4][crc&0xFF] ^
			crcTable[3][buf[4]] ^
			crcTable[2][buf[5]] ^
			crcTable[1][buf[6]] ^
			crcTable[0][buf[7]]

		buf = buf[8:]
	}

	for _, b := range buf {
		crc = (crc << 8) ^ crcTable[0][(crc>>24)^uint32(b)]
	}
	return crc
}

// crcTable is the 8x256 slicing-by-8 CRC lookup table for the Ogg polynomial.
// Generated from polynomial 0x04c11db7 (unreflected).
var crcTable [8][256]uint32

func init() {
	const poly = 0x04c11db7

	// Build the base table (slice 0).
	for i := 0; i < 256; i++ {
		crc := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if crc&(1<<31) != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
		}
		crcTable[0][i] = crc
	}

	// Build the extended tables (slices 1-7) for slicing-by-8.
	for i := 0; i < 256; i++ {
		for j := 1; j < 8; j++ {
			crcTable[j][i] = crcTable[0][(crcTable[j-1][i]>>24)&0xFF] ^ (crcTable[j-1][i] << 8)
		}
	}
}
