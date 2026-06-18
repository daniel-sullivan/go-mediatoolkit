package devices

import (
	"context"
	"sync"
)

// fakeBackend is a test-only Backend that lets tests drive device
// state directly. It supports two modes:
//
//   - event mode (default): Add/Remove/SetDefault publish to the
//     channel returned by Watch, exercising the native-event path.
//   - poll mode (DisableWatch): Watch returns ErrNotSupported; state
//     mutations are only observable via subsequent List calls, which
//     exercises System's polling fallback.
type fakeBackend struct {
	mu           sync.Mutex
	devices      map[string]Device
	events       chan Event
	disableWatch bool
	listErr      error
	closed       bool
}

func newFakeBackend(initial ...Device) *fakeBackend {
	f := &fakeBackend{
		devices: make(map[string]Device, len(initial)),
		events:  make(chan Event, 64),
	}
	for _, d := range initial {
		f.devices[d.ID] = d
	}
	return f
}

func (f *fakeBackend) List(_ context.Context) ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]Device, 0, len(f.devices))
	for _, d := range f.devices {
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeBackend) Watch(_ context.Context) (<-chan Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.disableWatch {
		return nil, ErrNotSupported
	}
	return f.events, nil
}

func (f *fakeBackend) OpenOutput(_ Device, _ StreamFormat, _ OutputCallback) (Stream, error) {
	return nil, ErrNotSupported
}

func (f *fakeBackend) OpenInput(_ Device, _ StreamFormat, _ InputCallback) (Stream, error) {
	return nil, ErrNotSupported
}

func (f *fakeBackend) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.events)
	}
	return nil
}

// add mutates state and, in event mode, publishes an Added event.
func (f *fakeBackend) add(d Device) {
	f.mu.Lock()
	f.devices[d.ID] = d
	emit := !f.disableWatch && !f.closed
	f.mu.Unlock()
	if emit {
		f.events <- Event{Kind: Added, Device: d}
	}
}

// remove mutates state and, in event mode, publishes a Removed event.
func (f *fakeBackend) remove(id string) {
	f.mu.Lock()
	d, ok := f.devices[id]
	delete(f.devices, id)
	emit := ok && !f.disableWatch && !f.closed
	f.mu.Unlock()
	if emit {
		f.events <- Event{Kind: Removed, Device: d}
	}
}

// setDefault marks a single device as default for its direction and
// clears the flag on any other default in the same direction.
func (f *fakeBackend) setDefault(id string) {
	f.mu.Lock()
	target, ok := f.devices[id]
	if !ok {
		f.mu.Unlock()
		return
	}
	for did, d := range f.devices {
		if d.Direction == target.Direction && d.IsDefault && did != id {
			d.IsDefault = false
			f.devices[did] = d
		}
	}
	target.IsDefault = true
	f.devices[id] = target
	emit := !f.disableWatch && !f.closed
	f.mu.Unlock()
	if emit {
		f.events <- Event{Kind: DefaultChanged, Device: target}
	}
}

func (f *fakeBackend) setListErr(err error) {
	f.mu.Lock()
	f.listErr = err
	f.mu.Unlock()
}

func (f *fakeBackend) disableWatchMode() {
	f.mu.Lock()
	f.disableWatch = true
	f.mu.Unlock()
}
