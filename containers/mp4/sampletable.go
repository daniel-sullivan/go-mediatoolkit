package mp4

import "encoding/binary"

// BoxCo64 is the 64-bit chunk-offset box (used in place of stco for files
// larger than 4 GiB).
var BoxCo64 = BoxType{'c', 'o', '6', '4'}

// BoxStts is the time-to-sample (decoding-time) box.
var BoxStts = BoxType{'s', 't', 't', 's'}

// parseStsz decodes the sample-size box body into one size per sample. When
// the box declares a single constant sampleSize, it is expanded to sampleCount
// entries for uniform indexing.
func parseStsz(body []byte) ([]uint32, error) {
	if len(body) < 12 {
		return nil, ErrInvalidSampleTable
	}
	sampleSize := binary.BigEndian.Uint32(body[4:8])
	sampleCount := binary.BigEndian.Uint32(body[8:12])

	sizes := make([]uint32, sampleCount)
	if sampleSize != 0 {
		for i := range sizes {
			sizes[i] = sampleSize
		}
		return sizes, nil
	}
	if len(body) < 12+int(sampleCount)*4 {
		return nil, ErrInvalidSampleTable
	}
	for i := uint32(0); i < sampleCount; i++ {
		sizes[i] = binary.BigEndian.Uint32(body[12+i*4 : 16+i*4])
	}
	return sizes, nil
}

// parseStsc decodes the sample-to-chunk box body into its run entries.
func parseStsc(body []byte) ([]SampleToChunkEntry, error) {
	if len(body) < 8 {
		return nil, ErrInvalidSampleTable
	}
	count := binary.BigEndian.Uint32(body[4:8])
	if len(body) < 8+int(count)*12 {
		return nil, ErrInvalidSampleTable
	}
	entries := make([]SampleToChunkEntry, count)
	for i := uint32(0); i < count; i++ {
		o := 8 + i*12
		entries[i] = SampleToChunkEntry{
			FirstChunk:             binary.BigEndian.Uint32(body[o : o+4]),
			SamplesPerChunk:        binary.BigEndian.Uint32(body[o+4 : o+8]),
			SampleDescriptionIndex: binary.BigEndian.Uint32(body[o+8 : o+12]),
		}
	}
	return entries, nil
}

// parseStco decodes the 32-bit chunk-offset box body, widening each offset
// into a uint64.
func parseStco(body []byte) ([]uint64, error) {
	if len(body) < 8 {
		return nil, ErrInvalidSampleTable
	}
	count := binary.BigEndian.Uint32(body[4:8])
	if len(body) < 8+int(count)*4 {
		return nil, ErrInvalidSampleTable
	}
	offsets := make([]uint64, count)
	for i := uint32(0); i < count; i++ {
		offsets[i] = uint64(binary.BigEndian.Uint32(body[8+i*4 : 12+i*4]))
	}
	return offsets, nil
}

// parseCo64 decodes the 64-bit chunk-offset box body.
func parseCo64(body []byte) ([]uint64, error) {
	if len(body) < 8 {
		return nil, ErrInvalidSampleTable
	}
	count := binary.BigEndian.Uint32(body[4:8])
	if len(body) < 8+int(count)*8 {
		return nil, ErrInvalidSampleTable
	}
	offsets := make([]uint64, count)
	for i := uint32(0); i < count; i++ {
		offsets[i] = binary.BigEndian.Uint64(body[8+i*8 : 16+i*8])
	}
	return offsets, nil
}

// sttsEntry is one (count, delta) run from the time-to-sample box.
type sttsEntry struct {
	count uint32
	delta uint32
}

// parseStts decodes the time-to-sample box body into its run entries, used
// to derive the per-sample decoding times (and thus the stream duration).
func parseStts(body []byte) ([]sttsEntry, error) {
	if len(body) < 8 {
		return nil, ErrInvalidSampleTable
	}
	count := binary.BigEndian.Uint32(body[4:8])
	if len(body) < 8+int(count)*8 {
		return nil, ErrInvalidSampleTable
	}
	entries := make([]sttsEntry, count)
	for i := uint32(0); i < count; i++ {
		o := 8 + i*8
		entries[i] = sttsEntry{
			count: binary.BigEndian.Uint32(body[o : o+4]),
			delta: binary.BigEndian.Uint32(body[o+4 : o+8]),
		}
	}
	return entries, nil
}

// totalDurationTicks sums count×delta across all stts runs, giving the track
// duration in media-timescale ticks.
func totalDurationTicks(entries []sttsEntry) uint64 {
	var ticks uint64
	for _, e := range entries {
		ticks += uint64(e.count) * uint64(e.delta)
	}
	return ticks
}

// sampleLocation is the absolute byte range of one access unit in the file.
type sampleLocation struct {
	offset uint64
	size   uint32
}

// resolveSampleOffsets expands the stsz / stsc / stco tables into one
// absolute (offset, size) per sample. Each chunk's samples are laid out
// contiguously starting at the chunk's offset; the stsc runs determine how
// many samples belong to each chunk.
func resolveSampleOffsets(st SampleTable) ([]sampleLocation, error) {
	if len(st.SampleToChunk) == 0 || len(st.ChunkOffsets) == 0 {
		return nil, ErrInvalidSampleTable
	}

	locs := make([]sampleLocation, 0, len(st.SampleSizes))
	sampleIdx := 0
	numChunks := uint32(len(st.ChunkOffsets))

	for run := 0; run < len(st.SampleToChunk); run++ {
		entry := st.SampleToChunk[run]
		firstChunk := entry.FirstChunk
		if firstChunk < 1 {
			return nil, ErrInvalidSampleTable
		}
		// This run applies to chunks [firstChunk, lastChunk].
		lastChunk := numChunks
		if run+1 < len(st.SampleToChunk) {
			next := st.SampleToChunk[run+1].FirstChunk
			if next < 1 || next-1 > numChunks {
				return nil, ErrInvalidSampleTable
			}
			lastChunk = next - 1
		}

		for chunk := firstChunk; chunk <= lastChunk; chunk++ {
			if chunk < 1 || chunk > numChunks {
				return nil, ErrInvalidSampleTable
			}
			offset := st.ChunkOffsets[chunk-1]
			for s := uint32(0); s < entry.SamplesPerChunk; s++ {
				if sampleIdx >= len(st.SampleSizes) {
					return nil, ErrInvalidSampleTable
				}
				size := st.SampleSizes[sampleIdx]
				locs = append(locs, sampleLocation{offset: offset, size: size})
				offset += uint64(size)
				sampleIdx++
			}
		}
	}

	if sampleIdx != len(st.SampleSizes) {
		return nil, ErrInvalidSampleTable
	}
	return locs, nil
}

// sliceAccessUnits cuts the full file bytes into one access unit per sample
// using the resolved sample locations.
func sliceAccessUnits(file []byte, locs []sampleLocation) ([][]byte, error) {
	units := make([][]byte, len(locs))
	for i, loc := range locs {
		end := loc.offset + uint64(loc.size)
		if loc.offset > uint64(len(file)) || end > uint64(len(file)) {
			return nil, ErrInvalidSampleTable
		}
		au := make([]byte, loc.size)
		copy(au, file[loc.offset:end])
		units[i] = au
	}
	return units, nil
}
