//go:build integration

// Integration tests exercise the real platform backend on whichever OS
// the test binary happens to run on. They assume the CI runner (or a
// local developer) has installed a virtual audio device first — see
// .github/workflows/ci.yml for the exact setup steps.
//
// Run with:
//
//	go test -race -count=1 -tags=integration -p 1 ./devices/...
//
// devices.GetSystem returns a process-wide singleton (sync.Once): every
// test in this package shares the same *System and the same underlying
// platform watcher. A test must therefore never call sys.Close() — doing
// so cancels the shared watcher's context for good, leaving every
// subsequent test with a dead System that emits no hotplug events. The
// singleton is owned by the test binary and is torn down implicitly when
// the process exits.
package devices_test

import (
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/devices"
)

// hotplugDeadline bounds how long a hotplug test will wait for a single
// event. CI runners are slow; give the backend generous headroom.
const hotplugDeadline = 10 * time.Second

// TestIntegration_GetSystemReturnsAtLeastOneDevice asserts that the
// process-wide System constructs cleanly and that the runner exposes at
// least one virtual audio endpoint. A zero-device list means the
// platform-specific setup step did not run — fail loudly.
func TestIntegration_GetSystemReturnsAtLeastOneDevice(t *testing.T) {
	sys, err := devices.GetSystem()
	require.NoError(t, err)
	require.NotNil(t, sys)
	// Do not Close the shared singleton; see the package doc comment.

	list := sys.List()
	require.NotEmpty(t, list, "CI runner should have at least one virtual audio device configured")
	t.Logf("enumerated %d devices on %s", len(list), runtime.GOOS)
	for _, d := range list {
		t.Logf("  - %s [%s] %q default=%v rate=%d ch=%d",
			d.ID, d.Direction, d.Name, d.IsDefault, d.SampleRate, d.Channels)
	}
}

// TestIntegration_SnapshotAtomicity asserts that Snapshot returns a
// coherent (devices, subscription) pair: the initial list matches
// List() and a subscription is returned.
func TestIntegration_SnapshotAtomicity(t *testing.T) {
	sys, err := devices.GetSystem()
	require.NoError(t, err)
	require.NotNil(t, sys)
	// Do not Close the shared singleton; see the package doc comment.

	snap, sub := sys.Snapshot(func(devices.Event) {})
	require.NotNil(t, sub)
	t.Cleanup(sub.Cancel)

	require.NotEmpty(t, snap, "snapshot must contain at least one device on a configured runner")

	// Snapshot and List draw from the same cache; their contents should
	// agree at this instant (ordering is not guaranteed).
	list := sys.List()
	assert.ElementsMatch(t, snap, list, "Snapshot and List must agree on cache contents")
}

// TestIntegration_SubscriptionCancelStopsDelivery verifies that once a
// subscription is cancelled it receives no further events. The test
// triggers a hotplug change where possible; otherwise it relies on the
// polling fallback's guarantee that Cancel is race-free.
func TestIntegration_SubscriptionCancelStopsDelivery(t *testing.T) {
	sys, err := devices.GetSystem()
	require.NoError(t, err)
	require.NotNil(t, sys)
	// Do not Close the shared singleton; see the package doc comment.

	var received atomic.Int64
	_, sub := sys.Snapshot(func(devices.Event) {
		received.Add(1)
	})
	require.NotNil(t, sub)

	// Cancel immediately; any events triggered after this point must not
	// increment the counter.
	sub.Cancel()
	before := received.Load()

	switch runtime.GOOS {
	case "linux":
		moduleID := loadNullSink(t, "cancel_test_sink")
		t.Cleanup(func() { unloadModule(t, moduleID) })
		// Give the backend enough time to propagate the add event.
		time.Sleep(2 * time.Second)
	default:
		// No hotplug simulation available; just wait briefly to let any
		// stray polling events fire.
		time.Sleep(500 * time.Millisecond)
	}

	after := received.Load()
	assert.Equal(t, before, after, "cancelled subscription must not receive further events")
}

// TestIntegration_HotplugAddRemoveLinux loads a null sink, asserts an
// Added event appears, unloads it, and asserts a Removed event appears.
// Skips on non-Linux platforms.
func TestIntegration_HotplugAddRemoveLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("hotplug simulation only wired up on Linux (pactl)")
	}

	sys, err := devices.GetSystem()
	require.NoError(t, err)
	require.NotNil(t, sys)
	// Do not Close the shared singleton; see the package doc comment.

	const sinkName = "hotplug_test"

	var (
		mu        sync.Mutex
		addedSeen bool
		removed   bool
		addedCh   = make(chan struct{}, 1)
		removedCh = make(chan struct{}, 1)
	)

	_, sub := sys.Snapshot(func(ev devices.Event) {
		if ev.Device.ID != sinkName {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch ev.Kind {
		case devices.Added:
			if !addedSeen {
				addedSeen = true
				select {
				case addedCh <- struct{}{}:
				default:
				}
			}
		case devices.Removed:
			if !removed {
				removed = true
				select {
				case removedCh <- struct{}{}:
				default:
				}
			}
		}
	})
	require.NotNil(t, sub)
	t.Cleanup(sub.Cancel)

	moduleID := loadNullSink(t, sinkName)
	select {
	case <-addedCh:
	case <-time.After(hotplugDeadline):
		unloadModule(t, moduleID)
		t.Fatalf("timed out waiting for Added event for sink %q", sinkName)
	}

	unloadModule(t, moduleID)
	select {
	case <-removedCh:
	case <-time.After(hotplugDeadline):
		t.Fatalf("timed out waiting for Removed event for sink %q", sinkName)
	}
}

// TestIntegration_HotplugDarwin is a placeholder for future macOS
// hotplug coverage. BlackHole cannot be hot-(un)loaded after install,
// so there is no reliable way to simulate add/remove events on a GH
// runner today.
//
// TODO(devices): find or author a macOS-safe virtual device that
// supports runtime load/unload, then port TestIntegration_HotplugAddRemoveLinux.
func TestIntegration_HotplugDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only placeholder")
	}
	t.Skip("TODO(devices): no hot-loadable virtual device available on macOS CI")
}

// TestIntegration_HotplugWindows is a placeholder for future Windows
// hotplug coverage. pnputil requires administrator rights and is flaky
// on hosted Windows runners.
//
// TODO(devices): investigate using an elevated self-hosted runner, or
// a software-only WASAPI endpoint that can be spawned from user space.
func TestIntegration_HotplugWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only placeholder")
	}
	t.Skip("TODO(devices): pnputil requires admin; hotplug skipped on hosted windows runners")
}

// loadNullSink runs `pactl load-module module-null-sink` and returns
// the resulting module id. Fails the test on error; the caller is
// responsible for arranging cleanup via unloadModule.
func loadNullSink(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("pactl", "load-module", "module-null-sink",
		"sink_name="+name).CombinedOutput()
	require.NoErrorf(t, err, "pactl load-module failed: %s", string(out))
	id := strings.TrimSpace(string(out))
	// pactl prints the module index on stdout; sanity-check it's numeric.
	require.Regexp(t, regexp.MustCompile(`^\d+$`), id, "expected numeric module id, got %q", id)
	return id
}

// unloadModule runs `pactl unload-module <id>`. Errors are logged but
// do not fail the test; the test body already asserted the behaviour
// it cared about.
func unloadModule(t *testing.T, id string) {
	t.Helper()
	out, err := exec.Command("pactl", "unload-module", id).CombinedOutput()
	if err != nil {
		t.Logf("pactl unload-module %s failed: %v: %s", id, err, string(out))
	}
}
