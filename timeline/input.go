package timeline

import (
	"io"
	"sync/atomic"
	"time"

	"go-mediatoolkit/buffers"
)

// InputSource is a Source whose samples arrive in real time from an
// external producer — typically a devices.InputCallback delivering
// microphone audio. Samples are buffered in a small SPSC ring
// written by the callback and drained by Pull; Pull returns a partial
// count (n < len(dst), err == nil) when the ring has insufficient
// data, which is the normal backpressure signal for live sources.
//
// InputSource.Live() returns true so the mixer knows to cap its
// ring-ahead and stay within a tight real-time latency budget when
// live sources are present.
type InputSource struct {
	ring       *buffers.Ring
	sampleRate int
	channels   int
	dropped    atomic.Uint64
	closed     atomic.Bool
}

// NewInputSource constructs an InputSource with an internal ring sized
// to bufferFrames. bufferFrames should be large enough to absorb the
// worst-case latency between device-callback arrivals and mixer Pulls
// — a handful of callback frame sizes is typical (e.g. 4× the device's
// callback period).
func NewInputSource(sampleRate, channels, bufferFrames int) (*InputSource, error) {
	if sampleRate <= 0 {
		return nil, ErrBadSampleRate
	}
	if channels <= 0 {
		return nil, ErrBadChannels
	}
	if bufferFrames <= 0 {
		return nil, ErrNegativeStart
	}
	return &InputSource{
		ring:       buffers.NewRing(bufferFrames * channels),
		sampleRate: sampleRate,
		channels:   channels,
	}, nil
}

// Callback returns a function suitable for installation as the
// devices.InputCallback for a capture stream. Invocations write the
// received interleaved samples into the internal ring. Samples that
// do not fit (consumer fell behind) are dropped and counted — see
// Dropped.
//
// The callback is safe to invoke concurrently with Pull; it must not
// be invoked from multiple goroutines (the underlying ring is SPSC on
// the producer side too).
func (s *InputSource) Callback() func(buf []float64) {
	return func(buf []float64) {
		if s.closed.Load() {
			return
		}
		n := s.ring.Write(buf)
		if n < len(buf) {
			s.dropped.Add(uint64(len(buf) - n))
		}
	}
}

// Pull drains up to len(dst) samples from the ring. Returns (n, nil)
// with n possibly less than len(dst) when the ring has insufficient
// data — callers should treat the unfilled tail as silence and pull
// again later. Returns (0, io.EOF) once Close has been called and the
// ring has been drained.
func (s *InputSource) Pull(dst []float64) (int, error) {
	n := s.ring.Read(dst)
	if s.closed.Load() && n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

// SampleRate reports the configured sample rate.
func (s *InputSource) SampleRate() int { return s.sampleRate }

// Channels reports the configured channel count.
func (s *InputSource) Channels() int { return s.channels }

// Duration returns -1 — live sources do not have a known length.
func (s *InputSource) Duration() time.Duration { return -1 }

// Live always returns true.
func (s *InputSource) Live() bool { return true }

// Dropped reports the cumulative number of samples discarded because
// the callback arrived faster than Pull drained the ring. Nonzero
// values indicate the consumer (mixer or other) is falling behind —
// either increase bufferFrames or reduce upstream CPU load.
func (s *InputSource) Dropped() uint64 { return s.dropped.Load() }

// Close marks the source closed. Callbacks received after Close are
// discarded; Pull drains any buffered samples and then returns
// io.EOF. Idempotent.
func (s *InputSource) Close() error {
	s.closed.Store(true)
	return nil
}
