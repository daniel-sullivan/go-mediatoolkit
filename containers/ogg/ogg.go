// Package ogg reads and writes Ogg-framed packet streams at the container
// level.
//
// A [Reader] consumes raw Ogg bytes from an [io.Reader], demultiplexes
// logical streams (BOS pages), and exposes each stream as a [Stream] that
// yields packets via [containers.PacketReader]. A [Writer] multiplexes one
// or more logical streams back into an [io.Writer], accepting packets via
// [containers.PacketWriter].
//
// This package is codec-agnostic. For Opus-specific framing (OpusHead +
// OpusTags headers, pre-skip, sample-rate discovery), use the helpers in
// [OpusReader] / [NewOpusWriter].
//
// Container-level Header fields (SampleRate, Channels, ...) are left zero
// by the generic reader because only codec-specific headers carry them.
// The Opus helper populates the uniform Header fully.
package ogg

import "go-mediatoolkit/containers"

// Header is the container Header specialised to Ogg [Extras].
type Header = containers.Header[Extras]

// Extras carries Ogg-specific metadata: per-stream serial numbers and the
// codec-specific header packets collected from the beginning of each
// logical bitstream.
type Extras struct {
	// Streams, in the order their BOS pages appear.
	Streams []StreamInfo
}

// StreamInfo summarises a single logical bitstream at the container level.
type StreamInfo struct {
	SerialNo      int32
	CodecHint     string // best-effort: "opus", "vorbis", "flac", "" if unknown
	HeaderPackets [][]byte
}
