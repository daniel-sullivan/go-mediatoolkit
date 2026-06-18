package nativeopus

// Port of libopus/celt/modes.h (struct and constant definitions).
//
// The full modes.c runtime-construction path (opus_custom_mode_create,
// compute_ebands, compute_allocation_table) is CUSTOM_MODES-only in
// libopus and is not needed by a standard decoder. Production libopus
// uses the pre-computed static mode tables in static_modes_float.h;
// porting those 2516 lines of generated data is deferred to Phase 11
// (end-to-end validation). For Phase 3 the parity tests construct
// Go OpusCustomMode instances by mirroring a C-built static mode's
// fields via Cgo getters.

// MAX_PERIOD — C: modes.h:40.
const MAX_PERIOD = 1024

// DEC_PITCH_BUF_SIZE — C: modes.h:42.
const DEC_PITCH_BUF_SIZE = 2048

// PulseCache — PVQ pulse-count lookup. C: modes.h:44-49.
type PulseCache struct {
	size  int
	index []opus_int16
	bits  []byte
	caps  []byte
}

// OpusCustomMode — CELT mode descriptor. C: modes.h:54-77.
//
// In C `window` / `eBands` / `allocVectors` / `logN` are `const` pointers
// into static tables. In Go they are plain slices; the backing data is
// either read from static_modes_float-derived Go tables (Phase 11) or
// mirrored from a C mode via Cgo getters (tests).
type OpusCustomMode struct {
	Fs      opus_int32
	overlap int

	nbEBands  int
	effEBands int
	preemph   [4]opus_val16
	eBands    []opus_int16

	maxLM         int
	nbShortMdcts  int
	shortMdctSize int

	nbAllocVectors int
	allocVectors   []byte
	logN           []opus_int16

	window celt_window
	mdct   mdct_lookup
	cache  PulseCache
}

// celt_window — alias for the window coefficient slice. The C typedef
// is `const celt_coef *window` which we represent as a slice.
type celt_window = []celt_coef

// CELTMode — typedef used in many libopus files. Matches C: `typedef
// struct OpusCustomMode CELTMode;` (celt.h).
type CELTMode = OpusCustomMode
