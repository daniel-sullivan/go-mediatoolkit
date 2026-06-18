//go:build !linux && !darwin && !windows

// This file provides a fallback newPlatformBackend for platforms that
// do not yet have a real implementation. The build tag is updated as
// per-platform backends land.

package devices

func newPlatformBackend() (Backend, error) {
	return nil, ErrBackendUnavailable
}
