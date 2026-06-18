//go:build darwin && cgo

// This file implements native hotplug for the darwin backend using
// CoreAudio property listener blocks. We subscribe to three system
// object properties — the device list, the default output device and
// the default input device — and on any change re-enumerate via List
// and diff the result against a backend-owned snapshot. The diff
// produces typed Added / Removed / DefaultChanged / PropertyChanged
// events which are published on the channel returned by Watch.

package devices

/*
#cgo LDFLAGS: -framework CoreAudio

#include <CoreAudio/AudioHardware.h>
#include <stdint.h>
#include <Block.h>

// goAudioPropertyChanged is implemented in Go via //export. It is
// invoked once per property address carried on a listener fire.
extern void goAudioPropertyChanged(uintptr_t handle, uint32_t selector);

// addListenerBlockC installs a property listener block for addr on obj,
// associating the block with the Go-side watcher identified by handle.
// The returned void* is a Block_copy-retained pointer that must be
// passed back to removeListenerBlockC to uninstall the listener; it is
// opaque to Go. The dispatch queue is NULL, which tells CoreAudio to
// invoke the block on an internal dispatch queue — do not do heavy
// work inside the block.
static void *addListenerBlockC(AudioObjectID obj, AudioObjectPropertyAddress addr, uintptr_t handle, OSStatus *status) {
    AudioObjectPropertyListenerBlock block = ^(UInt32 n, const AudioObjectPropertyAddress *addrs) {
        for (UInt32 i = 0; i < n; i++) {
            goAudioPropertyChanged(handle, addrs[i].mSelector);
        }
    };
    // AudioObjectAddPropertyListenerBlock copies the block internally,
    // but AudioObjectRemovePropertyListenerBlock requires us to pass the
    // exact same block pointer we originally handed in. Keep our own
    // Block_copy so we own a stable heap-resident pointer.
    void *retained = Block_copy(block);
    *status = AudioObjectAddPropertyListenerBlock(obj, &addr, NULL, (AudioObjectPropertyListenerBlock)retained);
    if (*status != noErr) {
        Block_release(retained);
        return NULL;
    }
    return retained;
}

// removeListenerBlockC tears down a listener previously installed by
// addListenerBlockC. The caller must pass the same opaque pointer
// addListenerBlockC returned. After the call, the block is released.
static OSStatus removeListenerBlockC(AudioObjectID obj, AudioObjectPropertyAddress addr, void *retained) {
    if (retained == NULL) {
        return noErr;
    }
    OSStatus rc = AudioObjectRemovePropertyListenerBlock(obj, &addr, NULL, (AudioObjectPropertyListenerBlock)retained);
    Block_release(retained);
    return rc;
}
*/
import "C"

import (
	"context"
	"errors"
	"log"
	"sync"
	"unsafe"
)

// darwinListenerSelectors names the three property selectors we
// subscribe to on kAudioObjectSystemObject. Any change on any of them
// triggers a full re-enumeration and diff.
var darwinListenerSelectors = []uint32{
	kAudioHardwarePropertyDevices,
	kAudioHardwarePropertyDefaultOutputDevice,
	kAudioHardwarePropertyDefaultInputDevice,
}

// darwinWatcher owns one active Watch subscription. Lifetime is:
// allocate in Watch, register, install C listeners, run goroutine
// until ctx cancels, deregister, uninstall C listeners, close output.
type darwinWatcher struct {
	backend *darwinBackend
	ctx     context.Context
	out     chan Event

	// dirty is a size-1 buffered channel. The C callback goroutine
	// performs a non-blocking send; the watch loop drains it and
	// re-enumerates. Coalescing is intentional: listener fires are
	// frequent during a single user-visible event (e.g. plugging a
	// USB device often triggers three separate property changes) and
	// one diff pass is enough to capture the net effect.
	dirty chan struct{}

	mu       sync.Mutex
	snapshot map[darwinDeviceKey]Device

	// retained holds Block_copy'd pointers for each installed listener,
	// indexed the same way as darwinListenerSelectors. Needed because
	// AudioObjectRemovePropertyListenerBlock requires the exact block
	// pointer originally installed.
	retained []unsafe.Pointer
}

