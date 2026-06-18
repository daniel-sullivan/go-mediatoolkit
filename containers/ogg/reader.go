package ogg

import (
	"bytes"
	"errors"
	"io"

	"go-mediatoolkit/containers"
	"go-mediatoolkit/libraries/ogg"
)

// Reader demultiplexes an Ogg bitstream into one or more logical streams.
type Reader struct {
	header  Header
	streams []*Stream
	byID    map[int32]*Stream
}

// Stream is a single logical bitstream inside an Ogg file. Use
// [Stream.Packets] to iterate packets in order.
type Stream struct {
	SerialNo      int32
	CodecHint     string
	HeaderPackets [][]byte

	pump *pump
}

// NewReader reads Ogg pages from r until all BOS pages have been observed,
// then returns a Reader with one [Stream] per logical bitstream.
//
// Per RFC 3533, every Ogg file starts with a contiguous run of BOS pages;
// NewReader stops the initial scan as soon as the first non-BOS page
// appears. The remainder of r is consumed lazily as callers read packets.
func NewReader(r io.Reader) (*Reader, error) {
	p, err := newPump(r)
	if err != nil {
		return nil, err
	}

	extras := Extras{}
	rd := &Reader{byID: map[int32]*Stream{}}

	for _, s := range p.orderedSerials {
		// Each stream's BOS packet is already queued by the pump. Pull it out
		// so the stream's subsequent Packets() iterator starts at data.
		bos, ok := p.peekNextFor(s)
		if !ok {
			continue
		}
		hdr := [][]byte{bos.Data}
		// consume the BOS packet from the queue
		_, _ = p.pullPacketFor(s)

		codec := detectCodec(bos.Data)
		extras.Streams = append(extras.Streams, StreamInfo{
			SerialNo:      s,
			CodecHint:     codec,
			HeaderPackets: hdr,
		})

		stream := &Stream{
			SerialNo:      s,
			CodecHint:     codec,
			HeaderPackets: hdr,
			pump:          p,
		}
		rd.streams = append(rd.streams, stream)
		rd.byID[s] = stream
	}

	if len(rd.streams) == 0 {
		return nil, ErrNoStreams
	}

	rd.header = Header{
		Format: "ogg",
		Extra:  extras,
	}
	return rd, nil
}

// Header returns the parsed Ogg container header. SampleRate/Channels are
// zero for the generic reader; use a codec helper (e.g. [NewOpusReader])
// to obtain a fully-populated header.
func (r *Reader) Header() Header { return r.header }

// Streams returns all logical streams in the order their BOS pages appeared.
func (r *Reader) Streams() []*Stream { return r.streams }

// Stream returns the logical stream with the given serial number, or nil.
func (r *Reader) Stream(serialNo int32) *Stream { return r.byID[serialNo] }

// Packets returns a [containers.PacketReader] over this stream's packets,
// starting with the packet after any packets already consumed via
// [Stream.ReadPacket] (including those fetched by codec helpers).
func (s *Stream) Packets() containers.PacketReader { return s }

// ReadPacket returns the next packet from this stream, or io.EOF when the
// stream ends.
func (s *Stream) ReadPacket() ([]byte, error) {
	pkt, err := s.pump.pullPacketFor(s.SerialNo)
	if err != nil {
		return nil, err
	}
	return pkt.Data, nil
}

// readHeaderPackets consumes n additional packets from this stream and
// appends them to HeaderPackets. Used by codec-specific helpers during
// initialisation (e.g. Opus pulls two: OpusHead + OpusTags).
func (s *Stream) readHeaderPackets(n int) error {
	for i := 0; i < n; i++ {
		pkt, err := s.pump.pullPacketFor(s.SerialNo)
		if err != nil {
			return err
		}
		s.HeaderPackets = append(s.HeaderPackets, pkt.Data)
	}
	return nil
}

// --- pump -------------------------------------------------------------------

