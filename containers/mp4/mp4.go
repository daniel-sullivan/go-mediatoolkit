// Package mp4 reads the ISO Base Media File Format (ISOBMFF / MP4)
// container — the framing used by .m4a and .mp4 audio files — in pure Go.
//
// AAC = packet codec (codec/aac via PacketReader/PacketWriter interface,
// mirroring the codec/opus archetype). M4A = ISO Base Media File Format
// (ISOBMFF/MP4) container parsed in pure Go by containers/mp4 box reader
// (ftyp/moov/mdat structures, esds AudioSpecificConfig, sample tables
// stsz/stsc/stco, ilst iTunes metadata) with zero C code in the container
// layer. The packet-codec split is identical to codec/opus +
// containers/ogg/opus.go: the AAC library exposes packet-oriented
// encoding/decoding (Encoder.Encode(pcm []float64) -> []byte;
// Decoder.Decode(pkt []byte) -> samplesPerChannel int), and codec/aac
// wraps this with StreamChunker + remainder buffering to support
// arbitrary-sized Read/Write calls from callers. FDK-AAC is fixed-point, so
// parity is EXACT integer equality on decode (the pure-Go port reproduces the
// vendored C libfdk-aac int32 PCM bit-for-bit) and a byte-identical AAC access
// unit on encode — there is no floating-point/ULP tolerance involved.
//
// An MP4 file is a tree of length-prefixed "boxes" (atoms): each box is a
// 32-bit big-endian size, a four-byte type, then a body (which may itself
// contain child boxes). This package's box reader walks that tree without
// any C code, extracting:
//
//   - ftyp — the file-type / brand box (identifies the major brand, e.g.
//     "M4A ").
//   - moov — the movie box, holding the track / sample-table metadata.
//   - mdat — the media-data box, holding the raw AAC access units.
//   - esds — the elementary-stream descriptor inside the audio sample
//     entry, carrying the AAC [AudioSpecificConfig].
//   - stsz / stsc / stco — the sample-size, sample-to-chunk, and
//     chunk-offset tables that locate each AAC access unit inside mdat.
//   - ilst — the iTunes-style metadata list (©nam, ©ART, ©alb, …),
//     projected onto [containers.StandardTags].
//
// A [Reader] parses the box tree and exposes the AAC access units as a
// [go-mediatoolkit/codec/aac.PacketReader], so callers can pipe them
// straight into [go-mediatoolkit/codec/aac.NewDecoder] together with the
// parsed AudioSpecificConfig. The codec layer owns the bitstream; this
// container layer adds no C and re-frames nothing — it only locates and
// hands out the existing access units.
//
// A [Writer] muxes encoded AAC access units back into an ISOBMFF file,
// copying each quantized access unit byte-for-byte (box-level
// byte-for-byte copy) and projecting [Header] tags onto the ilst box.
//
// A Reader is not safe for concurrent use.
package mp4

import "go-mediatoolkit/containers"

// Header is the container [containers.Header] specialised to MP4 [Extras].
type Header = containers.Header[Extras]

// FormatM4A is the [Header.Format] identifier this package reports.
const FormatM4A = "mp4"

// BoxType is a four-character-code box (atom) identifier, e.g. "ftyp",
// "moov", "mdat". It is stored as a fixed four-byte array so it can be
// compared and used as a map key without allocation.
type BoxType [4]byte

// Common box types this package recognises.
var (
	// BoxFtyp is the file-type box.
	BoxFtyp = BoxType{'f', 't', 'y', 'p'}
	// BoxMoov is the movie (metadata) box.
	BoxMoov = BoxType{'m', 'o', 'o', 'v'}
	// BoxMdat is the media-data box (raw AAC access units).
	BoxMdat = BoxType{'m', 'd', 'a', 't'}
	// BoxEsds is the elementary-stream descriptor box (AudioSpecificConfig).
	BoxEsds = BoxType{'e', 's', 'd', 's'}
	// BoxStsz is the sample-size box.
	BoxStsz = BoxType{'s', 't', 's', 'z'}
	// BoxStsc is the sample-to-chunk box.
	BoxStsc = BoxType{'s', 't', 's', 'c'}
	// BoxStco is the 32-bit chunk-offset box.
	BoxStco = BoxType{'s', 't', 'c', 'o'}
	// BoxIlst is the iTunes metadata-item-list box.
	BoxIlst = BoxType{'i', 'l', 's', 't'}
)

// String returns the four-character code as a string.
func (b BoxType) String() string { return string(b[:]) }
