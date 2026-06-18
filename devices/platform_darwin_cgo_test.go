//go:build darwin && cgo

// Unit tests for the pure-Go pieces of the CGo Watch path: the device
// diff helper and the handle registry. The listener callbacks
// themselves are integration-tested against real hardware, not here.

package devices

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotFromDevicesKeysByIDAndDirection(t *testing.T) {
	devs := []Device{
		{ID: "uid-a", Direction: Output, Name: "A out"},
		{ID: "uid-a", Direction: Input, Name: "A in"},
		{ID: "uid-b", Direction: Output, Name: "B"},
	}
	snap := snapshotFromDevices(devs)
	require.Len(t, snap, 3)
	assert.Equal(t, "A out", snap[darwinDeviceKey{ID: "uid-a", Direction: Output}].Name)
	assert.Equal(t, "A in", snap[darwinDeviceKey{ID: "uid-a", Direction: Input}].Name)
	assert.Equal(t, "B", snap[darwinDeviceKey{ID: "uid-b", Direction: Output}].Name)
}

func TestDiffDevicesEmpty(t *testing.T) {
	events, next := diffDevices(map[darwinDeviceKey]Device{}, nil)
	assert.Empty(t, events)
	assert.Empty(t, next)
}

func TestDiffDevicesAdded(t *testing.T) {
	cur := []Device{{ID: "uid-a", Direction: Output, Name: "A"}}
	events, next := diffDevices(map[darwinDeviceKey]Device{}, cur)
	require.Len(t, events, 1)
	assert.Equal(t, Added, events[0].Kind)
	assert.Equal(t, "uid-a", events[0].Device.ID)
	assert.Len(t, next, 1)
}

func TestDiffDevicesRemoved(t *testing.T) {
	prev := snapshotFromDevices([]Device{{ID: "uid-a", Direction: Output, Name: "A"}})
	events, next := diffDevices(prev, nil)
	require.Len(t, events, 1)
	assert.Equal(t, Removed, events[0].Kind)
	assert.Equal(t, "uid-a", events[0].Device.ID)
	assert.Empty(t, next)
}

func TestDiffDevicesDefaultChanged(t *testing.T) {
	prev := snapshotFromDevices([]Device{
		{ID: "uid-a", Direction: Output, Name: "A", IsDefault: true},
		{ID: "uid-b", Direction: Output, Name: "B"},
	})
	cur := []Device{
		{ID: "uid-a", Direction: Output, Name: "A"},
		{ID: "uid-b", Direction: Output, Name: "B", IsDefault: true},
	}
	events, _ := diffDevices(prev, cur)
	require.Len(t, events, 1)
	assert.Equal(t, DefaultChanged, events[0].Kind)
	assert.Equal(t, "uid-b", events[0].Device.ID)
}

func TestDiffDevicesPropertyChanged(t *testing.T) {
	prev := snapshotFromDevices([]Device{
		{ID: "uid-a", Direction: Output, Name: "A", SampleRate: 44100, Channels: 2},
	})
	tests := []struct {
		name string
		cur  Device
	}{
		{"sample rate changed", Device{ID: "uid-a", Direction: Output, Name: "A", SampleRate: 48000, Channels: 2}},
		{"channel count changed", Device{ID: "uid-a", Direction: Output, Name: "A", SampleRate: 44100, Channels: 6}},
		{"name changed", Device{ID: "uid-a", Direction: Output, Name: "A Renamed", SampleRate: 44100, Channels: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events, _ := diffDevices(prev, []Device{tc.cur})
			require.Len(t, events, 1)
			assert.Equal(t, PropertyChanged, events[0].Kind)
		})
	}
}

func TestDiffDevicesNoChange(t *testing.T) {
	prev := snapshotFromDevices([]Device{
		{ID: "uid-a", Direction: Output, Name: "A", SampleRate: 44100, Channels: 2},
	})
	events, next := diffDevices(prev, []Device{
		{ID: "uid-a", Direction: Output, Name: "A", SampleRate: 44100, Channels: 2},
	})
	assert.Empty(t, events)
	assert.Len(t, next, 1)
}

func TestDiffDevicesOrderRemovedBeforeAdded(t *testing.T) {
	// A device is replaced: one goes, a different one arrives. Removed
	// must come before Added so subscribers see the departure before
	// the new arrival.
	prev := snapshotFromDevices([]Device{{ID: "uid-old", Direction: Output, Name: "Old"}})
	cur := []Device{{ID: "uid-new", Direction: Output, Name: "New"}}
	events, _ := diffDevices(prev, cur)
	require.Len(t, events, 2)
	assert.Equal(t, Removed, events[0].Kind)
	assert.Equal(t, "uid-old", events[0].Device.ID)
	assert.Equal(t, Added, events[1].Kind)
	assert.Equal(t, "uid-new", events[1].Device.ID)
}

func TestDiffDevicesTreatsDirectionsSeparately(t *testing.T) {
	// An aggregate device exposing input and output shares a UID; the
	// diff must not confuse the two scopes.
	prev := snapshotFromDevices([]Device{
		{ID: "uid-a", Direction: Output, Name: "A"},
	})
	cur := []Device{
		{ID: "uid-a", Direction: Output, Name: "A"},
		{ID: "uid-a", Direction: Input, Name: "A"},
	}
	events, _ := diffDevices(prev, cur)
	require.Len(t, events, 1)
	assert.Equal(t, Added, events[0].Kind)
	assert.Equal(t, Input, events[0].Device.Direction)
}

func TestHandleRegistryRoundTrip(t *testing.T) {
	w := &darwinWatcher{}
	h := registerWatcher(w)
	assert.NotZero(t, h)
	assert.Same(t, w, lookupWatcher(h))
	unregisterWatcher(h)
	assert.Nil(t, lookupWatcher(h))
}

func TestHandleRegistryUniqueHandles(t *testing.T) {
	w1 := &darwinWatcher{}
	w2 := &darwinWatcher{}
	h1 := registerWatcher(w1)
	h2 := registerWatcher(w2)
	defer unregisterWatcher(h1)
	defer unregisterWatcher(h2)
	assert.NotEqual(t, h1, h2)
	assert.Same(t, w1, lookupWatcher(h1))
	assert.Same(t, w2, lookupWatcher(h2))
}

func TestHandleRegistryLookupMissing(t *testing.T) {
	// An unregistered handle (or one that has already been removed)
	// must return nil so the C callback path can short-circuit.
	assert.Nil(t, lookupWatcher(0))
}

func TestHandleRegistryConcurrentAccess(t *testing.T) {
	// Exercise the mutex under -race.
	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	handles := make([]uintptr, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			handles[i] = registerWatcher(&darwinWatcher{})
		}()
	}
	wg.Wait()

	seen := make(map[uintptr]struct{}, n)
	for _, h := range handles {
		assert.NotZero(t, h)
		seen[h] = struct{}{}
	}
	assert.Len(t, seen, n, "registerWatcher must mint unique handles")

	wg.Add(n)
	for i := 0; i < n; i++ {
		h := handles[i]
		go func() {
			defer wg.Done()
			unregisterWatcher(h)
		}()
	}
	wg.Wait()
	for _, h := range handles {
		assert.Nil(t, lookupWatcher(h))
	}
}
