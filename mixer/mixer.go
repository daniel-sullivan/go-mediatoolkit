// Package mixer combines several timeline.Source streams into a single
// audio output stream suitable for driving a devices.Stream callback.
//
// # Data flow
//
// Callers add Sources as tracks via AddSource. A dedicated mix
// goroutine runs ahead of real time, pulling each track's samples in
// fixed-size chunks, summing them with per-track gain, soft-saturating
// the result, and writing into an SPSC buffers.Ring. The output side
// — typically a devices.Stream output callback — reads from the ring
// in a pure memcpy via Fill. On underrun the ring returns fewer
// samples than requested; Fill zeros the remainder and atomically
// increments an underrun counter.
//
// This decoupling keeps hard-realtime work (the device callback) free
// of mixing, resampling, or allocation; all of that happens on the
// mix goroutine ahead of the callback. Latency is approximately
// ringFrames / sampleRate; shrink ringFrames (via Config) for tighter
// latency, at the cost of more frequent underrun risk.
//
// # Adaptation
//
// Tracks whose Source has a different sample rate or channel count
// are transparently wrapped with rate and channel adapters at
// AddSource time. Currently mono↔stereo channel adaptation is
// supported; other channel topologies return ErrUnsupportedChannels.
//
// # Concurrency
//
// AddSource, the TrackHandle methods, and Close are safe to call from
// any goroutine. Fill is expected to have a single consumer (the
// output device callback).
//
// # Device integration
//
// The mixer does not import devices/ directly. Wire the Fill method
// as the OutputCallback:
//
//	mx, _ := mixer.New(mixer.Config{SampleRate: 48000, Channels: 2})
//	stream, _ := system.OpenOutput(dev, format, mx.Fill)
//	stream.Start()
package mixer

