// Package ogg implements the Ogg bitstream format (RFC 3533) in pure Go.
//
// Ogg is a general-purpose container format for multiplexing one or more
// streams of audio, video, or other media. It provides framing, error
// detection (CRC-32), and interleaving of logical bitstreams.
//
// A Sync reads raw bytes and extracts Ogg pages (demuxing). A Decoder
// takes pages for a specific logical stream and reassembles them into
// packets. An Encoder takes packets and produces pages.
//
// The default constructors (NewSync, NewDecoder, NewEncoder) use a pure
// Go implementation that is faster than C libogg (no cgo call overhead).
// The C libogg 1.3.6 implementation is available via NewCgoSync,
// NewCgoDecoder, and NewCgoEncoder when built with Cgo enabled. Both
// implementations produce bit-identical output.
package ogg

// Page represents a single Ogg page. A page is the basic unit of the
// Ogg bitstream, containing a header and a body. The header includes
// the capture pattern ("OggS"), stream serial number, page sequence
// number, granule position, CRC-32 checksum, and a segment table.
type Page struct {
	Header []byte
	Body   []byte
}

// Version returns the Ogg stream structure version (always 0).
func (p *Page) Version() int {
	if len(p.Header) < 5 {
		return 0
	}
	return int(p.Header[4])
}

// Continued returns true if this page contains data continued from the
// previous page (i.e. the first packet is a continuation).
func (p *Page) Continued() bool {
	if len(p.Header) < 6 {
		return false
	}
	return p.Header[5]&0x01 != 0
}

// BOS returns true if this is the first page of a logical bitstream
// (beginning of stream).
func (p *Page) BOS() bool {
	if len(p.Header) < 6 {
		return false
	}
	return p.Header[5]&0x02 != 0
}

// EOS returns true if this is the last page of a logical bitstream
// (end of stream).
func (p *Page) EOS() bool {
	if len(p.Header) < 6 {
		return false
	}
	return p.Header[5]&0x04 != 0
}

// GranulePos returns the granule position of this page. The meaning of
// the granule position is codec-specific (e.g. sample count for audio).
// Returns -1 if no granule position is set.
func (p *Page) GranulePos() int64 {
	if len(p.Header) < 14 {
		return -1
	}
	return int64(p.Header[6]) |
		int64(p.Header[7])<<8 |
		int64(p.Header[8])<<16 |
		int64(p.Header[9])<<24 |
		int64(p.Header[10])<<32 |
		int64(p.Header[11])<<40 |
		int64(p.Header[12])<<48 |
		int64(p.Header[13])<<56
}

// SerialNo returns the serial number identifying the logical bitstream.
func (p *Page) SerialNo() int32 {
	if len(p.Header) < 18 {
		return 0
	}
	return int32(p.Header[14]) |
		int32(p.Header[15])<<8 |
		int32(p.Header[16])<<16 |
		int32(p.Header[17])<<24
}

// PageNo returns the page sequence number within the logical bitstream.
func (p *Page) PageNo() int32 {
	if len(p.Header) < 22 {
		return 0
	}
	return int32(p.Header[18]) |
		int32(p.Header[19])<<8 |
		int32(p.Header[20])<<16 |
		int32(p.Header[21])<<24
}

// Packets returns the number of completed packets on this page.
func (p *Page) Packets() int {
	if len(p.Header) < 27 {
		return 0
	}
	n := int(p.Header[26])
	if 27+n > len(p.Header) {
		return 0
	}
	count := 0
	for i := 0; i < n; i++ {
		if p.Header[27+i] < 255 {
			count++
		}
	}
	return count
}

// Packet represents a logical data packet extracted from an Ogg stream.
type Packet struct {
	Data       []byte
	BOS        bool  // true for the first packet of a logical stream
	EOS        bool  // true for the last packet of a logical stream
	GranulePos int64 // codec-specific position marker
	PacketNo   int64 // sequence number
}

// Sync reads raw byte data and extracts Ogg pages. It handles finding
// page boundaries, validating CRC-32 checksums, and recovering from
// sync loss (e.g. after seeking).
type Sync interface {
	// Write feeds raw data into the sync buffer. Returns the number
	// of bytes consumed.
	Write(data []byte) (int, error)

	// PageOut attempts to extract the next page from the buffered data.
	// Returns:
	//   page, 1, nil  — a complete page was extracted
	//   page, 0, nil  — need more data
	//   page, -1, nil — hole in data (sync loss); call again for the page
	PageOut() (Page, int, error)

	// Reset discards buffered data and resets sync state.
	Reset()
}

// NewSync creates a Sync for reading raw Ogg data.
// Uses the pure Go implementation, which is faster than the C libogg
// for this library (no cgo call overhead). Use [NewCgoSync] to
// explicitly use the C libogg implementation.
func NewSync() Sync {
	return newNativeSync()
}

// Decoder reassembles packets from pages belonging to a specific
// logical bitstream. Feed it pages via PageIn, then extract packets
// via PacketOut.
type Decoder interface {
	// PageIn submits a page to the decoder. The page must belong to the
	// same logical stream (matching serial number).
	PageIn(page *Page) error

	// PacketOut extracts the next packet from the stream.
	// Returns:
	//   pkt, 1, nil  — a packet was extracted
	//   pkt, 0, nil  — need more data (submit another page)
	//   pkt, -1, nil — hole in data (lost packets)
	PacketOut() (Packet, int, error)

	// PacketPeek is like PacketOut but does not advance the stream.
	PacketPeek() (Packet, int, error)

	// SerialNo returns the stream serial number.
	SerialNo() int32

	// EOS returns true if the end of stream has been reached.
	EOS() bool

	// Reset resets the decoder state for reuse or seeking.
	Reset()
}

// NewDecoder creates a Decoder for the logical stream with the given
// serial number. The serial number must match the pages submitted.
// Uses the pure Go implementation. Use [NewCgoDecoder] to explicitly
// use the C libogg implementation.
func NewDecoder(serialNo int32) (Decoder, error) {
	return newNativeDecoder(serialNo)
}

// Encoder takes packets and produces Ogg pages for a logical bitstream.
type Encoder interface {
	// PacketIn submits a packet to be encoded into pages.
	PacketIn(pkt *Packet) error

	// PageOut returns the next available page, if enough data has
	// accumulated. Returns ok=false if no page is ready yet.
	PageOut() (page Page, ok bool)

	// Flush forces all buffered packet data into a page, even if the
	// page would be undersized. Returns ok=false if no data is buffered.
	Flush() (page Page, ok bool)

	// SerialNo returns the stream serial number.
	SerialNo() int32

	// EOS returns true if the end of stream has been signalled.
	EOS() bool

	// Reset resets the encoder state.
	Reset()

	// ResetSerialNo resets the encoder and changes the serial number.
	ResetSerialNo(serialNo int32)

	// GranulePos returns the current granule position.
	GranulePos() int64
}

// NewEncoder creates an Encoder for a logical stream with the given
// serial number. Uses the pure Go implementation. Use [NewCgoEncoder]
// to explicitly use the C libogg implementation.
func NewEncoder(serialNo int32) (Encoder, error) {
	return newNativeEncoder(serialNo)
}