// darwinDeviceKey uniquely identifies a device within a watcher
// snapshot. Device.ID alone is not sufficient: darwin emits the same
// UID for aggregate devices that expose both input and output streams.
type darwinDeviceKey struct {
	ID        string
	Direction Direction
}

var (
	handleMu     sync.Mutex
	handleNextID uintptr
	handleMap    = map[uintptr]*darwinWatcher{}
)

// registerWatcher stores w in the global map and returns the handle
// that the C callback will pass back through goAudioPropertyChanged.
func registerWatcher(w *darwinWatcher) uintptr {
	handleMu.Lock()
	defer handleMu.Unlock()
	handleNextID++
	h := handleNextID
	handleMap[h] = w
	return h
}

// unregisterWatcher removes h from the map. Safe to call even if h is
// absent (e.g. if Watch failed partway through installation).
func unregisterWatcher(h uintptr) {
	handleMu.Lock()
	defer handleMu.Unlock()
	delete(handleMap, h)
}

// lookupWatcher fetches the watcher for h, or nil if the watcher has
// already been unregistered. The C callback may still fire briefly
// after unregisterWatcher because CoreAudio processes pending listener
// invocations asynchronously on its dispatch queue.
func lookupWatcher(h uintptr) *darwinWatcher {
	handleMu.Lock()
	defer handleMu.Unlock()
	return handleMap[h]
}

// Watch installs CoreAudio property listeners on the system object and
// returns a channel that receives typed Event values until ctx is
// cancelled. On cancellation the listeners are removed and the channel
// is closed. Errors from ListeningInstallation are returned eagerly
// and the channel is not created.
func (b *darwinBackend) Watch(ctx context.Context) (<-chan Event, error) {
	initial, err := b.List(ctx)
	if err != nil {
		return nil, err
	}

	w := &darwinWatcher{
		backend:  b,
		ctx:      ctx,
		out:      make(chan Event, 16),
		dirty:    make(chan struct{}, 1),
		snapshot: snapshotFromDevices(initial),
		retained: make([]unsafe.Pointer, len(darwinListenerSelectors)),
	}

	handle := registerWatcher(w)
	if err := w.installListeners(handle); err != nil {
		w.uninstallListeners()
		unregisterWatcher(handle)
		return nil, err
	}

	go w.run(handle)
	return w.out, nil
}

// installListeners subscribes to each selector in darwinListenerSelectors
// on the system object. Any failure leaves partially-installed listeners
// in retained so the caller (Watch) can clean up via uninstallListeners.
func (w *darwinWatcher) installListeners(handle uintptr) error {
	for i, sel := range darwinListenerSelectors {
		addr := C.AudioObjectPropertyAddress{
			mSelector: C.AudioObjectPropertySelector(sel),
			mScope:    C.AudioObjectPropertyScope(kAudioObjectPropertyScopeGlobal),
			mElement:  C.AudioObjectPropertyElement(kAudioObjectPropertyElementMain),
		}
		var status C.OSStatus
		retained := C.addListenerBlockC(
			C.AudioObjectID(kAudioObjectSystemObject),
			addr,
			C.uintptr_t(handle),
			&status,
		)
		if status != 0 {
			return errors.New("devices: AudioObjectAddPropertyListenerBlock failed")
		}
		w.retained[i] = retained
	}
	return nil
}

// uninstallListeners removes every listener recorded in retained,
// tolerating nil entries for selectors that were never installed or
// already cleaned up. Errors from CoreAudio are logged but not returned
// because shutdown must proceed regardless.
func (w *darwinWatcher) uninstallListeners() {
	for i, sel := range darwinListenerSelectors {
		if w.retained[i] == nil {
			continue
		}
		addr := C.AudioObjectPropertyAddress{
			mSelector: C.AudioObjectPropertySelector(sel),
			mScope:    C.AudioObjectPropertyScope(kAudioObjectPropertyScopeGlobal),
			mElement:  C.AudioObjectPropertyElement(kAudioObjectPropertyElementMain),
		}
		if rc := C.removeListenerBlockC(
			C.AudioObjectID(kAudioObjectSystemObject),
			addr,
			w.retained[i],
		); rc != 0 {
			log.Printf("devices: AudioObjectRemovePropertyListenerBlock failed: %d", int32(rc))
		}
		w.retained[i] = nil
	}
}

