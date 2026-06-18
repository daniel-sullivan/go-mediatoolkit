package nativeopus

// Port of libopus/src/opus_multistream.c.
//
// The file ports the ChannelLayout struct (from src/opus_private.h),
// the MappingType enum, and the four validator/channel-lookup helpers
// used by the multistream encoder and decoder. The full multistream
// encoder/decoder logic lives in separate files; this file is the
// shared foundation.

// ChannelLayout mirrors `typedef struct ChannelLayout` in
// src/opus_private.h. The mapping array is a fixed-size byte array so
// the Go layout matches the C struct for future in-place ports that
// share byte buffers with cgo helpers.
type ChannelLayout struct {
	nb_channels        int
	nb_streams         int
	nb_coupled_streams int
	mapping            [256]byte
}

// MappingType mirrors the C `typedef enum { MAPPING_TYPE_NONE, ... }`
// in src/opus_private.h.
type MappingType int

const (
	MAPPING_TYPE_NONE MappingType = iota
	MAPPING_TYPE_SURROUND
	MAPPING_TYPE_AMBISONICS
)

// validate_layout — C: src/opus_multistream.c:41.
func validate_layout(layout *ChannelLayout) int {
	var i, max_channel int

	max_channel = layout.nb_streams + layout.nb_coupled_streams
	if max_channel > 255 {
		return 0
	}
	for i = 0; i < layout.nb_channels; i++ {
		if int(layout.mapping[i]) >= max_channel && int(layout.mapping[i]) != 255 {
			return 0
		}
	}
	return 1
}

// get_left_channel — C: src/opus_multistream.c:57.
func get_left_channel(layout *ChannelLayout, stream_id int, prev int) int {
	var i int
	if prev < 0 {
		i = 0
	} else {
		i = prev + 1
	}
	for ; i < layout.nb_channels; i++ {
		if int(layout.mapping[i]) == stream_id*2 {
			return i
		}
	}
	return -1
}

// get_right_channel — C: src/opus_multistream.c:69.
func get_right_channel(layout *ChannelLayout, stream_id int, prev int) int {
	var i int
	if prev < 0 {
		i = 0
	} else {
		i = prev + 1
	}
	for ; i < layout.nb_channels; i++ {
		if int(layout.mapping[i]) == stream_id*2+1 {
			return i
		}
	}
	return -1
}

// get_mono_channel — C: src/opus_multistream.c:81.
func get_mono_channel(layout *ChannelLayout, stream_id int, prev int) int {
	var i int
	if prev < 0 {
		i = 0
	} else {
		i = prev + 1
	}
	for ; i < layout.nb_channels; i++ {
		if int(layout.mapping[i]) == stream_id+layout.nb_coupled_streams {
			return i
		}
	}
	return -1
}
