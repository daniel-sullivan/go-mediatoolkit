package wav

import "time"

// Extras carries WAV-specific metadata that does not fit the uniform
// [containers.Header] view.
type Extras struct {
	// Bext is the Broadcast Wave Extension chunk, if present.
	Bext *BroadcastExt

	// Cues is the list of cue points from the "cue " chunk, if present.
	Cues []CuePoint

	// Unknown holds chunks the reader did not recognise, keyed by four-CC.
	// The reader preserves the raw chunk body so the writer can round-trip
	// them unchanged.
	Unknown map[string][]byte

	// FormatTag is the raw WAVEFORMATEX wFormatTag value from the fmt chunk.
	// Useful for callers that need to distinguish e.g. WAVE_FORMAT_PCM (1)
	// from WAVE_FORMAT_IEEE_FLOAT (3) or WAVE_FORMAT_EXTENSIBLE (0xFFFE).
	FormatTag uint16

	// BitsPerSample is the raw wBitsPerSample from the fmt chunk.
	BitsPerSample uint16
}

// BroadcastExt mirrors the "bext" chunk (EBU Tech 3285).
type BroadcastExt struct {
	Description     string // up to 256 chars
	Originator      string // up to 32 chars
	OriginatorRef   string // up to 32 chars
	OriginationDate string // YYYY-MM-DD
	OriginationTime string // HH:MM:SS
	TimeReference   uint64 // samples since midnight
	Version         uint16
	UMID            [64]byte
	CodingHistory   string
}

// CuePoint is a single entry from the "cue " chunk.
type CuePoint struct {
	ID           uint32
	Position     uint32
	DataChunkID  [4]byte
	ChunkStart   uint32
	BlockStart   uint32
	SampleOffset uint32
}

// DurationFromSamples computes a Duration from a sample frame count.
func DurationFromSamples(frames, sampleRate int) time.Duration {
	if sampleRate <= 0 {
		return 0
	}
	return time.Duration(float64(frames) / float64(sampleRate) * float64(time.Second))
}
