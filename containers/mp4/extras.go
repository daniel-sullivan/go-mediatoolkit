package mp4

import aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"

// Extras carries MP4/ISOBMFF-specific metadata that does not fit the
// uniform [containers.Header] view: the file brand, the decoder
// configuration (esds AudioSpecificConfig), the sample tables that locate
// AAC access units inside mdat, and any iTunes metadata not projected onto
// the standard tag fields.
type Extras struct {
	// MajorBrand is the ftyp box major brand (e.g. "M4A ", "mp42").
	MajorBrand string

	// CompatibleBrands lists the ftyp compatible brands.
	CompatibleBrands []string

	// Config is the AAC AudioSpecificConfig parsed from the esds box. Its
	// Raw field preserves the verbatim ASC bytes so a re-muxer can copy
	// them byte-for-byte.
	Config aaclib.AudioSpecificConfig

	// SampleTable locates each AAC access unit inside the mdat box.
	SampleTable SampleTable

	// FreeformTags preserves iTunes ilst items that do not map onto a
	// [containers.StandardTags] field, keyed by their four-character (or
	// "----:mean:name" freeform) atom name.
	FreeformTags map[string][]string

	// CoverArt holds the raw bytes of the "covr" artwork atoms, if any.
	CoverArt [][]byte
}

// SampleTable is the decoded sample-location metadata from the stbl box —
// the stsz / stsc / stco tables — sufficient to slice the mdat payload into
// individual AAC access units.
type SampleTable struct {
	// SampleSizes holds the byte length of each sample (access unit). When
	// the stsz box declares a single constant size, this slice is expanded
	// to one entry per sample for uniform indexing.
	SampleSizes []uint32

	// ChunkOffsets holds the absolute file offset of each chunk's first
	// sample (from the stco box; the 64-bit co64 variant is widened into
	// the same slice).
	ChunkOffsets []uint64

	// SampleToChunk maps chunk runs to their samples-per-chunk (from the
	// stsc box). Each run applies from FirstChunk until the next run's
	// FirstChunk.
	SampleToChunk []SampleToChunkEntry
}

// SampleToChunkEntry is one run in the stsc (sample-to-chunk) table.
type SampleToChunkEntry struct {
	// FirstChunk is the 1-based index of the first chunk this run applies
	// to.
	FirstChunk uint32

	// SamplesPerChunk is the number of samples in each chunk of the run.
	SamplesPerChunk uint32

	// SampleDescriptionIndex selects the sample description (1-based) that
	// applies to the run.
	SampleDescriptionIndex uint32
}
