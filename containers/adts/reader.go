package adts

import (
	"bufio"
	"io"

	"go-mediatoolkit/containers"
	aaclib "go-mediatoolkit/libraries/aac"
	"go-mediatoolkit/mutations"
)

// maxResyncScan bounds how many bytes the Reader will skip looking for the
// next syncword before giving up with [ErrNoSync]. One ADTS frame is at most
// 8191 bytes, so a window comfortably larger than that catches a single run
// of garbage between frames without unbounded scanning.
const maxResyncScan = 1 << 16

// Reader scans ADTS frames from an [io.Reader] and exposes each AAC access
// unit as a packet. The configuration (AOT / sample rate / channels) is
// derived from the first frame header — there is no out-of-band
// AudioSpecificConfig in ADTS — so [Reader.Header] is populated after the
// first [Reader.ReadPacket] (or eagerly by [NewReader], which peeks the first
// header).
//
// ReadPacket yields the raw AAC access unit of each frame (the header and
// optional CRC stripped), making the Reader a
// [go-mediatoolkit/codec/aac.PacketReader]: feed [Reader.ASC] and the Reader
// itself straight into codec/aac.NewDecoder. On a corrupt or mis-aligned
// stream it resyncs by scanning forward to the next plausible syncword.
type Reader struct {
	br     *bufio.Reader
	header Header
	asc    aaclib.AudioSpecificConfig
	frames int
	gotHdr bool // first header parsed (header/asc populated)
	eof    bool
}

// NewReader wraps r and parses the first ADTS frame header so [Reader.Header]
// and [Reader.ASC] are available before any packet is read. The first frame's
// access unit is buffered and returned by the first [Reader.ReadPacket] call,
// so no audio is lost.
//
// It returns [ErrBadArg] if r is nil, [ErrNoSync] if no syncword is found
// within the resync window, and io.EOF if r is empty.
func NewReader(r io.Reader) (*Reader, error) {
	if r == nil {
		return nil, ErrBadArg
	}
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	rd := &Reader{br: br}

	// Peek the first header (without consuming the frame) to populate
	// Header/ASC eagerly. peekHeader resyncs to the syncword as needed.
	h, err := rd.peekHeader()
	if err != nil {
		return nil, err
	}
	rd.applyFirstHeader(h)
	return rd, nil
}

// Header returns the container header derived from the first frame. Its
// SampleRate / Channels / BitRate and Extra fields are valid once the first
// header has been parsed (NewReader does this eagerly). Frame count in
// Extra.Frames updates as the stream is consumed.
func (r *Reader) Header() Header {
	r.header.Extra.Frames = r.frames
	return r.header
}

// ASC returns the [aaclib.AudioSpecificConfig] derived from the first frame
// header, ready to pass to [go-mediatoolkit/codec/aac.NewDecoder].
func (r *Reader) ASC() aaclib.AudioSpecificConfig { return r.asc }

// ReadPacket returns the next AAC access unit (header + optional CRC
// stripped). It resyncs past garbage to the next syncword when necessary, and
// returns io.EOF when the stream is exhausted. Reader implements
// [go-mediatoolkit/codec/aac.PacketReader].
func (r *Reader) ReadPacket() ([]byte, error) {
	if r.eof {
		return nil, io.EOF
	}
	h, err := r.peekHeader()
	if err != nil {
		if err == io.EOF {
			r.eof = true
		}
		return nil, err
	}
	if !r.gotHdr {
		r.applyFirstHeader(h)
	}

	// Read the whole frame (header + CRC + payload), then slice off the AU.
	frame := make([]byte, h.FrameLength)
	if _, err := io.ReadFull(r.br, frame); err != nil {
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			r.eof = true
			return nil, io.EOF
		}
		return nil, err
	}
	r.frames++

	hdrSize := h.HeaderSize()
	if hdrSize > len(frame) {
		// Defensive: a frame length that does not cover its header was
		// rejected by ParseHeader, but guard the slice regardless.
		return nil, ErrBadFrameLength
	}
	au := frame[hdrSize:]
	return au, nil
}

// applyFirstHeader records the first parsed header onto Header/ASC.
func (r *Reader) applyFirstHeader(h FrameHeader) {
	r.asc = h.AudioSpecificConfig()
	r.header = Header{
		Format:       "adts",
		SampleRate:   h.SampleRate(),
		Channels:     h.Channels(),
		SampleFormat: mutations.FormatFloat64,
		Extra: Extras{
			Config:               r.asc,
			MPEGVersion:          h.MPEGVersion,
			Profile:              h.Profile,
			SampleRateIndex:      h.SampleRateIndex,
			ChannelConfiguration: h.ChannelConfiguration,
			CRCPresent:           h.CRCPresent,
		},
		Tags: containers.StandardTags{},
	}
	r.gotHdr = true
}

// peekHeader resyncs to the next ADTS syncword and parses its header without
// consuming the frame body. It scans forward over non-syncword bytes (up to
// [maxResyncScan]) and validates each candidate by parsing the fixed header,
// leaving the buffered reader positioned at the syncword on success.
func (r *Reader) peekHeader() (FrameHeader, error) {
	scanned := 0
	for {
		// Need the fixed header to validate a candidate; peek that many bytes.
		peek, err := r.br.Peek(HeaderLen)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				// Fewer than a full header remains. If we never scanned any
				// garbage and there are no bytes at all, the stream is simply
				// exhausted (io.EOF). Otherwise we ran out looking for a
				// syncword amid trailing/garbage bytes — that is a no-sync.
				if len(peek) == 0 && scanned == 0 {
					return FrameHeader{}, io.EOF
				}
				if scanned > 0 {
					return FrameHeader{}, ErrNoSync
				}
				return FrameHeader{}, io.EOF
			}
			return FrameHeader{}, err
		}

		h, perr := ParseHeader(peek)
		if perr == nil {
			return h, nil
		}

		// Not a valid header at this position: advance one byte and retry,
		// scanning for the next syncword.
		if scanned >= maxResyncScan {
			return FrameHeader{}, ErrNoSync
		}
		if _, err := r.br.Discard(1); err != nil {
			if err == io.EOF {
				if scanned > 0 {
					return FrameHeader{}, ErrNoSync
				}
				return FrameHeader{}, io.EOF
			}
			return FrameHeader{}, err
		}
		scanned++
	}
}

// AccessUnits drains the Reader and returns every remaining AAC access unit.
// It is a convenience for callers that want all packets up front (e.g. to
// feed [go-mediatoolkit/codec/aac.NewSlicePacketReader]); it consumes the
// stream.
func (r *Reader) AccessUnits() ([][]byte, error) {
	var aus [][]byte
	for {
		au, err := r.ReadPacket()
		if err == io.EOF {
			return aus, nil
		}
		if err != nil {
			return aus, err
		}
		// Copy: ReadPacket returns a slice of a per-frame allocation, which is
		// safe to retain, but copy to decouple from the frame buffer's tail.
		cp := make([]byte, len(au))
		copy(cp, au)
		aus = append(aus, cp)
	}
}