import (
	"sync"
	"sync/atomic"
	"time"

	hpt "github.com/daniel-sullivan/go-hpt"

	"github.com/daniel-sullivan/go-mediatoolkit/buffers"
	"github.com/daniel-sullivan/go-mediatoolkit/events"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

// Config configures a Mixer. SampleRate and Channels are required;
// other fields have sensible defaults.
type Config struct {
	// SampleRate is the output sample rate in Hz. Tracks with a
	// different rate are resampled.
	SampleRate int

	// Channels is the output channel count. Tracks with a different
	// count are wrapped with a channel adapter (mono↔stereo
	// supported).
	Channels int

	// RingFrames is the size of the mixer→callback ring in frames.
	// Zero picks a default of ~200ms at SampleRate. Larger rings
	// mean more latency but greater jitter tolerance.
	RingFrames int

	// ChunkFrames is how many frames the mix goroutine processes in
	// one iteration. Zero picks a default of ~10ms at SampleRate.
	ChunkFrames int

	// IdleTick is how long the mix goroutine waits when the output
	// ring is full before checking again. Zero picks a default of
	// 1ms. The wait uses github.com/daniel-sullivan/go-hpt so the
	// actual resolution is sub-millisecond on Linux and macOS — not
	// the 1-15ms the stdlib time package typically delivers. This
	// matters for low-latency rings where a few ms of jitter would
	// cause underruns.
	IdleTick time.Duration

	// LiveRingFrames is the floor for the live-source pre-roll cap.
	// Live sources (microphone input, looping real-time feeds) cannot
	// be consumed faster than wall clock, so letting the mixer pre-
	// roll 200ms of output would stall it on empty live-source reads.
	// A tight cap keeps the mixer near real time.
	//
	// The mixer auto-grows the effective cap based on the largest
	// Fill it has observed: the runtime cap is
	// max(LiveRingFrames, observedCallbackFrames + ChunkFrames*2).
	// That means callers can set LiveRingFrames to the smallest value
	// that suits their use case and the mixer will widen it
	// automatically when a backend hands it larger callback buffers
	// (BT outputs typically negotiate 1024–2048 frames vs
	// CoreAudio's 256-frame default). Zero picks 4× ChunkFrames as
	// the floor.
	LiveRingFrames int

	// Saturator is applied to every mixed sample before it enters
	// the output ring. Nil selects mutations.SoftSaturate. Other
	// built-in options are mutations.HardClip and
	// mutations.TanhSaturate; callers may also supply their own
	// mutations.Saturator function.
	Saturator mutations.Saturator
}

// Mixer combines multiple Sources into one output stream.
type Mixer struct {
	sampleRate  int
	channels    int
	chunkFrames int
	idleTick    time.Duration

	ring *buffers.Ring

	liveRingSamples int // fill cap when any live source is registered
	saturator       mutations.Saturator

	pending chan mixerOp
	stop    chan struct{}
	done    chan struct{}

	tracks     []*track // owned by run()
	liveTracks int      // owned by run(); count of tracks with Live() == true

	nextID                  atomic.Uint64
	underruns               atomic.Uint64
	observedCallbackSamples atomic.Int64
	bus                     *events.Bus[Event]

	mu      sync.Mutex
	closed  bool
	started bool
}

// New returns a running Mixer. The mix goroutine is launched
// immediately; call Close to stop it.
func New(cfg Config) (*Mixer, error) {
	if cfg.SampleRate <= 0 {
		return nil, ErrBadSampleRate
	}
	if cfg.Channels <= 0 {
		return nil, ErrBadChannels
	}
	if cfg.ChunkFrames <= 0 {
		cfg.ChunkFrames = cfg.SampleRate / 100 // ~10ms
	}
	if cfg.RingFrames <= 0 {
		cfg.RingFrames = cfg.SampleRate / 5 // ~200ms
	}
	if cfg.RingFrames < cfg.ChunkFrames*2 {
		cfg.RingFrames = cfg.ChunkFrames * 2
	}
	if cfg.IdleTick <= 0 {
		cfg.IdleTick = time.Millisecond
	}
	if cfg.LiveRingFrames <= 0 {
		cfg.LiveRingFrames = cfg.ChunkFrames * 4
	}
	if cfg.Saturator == nil {
		cfg.Saturator = mutations.SoftSaturate
	}
	m := &Mixer{
		sampleRate:      cfg.SampleRate,
		channels:        cfg.Channels,
		chunkFrames:     cfg.ChunkFrames,
		idleTick:        cfg.IdleTick,
		liveRingSamples: cfg.LiveRingFrames * cfg.Channels,
		saturator:       cfg.Saturator,
		ring:            buffers.NewRing(cfg.RingFrames * cfg.Channels),
		pending:         make(chan mixerOp, 32),
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
		bus:             events.New[Event](),
	}
	m.started = true
	go m.run()
	return m, nil
}

// SampleRate reports the mixer's output sample rate.
func (m *Mixer) SampleRate() int { return m.sampleRate }

// Channels reports the mixer's output channel count.
func (m *Mixer) Channels() int { return m.channels }

// Events returns a bus emitting Mixer-scoped events (underruns, track
// lifecycle). Per-track completion is also observable via
// TrackHandle.Done() — use whichever is ergonomic.
func (m *Mixer) Events() *events.Bus[Event] { return m.bus }

// Underruns returns the cumulative count of output callback
// invocations that received fewer samples than requested.
func (m *Mixer) Underruns() uint64 { return m.underruns.Load() }

// AddSource installs src as a new track. The returned TrackHandle
// lets the caller adjust gain, remove the track, or wait for it to
// EOF. The Source is wrapped with rate and channel adapters if its
// format differs from the mixer's.
func (m *Mixer) AddSource(src timeline.Source) (TrackHandle, error) {
	if src == nil {
		return nil, ErrNilSource
	}
	adapted, err := adaptSource(src, m.sampleRate, m.channels)
	if err != nil {
		return nil, err
	}
	id := m.nextID.Add(1)
	h := newTrackHandle(id, m)
	t := &track{
		id:     id,
		source: adapted,
		gain:   1.0,
		handle: h,
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrMixerClosed
	}
	m.mu.Unlock()
	select {
	case m.pending <- addTrackOp{t: t}:
	case <-m.stop:
		return nil, ErrMixerClosed
	}
	return h, nil
}

// Fill writes up to len(buf) mixed samples into buf, suitable for use
// as a devices.OutputCallback. The length of buf must be a whole
// number of frames (i.e. a multiple of Channels). Samples not
// satisfied by the ring are zeroed and counted as an underrun.
//
// Fill also records len(buf) as a monotonic high-water mark so the
// mix goroutine can grow the live-source pre-roll cap to suit the
// actual device callback size — see Config.LiveRingFrames.
func (m *Mixer) Fill(buf []float64) {
	if len(buf)%m.channels != 0 {
		// Nothing sensible to do — zero the whole buffer.
		for i := range buf {
			buf[i] = 0
		}
		return
	}
	if observed := int64(len(buf)); observed > m.observedCallbackSamples.Load() {
		m.observedCallbackSamples.Store(observed)
	}
	n := m.ring.Read(buf)
	for i := n; i < len(buf); i++ {
		buf[i] = 0
	}
	if n < len(buf) {
		m.underruns.Add(1)
		m.bus.Publish(Event{Kind: EventUnderrun, SilenceSamples: len(buf) - n})
	}
}

// Close stops the mix goroutine and releases resources. Existing
// TrackHandles have their Done channels closed. Idempotent.
func (m *Mixer) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()
	close(m.stop)
	<-m.done
	return nil
}

