//go:build darwin

// This file implements the shared (CGo-agnostic) pieces of the darwin
// devices.Backend: enumeration via purego dlopen of CoreAudio and
// CoreFoundation. Watch is split out into platform_darwin_nocgo.go
// (returns ErrNotSupported, polling fallback) and platform_darwin_cgo.go
// (real hotplug via AudioObjectAddPropertyListenerBlock) so the same
// struct is shared across both CGo modes.

package devices

import (
	"context"
	"errors"
	"unsafe"
)

// darwinBackend enumerates CoreAudio devices via purego. It is
// stateless once loadCoreAudio has bound symbols; every call to List
// talks to the OS afresh.
type darwinBackend struct {
	ca *coreAudio
}

// newPlatformBackend constructs a darwin CoreAudio backend. It fails
// only if dlopen of CoreAudio or CoreFoundation fails, which should
// never happen on a normal macOS install.
func newPlatformBackend() (Backend, error) {
	ca, err := loadCoreAudio()
	if err != nil {
		return nil, err
	}
	return &darwinBackend{ca: ca}, nil
}

// List returns every (device, direction) pair CoreAudio reports as
// having at least one channel in the matching scope. Devices that
// support both input and output (aggregate or loopback devices) are
// emitted twice, once per direction.
func (b *darwinBackend) List(ctx context.Context) ([]Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ids, err := b.listDeviceIDs()
	if err != nil {
		return nil, err
	}
	defaultOut, _ := b.defaultDeviceID(kAudioHardwarePropertyDefaultOutputDevice)
	defaultIn, _ := b.defaultDeviceID(kAudioHardwarePropertyDefaultInputDevice)

	out := make([]Device, 0, len(ids))
	for _, id := range ids {
		name := b.deviceName(id)
		uid := b.deviceUID(id)
		rate := b.deviceSampleRate(id)

		if ch := b.channelCount(id, kAudioObjectPropertyScopeOutput); ch > 0 {
			out = append(out, Device{
				ID:         uid,
				Name:       name,
				Direction:  Output,
				IsDefault:  id == defaultOut,
				SampleRate: rate,
				Channels:   ch,
			})
		}
		if ch := b.channelCount(id, kAudioObjectPropertyScopeInput); ch > 0 {
			out = append(out, Device{
				ID:         uid,
				Name:       name,
				Direction:  Input,
				IsDefault:  id == defaultIn,
				SampleRate: rate,
				Channels:   ch,
			})
		}
	}
	return out, nil
}

// Close is a no-op. CoreAudio / CoreFoundation are dlopen'd once at
// process start and never released — no per-backend state needs to be
// torn down.
func (b *darwinBackend) Close() error { return nil }

// listDeviceIDs queries kAudioHardwarePropertyDevices on the system
// object and returns every AudioDeviceID the OS knows about.
func (b *darwinBackend) listDeviceIDs() ([]uint32, error) {
	addr := audioPropAddr{
		Selector: kAudioHardwarePropertyDevices,
		Scope:    kAudioObjectPropertyScopeGlobal,
		Element:  kAudioObjectPropertyElementMain,
	}
	var size uint32
	if rc := b.ca.AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &addr, 0, nil, &size); rc != 0 {
		return nil, errors.New("devices: AudioObjectGetPropertyDataSize(devices) failed")
	}
	if size == 0 {
		return nil, nil
	}
	count := int(size) / 4 // sizeof(AudioDeviceID) == 4
	ids := make([]uint32, count)
	if rc := b.ca.AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, nil, &size, unsafe.Pointer(&ids[0])); rc != 0 {
		return nil, errors.New("devices: AudioObjectGetPropertyData(devices) failed")
	}
	return ids, nil
}

// defaultDeviceID resolves one of
// kAudioHardwarePropertyDefaultOutputDevice or
// kAudioHardwarePropertyDefaultInputDevice to an AudioDeviceID.
func (b *darwinBackend) defaultDeviceID(selector uint32) (uint32, error) {
	addr := audioPropAddr{
		Selector: selector,
		Scope:    kAudioObjectPropertyScopeGlobal,
		Element:  kAudioObjectPropertyElementMain,
	}
	var id uint32
	size := uint32(4)
	if rc := b.ca.AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, nil, &size, unsafe.Pointer(&id)); rc != 0 {
		return 0, errors.New("devices: AudioObjectGetPropertyData(default) failed")
	}
	return id, nil
}

// deviceName reads kAudioObjectPropertyName as a CFString and returns
// the decoded Go string, or "" if the property is missing or the OS
// rejects the call.
func (b *darwinBackend) deviceName(id uint32) string {
	return b.cfStringProperty(id, kAudioObjectPropertyName, kAudioObjectPropertyScopeGlobal)
}

// deviceUID reads kAudioDevicePropertyDeviceUID and returns the decoded
// string. The UID is stable across reboots and is used as Device.ID.
func (b *darwinBackend) deviceUID(id uint32) string {
	return b.cfStringProperty(id, kAudioDevicePropertyDeviceUID, kAudioObjectPropertyScopeGlobal)
}

// cfStringProperty fetches a single CFStringRef property, decodes it
// to Go, and releases the CF reference.
func (b *darwinBackend) cfStringProperty(id, selector, scope uint32) string {
	addr := audioPropAddr{
		Selector: selector,
		Scope:    scope,
		Element:  kAudioObjectPropertyElementMain,
	}
	if !b.ca.AudioObjectHasProperty(id, &addr) {
		return ""
	}
	var ref uintptr
	size := uint32(unsafe.Sizeof(ref))
	if rc := b.ca.AudioObjectGetPropertyData(id, &addr, 0, nil, &size, unsafe.Pointer(&ref)); rc != 0 {
		return ""
	}
	return cfStringToGo(b.ca, ref)
}

// deviceSampleRate returns the device's nominal sample rate in Hz, or
// 0 if CoreAudio refuses to report it.
func (b *darwinBackend) deviceSampleRate(id uint32) int {
	addr := audioPropAddr{
		Selector: kAudioDevicePropertyNominalSampleRate,
		Scope:    kAudioObjectPropertyScopeGlobal,
		Element:  kAudioObjectPropertyElementMain,
	}
	if !b.ca.AudioObjectHasProperty(id, &addr) {
		return 0
	}
	var rate float64
	size := uint32(8)
	if rc := b.ca.AudioObjectGetPropertyData(id, &addr, 0, nil, &size, unsafe.Pointer(&rate)); rc != 0 {
		return 0
	}
	return int(rate)
}

// channelCount queries kAudioDevicePropertyStreamConfiguration with
// the given scope, parses the returned AudioBufferList, and sums
// channels across all buffers. Zero indicates the device has no
// streams in this direction.
func (b *darwinBackend) channelCount(id, scope uint32) int {
	addr := audioPropAddr{
		Selector: kAudioDevicePropertyStreamConfiguration,
		Scope:    scope,
		Element:  kAudioObjectPropertyElementMain,
	}
	var size uint32
	if rc := b.ca.AudioObjectGetPropertyDataSize(id, &addr, 0, nil, &size); rc != 0 || size == 0 {
		return 0
	}
	buf := make([]byte, size)
	if rc := b.ca.AudioObjectGetPropertyData(id, &addr, 0, nil, &size, unsafe.Pointer(&buf[0])); rc != 0 {
		return 0
	}
	return countChannelsInBufferList(buf[:size])
}
