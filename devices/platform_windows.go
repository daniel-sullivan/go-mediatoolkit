//go:build windows

package devices

import (
	"context"
	"runtime"
	"sync"
	"unsafe"
)

// Windows audio-device backend built on the MMDevice API and WASAPI.
//
// Concurrency model: all COM work runs on a single OS-locked goroutine
// so that the apartment initialised by CoInitializeEx(MULTITHREADED) is
// a consistent partner for the IMMDeviceEnumerator pointer we cache.
// Callers post closures through the comThread.

// newPlatformBackend constructs the Windows backend. It initialises
// COM on a dedicated goroutine and creates the MMDevice enumerator;
// any failure unwinds both.
func newPlatformBackend() (Backend, error) {
	t, err := newCOMThread()
	if err != nil {
		return nil, err
	}

	b := &wasapiBackend{com: t}

	var ferr error
	t.run(func() {
		ppv, err := coCreateInstance(&clsidMMDeviceEnumerator, clsctxInprocServer, &iidIMMDeviceEnumerator)
		if err != nil {
			ferr = err
			return
		}
		b.enumerator = (*iMMDeviceEnumerator)(ppv)
	})
	if ferr != nil {
		t.stop()
		return nil, ferr
	}
	return b, nil
}

// wasapiBackend implements Backend using the MMDevice COM API.
type wasapiBackend struct {
	com        *comThread
	enumerator *iMMDeviceEnumerator

	mu     sync.Mutex
	client *notificationClient
	closed bool
}

// List returns a snapshot of all active render and capture endpoints.
// Sample rate / channel count come from PKEY_AudioEngine_DeviceFormat;
// the default-flag is derived from GetDefaultAudioEndpoint(eConsole).
func (b *wasapiBackend) List(ctx context.Context) ([]Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []Device
	var ferr error
	b.com.run(func() {
		out, ferr = b.listOnCOMThread()
	})
	return out, ferr
}

// Watch registers an IMMNotificationClient and returns a channel that
// receives translated events. The returned channel is closed when ctx
// is cancelled (and any registered client unregistered).
func (b *wasapiBackend) Watch(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event, 32)
	sink := &eventSink{
		out:      ch,
		resolve:  b.resolveDevice,
		fallback: func(id string, dir Direction) Device { return Device{ID: id, Direction: dir} },
	}
	client := newNotificationClient(sink)

	var ferr error
	b.com.run(func() {
		if err := b.enumerator.RegisterEndpointNotificationCallback(unsafe.Pointer(&client.com)); err != nil {
			ferr = err
			return
		}
		b.mu.Lock()
		b.client = client
		b.mu.Unlock()
	})
	if ferr != nil {
		close(ch)
		return nil, ferr
	}

	go func() {
		<-ctx.Done()
		b.com.run(func() {
			b.mu.Lock()
			c := b.client
			b.client = nil
			b.mu.Unlock()
			if c != nil {
				_ = b.enumerator.UnregisterEndpointNotificationCallback(unsafe.Pointer(&c.com))
			}
			sink.close()
			close(ch)
		})
	}()

	return ch, nil
}

// Close stops the COM thread and releases the enumerator.
func (b *wasapiBackend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()

	b.com.run(func() {
		b.mu.Lock()
		c := b.client
		b.client = nil
		b.mu.Unlock()
		if c != nil {
			_ = b.enumerator.UnregisterEndpointNotificationCallback(unsafe.Pointer(&c.com))
		}
		if b.enumerator != nil {
			b.enumerator.Release()
			b.enumerator = nil
		}
	})
	b.com.stop()
	return nil
}

// resolveDevice fetches the current state of a single endpoint by ID.
// Used by the notification client to populate events with names and
// formats instead of bare IDs.
func (b *wasapiBackend) resolveDevice(id string) (Device, bool) {
	var dev Device
	var found bool
	b.com.run(func() {
		all, err := b.listOnCOMThread()
		if err != nil {
			return
		}
		for _, d := range all {
			if d.ID == id {
				dev = d
				found = true
				return
			}
		}
	})
	return dev, found
}

