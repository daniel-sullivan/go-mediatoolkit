package hwaccel

import (
	"github.com/daniel-sullivan/go-mediatoolkit/events"
	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// HardwareFallbackEvent is published when Open* under PreferHardware
// fails to find a usable hardware backend and degrades to the software
// tier (or to nothing). It carries enough context for an application to
// surface the degradation — a silent CPU-melting software fallback in
// an NVR is an operational trap, so the framework makes the fallback
// observable both here and via a loud log.
//
// No event is published under RequireHardware (which errors instead of
// falling back) or SoftwareOnly (which never attempts hardware).
type HardwareFallbackEvent struct {
	// Codec and Direction identify what was being opened.
	Codec     video.Codec
	Direction Direction
	// Mode is the policy mode in effect (always PreferHardware for a
	// published event).
	Mode Mode
	// Attempted lists, in order, the names of the hardware backends
	// that were tried.
	Attempted []string
	// Reasons maps each attempted backend name to why it was rejected
	// (unavailable, codec unsupported, or a construction error).
	Reasons map[string]error
	// FellBackTo is "software" if a software encoder/decoder was
	// successfully constructed, or "" if even that failed (in which
	// case Open* also returns ErrNoBackend).
	FellBackTo string
}

// NewFallbackBus returns a fresh, empty event bus for fallback notices.
// Pass it to a Policy to receive HardwareFallbackEvents; callers
// Subscribe before the first Open*.
func NewFallbackBus() *events.Bus[HardwareFallbackEvent] {
	return events.New[HardwareFallbackEvent]()
}
