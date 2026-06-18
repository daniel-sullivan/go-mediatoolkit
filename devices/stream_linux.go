//go:build linux

package devices

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// linux streaming via PulseAudio.
//
// The enumeration backend already owns a low-level proto.Client socket
// used for sink/source listing and subscription events; streaming adds
// a second connection managed by the high-level pulse.Client, which
// knows how to pump playback/record data packets. Streams all share
// this single second connection — pulse connections are cheap but not
// free, and multiplexing is the native model anyway.

// defaultLinuxFrames is the callback size used when the caller passes
// StreamFormat.Frames == 0. Small enough for responsive interactivity,
// large enough to avoid excessive syscall overhead.
const defaultLinuxFrames = 1024

// ensureStreamClient lazily opens the pulse.Client used for streaming.
// The first successful open is cached; subsequent callers reuse it.
func (b *pulseBackend) ensureStreamClient() (*pulse.Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.streamClient != nil {
		return b.streamClient, nil
	}
	c, err := pulse.NewClient()
	if err != nil {
		return nil, err
	}
	b.streamClient = c
	return c, nil
}

// OpenOutput resolves the sink by name, creates a playback stream at
// the requested format, and returns a Stream adapter that invokes cb
// with fixed-size float64 buffers.
func (b *pulseBackend) OpenOutput(dev Device, format StreamFormat, cb OutputCallback) (Stream, error) {
	if format.SampleRate <= 0 || format.Channels <= 0 {
		return nil, ErrInvalidFormat
	}
	frames := format.Frames
	if frames <= 0 {
		frames = defaultLinuxFrames
	}

	c, err := b.ensureStreamClient()
	if err != nil {
		return nil, err
	}
	sink, err := c.SinkByID(dev.ID)
	if err != nil {
		return nil, ErrDeviceNotFound
	}

	s := &linuxStream{
		channels: format.Channels,
		rate:     format.SampleRate,
		frames:   frames,
		out:      cb,
		scratch:  make([]float64, frames*format.Channels),
		outReady: 0,
	}

	reader := pulse.Float32Reader(s.readFloat32)
	ps, err := c.NewPlayback(reader,
		pulse.PlaybackSink(sink),
		pulse.PlaybackChannels(channelMap(format.Channels)),
		pulse.PlaybackSampleRate(format.SampleRate),
		pulse.PlaybackBufferSize(frames*format.Channels),
		pulse.PlaybackMediaName("go-mediatoolkit playback"),
	)
	if err != nil {
		return nil, err
	}
	s.playback = ps
	s.actual = StreamFormat{
		SampleRate: ps.SampleRate(),
		Channels:   ps.Channels(),
		Frames:     frames,
	}
	return s, nil
}

// OpenInput mirrors OpenOutput for a capture stream.
func (b *pulseBackend) OpenInput(dev Device, format StreamFormat, cb InputCallback) (Stream, error) {
	if format.SampleRate <= 0 || format.Channels <= 0 {
		return nil, ErrInvalidFormat
	}
	frames := format.Frames
	if frames <= 0 {
		frames = defaultLinuxFrames
	}

	c, err := b.ensureStreamClient()
	if err != nil {
		return nil, err
	}
	source, err := c.SourceByID(dev.ID)
	if err != nil {
		return nil, ErrDeviceNotFound
	}

	s := &linuxStream{
		channels: format.Channels,
		rate:     format.SampleRate,
		frames:   frames,
		in:       cb,
		scratch:  make([]float64, frames*format.Channels),
	}

	writer := pulse.Float32Writer(s.writeFloat32)
	rs, err := c.NewRecord(writer,
		pulse.RecordSource(source),
		pulse.RecordChannels(channelMap(format.Channels)),
		pulse.RecordSampleRate(format.SampleRate),
		pulse.RecordBufferFragmentSize(uint32(frames*format.Channels*4)),
		pulse.RecordMediaName("go-mediatoolkit capture"),
	)
	if err != nil {
		return nil, err
	}
	s.record = rs
	s.actual = StreamFormat{
		SampleRate: rs.SampleRate(),
		Channels:   rs.Channels(),
		Frames:     frames,
	}
	return s, nil
}

