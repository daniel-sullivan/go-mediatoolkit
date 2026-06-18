package devices

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func builtin() Device {
	return Device{ID: "builtin", Name: "Built-in Output", Direction: Output, IsDefault: true, SampleRate: 48000, Channels: 2}
}

func usb() Device {
	return Device{ID: "usb-1", Name: "USB Headset", Direction: Output, SampleRate: 44100, Channels: 2}
}

func newTestSystem(t *testing.T, fake *fakeBackend, opts ...Option) *System {
	t.Helper()
	sys, err := newSystem(fake, opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sys.Close() })
	return sys
}

func TestSystem_SnapshotReturnsInitialDevices(t *testing.T) {
	fake := newFakeBackend(builtin(), usb())
	sys := newTestSystem(t, fake)

	devs, sub := sys.Snapshot(func(Event) {})
	defer sub.Cancel()

	ids := []string{}
	for _, d := range devs {
		ids = append(ids, d.ID)
	}
	assert.ElementsMatch(t, []string{"builtin", "usb-1"}, ids)
}

func TestSystem_DefaultOutputAndInput(t *testing.T) {
	builtinOut := builtin()
	builtinMic := Device{ID: "mic", Name: "Built-in Mic", Direction: Input, IsDefault: true, SampleRate: 48000, Channels: 1}
	fake := newFakeBackend(builtinOut, builtinMic, usb())
	sys := newTestSystem(t, fake)

	out, ok := sys.DefaultOutput()
	require.True(t, ok)
	assert.Equal(t, "builtin", out.ID)

	in, ok := sys.DefaultInput()
	require.True(t, ok)
	assert.Equal(t, "mic", in.ID)
}

func TestSystem_DefaultReturnsFalseWhenAbsent(t *testing.T) {
	// Only an output default configured; no input exists at all.
	fake := newFakeBackend(builtin())
	sys := newTestSystem(t, fake)

	_, ok := sys.DefaultInput()
	assert.False(t, ok)

	out, ok := sys.DefaultOutput()
	require.True(t, ok)
	assert.Equal(t, "builtin", out.ID)
}

func TestSystem_InitPropagatesListError(t *testing.T) {
	fake := newFakeBackend()
	fake.setListErr(errors.New("boom"))

	_, err := newSystem(fake)
	require.Error(t, err)
}