// pump drives the underlying Sync/Decoder machinery and queues decoded
// packets per logical stream.
type pump struct {
	r              io.Reader
	sync           ogg.Sync
	decs           map[int32]ogg.Decoder
	queues         map[int32][]ogg.Packet
	orderedSerials []int32
	done           bool
	buf            []byte
	sawData        bool // set after the first non-BOS page is observed
}

func newPump(r io.Reader) (*pump, error) {
	p := &pump{
		r:      r,
		sync:   ogg.NewSync(),
		decs:   map[int32]ogg.Decoder{},
		queues: map[int32][]ogg.Packet{},
		buf:    make([]byte, 8192),
	}
	// Drain until we've crossed the BOS boundary or we hit EOF. This
	// guarantees every logical stream's first packet is queued before
	// NewReader returns.
	for !p.sawData && !p.done {
		if err := p.feedOne(); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
	}
	return p, nil
}

// feedOne reads a chunk from the underlying reader, extracts any pages
// ready, routes each page to its stream's decoder, and dequeues all
// immediately-available packets into the per-stream queues.
func (p *pump) feedOne() error {
	if p.done {
		return io.EOF
	}
	n, rerr := p.r.Read(p.buf)
	if n > 0 {
		if _, werr := p.sync.Write(p.buf[:n]); werr != nil {
			return werr
		}
	}
	if rerr == io.EOF {
		p.done = true
	} else if rerr != nil {
		return rerr
	}

	for {
		page, ret, err := p.sync.PageOut()
		if err != nil {
			return err
		}
		if ret == 0 {
			break
		}
		if ret < 0 {
			continue // sync loss; keep trying
		}
		serial := page.SerialNo()

		if page.BOS() {
			if _, ok := p.decs[serial]; !ok {
				dec, derr := ogg.NewDecoder(serial)
				if derr != nil {
					return derr
				}
				p.decs[serial] = dec
				p.orderedSerials = append(p.orderedSerials, serial)
			}
		} else {
			p.sawData = true
		}

		dec, ok := p.decs[serial]
		if !ok {
			// Page for an unknown stream (should not happen given BOS rules).
			continue
		}
		if err := dec.PageIn(&page); err != nil {
			return err
		}
		for {
			pkt, pret, perr := dec.PacketOut()
			if perr != nil {
				return perr
			}
			if pret == 0 {
				break
			}
			if pret < 0 {
				continue
			}
			p.queues[serial] = append(p.queues[serial], pkt)
		}
	}
	return nil
}

// peekNextFor returns the next queued packet for serial without removing
// it, pulling additional data from the underlying reader if the queue is
// empty.
func (p *pump) peekNextFor(serial int32) (ogg.Packet, bool) {
	for len(p.queues[serial]) == 0 {
		if p.done {
			return ogg.Packet{}, false
		}
		if err := p.feedOne(); err != nil {
			return ogg.Packet{}, false
		}
	}
	return p.queues[serial][0], true
}

// pullPacketFor removes and returns the next packet for serial. Returns
// io.EOF when the stream is exhausted.
func (p *pump) pullPacketFor(serial int32) (ogg.Packet, error) {
	for len(p.queues[serial]) == 0 {
		if p.done {
			return ogg.Packet{}, io.EOF
		}
		if err := p.feedOne(); err != nil {
			return ogg.Packet{}, err
		}
	}
	pkt := p.queues[serial][0]
	p.queues[serial] = p.queues[serial][1:]
	return pkt, nil
}

// detectCodec does a best-effort codec identification from a BOS packet.
func detectCodec(bos []byte) string {
	switch {
	case bytes.HasPrefix(bos, []byte("OpusHead")):
		return "opus"
	case len(bos) >= 7 && bos[0] == 0x01 && bytes.Equal(bos[1:7], []byte("vorbis")):
		return "vorbis"
	case bytes.HasPrefix(bos, []byte("\x7fFLAC")):
		return "flac"
	}
	return ""
}
