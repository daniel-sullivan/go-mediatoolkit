//go:build windows

package devices

import (
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// notificationClient is a Go-implemented COM object satisfying
// IMMNotificationClient. The COM ABI requires that the first field be
// a pointer to a vtable of function pointers; we build one lazily via
// syscall.NewCallback so Windows can call back into Go on its own
// thread.
//
// Lifetime: a single notificationClient instance services one Watch
// call for its lifetime. There is no reference counting beyond the
// enumerator's register/unregister pair — AddRef returns 2 and Release
// returns 1 so Windows never believes it holds the only reference.
//
// Thread safety: callbacks run on arbitrary Windows threads; they
// publish events through sink.emit which takes the sink's own mutex.

// iMMNotificationClient is the virtual layout Windows consumes. Each
// method slot is a syscall.NewCallback(fn) uintptr.
type iMMNotificationClient struct {
	vtbl *iMMNotificationClientVtbl
}

type iMMNotificationClientVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr

	OnDeviceStateChanged   uintptr
	OnDeviceAdded          uintptr
	OnDeviceRemoved        uintptr
	OnDefaultDeviceChanged uintptr
	OnPropertyValueChanged uintptr
}

// notificationClient binds the COM vtable to a Go-side event sink.
type notificationClient struct {
	com  iMMNotificationClient // MUST be the first field — Windows passes &com.
	sink *eventSink
}

// eventSink is the Go-side fan-in for native callbacks. It owns the
// output channel; callers supply a resolver so the sink can look up
// current device details by endpoint ID when the OS only tells us an
// ID changed.
type eventSink struct {
	mu       sync.Mutex
	out      chan<- Event
	closed   atomic.Bool
	resolve  func(id string) (Device, bool)
	fallback func(id string, dir Direction) Device
}

// emit publishes an Event to the output channel if the sink is still
// open; otherwise the event is dropped silently (the watcher is tearing
// down).
func (s *eventSink) emit(ev Event) {
	if s.closed.Load() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return
	}
	// Non-blocking send; the consumer (System.watchLoop) is expected
	// to drain promptly. If it's slow, drop rather than deadlock the
	// COM notification thread.
	select {
	case s.out <- ev:
	default:
	}
}

// close disables further emits. Idempotent.
func (s *eventSink) close() {
	s.closed.Store(true)
}

// Shared callbacks — registered once on first use. Each receives the
// raw COM arguments Windows passes and dispatches through the
// notificationClient's sink.

var (
	notifCallbacksOnce sync.Once
	notifVtable        iMMNotificationClientVtbl
)

func initNotifCallbacks() {
	notifVtable = iMMNotificationClientVtbl{
		QueryInterface:         syscall.NewCallback(notifQueryInterface),
		AddRef:                 syscall.NewCallback(notifAddRef),
		Release:                syscall.NewCallback(notifRelease),
		OnDeviceStateChanged:   syscall.NewCallback(notifOnDeviceStateChanged),
		OnDeviceAdded:          syscall.NewCallback(notifOnDeviceAdded),
		OnDeviceRemoved:        syscall.NewCallback(notifOnDeviceRemoved),
		OnDefaultDeviceChanged: syscall.NewCallback(notifOnDefaultDeviceChanged),
		OnPropertyValueChanged: syscall.NewCallback(notifOnPropertyValueChanged),
	}
}

// newNotificationClient allocates a notificationClient whose vtable is
// wired to the shared callbacks. The returned pointer must stay live
// (pinned) for as long as the client is registered with the OS.
func newNotificationClient(sink *eventSink) *notificationClient {
	notifCallbacksOnce.Do(initNotifCallbacks)
	return &notificationClient{
		com:  iMMNotificationClient{vtbl: &notifVtable},
		sink: sink,
	}
}

// HRESULTs used from callbacks.
const (
	sOk          = 0
	eNoInterface = 0x80004002
	eUnexpected  = 0x8000FFFF
	ePointer     = 0x80004003
)

func clientFromThis(this uintptr) *notificationClient {
	return (*notificationClient)(unsafe.Pointer(this))
}

