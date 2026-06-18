// Package events provides a typed pub-sub event bus.
//
// A Bus[T] carries events of a single concrete type T. Subscribers register
// a callback with Subscribe and receive every subsequent Publish call
// synchronously, in registration order. A callback that needs to block
// must spawn its own goroutine — the publisher waits for each callback
// to return before moving to the next subscriber.
//
// Bus[T] is safe for concurrent use. Subscribe and Cancel may be called
// from within a callback without deadlocking; new subscribers will not
// receive the in-flight event, and cancelled subscribers will not fire
// for subsequent events in the same Publish call.
//
// If a callback panics the panic is recovered and logged via the stdlib
// log package; remaining subscribers still run.
package events

import (
	"log"
	"sync"
	"sync/atomic"
)

// Bus is a typed pub-sub event bus.
type Bus[T any] struct {
	mu   sync.RWMutex
	subs []*Subscription[T]
}

// New returns an empty Bus.
func New[T any]() *Bus[T] {
	return &Bus[T]{}
}

// Subscribe registers cb to receive future events. Use the returned
// Subscription to unsubscribe.
func (b *Bus[T]) Subscribe(cb func(T)) *Subscription[T] {
	s := &Subscription[T]{bus: b, cb: cb}
	b.mu.Lock()
	b.subs = append(b.subs, s)
	b.mu.Unlock()
	return s
}

// Publish delivers evt to every active subscriber in registration order.
func (b *Bus[T]) Publish(evt T) {
	b.mu.RLock()
	snapshot := make([]*Subscription[T], len(b.subs))
	copy(snapshot, b.subs)
	b.mu.RUnlock()

	for _, s := range snapshot {
		s.deliver(evt)
	}
}

// Subscription represents a registered callback.
type Subscription[T any] struct {
	bus       *Bus[T]
	cb        func(T)
	cancelled atomic.Bool
}

// Cancel unsubscribes. After Cancel returns, no further events will be
// delivered to this subscription. Cancel is idempotent and safe to call
// from within a callback.
func (s *Subscription[T]) Cancel() {
	if !s.cancelled.CompareAndSwap(false, true) {
		return
	}
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	for i, x := range s.bus.subs {
		if x == s {
			s.bus.subs = append(s.bus.subs[:i], s.bus.subs[i+1:]...)
			return
		}
	}
}

func (s *Subscription[T]) deliver(evt T) {
	if s.cancelled.Load() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("events: subscriber panicked: %v", r)
		}
	}()
	s.cb(evt)
}
