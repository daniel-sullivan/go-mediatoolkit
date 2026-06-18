package ogg

import "bytes"

// nativeSync implements the Sync interface in pure Go.
// Port of ogg_sync_state from libogg/src/framing.c.
type nativeSync struct {
	data     []byte
	fill     int
	returned int

	unsynced    bool
	headerBytes int
	bodyBytes   int
}

func newNativeSync() Sync {
	return &nativeSync{}
}

func (s *nativeSync) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	// Compact returned data first.
	if s.returned > 0 {
		s.fill -= s.returned
		if s.fill > 0 {
			copy(s.data, s.data[s.returned:s.returned+s.fill])
		}
		s.returned = 0
	}

	// Grow buffer if needed (exponential growth).
	need := s.fill + len(data)
	if need > len(s.data) {
		newSize := 2 * need
		if newSize < 8192 {
			newSize = 8192
		}
		buf := make([]byte, newSize)
		copy(buf, s.data[:s.fill])
		s.data = buf
	}

	copy(s.data[s.fill:], data)
	s.fill += len(data)
	return len(data), nil
}

// pageSeek attempts to find a complete Ogg page in the buffer.
// Returns:
//
//	>0: page found, length in bytes
//	 0: need more data
//	<0: skipped -n bytes (sync loss)
func (s *nativeSync) pageSeek(page *Page) int {
	buf := s.data[s.returned:]
	available := s.fill - s.returned

	if s.headerBytes == 0 {
		if available < 27 {
			return 0 // not enough for a header
		}

		// Verify capture pattern "OggS"
		if buf[0] != 'O' || buf[1] != 'g' || buf[2] != 'g' || buf[3] != 'S' {
			return s.syncFail(buf, available)
		}

		headerBytes := int(buf[26]) + 27
		if available < headerBytes {
			return 0 // not enough for header + segment table
		}

		// Count body length from segment table.
		bodyBytes := 0
		for i := 0; i < int(buf[26]); i++ {
			bodyBytes += int(buf[27+i])
		}
		s.headerBytes = headerBytes
		s.bodyBytes = bodyBytes
	}

	totalBytes := s.headerBytes + s.bodyBytes
	if available < totalBytes {
		return 0
	}

	// Verify CRC-32 checksum.
	{
		// Save and zero the checksum field.
		var savedCRC [4]byte
		copy(savedCRC[:], buf[22:26])
		buf[22] = 0
		buf[23] = 0
		buf[24] = 0
		buf[25] = 0

		crc := oggCRC32(0, buf[:s.headerBytes])
		crc = oggCRC32(crc, buf[s.headerBytes:totalBytes])

		// Restore original checksum bytes.
		copy(buf[22:26], savedCRC[:])

		computed := [4]byte{
			byte(crc),
			byte(crc >> 8),
			byte(crc >> 16),
			byte(crc >> 24),
		}
		if savedCRC != computed {
			return s.syncFail(buf, available)
		}
	}

	// Valid page found — single allocation for header+body.
	if page != nil {
		raw := make([]byte, totalBytes)
		copy(raw, buf[:totalBytes])
		page.Header = raw[:s.headerBytes]
		page.Body = raw[s.headerBytes:]
	}

	s.unsynced = false
	s.returned += totalBytes
	s.headerBytes = 0
	s.bodyBytes = 0
	return totalBytes
}

func (s *nativeSync) syncFail(buf []byte, available int) int {
	s.headerBytes = 0
	s.bodyBytes = 0

	// Search for next possible capture pattern.
	idx := bytes.IndexByte(buf[1:available], 'O')
	var next int
	if idx < 0 {
		next = s.fill
	} else {
		next = s.returned + 1 + idx
	}

	skipped := next - s.returned
	s.returned = next
	return -skipped
}

func (s *nativeSync) PageOut() (Page, int, error) {
	for {
		var page Page
		ret := s.pageSeek(&page)
		if ret > 0 {
			return page, 1, nil
		}
		if ret == 0 {
			return Page{}, 0, nil
		}
		// Skipped bytes — report sync loss once.
		if !s.unsynced {
			s.unsynced = true
			return Page{}, -1, nil
		}
		// Continue looking.
	}
}

func (s *nativeSync) Reset() {
	s.fill = 0
	s.returned = 0
	s.unsynced = false
	s.headerBytes = 0
	s.bodyBytes = 0
}
