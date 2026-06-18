//go:build darwin && !cgo

// This file provides the no-CGo Watch stub for the darwin backend.
// Without CGo we cannot install AudioObjectPropertyListenerBlocks, so
// we return ErrNotSupported and let System.watchLoop fall back to
// polling List at WithPollInterval.

package devices

import "context"

// Watch reports that native event delivery is unavailable when CGo is
// disabled. System then polls List at its configured poll interval.
// The real hotplug implementation lives in platform_darwin_cgo.go.
func (b *darwinBackend) Watch(ctx context.Context) (<-chan Event, error) {
	return nil, ErrNotSupported
}
