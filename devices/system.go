package devices

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/events"
)

// defaultPollInterval is how often the polling fallback asks the
// backend for a fresh device list when native Watch is unavailable.
const defaultPollInterval = 2 * time.Second

// System is the process-wide view of OS audio devices. Obtain it via
// GetSystem.
type System struct {
	backend      Backend
	bus          *events.Bus[Event]
	pollInterval time.Duration

	mu    sync.RWMutex
	cache map[string]Device

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var (
	systemOnce sync.Once
	systemInst *System
	systemErr  error
)

// GetSystem returns the process-wide System. The first call constructs
// it by initialising the platform backend and loading the initial
// device list; subsequent calls return the same instance (and, if
// construction failed, the same error).
//
// An error from GetSystem means no device enumeration is available on
// this platform; retrying will not help.
func GetSystem() (*System, error) {
	systemOnce.Do(func() {
		backend, err := newPlatformBackend()
		if err != nil {
			systemErr = err
			return
		}
		systemInst, systemErr = newSystem(backend)
	})
	return systemInst, systemErr
}

// Option configures a System during construction.
type Option func(*System)

// WithPollInterval overrides the polling-fallback interval used when
// the backend does not support native Watch. Intended for tests.
func WithPollInterval(d time.Duration) Option {
	return func(s *System) { s.pollInterval = d }
}

// newSystem is the test-facing constructor.
func newSystem(backend Backend, opts ...Option) (*System, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &System{
		backend:      backend,
		bus:          events.New[Event](),
		pollInterval: defaultPollInterval,
		cache:        make(map[string]Device),
		cancel:       cancel,
	}
	for _, opt := range opts {
		opt(s)
	}

	initial, err := backend.List(ctx)
	if err != nil {
		cancel()
		_ = backend.Close()
		return nil, err
	}
	for _, d := range initial {
		s.cache[d.ID] = d
	}

	s.wg.Add(1)
	go s.watchLoop(ctx)

	return s, nil
}

// Snapshot atomically captures the current device list and subscribes
// cb to future events. No event occurring after the snapshot is missed,
// and no change already reflected in the snapshot is redelivered.
//
// cb runs on the publisher's goroutine; callbacks that need to block —
// or that need to call back into System — must spawn their own
// goroutine. Calling System methods from within cb on the same
// goroutine will deadlock.
//
// Call Cancel on the returned Subscription to stop receiving events.
func (s *System) Snapshot(cb func(Event)) ([]Device, *events.Subscription[Event]) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Device, 0, len(s.cache))
	for _, d := range s.cache {
		out = append(out, d)
	}
	sub := s.bus.Subscribe(cb)
	return out, sub
}

// List returns the current device set without subscribing to events.
// Prefer Snapshot when you also plan to listen for changes.
func (s *System) List() []Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Device, 0, len(s.cache))
	for _, d := range s.cache {
		out = append(out, d)
	}
	return out
}

// DefaultOutput returns the system default output device. The second
// return is false if the OS reports no default — typically because
// the host has no audio hardware or no output endpoint is enabled.
func (s *System) DefaultOutput() (Device, bool) {
	return s.defaultFor(Output)
}

// DefaultInput returns the system default input device. See
// DefaultOutput for the ok=false conditions.
func (s *System) DefaultInput() (Device, bool) {
	return s.defaultFor(Input)
}

// defaultFor scans the cached device set for an IsDefault entry
// matching dir. Used by DefaultOutput and DefaultInput.
func (s *System) defaultFor(dir Direction) (Device, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.cache {
		if d.IsDefault && d.Direction == dir {
			return d, true
		}
	}
	return Device{}, false
}

// Close stops the internal watcher and releases backend resources.
// After Close, Snapshot and List return empty results and further
// events are not delivered.
func (s *System) Close() error {
	s.cancel()
	s.wg.Wait()
	return s.backend.Close()
}

// applyEvent updates the cache and publishes the event atomically
// under s.mu so that Snapshot observes a consistent (cache, subscribe)
// pair.
func (s *System) applyEvent(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	d := ev.Device
	switch ev.Kind {
	case Added, PropertyChanged:
		s.cache[d.ID] = d
	case Removed:
		delete(s.cache, d.ID)
	case DefaultChanged:
		for id, existing := range s.cache {
			if existing.IsDefault && existing.Direction == d.Direction && id != d.ID {
				existing.IsDefault = false
				s.cache[id] = existing
			}
		}
		d.IsDefault = true
		s.cache[d.ID] = d
	}

	s.bus.Publish(ev)
}

// diffAndEmit compares current against the cache, publishes synthetic
// Added/Removed/DefaultChanged/PropertyChanged events for the difference,
// and updates the cache to match. Used by the polling fallback.
func (s *System) diffAndEmit(current []Device) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byID := make(map[string]Device, len(current))
	for _, d := range current {
		byID[d.ID] = d
	}

	for id, old := range s.cache {
		if _, ok := byID[id]; !ok {
			delete(s.cache, id)
			s.bus.Publish(Event{Kind: Removed, Device: old})
		}
	}

	for id, d := range byID {
		old, existed := s.cache[id]
		s.cache[id] = d
		if !existed {
			s.bus.Publish(Event{Kind: Added, Device: d})
			continue
		}
		if !old.IsDefault && d.IsDefault {
			s.bus.Publish(Event{Kind: DefaultChanged, Device: d})
			continue
		}
		if old.SampleRate != d.SampleRate || old.Channels != d.Channels || old.Name != d.Name {
			s.bus.Publish(Event{Kind: PropertyChanged, Device: d})
		}
	}
}

func (s *System) watchLoop(ctx context.Context) {
	defer s.wg.Done()

	ch, err := s.backend.Watch(ctx)
	if errors.Is(err, ErrNotSupported) {
		s.pollLoop(ctx)
		return
	}
	if err != nil {
		log.Printf("devices: backend watch failed: %v", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			s.applyEvent(ev)
		}
	}
}

func (s *System) pollLoop(ctx context.Context) {
	t := time.NewTicker(s.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			current, err := s.backend.List(ctx)
			if err != nil {
				log.Printf("devices: poll list failed: %v", err)
				continue
			}
			s.diffAndEmit(current)
		}
	}
}