// linuxStream adapts a pulse PlaybackStream or RecordStream to our
// fixed-size callback contract. Only one of playback/record is set.
type linuxStream struct {
	playback *pulse.PlaybackStream
	record   *pulse.RecordStream

	channels int
	rate     int
	frames   int
	actual   StreamFormat

	out OutputCallback
	in  InputCallback

	mu sync.Mutex

	// scratch holds one fixed-size frame of audio, in float64. For
	// output it is filled by the user callback then drained into pulse
	// as pulse asks for bytes. For input it is filled from pulse bytes
	// then handed to the user callback once full.
	scratch []float64

	// outPos is the next-to-read index into scratch while draining a
	// pre-filled output frame; when it reaches len(scratch) we call
	// the user callback again and reset to 0.
	outPos int
	// outReady indicates whether scratch has been filled at least once
	// by the output callback (1) or is still fresh (0).
	outReady int

	// inPos is the next-to-write index into scratch while accumulating
	// an input frame; when it reaches len(scratch) we deliver to the
	// user callback and reset.
	inPos int

	closed atomic.Bool
	state  atomic.Int32 // 0 idle, 1 running
}

// readFloat32 serves pulse's request for output samples. We synthesise
// in fixed-size units: each time scratch is drained we invoke the
// user's callback to refill it, then memcpy into pulse's buffer.
func (s *linuxStream) readFloat32(buf []float32) (int, error) {
	if s.closed.Load() {
		return 0, io.EOF
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	written := 0
	for written < len(buf) {
		if s.outPos >= len(s.scratch) || s.outReady == 0 {
			for i := range s.scratch {
				s.scratch[i] = 0
			}
			s.out(s.scratch)
			s.outPos = 0
			s.outReady = 1
		}
		remaining := len(s.scratch) - s.outPos
		n := len(buf) - written
		if n > remaining {
			n = remaining
		}
		for i := 0; i < n; i++ {
			v := s.scratch[s.outPos+i]
			if v > 1 {
				v = 1
			} else if v < -1 {
				v = -1
			}
			buf[written+i] = float32(v)
		}
		s.outPos += n
		written += n
	}
	return written, nil
}

// writeFloat32 receives pulse's recorded bytes. We accumulate into
// scratch until a full frame is ready, then invoke the user callback.
// The buffer pulse hands us is not retained past the call, so the
// callback may repeat immediately.
func (s *linuxStream) writeFloat32(buf []float32) (int, error) {
	if s.closed.Load() {
		return 0, io.EOF
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	consumed := 0
	for consumed < len(buf) {
		remaining := len(s.scratch) - s.inPos
		n := len(buf) - consumed
		if n > remaining {
			n = remaining
		}
		for i := 0; i < n; i++ {
			s.scratch[s.inPos+i] = float64(buf[consumed+i])
		}
		s.inPos += n
		consumed += n
		if s.inPos == len(s.scratch) {
			s.in(s.scratch)
			s.inPos = 0
		}
	}
	return consumed, nil
}

// Start runs the stream. pulse.PlaybackStream.Start and RecordStream.Start
// are synchronous with respect to network state; they do not propagate
// errors, so we assume success after the call returns.
func (s *linuxStream) Start() error {
	if s.closed.Load() {
		return ErrStreamClosed
	}
	if !s.state.CompareAndSwap(0, 1) {
		return nil
	}
	if s.playback != nil {
		s.playback.Start()
	}
	if s.record != nil {
		s.record.Start()
	}
	return nil
}

// Stop suspends the stream without tearing it down. Start can be used
// to resume.
func (s *linuxStream) Stop() error {
	if s.closed.Load() {
		return ErrStreamClosed
	}
	if !s.state.CompareAndSwap(1, 0) {
		return nil
	}
	if s.playback != nil {
		s.playback.Stop()
	}
	if s.record != nil {
		s.record.Stop()
	}
	return nil
}

// Close stops the stream and deletes it server-side. The shared
// pulse.Client stays open; it is torn down in pulseBackend.Close.
func (s *linuxStream) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if s.playback != nil {
		s.playback.Close()
	}
	if s.record != nil {
		s.record.Close()
	}
	return nil
}

// Format returns the format pulse negotiated.
func (s *linuxStream) Format() StreamFormat { return s.actual }

// channelMap returns a simple mono/stereo/surround channel layout of
// length n. For small values we use the well-known maps; higher
// counts default to all-centre, which is semantically meaningless but
// avoids pulse refusing the request for want of a map.
func channelMap(n int) proto.ChannelMap {
	switch n {
	case 1:
		return proto.ChannelMap{proto.ChannelMono}
	case 2:
		return proto.ChannelMap{proto.ChannelLeft, proto.ChannelRight}
	}
	out := make(proto.ChannelMap, n)
	for i := range out {
		out[i] = proto.ChannelCenter
	}
	return out
}

// closeStreamClient releases the streaming pulse.Client, if it was
// lazily opened. Safe to call when no streams were ever created.
func (b *pulseBackend) closeStreamClient() {
	b.mu.Lock()
	c := b.streamClient
	b.streamClient = nil
	b.mu.Unlock()
	if c != nil {
		c.Close()
	}
}