func TestSystem_EventPathDeliversAddedAfterSnapshot(t *testing.T) {
	fake := newFakeBackend(builtin())
	sys := newTestSystem(t, fake)

	got := make(chan Event, 4)
	devs, sub := sys.Snapshot(func(e Event) { got <- e })
	defer sub.Cancel()
	require.Len(t, devs, 1)

	fake.add(usb())

	select {
	case ev := <-got:
		assert.Equal(t, Added, ev.Kind)
		assert.Equal(t, "usb-1", ev.Device.ID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Added event")
	}

	assert.Len(t, sys.List(), 2)
}

func TestSystem_EventPathDeliversRemoved(t *testing.T) {
	fake := newFakeBackend(builtin(), usb())
	sys := newTestSystem(t, fake)

	got := make(chan Event, 4)
	_, sub := sys.Snapshot(func(e Event) { got <- e })
	defer sub.Cancel()

	fake.remove("usb-1")

	select {
	case ev := <-got:
		assert.Equal(t, Removed, ev.Kind)
		assert.Equal(t, "usb-1", ev.Device.ID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Removed event")
	}

	assert.Len(t, sys.List(), 1)
}

func TestSystem_EventPathHandlesDefaultChangedAndClearsPriorDefault(t *testing.T) {
	fake := newFakeBackend(builtin(), usb())
	sys := newTestSystem(t, fake)

	got := make(chan Event, 4)
	_, sub := sys.Snapshot(func(e Event) { got <- e })
	defer sub.Cancel()

	fake.setDefault("usb-1")

	select {
	case ev := <-got:
		assert.Equal(t, DefaultChanged, ev.Kind)
		assert.Equal(t, "usb-1", ev.Device.ID)
		assert.True(t, ev.Device.IsDefault)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for DefaultChanged event")
	}

	list := sys.List()
	byID := map[string]Device{}
	for _, d := range list {
		byID[d.ID] = d
	}
	assert.False(t, byID["builtin"].IsDefault, "previous default should be cleared")
	assert.True(t, byID["usb-1"].IsDefault, "new default should be set")
}

func TestSystem_SnapshotAndSubscribeAreAtomic(t *testing.T) {
	// Publish a burst of events from a parallel goroutine while a reader
	// calls Snapshot. Assert that every event after the snapshot lands in
	// the subscription and nothing reflected in the snapshot is duplicated.
	fake := newFakeBackend(builtin())
	sys := newTestSystem(t, fake)

	const n = 50
	done := make(chan struct{})
	var received atomic.Int64

	go func() {
		for i := 0; i < n; i++ {
			fake.add(Device{ID: deviceID(i), Name: "d", Direction: Output})
		}
		close(done)
	}()

	// Give the publisher a head start so Snapshot lands mid-stream.
	time.Sleep(time.Millisecond)

	seen := map[string]bool{}
	var seenMu sync.Mutex
	devs, sub := sys.Snapshot(func(e Event) {
		if e.Kind != Added {
			return
		}
		seenMu.Lock()
		seen[e.Device.ID] = true
		seenMu.Unlock()
		received.Add(1)
	})
	defer sub.Cancel()

	initialIDs := map[string]bool{}
	for _, d := range devs {
		initialIDs[d.ID] = true
	}

	<-done

	// Total unique devices ever added = n + 1 (builtin). Snapshot + callback
	// set must cover everything without double-counting.
	assert.Eventually(t, func() bool {
		seenMu.Lock()
		defer seenMu.Unlock()
		total := len(initialIDs)
		for id := range seen {
			if !initialIDs[id] {
				total++
			}
		}
		return total == n+1
	}, 2*time.Second, 10*time.Millisecond)

	seenMu.Lock()
	for id := range seen {
		assert.False(t, initialIDs[id], "event for device %s already in snapshot must not be delivered", id)
	}
	seenMu.Unlock()
}

func TestSystem_PollingFallbackDetectsAddedDevice(t *testing.T) {
	fake := newFakeBackend(builtin())
	fake.disableWatchMode()
	sys := newTestSystem(t, fake, WithPollInterval(10*time.Millisecond))

	got := make(chan Event, 4)
	_, sub := sys.Snapshot(func(e Event) { got <- e })
	defer sub.Cancel()

	fake.add(usb())

	select {
	case ev := <-got:
		assert.Equal(t, Added, ev.Kind)
		assert.Equal(t, "usb-1", ev.Device.ID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for polled Added event")
	}
}

func TestSystem_PollingFallbackDetectsRemovedDevice(t *testing.T) {
	fake := newFakeBackend(builtin(), usb())
	fake.disableWatchMode()
	sys := newTestSystem(t, fake, WithPollInterval(10*time.Millisecond))

	got := make(chan Event, 4)
	_, sub := sys.Snapshot(func(e Event) { got <- e })
	defer sub.Cancel()

	fake.remove("usb-1")

	select {
	case ev := <-got:
		assert.Equal(t, Removed, ev.Kind)
		assert.Equal(t, "usb-1", ev.Device.ID)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for polled Removed event")
	}
}

func TestSystem_PollingFallbackDetectsDefaultChanged(t *testing.T) {
	fake := newFakeBackend(builtin(), usb())
	fake.disableWatchMode()
	sys := newTestSystem(t, fake, WithPollInterval(10*time.Millisecond))

	got := make(chan Event, 4)
	_, sub := sys.Snapshot(func(e Event) { got <- e })
	defer sub.Cancel()

	fake.setDefault("usb-1")

	deadline := time.After(time.Second)
	for {
		select {
		case ev := <-got:
			if ev.Kind == DefaultChanged && ev.Device.ID == "usb-1" {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for polled DefaultChanged event")
		}
	}
}

func TestSystem_CloseStopsWatcher(t *testing.T) {
	fake := newFakeBackend(builtin())
	sys, err := newSystem(fake)
	require.NoError(t, err)

	require.NoError(t, sys.Close())

	// Calling Close again would panic if watcher goroutine still held state;
	// we just verify the cache is still readable without hanging.
	_ = sys.List()
}

func TestDirectionString(t *testing.T) {
	assert.Equal(t, "output", Output.String())
	assert.Equal(t, "input", Input.String())
	assert.Equal(t, "unknown", Direction(0).String())
}

func TestEventKindString(t *testing.T) {
	cases := []struct {
		k    EventKind
		want string
	}{
		{Added, "added"},
		{Removed, "removed"},
		{DefaultChanged, "default-changed"},
		{PropertyChanged, "property-changed"},
		{EventKind(0), "unknown"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.k.String())
	}
}

func deviceID(i int) string {
	return "dev-" + itoa(i)
}

func itoa(i int) string {
	// Avoid importing strconv here just for a loop; faster to do by hand.
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