// listOnCOMThread performs the enumeration with the assumption that it
// already runs on the COM goroutine. It must not be called directly.
func (b *wasapiBackend) listOnCOMThread() ([]Device, error) {
	var out []Device
	for _, spec := range []struct {
		flow uint32
		dir  Direction
	}{{eRender, Output}, {eCapture, Input}} {
		defaultID, _ := b.defaultEndpointID(spec.flow)

		coll, err := b.enumerator.EnumAudioEndpoints(spec.flow, deviceStateActive)
		if err != nil {
			return nil, err
		}
		count, err := coll.GetCount()
		if err != nil {
			coll.Release()
			return nil, err
		}
		for i := uint32(0); i < count; i++ {
			dev, err := coll.Item(i)
			if err != nil {
				continue
			}
			d, ok := describeDevice(dev, spec.dir, defaultID)
			dev.Release()
			if ok {
				out = append(out, d)
			}
		}
		coll.Release()
	}
	return out, nil
}

// defaultEndpointID returns the console-role default endpoint ID for
// the given dataflow, or "" if there is no default device.
func (b *wasapiBackend) defaultEndpointID(flow uint32) (string, error) {
	dev, err := b.enumerator.GetDefaultAudioEndpoint(flow, eConsole)
	if err != nil {
		return "", err
	}
	defer dev.Release()
	return dev.GetId()
}

// describeDevice fills a Device from an IMMDevice. Failures on
// individual properties are swallowed: we return a best-effort record
// and rely on the caller to filter with ok=false if even the ID is
// missing.
func describeDevice(dev *iMMDevice, dir Direction, defaultID string) (Device, bool) {
	id, err := dev.GetId()
	if err != nil || id == "" {
		return Device{}, false
	}
	out := Device{
		ID:        id,
		Direction: dir,
		IsDefault: id == defaultID,
	}
	store, err := dev.OpenPropertyStore(stgmRead)
	if err != nil {
		return out, true
	}
	defer store.Release()

	var pv propVariant
	if err := store.GetValue(&pkeyDeviceFriendlyName, &pv); err == nil {
		if pv.vt == vtLPWSTR {
			out.Name = pv.lpwstr()
		}
		propVariantClear(&pv)
	}

	pv = propVariant{}
	if err := store.GetValue(&pkeyAudioEngineDeviceFormat, &pv); err == nil {
		if pv.vt == vtBlob {
			if ch, sr, ok := parseWaveformatex(pv.blob()); ok {
				out.Channels = ch
				out.SampleRate = sr
			}
		}
		propVariantClear(&pv)
	}

	return out, true
}

// -- COM goroutine pump -----------------------------------------------
//
// Windows COM apartments are thread-affine: a pointer obtained under
// CoInitializeEx(MULTITHREADED) on thread T must only be used from T
// (for STA) or any thread in the MTA. We conservatively funnel every
// call through a single locked goroutine so the enumerator pointer is
// always used from the same underlying OS thread that initialised it.

type comThread struct {
	fn   chan func()
	done chan struct{}

	mu      sync.Mutex
	stopped bool
}

func newCOMThread() (*comThread, error) {
	t := &comThread{
		fn:   make(chan func()),
		done: make(chan struct{}),
	}
	errCh := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		state, err := coInitialize()
		if err != nil {
			errCh <- err
			close(t.done)
			return
		}
		defer coUninitialize(state)

		close(errCh)
		for f := range t.fn {
			f()
		}
		close(t.done)
	}()
	if err := <-errCh; err != nil {
		return nil, err
	}
	return t, nil
}

// run submits f to the COM thread and blocks until it completes. If
// the thread has been stopped, run becomes a no-op so shutdown races
// cannot panic on send-to-closed-channel.
func (t *comThread) run(f func()) {
	done := make(chan struct{})
	job := func() {
		defer close(done)
		f()
	}
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	// Send while holding the mutex so a concurrent stop cannot close
	// t.fn between the stopped check and the send.
	t.fn <- job
	t.mu.Unlock()
	<-done
}

// stop closes the command channel and waits for the COM thread to
// uninitialise and exit. Idempotent.
func (t *comThread) stop() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		<-t.done
		return
	}
	t.stopped = true
	close(t.fn)
	t.mu.Unlock()
	<-t.done
}
