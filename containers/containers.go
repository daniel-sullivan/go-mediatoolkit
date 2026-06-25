// Package containers provides a uniform view over audio container formats.
//
// Each container (WAV, Ogg, ...) parses its own framing and metadata and
// exposes a [Header] describing the stream plus either an [io.Reader] /
// [io.Writer] (for byte-oriented containers like WAV) or a [PacketReader] /
// [PacketWriter] (for packet-oriented containers like Ogg).
//
// Metadata is normalized to Vorbis-comment-style tag keys. See [Tags].
//
// The type parameter E on [Header] is a format-specific extras struct that
// carries information a uniform view cannot represent (e.g., broadcast WAV
// coding history, Ogg stream tables, Opus pre-skip).
package containers

import (
	"strings"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Header describes a container's stream metadata in a format-agnostic way.
// The type parameter E is a format-specific "extras" payload — callers
// switch on the concrete container type to read it.
type Header[E any] struct {
	// Format is a short identifier such as "wav", "ogg", or "ogg/opus".
	Format string

	// SampleRate is the sample rate in Hz. Zero if unknown at the container
	// level (e.g., generic Ogg readers that have not inspected the codec BOS).
	SampleRate int

	// Channels is the channel count. Zero if unknown at the container level.
	Channels int

	// SampleFormat is the best-fit PCM representation for the stream.
	// For compressed codecs this is typically FormatFloat64 since the
	// decoder produces float64 samples.
	SampleFormat mutations.SampleFormat

	// BitRate is the nominal bitrate in bits per second. Zero if unknown
	// (e.g., VBR streams without an explicit nominal bitrate).
	BitRate int

	// Duration is the stream duration. Zero if unknown or unseekable.
	Duration time.Duration

	// Tags carries common metadata as typed optional fields, with a
	// pass-through map for multi-value or non-standard entries.
	Tags StandardTags

	// Extra holds format-specific metadata that does not fit the uniform view.
	Extra E
}

// StandardTags holds commonly-used audio metadata as typed optional
// fields. A nil pointer means "not set" and is omitted on write. Tags not
// covered by the typed fields — or additional values for multi-value tags
// — live in AdditionalTags.
//
// Keys in AdditionalTags follow Vorbis-comment conventions (upper-case).
// Container readers map their native metadata (e.g. WAV INFO four-CCs,
// Ogg Vorbis comments) onto these fields; writers project them back.
//
// To set a field inline, use Go 1.26's value-form new:
//
//	Tags: containers.StandardTags{ Title: new("Song Name") }
type StandardTags struct {
	Title        *string
	Artist       *string
	Album        *string
	AlbumArtist  *string
	Date         *string
	Genre        *string
	TrackNumber  *string
	Comment      *string
	Composer     *string
	Performer    *string
	Copyright    *string
	Encoder      *string
	Description  *string
	Organization *string
	License      *string
	ISRC         *string

	// AdditionalTags preserves tags that do not map to a typed field and
	// any extra values beyond the first for multi-value standard tags.
	AdditionalTags Tags
}

// Map flattens StandardTags into a tag map. Typed fields that are non-nil
// appear as the first value for their key; AdditionalTags entries are
// appended. Used by container writers to iterate all tags uniformly.
func (t StandardTags) Map() Tags {
	m := NewTags()
	for k, vs := range t.AdditionalTags {
		if len(vs) == 0 {
			continue
		}
		cp := make([]string, len(vs))
		copy(cp, vs)
		m[strings.ToUpper(k)] = cp
	}
	prepend := func(key string, p *string) {
		if p == nil {
			return
		}
		key = strings.ToUpper(key)
		m[key] = append([]string{*p}, m[key]...)
	}
	prepend("TITLE", t.Title)
	prepend("ARTIST", t.Artist)
	prepend("ALBUM", t.Album)
	prepend("ALBUMARTIST", t.AlbumArtist)
	prepend("DATE", t.Date)
	prepend("GENRE", t.Genre)
	prepend("TRACKNUMBER", t.TrackNumber)
	prepend("COMMENT", t.Comment)
	prepend("COMPOSER", t.Composer)
	prepend("PERFORMER", t.Performer)
	prepend("COPYRIGHT", t.Copyright)
	prepend("ENCODER", t.Encoder)
	prepend("DESCRIPTION", t.Description)
	prepend("ORGANIZATION", t.Organization)
	prepend("LICENSE", t.License)
	prepend("ISRC", t.ISRC)
	return m
}

// StandardTagsFromMap populates a StandardTags from a flat tag map.
// Recognised keys populate typed fields with the first value; additional
// values for recognised keys, and any unrecognised keys, are preserved in
// AdditionalTags. Used by container readers.
func StandardTagsFromMap(m Tags) StandardTags {
	var t StandardTags
	extra := NewTags()

	assign := func(key string, vs []string) (ptr **string, consumed bool) {
		switch key {
		case "TITLE":
			return &t.Title, true
		case "ARTIST":
			return &t.Artist, true
		case "ALBUM":
			return &t.Album, true
		case "ALBUMARTIST":
			return &t.AlbumArtist, true
		case "DATE":
			return &t.Date, true
		case "GENRE":
			return &t.Genre, true
		case "TRACKNUMBER":
			return &t.TrackNumber, true
		case "COMMENT":
			return &t.Comment, true
		case "COMPOSER":
			return &t.Composer, true
		case "PERFORMER":
			return &t.Performer, true
		case "COPYRIGHT":
			return &t.Copyright, true
		case "ENCODER":
			return &t.Encoder, true
		case "DESCRIPTION":
			return &t.Description, true
		case "ORGANIZATION":
			return &t.Organization, true
		case "LICENSE":
			return &t.License, true
		case "ISRC":
			return &t.ISRC, true
		}
		return nil, false
	}

	for rawKey, vs := range m {
		if len(vs) == 0 {
			continue
		}
		key := strings.ToUpper(rawKey)
		ptr, ok := assign(key, vs)
		if !ok {
			cp := make([]string, len(vs))
			copy(cp, vs)
			extra[key] = cp
			continue
		}
		first := vs[0]
		*ptr = &first
		if len(vs) > 1 {
			cp := make([]string, len(vs)-1)
			copy(cp, vs[1:])
			extra[key] = cp
		}
	}

	if len(extra) > 0 {
		t.AdditionalTags = extra
	}
	return t
}

// Tags is a case-insensitive multi-value string map using Vorbis-comment
// conventions. Common keys: TITLE, ARTIST, ALBUM, DATE, GENRE, COMMENT,
// TRACKNUMBER, ALBUMARTIST, COMPOSER, COPYRIGHT, ENCODER, DESCRIPTION,
// PERFORMER, ORGANIZATION, LICENSE, ISRC.
//
// Keys are upper-cased on access. Multiple values per key are allowed
// (e.g., two ARTIST entries for a collaboration).
type Tags map[string][]string

// NewTags returns an empty Tags map.
func NewTags() Tags { return Tags{} }

// Get returns the first value for key, or "" if key is not present.
// The lookup is case-insensitive.
func (t Tags) Get(key string) string {
	v := t[strings.ToUpper(key)]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// GetAll returns all values for key, or nil if key is not present.
func (t Tags) GetAll(key string) []string {
	return t[strings.ToUpper(key)]
}

// Set replaces any existing values for key with a single value.
func (t Tags) Set(key, value string) {
	t[strings.ToUpper(key)] = []string{value}
}

// Add appends value to the existing values for key.
func (t Tags) Add(key, value string) {
	k := strings.ToUpper(key)
	t[k] = append(t[k], value)
}

// Delete removes all values for key.
func (t Tags) Delete(key string) {
	delete(t, strings.ToUpper(key))
}

// Has reports whether key exists with at least one value.
func (t Tags) Has(key string) bool {
	return len(t[strings.ToUpper(key)]) > 0
}

// Keys returns the keys present in the tag map, in unspecified order.
func (t Tags) Keys() []string {
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	return keys
}

// PacketReader reads one encoded packet at a time from a packet-oriented
// container (e.g., Ogg). It is structurally compatible with
// [github.com/daniel-sullivan/go-mediatoolkit/codec/opus.PacketReader].
type PacketReader interface {
	// ReadPacket returns the next packet. Returns io.EOF when exhausted.
	ReadPacket() ([]byte, error)
}

// PacketWriter writes one encoded packet at a time to a packet-oriented
// container. It is structurally compatible with
// [github.com/daniel-sullivan/go-mediatoolkit/codec/opus.PacketWriter].
type PacketWriter interface {
	WritePacket([]byte) error
}
