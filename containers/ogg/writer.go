package ogg

import (
	"io"

	"go-mediatoolkit/containers"
	"go-mediatoolkit/libraries/ogg"
)

// Writer multiplexes packets from one or more logical streams into an Ogg
// bitstream. Each logical stream is created with [Writer.AddStream] and
// accepts packets via its [StreamWriter.WritePacket] method.
type Writer struct {
	w       io.Writer
	streams []*StreamWriter
	byID    map[int32]*StreamWriter
	closed  bool
}

// StreamWriter is a single logical bitstream inside a Writer. It implements
// [containers.PacketWriter] and is structurally compatible with
// [go-mediatoolkit/codec/opus.PacketWriter].
//
// Internally, WritePacket buffers the most recent packet so the EOS flag
// can be applied to the true final packet at Close time.
type StreamWriter struct {
	SerialNo int32
	enc      ogg.Encoder
	w        io.Writer
	granule  int64
	nextPkt  int64
	bosSent  bool
	eosFlag  bool
	pending  *ogg.Packet
}

// NewWriter returns a Writer that emits Ogg pages to w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{
		w:    w,
		byID: map[int32]*StreamWriter{},
	}
}

// AddStream registers a new logical stream with the given serial number.
func (w *Writer) AddStream(serialNo int32) (*StreamWriter, error) {
	if _, exists := w.byID[serialNo]; exists {
		return nil, ErrUnknownStream
	}
	enc, err := ogg.NewEncoder(serialNo)
	if err != nil {
		return nil, err
	}
	s := &StreamWriter{
		SerialNo: serialNo,
		enc:      enc,
		w:        w.w,
	}
	w.streams = append(w.streams, s)
	w.byID[serialNo] = s
	return s, nil
}

// Streams returns the registered stream writers in the order they were added.
func (w *Writer) Streams() []*StreamWriter { return w.streams }

// Close flushes any remaining buffered pages for every stream. It does not
// close the underlying io.Writer.
func (w *Writer) Close() error {
	if w.closed {
		return ErrAlreadyClosed
	}
	w.closed = true
	for _, s := range w.streams {
		if err := s.finalFlush(); err != nil {
			return err
		}
	}
	return nil
}

// SetGranule sets the granule position that will be attached to the NEXT
// packet buffered by WritePacket.
func (s *StreamWriter) SetGranule(g int64) { s.granule = g }

// SetEOS arranges for the final packet written to this stream to carry the
// EOS flag. Call this before [StreamWriter.WritePacket] for the last packet,
// or before Close.
func (s *StreamWriter) SetEOS() { s.eosFlag = true }

// WritePacket appends a packet to this stream. The first call is marked BOS
// automatically. Only one packet is buffered at a time; earlier packets are
// submitted to the underlying encoder immediately.
func (s *StreamWriter) WritePacket(data []byte) error {
	if s.pending != nil {
		if err := s.submit(s.pending); err != nil {
			return err
		}
		s.pending = nil
	}
	s.pending = &ogg.Packet{
		Data:       append([]byte{}, data...),
		BOS:        !s.bosSent,
		GranulePos: s.granule,
		PacketNo:   s.nextPkt,
	}
	s.bosSent = true
	s.nextPkt++
	return nil
}

// forceFlush submits any pending packet and flushes all buffered pages.
// Used to respect codec-specific page-alignment requirements (e.g. Opus
// requires OpusHead and OpusTags to each occupy their own page).
func (s *StreamWriter) forceFlush() error {
	if s.pending != nil {
		if err := s.submit(s.pending); err != nil {
			return err
		}
		s.pending = nil
	}
	return s.flushPages()
}

// finalFlush stamps EOS on the buffered packet (if SetEOS was called),
// submits it, and drains the encoder.
func (s *StreamWriter) finalFlush() error {
	if s.pending != nil {
		if s.eosFlag {
			s.pending.EOS = true
		}
		if err := s.submit(s.pending); err != nil {
			return err
		}
		s.pending = nil
	}
	return s.flushPages()
}

// submit hands a packet to the encoder and drains any resulting pages.
func (s *StreamWriter) submit(pkt *ogg.Packet) error {
	if err := s.enc.PacketIn(pkt); err != nil {
		return err
	}
	for {
		page, ok := s.enc.PageOut()
		if !ok {
			return nil
		}
		if _, err := s.w.Write(page.Header); err != nil {
			return err
		}
		if _, err := s.w.Write(page.Body); err != nil {
			return err
		}
	}
}

// flushPages forces the encoder to emit any buffered pages.
func (s *StreamWriter) flushPages() error {
	for {
		page, ok := s.enc.Flush()
		if !ok {
			return nil
		}
		if _, err := s.w.Write(page.Header); err != nil {
			return err
		}
		if _, err := s.w.Write(page.Body); err != nil {
			return err
		}
	}
}

// compile-time check
var _ containers.PacketWriter = (*StreamWriter)(nil)