func notifQueryInterface(this uintptr, riid uintptr, ppv uintptr) uintptr {
	if ppv == 0 {
		return ePointer
	}
	// Accept IUnknown and IMMNotificationClient; everything else is
	// E_NOINTERFACE.
	var iidUnknown = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	wanted := (*windows.GUID)(unsafe.Pointer(riid))
	if wanted == nil {
		return ePointer
	}
	if *wanted == iidIMMNotificationClient || *wanted == iidUnknown {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		// No true refcount — just signal that we "added" one.
		return sOk
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	return eNoInterface
}

func notifAddRef(this uintptr) uintptr  { return 2 }
func notifRelease(this uintptr) uintptr { return 1 }

func notifOnDeviceStateChanged(this uintptr, pwstrDeviceId uintptr, dwNewState uintptr) uintptr {
	c := clientFromThis(this)
	if c == nil || c.sink == nil {
		return sOk
	}
	id := lpwstrToString(pwstrDeviceId)
	state := uint32(dwNewState)
	if state == deviceStateActive {
		if d, ok := c.sink.resolve(id); ok {
			c.sink.emit(Event{Kind: Added, Device: d})
		}
	} else {
		if d, ok := c.sink.resolve(id); ok {
			c.sink.emit(Event{Kind: Removed, Device: d})
		}
	}
	return sOk
}

func notifOnDeviceAdded(this uintptr, pwstrDeviceId uintptr) uintptr {
	c := clientFromThis(this)
	if c == nil || c.sink == nil {
		return sOk
	}
	id := lpwstrToString(pwstrDeviceId)
	if d, ok := c.sink.resolve(id); ok {
		c.sink.emit(Event{Kind: Added, Device: d})
	}
	return sOk
}

func notifOnDeviceRemoved(this uintptr, pwstrDeviceId uintptr) uintptr {
	c := clientFromThis(this)
	if c == nil || c.sink == nil {
		return sOk
	}
	id := lpwstrToString(pwstrDeviceId)
	// Removal: we can no longer resolve the device via COM; emit a
	// stub so callers get a Removed event with at least the ID.
	if d, ok := c.sink.resolve(id); ok {
		c.sink.emit(Event{Kind: Removed, Device: d})
	} else {
		c.sink.emit(Event{Kind: Removed, Device: Device{ID: id}})
	}
	return sOk
}

func notifOnDefaultDeviceChanged(this uintptr, flow uintptr, role uintptr, pwstrDefaultDeviceId uintptr) uintptr {
	c := clientFromThis(this)
	if c == nil || c.sink == nil {
		return sOk
	}
	// Only eConsole changes are reported as DefaultChanged; the other
	// roles (multimedia, communications) are collapsed onto the same
	// event kind to keep the public API simple.
	if uint32(role) != eConsole {
		return sOk
	}
	id := lpwstrToString(pwstrDefaultDeviceId)
	if id == "" {
		return sOk
	}
	if d, ok := c.sink.resolve(id); ok {
		d.IsDefault = true
		c.sink.emit(Event{Kind: DefaultChanged, Device: d})
		return sOk
	}
	dir := Output
	if uint32(flow) == eCapture {
		dir = Input
	}
	d := c.sink.fallback(id, dir)
	d.IsDefault = true
	c.sink.emit(Event{Kind: DefaultChanged, Device: d})
	return sOk
}

func notifOnPropertyValueChanged(this uintptr, pwstrDeviceId uintptr, key uintptr) uintptr {
	c := clientFromThis(this)
	if c == nil || c.sink == nil {
		return sOk
	}
	id := lpwstrToString(pwstrDeviceId)
	if d, ok := c.sink.resolve(id); ok {
		c.sink.emit(Event{Kind: PropertyChanged, Device: d})
	}
	return sOk
}

// lpwstrToString converts a raw LPWSTR uintptr argument to a Go
// string without freeing the underlying memory (Windows owns the
// lifetime of device-ID pointers passed to callbacks).
func lpwstrToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(p)))
}
