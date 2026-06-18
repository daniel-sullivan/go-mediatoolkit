package mixer

// EventKind enumerates mixer-scoped event types.
type EventKind int

const (
	// EventTrackAdded fires after the mix goroutine installs a new
	// track. TrackID is set.
	EventTrackAdded EventKind = iota

	// EventTrackRemoved fires when a track is removed via
	// TrackHandle.Remove. TrackID is set.
	EventTrackRemoved

	// EventTrackEnded fires when a track's Source EOFs naturally.
	// TrackID is set.
	EventTrackEnded

	// EventUnderrun fires when Fill writes fewer samples than were
	// requested. SilenceSamples reports how many samples were
	// zeroed in the callback.
	EventUnderrun
)

// Event is published on the Mixer's events.Bus. Field relevance
// depends on Kind; unused fields are zero.
type Event struct {
	Kind           EventKind
	TrackID        uint64
	SilenceSamples int
}
