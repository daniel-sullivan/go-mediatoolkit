//go:build linux

package devices

import (
	"testing"

	"github.com/jfreymuth/pulse/proto"
	"github.com/stretchr/testify/assert"
)

func TestDeviceFromSink(t *testing.T) {
	cases := []struct {
		name        string
		info        *proto.GetSinkInfoReply
		defaultSink string
		want        Device
	}{
		{
			name: "happy path, is default",
			info: &proto.GetSinkInfoReply{
				SinkIndex:  0,
				SinkName:   "alsa_output.pci-0000_00_1f.3.analog-stereo",
				Device:     "Built-in Audio Analog Stereo",
				SampleSpec: proto.SampleSpec{Rate: 48000, Channels: 2},
			},
			defaultSink: "alsa_output.pci-0000_00_1f.3.analog-stereo",
			want: Device{
				ID:         "alsa_output.pci-0000_00_1f.3.analog-stereo",
				Name:       "Built-in Audio Analog Stereo",
				Direction:  Output,
				IsDefault:  true,
				SampleRate: 48000,
				Channels:   2,
			},
		},
		{
			name: "not default",
			info: &proto.GetSinkInfoReply{
				SinkName:   "usb-headset",
				Device:     "USB Headset",
				SampleSpec: proto.SampleSpec{Rate: 44100, Channels: 2},
			},
			defaultSink: "some-other-sink",
			want: Device{
				ID:         "usb-headset",
				Name:       "USB Headset",
				Direction:  Output,
				IsDefault:  false,
				SampleRate: 44100,
				Channels:   2,
			},
		},
		{
			name: "empty human name falls back to sink name",
			info: &proto.GetSinkInfoReply{
				SinkName:   "internal",
				Device:     "",
				SampleSpec: proto.SampleSpec{Rate: 96000, Channels: 6},
			},
			defaultSink: "",
			want: Device{
				ID:         "internal",
				Name:       "internal",
				Direction:  Output,
				SampleRate: 96000,
				Channels:   6,
			},
		},
		{
			name: "empty sink name is never treated as default even if default is also empty",
			info: &proto.GetSinkInfoReply{
				SinkName:   "",
				Device:     "",
				SampleSpec: proto.SampleSpec{},
			},
			defaultSink: "",
			want: Device{
				Direction: Output,
			},
		},
		{
			name: "zero sample-spec still produces a Device",
			info: &proto.GetSinkInfoReply{
				SinkName:   "quiet",
				Device:     "Quiet",
				SampleSpec: proto.SampleSpec{},
			},
			defaultSink: "",
			want: Device{
				ID:        "quiet",
				Name:      "Quiet",
				Direction: Output,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deviceFromSink(tc.info, tc.defaultSink)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDeviceFromSource(t *testing.T) {
	cases := []struct {
		name          string
		info          *proto.GetSourceInfoReply
		defaultSource string
		want          Device
	}{
		{
			name: "happy path, is default",
			info: &proto.GetSourceInfoReply{
				SourceName: "alsa_input.pci-0000_00_1f.3.analog-stereo",
				Device:     "Built-in Audio Analog Stereo",
				SampleSpec: proto.SampleSpec{Rate: 48000, Channels: 2},
			},
			defaultSource: "alsa_input.pci-0000_00_1f.3.analog-stereo",
			want: Device{
				ID:         "alsa_input.pci-0000_00_1f.3.analog-stereo",
				Name:       "Built-in Audio Analog Stereo",
				Direction:  Input,
				IsDefault:  true,
				SampleRate: 48000,
				Channels:   2,
			},
		},
		{
			name: "monitor source is still returned",
			info: &proto.GetSourceInfoReply{
				SourceName: "alsa_output.pci-0000_00_1f.3.analog-stereo.monitor",
				Device:     "Monitor of Built-in Audio",
				SampleSpec: proto.SampleSpec{Rate: 48000, Channels: 2},
			},
			defaultSource: "alsa_input.pci-0000_00_1f.3.analog-stereo",
			want: Device{
				ID:         "alsa_output.pci-0000_00_1f.3.analog-stereo.monitor",
				Name:       "Monitor of Built-in Audio",
				Direction:  Input,
				IsDefault:  false,
				SampleRate: 48000,
				Channels:   2,
			},
		},
		{
			name: "empty human name falls back to source name",
			info: &proto.GetSourceInfoReply{
				SourceName: "mic",
				Device:     "",
				SampleSpec: proto.SampleSpec{Rate: 16000, Channels: 1},
			},
			defaultSource: "other",
			want: Device{
				ID:         "mic",
				Name:       "mic",
				Direction:  Input,
				SampleRate: 16000,
				Channels:   1,
			},
		},
		{
			name:          "zero values",
			info:          &proto.GetSourceInfoReply{},
			defaultSource: "",
			want:          Device{Direction: Input},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deviceFromSource(tc.info, tc.defaultSource)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyPulseEvent(t *testing.T) {
	cases := []struct {
		name string
		ev   proto.SubscriptionEventType
		want pulseEventClass
	}{
		{"sink new", proto.EventSink | proto.EventNew, pulseEventSinkNewOrChange},
		{"sink change", proto.EventSink | proto.EventChange, pulseEventSinkNewOrChange},
		{"sink remove", proto.EventSink | proto.EventRemove, pulseEventSinkRemove},
		{"source new", proto.EventSource | proto.EventNew, pulseEventSourceNewOrChange},
		{"source change", proto.EventSource | proto.EventChange, pulseEventSourceNewOrChange},
		{"source remove", proto.EventSource | proto.EventRemove, pulseEventSourceRemove},
		{"server change", proto.EventServer | proto.EventChange, pulseEventServerChange},
		{"server new treated as change", proto.EventServer | proto.EventNew, pulseEventServerChange},
		{"server remove treated as change", proto.EventServer | proto.EventRemove, pulseEventServerChange},
		{"sink input ignored", proto.EventSinkSinkInput | proto.EventNew, pulseEventIgnore},
		{"source output ignored", proto.EventSinkSourceOutput | proto.EventNew, pulseEventIgnore},
		{"module ignored", proto.EventModule | proto.EventNew, pulseEventIgnore},
		{"client ignored", proto.EventClient | proto.EventChange, pulseEventIgnore},
		{"sample cache ignored", proto.EventSampleCache | proto.EventRemove, pulseEventIgnore},
		{"autoload ignored", proto.EventAutoload | proto.EventNew, pulseEventIgnore},
		{"card ignored", proto.EventCard | proto.EventNew, pulseEventIgnore},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyPulseEvent(tc.ev)
			assert.Equal(t, tc.want, got)
		})
	}
}
