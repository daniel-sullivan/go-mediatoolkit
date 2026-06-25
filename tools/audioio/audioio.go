// Package audioio bridges devices.Stream and timeline-side primitives
// (InputSource, mixer Fill callbacks). Examples that need to open a
// capture or render device against a Source/Mixer should reach for
// the helpers here rather than hand-rolling the open-then-learn-format
// closure dance — that pattern is unavoidable because backends only
// reveal the negotiated sample rate/channel count after Open returns,
// but it doesn't have to clutter every example.
//
// The package depends on both timeline and devices (and so cannot be
// imported from either), which is why it lives in tools/.
package audioio

import (
	"sync/atomic"

	"github.com/daniel-sullivan/go-mediatoolkit/devices"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

// InputCapture pairs a capture-side devices.Stream with a
// timeline.InputSource constructed at the stream's negotiated format.
// It embeds the InputSource so Pull/Live/Dropped/SampleRate/Channels
// forward through unchanged; Start, Stop, and Close additionally drive
// the underlying device stream.
type InputCapture struct {
	*timeline.InputSource
	stream devices.Stream
}

// OpenInput opens a capture stream from dev with hint as the format
// hint, learns the backend-negotiated format, and constructs an
// InputSource sized to bufferFrames at that format. Pass 0 for
// bufferFrames to default to 8× the negotiated callback size, which
// is what real systems need to absorb mix-goroutine scheduling
// jitter without dropping mic samples — measured against macOS +
// Bluetooth A2DP outputs, anything tighter drops a meaningful
// fraction of input. Unlike the mixer's live cap, the input ring is
// allocated up front and can't auto-grow, so we lean conservative.
// The returned capture owns both lifecycles — Close it once when
// finished.
func OpenInput(sys *devices.System, dev devices.Device, hint devices.StreamFormat, bufferFrames int) (*InputCapture, error) {
	c := &InputCapture{}
	// Late-bind the input callback: NewInputSource needs the
	// negotiated format, which we only learn after OpenInput returns.
	// Stash a function pointer in an atomic so the closure is
	// race-free against the device thread once Start is called.
	var cb atomic.Pointer[func([]float64)]
	stream, err := sys.OpenInput(dev, hint, func(buf []float64) {
		if f := cb.Load(); f != nil {
			(*f)(buf)
		}
	})
	if err != nil {
		return nil, err
	}
	fmt := stream.Format()
	if bufferFrames <= 0 {
		bufferFrames = fmt.Frames * 8
		if bufferFrames <= 0 {
			// Backend reported no frame size — fall back to a small
			// fixed depth so NewInputSource doesn't reject zero.
			bufferFrames = 4096
		}
	}
	src, err := timeline.NewInputSource(fmt.SampleRate, fmt.Channels, bufferFrames)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	c.stream = stream
	c.InputSource = src
	f := src.Callback()
	cb.Store(&f)
	return c, nil
}

// Format reports the format the device negotiated.
func (c *InputCapture) Format() devices.StreamFormat { return c.stream.Format() }

// Start begins capture. Idempotent while running.
func (c *InputCapture) Start() error { return c.stream.Start() }

// Stop halts capture. Idempotent while stopped.
func (c *InputCapture) Stop() error { return c.stream.Stop() }

// Close stops the stream and closes the InputSource. Idempotent.
func (c *InputCapture) Close() error {
	err := c.stream.Close()
	if cerr := c.InputSource.Close(); err == nil {
		err = cerr
	}
	return err
}

// OutputCapture wraps a render-side devices.Stream so the producer
// (typically a mixer.Mixer.Fill) can be attached after the stream is
// opened. That ordering matters because mixers are usually constructed
// at the stream's negotiated format, which isn't known until Open
// returns. Until Bind is called the device callback writes silence.
type OutputCapture struct {
	stream devices.Stream
	cb     atomic.Pointer[func([]float64)]
}

// OpenOutput opens a render stream targeting dev with hint as the
// format hint. Use Format to learn the negotiated layout, build the
// producer at that format, then call Bind to attach it.
func OpenOutput(sys *devices.System, dev devices.Device, hint devices.StreamFormat) (*OutputCapture, error) {
	c := &OutputCapture{}
	stream, err := sys.OpenOutput(dev, hint, func(buf []float64) {
		if f := c.cb.Load(); f != nil {
			(*f)(buf)
			return
		}
		// Per the OutputCallback contract, the device buffer arrives
		// uncleared — explicitly zero it until a producer is bound.
		for i := range buf {
			buf[i] = 0
		}
	})
	if err != nil {
		return nil, err
	}
	c.stream = stream
	return c, nil
}

// Format reports the format the device negotiated.
func (c *OutputCapture) Format() devices.StreamFormat { return c.stream.Format() }

// Bind installs cb as the producer. Safe to call after Start; the
// switch is observed atomically by the device thread on its next
// callback.
func (c *OutputCapture) Bind(cb func([]float64)) {
	c.cb.Store(&cb)
}

// Start begins playback. Idempotent while running.
func (c *OutputCapture) Start() error { return c.stream.Start() }

// Stop halts playback. Idempotent while stopped.
func (c *OutputCapture) Stop() error { return c.stream.Stop() }

// Close stops the stream and releases backend resources. Idempotent.
func (c *OutputCapture) Close() error { return c.stream.Close() }
