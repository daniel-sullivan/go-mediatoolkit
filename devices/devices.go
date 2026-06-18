// Package devices provides cross-platform enumeration of, and hotplug
// notifications for, OS audio devices.
//
// Obtain the process-wide System via GetSystem. Callers retrieve the
// current device list and subscribe to change events atomically via
// System.Snapshot. Event delivery is callback-based and synchronous;
// callbacks that need to block must spawn their own goroutine.
//
// Backends per platform:
//
//   - linux:   PulseAudio via native protocol (no CGo)
//   - darwin:  CoreAudio via purego dlopen (no CGo); hotplug is polled
//     unless built with CGO_ENABLED=1, in which case property
//     listeners are used automatically.
//   - windows: WASAPI via golang.org/x/sys/windows COM (no CGo)
//
// System is safe for concurrent use.
package devices

// Direction distinguishes audio output (playback) from input (capture).
type Direction int

const (
	Output Direction = iota + 1
	Input
)

// String returns "output" or "input".
func (d Direction) String() string {
	switch d {
	case Output:
		return "output"
	case Input:
		return "input"
	}
	return "unknown"
}

// Device describes an OS audio endpoint at a point in time.
//
// ID is an opaque platform-specific string suitable for equality
// comparison and for passing back to platform APIs (e.g. WASAPI's
// endpoint ID, CoreAudio's UID, PulseAudio's sink name). IDs are
// stable for the lifetime of a device on a given host but are not
// portable across hosts.
type Device struct {
	ID         string
	Name       string
	Direction  Direction
	IsDefault  bool
	SampleRate int // native rate in Hz, 0 if unknown
	Channels   int // 0 if unknown
}

// EventKind classifies the change that produced an Event.
type EventKind int

const (
	// Added means a device appeared.
	Added EventKind = iota + 1
	// Removed means a device disappeared.
	Removed
	// DefaultChanged means the system default for this Direction changed;
	// Device is the new default.
	DefaultChanged
	// PropertyChanged means a device's non-identity properties changed
	// (e.g. sample rate, channel count). Device carries the new state.
	PropertyChanged
)

// String returns a human-readable label.
func (k EventKind) String() string {
	switch k {
	case Added:
		return "added"
	case Removed:
		return "removed"
	case DefaultChanged:
		return "default-changed"
	case PropertyChanged:
		return "property-changed"
	}
	return "unknown"
}

// Event describes a single device-state change.
type Event struct {
	Kind   EventKind
	Device Device
}
