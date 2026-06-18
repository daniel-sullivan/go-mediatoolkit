//go:build linux

package devices

import (
	"github.com/jfreymuth/pulse/proto"
)

// deviceFromSink converts a PulseAudio sink info reply to a Device.
// defaultSink is the server's current default sink name; when it
// matches info.SinkName the returned Device has IsDefault=true.
func deviceFromSink(info *proto.GetSinkInfoReply, defaultSink string) Device {
	name := info.Device
	if name == "" {
		name = info.SinkName
	}
	return Device{
		ID:         info.SinkName,
		Name:       name,
		Direction:  Output,
		IsDefault:  info.SinkName != "" && info.SinkName == defaultSink,
		SampleRate: int(info.SampleSpec.Rate),
		Channels:   int(info.SampleSpec.Channels),
	}
}

// deviceFromSource converts a PulseAudio source info reply to a Device.
// defaultSource is the server's current default source name; when it
// matches info.SourceName the returned Device has IsDefault=true.
//
// Monitor sources (virtual sources that echo a sink's output) are
// returned just like any other source — callers that want physical
// inputs only must filter by name suffix or properties.
func deviceFromSource(info *proto.GetSourceInfoReply, defaultSource string) Device {
	name := info.Device
	if name == "" {
		name = info.SourceName
	}
	return Device{
		ID:         info.SourceName,
		Name:       name,
		Direction:  Input,
		IsDefault:  info.SourceName != "" && info.SourceName == defaultSource,
		SampleRate: int(info.SampleSpec.Rate),
		Channels:   int(info.SampleSpec.Channels),
	}
}

// pulseEventClass is the coarse category of a PulseAudio subscription
// event after splitting facility and type bits.
type pulseEventClass int

const (
	pulseEventIgnore pulseEventClass = iota
	pulseEventSinkNewOrChange
	pulseEventSinkRemove
	pulseEventSourceNewOrChange
	pulseEventSourceRemove
	pulseEventServerChange
)

// classifyPulseEvent maps a raw proto.SubscriptionEventType to a
// high-level category the backend knows how to react to. Events for
// facilities we do not care about (sink inputs, modules, etc.) map to
// pulseEventIgnore.
func classifyPulseEvent(ev proto.SubscriptionEventType) pulseEventClass {
	facility := ev.GetFacility()
	kind := ev.GetType()

	switch facility {
	case proto.EventSink:
		if kind == proto.EventRemove {
			return pulseEventSinkRemove
		}
		return pulseEventSinkNewOrChange
	case proto.EventSource:
		if kind == proto.EventRemove {
			return pulseEventSourceRemove
		}
		return pulseEventSourceNewOrChange
	case proto.EventServer:
		return pulseEventServerChange
	}
	return pulseEventIgnore
}
