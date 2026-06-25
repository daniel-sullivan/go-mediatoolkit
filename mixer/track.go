package mixer

import (
	"sync"
	"sync/atomic"

	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

// TrackHandle is the caller's view of a track installed on a Mixer.
// It exposes gain control, removal, and completion notification.
type TrackHandle interface {
	// ID uniquely identifies this track within the mixer for the
	// program lifetime.
	ID() uint64

	// SetGain adjusts the track's linear gain multiplier. 1.0 is
	// unity; 0.0 silences the track. Applied on the next mix
	// iteration.
	SetGain(g float64)

	// Gain returns the currently-applied linear gain.
	Gain() float64

	// Remove removes the track from the mixer. Subsequent Pulls
	// will not include its samples. Remove is idempotent; after the
	// mixer processes the removal, Done is closed.
	Remove()

	// Done returns a channel closed when the track is removed, its
	// Source EOFs, or the mixer is closed.
	Done() <-chan struct{}
}

// track is the mixer-goroutine-owned side of a scheduled track.
type track struct {
	id      uint64
	source  timeline.Source
	gain    float64
	removed atomic.Bool
	handle  *trackHandle
}

// trackHandle is returned to callers.
type trackHandle struct {
	id        uint64
	mixer     *Mixer
	gain      atomic.Uint64 // float64 bits; SetGain is the only writer the mixer reads
	done      chan struct{}
	closeOnce sync.Once
}

func newTrackHandle(id uint64, m *Mixer) *trackHandle {
	h := &trackHandle{
		id:    id,
		mixer: m,
		done:  make(chan struct{}),
	}
	h.gain.Store(math64FloatToBits(1.0))
	return h
}

func (h *trackHandle) ID() uint64 { return h.id }

func (h *trackHandle) SetGain(g float64) {
	h.gain.Store(math64FloatToBits(g))
	select {
	case h.mixer.pending <- setGainOp{id: h.id, gain: g}:
	case <-h.mixer.stop:
	}
}

func (h *trackHandle) Gain() float64 {
	return math64BitsToFloat(h.gain.Load())
}

func (h *trackHandle) Remove() {
	select {
	case h.mixer.pending <- removeTrackOp{id: h.id}:
	case <-h.mixer.stop:
	}
}

func (h *trackHandle) Done() <-chan struct{} { return h.done }

func (h *trackHandle) finish() {
	h.closeOnce.Do(func() { close(h.done) })
}

// mixerOp is an operation applied to the mixer's owned state by the
// mix goroutine.
type mixerOp interface{ apply(*Mixer) }

type addTrackOp struct{ t *track }

func (op addTrackOp) apply(m *Mixer) {
	m.tracks = append(m.tracks, op.t)
	if op.t.source.Live() {
		m.liveTracks++
	}
	m.bus.Publish(Event{Kind: EventTrackAdded, TrackID: op.t.id})
}

type setGainOp struct {
	id   uint64
	gain float64
}

func (op setGainOp) apply(m *Mixer) {
	for _, t := range m.tracks {
		if t.id == op.id {
			t.gain = op.gain
			return
		}
	}
}

type removeTrackOp struct{ id uint64 }

func (op removeTrackOp) apply(m *Mixer) {
	for _, t := range m.tracks {
		if t.id == op.id {
			t.removed.Store(true)
			t.handle.finish()
			if t.source.Live() {
				m.liveTracks--
			}
			m.bus.Publish(Event{Kind: EventTrackRemoved, TrackID: op.id})
			return
		}
	}
}

// The atomic.Uint64 approach avoids a lock on gain reads; we encode
// float64 in its IEEE-754 bit pattern.
func math64FloatToBits(f float64) uint64 { return floatBits(f) }
func math64BitsToFloat(u uint64) float64 { return bitsFloat(u) }