// run is the mix goroutine. It owns the tracks slice and all
// per-track state.
func (m *Mixer) run() {
	defer close(m.done)
	scratch := make([]float64, m.chunkFrames*m.channels)
	accum := make([]float64, m.chunkFrames*m.channels)
	chunkSamples := m.chunkFrames * m.channels

	for {
		// Drain all pending ops non-blocking.
		if !m.drainOps() {
			return
		}

		// If the ring is full, wait for space or an op. When any
		// live source is registered the effective cap auto-grows
		// from observed callback size so the ring always covers one
		// device drain plus a chunk of jitter headroom — keeps the
		// mix close to wall-clock for the wired-output case while
		// remaining safe for backends (BT, etc.) that hand us
		// kilo-frame buffers.
		ringLen := m.ring.Len()
		free := m.ring.Cap() - ringLen
		atCap := free < chunkSamples
		if !atCap && m.liveTracks > 0 {
			liveCap := m.liveRingSamples
			if obs := int(m.observedCallbackSamples.Load()); obs > 0 {
				needed := obs + 2*chunkSamples
				if needed > liveCap {
					liveCap = needed
				}
			}
			if ringLen >= liveCap {
				atCap = true
			}
		}
		if atCap {
			select {
			case op := <-m.pending:
				op.apply(m)
			case <-m.stop:
				m.finalize()
				return
			case <-hpt.After(m.idleTick):
			}
			continue
		}

		// Mix one chunk.
		for i := range accum {
			accum[i] = 0
		}
		kept := m.tracks[:0]
		for _, t := range m.tracks {
			if t.removed.Load() {
				// handle.finish() was called by removeTrackOp; just drop.
				continue
			}
			scratch = mutations.ResizeScratch(scratch, chunkSamples)
			n, err := t.source.Pull(scratch)
			if n > 0 {
				g := t.gain
				for i := 0; i < n; i++ {
					accum[i] += scratch[i] * g
				}
			}
			if err != nil {
				t.handle.finish()
				if t.source.Live() {
					m.liveTracks--
				}
				m.bus.Publish(Event{Kind: EventTrackEnded, TrackID: t.id})
				continue
			}
			kept = append(kept, t)
		}
		m.tracks = kept

		mutations.ApplySaturator(accum, m.saturator)

		// Write to ring.
		m.ring.Write(accum)
	}
}

// drainOps non-blockingly applies all queued ops. Returns false if
// stop was signalled.
func (m *Mixer) drainOps() bool {
	for {
		select {
		case op := <-m.pending:
			op.apply(m)
		case <-m.stop:
			m.finalize()
			return false
		default:
			return true
		}
	}
}

// finalize finishes all remaining track handles on close.
func (m *Mixer) finalize() {
	for _, t := range m.tracks {
		t.handle.finish()
	}
	m.tracks = nil
	// Drain any remaining pending ops so their handles resolve.
	for {
		select {
		case op := <-m.pending:
			if at, ok := op.(addTrackOp); ok {
				at.t.handle.finish()
			}
		default:
			return
		}
	}
}