// run is the watcher goroutine. It consumes dirty pulses, re-enumerates
// devices, emits diff events, and exits on ctx cancellation.
func (w *darwinWatcher) run(handle uintptr) {
	defer close(w.out)
	defer unregisterWatcher(handle)
	defer w.uninstallListeners()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.dirty:
			w.refresh()
		}
	}
}

// refresh re-lists devices and publishes diff events. Errors from List
// are logged; the snapshot is left untouched so a later successful
// listing can recover the true state.
func (w *darwinWatcher) refresh() {
	current, err := w.backend.List(w.ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("devices: darwin refresh list failed: %v", err)
		}
		return
	}
	w.mu.Lock()
	events, next := diffDevices(w.snapshot, current)
	w.snapshot = next
	w.mu.Unlock()
	for _, ev := range events {
		select {
		case w.out <- ev:
		case <-w.ctx.Done():
			return
		}
	}
}

// snapshotFromDevices builds a darwinDeviceKey->Device map from a slice.
// Extracted so the diff logic can be unit-tested without touching the
// backend.
func snapshotFromDevices(devs []Device) map[darwinDeviceKey]Device {
	out := make(map[darwinDeviceKey]Device, len(devs))
	for _, d := range devs {
		out[darwinDeviceKey{ID: d.ID, Direction: d.Direction}] = d
	}
	return out
}

// diffDevices compares prev against cur and returns the sequence of
// Events needed to turn prev into cur, plus cur as a snapshot map. The
// emission order is: Removed, then Added, then DefaultChanged, then
// PropertyChanged — matching the conceptual order a subscriber would
// expect (gone devices first, then new arrivals, then default moves,
// finally in-place mutations).
//
// diffDevices deliberately duplicates the classification logic from
// System.diffAndEmit rather than reusing it, because the polling path
// owns System's cache under System.mu and mixes diff + publish + cache
// update atomically. Native Watch owns a separate snapshot under the
// watcher's mutex and only emits events; System.applyEvent merges them
// into its cache on receipt. Keeping the two diff sites independent
// preserves that boundary.
func diffDevices(prev map[darwinDeviceKey]Device, cur []Device) ([]Event, map[darwinDeviceKey]Device) {
	next := snapshotFromDevices(cur)

	var removed, added, defaultChanged, propChanged []Event
	for key, old := range prev {
		if _, ok := next[key]; !ok {
			removed = append(removed, Event{Kind: Removed, Device: old})
		}
	}
	for key, d := range next {
		old, existed := prev[key]
		if !existed {
			added = append(added, Event{Kind: Added, Device: d})
			continue
		}
		if !old.IsDefault && d.IsDefault {
			defaultChanged = append(defaultChanged, Event{Kind: DefaultChanged, Device: d})
			continue
		}
		if old.SampleRate != d.SampleRate || old.Channels != d.Channels || old.Name != d.Name {
			propChanged = append(propChanged, Event{Kind: PropertyChanged, Device: d})
		}
	}

	out := make([]Event, 0, len(removed)+len(added)+len(defaultChanged)+len(propChanged))
	out = append(out, removed...)
	out = append(out, added...)
	out = append(out, defaultChanged...)
	out = append(out, propChanged...)
	return out, next
}

//export goAudioPropertyChanged
func goAudioPropertyChanged(handle C.uintptr_t, selector C.uint32_t) {
	w := lookupWatcher(uintptr(handle))
	if w == nil {
		// Watcher was torn down; a late callback slipped through.
		return
	}
	// Non-blocking nudge: the watch goroutine only needs one pulse to
	// do a full re-list. Coalescing multiple rapid fires into one diff
	// is the correct behaviour, not a lost update.
	select {
	case w.dirty <- struct{}{}:
	default:
	}
}
