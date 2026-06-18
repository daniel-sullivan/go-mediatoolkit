// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package ics_parse

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// bufBytes is the fabricated bit-buffer length. It must be a power of two
// (the FDK FDK_BITBUF invariant, mirrored by nativeaac's bit reader). One
// raw_data_block ics_info + section_data fits comfortably in 512 bytes.
const bufBytes = 512

// flagsLC is the AC_* flag set for plain AAC-LC: none. IcsRead /
// CBlock_ReadSectionData take their plain path.
const flagsLC = 0

// msbWriter is an MSB-first bit writer used to fabricate syntactically valid
// ics_info + section_data bit streams. Both the C oracle and the Go port read
// the SAME bytes back, so this writer only needs to lay the fields down in the
// correct order/width — it is not itself under test. It mirrors the byte/bit
// order of the FDK reader (big-endian, most-significant bit first).
type msbWriter struct {
	buf    []byte
	bitPos int
}

func newMsbWriter(n int) *msbWriter { return &msbWriter{buf: make([]byte, n)} }

// writeBits lays numberOfBits of value (right-aligned) MSB-first.
func (w *msbWriter) writeBits(value uint32, numberOfBits int) {
	for i := numberOfBits - 1; i >= 0; i-- {
		bit := (value >> uint(i)) & 1
		if bit != 0 {
			byteIdx := w.bitPos >> 3
			bitIdx := uint(7 - (w.bitPos & 7))
			w.buf[byteIdx] |= byte(1 << bitIdx)
		}
		w.bitPos++
	}
}

// asScfg lists the AAC-LC sampling-rate configurations the parser supports
// (1024-line frames). Each pair is (samplingRateIndex, samplingRate) per the
// MPEG-4 sampling-frequency table; the rate is only used by getSamplingRateInfo
// when the index is >= 15, so the index is authoritative here.
var ascCfg = []struct {
	index uint32
	rate  uint32
}{
	{0, 96000}, {1, 88200}, {2, 64000}, {3, 48000}, {4, 44100},
	{5, 32000}, {6, 24000}, {7, 22050}, {8, 16000}, {9, 12000},
	{10, 11025}, {11, 8000},
}

// numSfbLong / numSfbShort give the total long/short scalefactor-band counts
// per sampling-rate index for 1024-line AAC-LC, matching the ROM the parser
// selects (sfbOffsetTables[0][index]). They bound the fabricated max_sfb so the
// success path is exercised (max_sfb <= total never trips the parse error).
//
// Values transcribed from ISO/IEC 14496-3 Table 4.130 / the FDK
// sfb_*_1024 ROM lengths; cross-checked at run time against TotalSfBands the C
// oracle reports.
var numSfbLong = [12]int{41, 41, 47, 49, 49, 51, 47, 47, 43, 43, 43, 40}
var numSfbShort = [12]int{12, 12, 12, 14, 14, 14, 15, 15, 15, 15, 15, 15}

// fabricateLong lays a long-block ics_info + section_data raw_data_block for the
// given config, with the given max_sfb and a random valid section layout, and
// returns the buffer plus the chosen max_sfb. predictorDataPresent is written 0
// (AAC-LC). The section layout is a random partition of [0, max_sfb) into runs,
// each tagged with a legal codebook.
func fabricateLong(r *rand.Rand, index uint32, maxSfb int, commonWindow uint8) []byte {
	w := newMsbWriter(bufBytes)

	// --- ics_info (long) ---
	w.writeBits(0, 1)                 // ics_reserved_bit
	w.writeBits(0, 2)                 // window_sequence = BLOCK_LONG
	w.writeBits(uint32(r.IntN(2)), 1) // window_shape
	w.writeBits(uint32(maxSfb), 6)    // max_sfb (6 bits for long)
	w.writeBits(0, 1)                 // predictor_data_present = 0

	// --- section_data (long): nbits=5, sect_esc_val=31 ---
	writeSections(w, r, 1 /* one window group for long */, maxSfb, 5, commonWindow)
	return w.buf
}

// fabricateShort lays a short-block ics_info + section_data raw_data_block: an
// 8-window short sequence with a random scale_factor_grouping, the given
// per-group max_sfb, and a random section layout per group.
func fabricateShort(r *rand.Rand, maxSfb int, scaleFactorGrouping uint32, commonWindow uint8, windowGroups int) []byte {
	w := newMsbWriter(bufBytes)

	// --- ics_info (short) ---
	w.writeBits(0, 1)                   // ics_reserved_bit
	w.writeBits(2, 2)                   // window_sequence = BLOCK_SHORT
	w.writeBits(uint32(r.IntN(2)), 1)   // window_shape
	w.writeBits(uint32(maxSfb), 4)      // max_sfb (4 bits for short)
	w.writeBits(scaleFactorGrouping, 7) // scale_factor_grouping

	// --- section_data (short): nbits=3, sect_esc_val=7, per window group ---
	writeSections(w, r, windowGroups, maxSfb, 3, commonWindow)
	return w.buf
}

// writeSections lays a random legal section_data partition for `groups` window
// groups, each covering [0, maxSfb). nbits is the run-length field width (5 for
// long, 3 for short). Codebooks are drawn from the legal set: 0..11 always, and
// 14/15 (intensity) only when commonWindow != 0. The run lengths use the
// all-ones escape when a run exceeds the field max, exercising the escape loop.
func writeSections(w *msbWriter, r *rand.Rand, groups, maxSfb, nbits int, commonWindow uint8) {
	escVal := (1 << uint(nbits)) - 1
	for g := 0; g < groups; g++ {
		band := 0
		for band < maxSfb {
			// Choose a legal section codebook.
			cb := legalCodebook(r, commonWindow)
			// Choose a run length in [1, maxSfb-band].
			run := 1 + r.IntN(maxSfb-band)

			w.writeBits(uint32(cb), 4) // sect_cb (4 bits, non-VCB11)

			// Emit run as escape-coded increments of width nbits.
			rem := run
			for rem >= escVal {
				w.writeBits(uint32(escVal), nbits)
				rem -= escVal
			}
			w.writeBits(uint32(rem), nbits)

			band += run
		}
	}
}

// legalCodebook returns a codebook index legal as a section codebook: 0..11
// (ZERO_HCB .. ESCBOOK), plus the intensity books 14/15 only when commonWindow
// is set. BOOKSCL (12), NOISE_HCB (13) and the reserved book are excluded so
// readSectionData never returns AAC_DEC_INVALID_CODE_BOOK on the success sweep.
func legalCodebook(r *rand.Rand, commonWindow uint8) int {
	if commonWindow != 0 {
		opts := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 14, 15}
		return opts[r.IntN(len(opts))]
	}
	return r.IntN(12) // 0..11
}

// TestParityIcsParseLong sweeps long-block ics_info + section_data across every
// AAC-LC sampling-rate config, every legal max_sfb, both common-window settings,
// and many random section layouts. The full parsed ics struct, the section
// codebook array, the section count, the return code AND the bit position are
// compared bit-for-bit.
func TestParityIcsParseLong(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for cfgIdx, cfg := range ascCfg {
		totalLong := numSfbLong[cfgIdx]
		for trial := 0; trial < 400; trial++ {
			maxSfb := r.IntN(totalLong + 1) // 0..totalLong (all legal)
			commonWindow := uint8(r.IntN(2))
			buf := fabricateLong(r, cfg.index, maxSfb, commonWindow)

			gotC := cIcsParse(buf, uint32(bufBytes*8), 1024, cfg.index, cfg.rate, commonWindow, flagsLC)
			gotN := nativeaac.ParseIcsAndSectionData(buf, bufBytes, uint32(bufBytes*8),
				1024, cfg.index, cfg.rate, commonWindow, flagsLC)
			bitN := nativeaac.ParseIcsAndSectionDataBitPos(buf, bufBytes, uint32(bufBytes*8),
				1024, cfg.index, cfg.rate, commonWindow, flagsLC)

			requireEqual(t, gotC, gotN, bitN, "long cfg=%d trial=%d maxSfb=%d cw=%d",
				cfg.index, trial, maxSfb, commonWindow)
		}
	}
}

// TestParityIcsParseShort sweeps short-block ics_info + section_data: every
// scale_factor_grouping (0..127, exhaustive), legal max_sfb, both common-window
// settings, and random section layouts. This exercises the window-group-length
// derivation (the grouping bit-mask loop) plus the short-block section path.
func TestParityIcsParseShort(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	// Use a mid-rate config; the short total-sfb bound is what matters, and it
	// is exercised across configs in the random sub-sweep below.
	const cfgIdx = 3 // 48 kHz
	cfg := ascCfg[cfgIdx]
	totalShort := numSfbShort[cfgIdx]

	for grouping := 0; grouping < 128; grouping++ {
		for trial := 0; trial < 16; trial++ {
			maxSfb := r.IntN(totalShort + 1)
			commonWindow := uint8(r.IntN(2))

			// Derive window-group count from grouping the same way the parser
			// does, to size the per-group section sweep.
			windowGroups := windowGroupsOf(uint32(grouping))
			buf := fabricateShort(r, maxSfb, uint32(grouping), commonWindow, windowGroups)

			gotC := cIcsParse(buf, uint32(bufBytes*8), 1024, cfg.index, cfg.rate, commonWindow, flagsLC)
			gotN := nativeaac.ParseIcsAndSectionData(buf, bufBytes, uint32(bufBytes*8),
				1024, cfg.index, cfg.rate, commonWindow, flagsLC)
			bitN := nativeaac.ParseIcsAndSectionDataBitPos(buf, bufBytes, uint32(bufBytes*8),
				1024, cfg.index, cfg.rate, commonWindow, flagsLC)

			requireEqual(t, gotC, gotN, bitN, "short grouping=%d trial=%d maxSfb=%d cw=%d",
				grouping, trial, maxSfb, commonWindow)
		}
	}
}

// TestParityIcsParseShortConfigs sweeps short blocks across all sampling-rate
// configs with random groupings, to cover every short-block ROM row.
func TestParityIcsParseShortConfigs(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for cfgIdx, cfg := range ascCfg {
		totalShort := numSfbShort[cfgIdx]
		for trial := 0; trial < 200; trial++ {
			maxSfb := r.IntN(totalShort + 1)
			commonWindow := uint8(r.IntN(2))
			grouping := uint32(r.IntN(128))
			windowGroups := windowGroupsOf(grouping)
			buf := fabricateShort(r, maxSfb, grouping, commonWindow, windowGroups)

			gotC := cIcsParse(buf, uint32(bufBytes*8), 1024, cfg.index, cfg.rate, commonWindow, flagsLC)
			gotN := nativeaac.ParseIcsAndSectionData(buf, bufBytes, uint32(bufBytes*8),
				1024, cfg.index, cfg.rate, commonWindow, flagsLC)
			bitN := nativeaac.ParseIcsAndSectionDataBitPos(buf, bufBytes, uint32(bufBytes*8),
				1024, cfg.index, cfg.rate, commonWindow, flagsLC)

			requireEqual(t, gotC, gotN, bitN, "shortcfg cfg=%d trial=%d maxSfb=%d cw=%d grp=%d",
				cfg.index, trial, maxSfb, commonWindow, grouping)
		}
	}
}

// TestParityIcsParseMaxSfbOverflow drives the IcsReadMaxSfb parse-error path:
// max_sfb deliberately exceeds the total band count, so IcsRead returns
// AAC_DEC_PARSE_ERROR before section_data. Both sides must agree on the error
// code and the (partial) ics struct.
func TestParityIcsParseMaxSfbOverflow(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for cfgIdx, cfg := range ascCfg {
		totalLong := numSfbLong[cfgIdx]
		for trial := 0; trial < 100; trial++ {
			// max_sfb in (total, 63] — overflow.
			over := totalLong + 1 + r.IntN(63-totalLong)
			w := newMsbWriter(bufBytes)
			w.writeBits(0, 1)
			w.writeBits(0, 2) // long
			w.writeBits(uint32(r.IntN(2)), 1)
			w.writeBits(uint32(over), 6)
			buf := w.buf

			gotC := cIcsParse(buf, uint32(bufBytes*8), 1024, cfg.index, cfg.rate, 0, flagsLC)
			gotN := nativeaac.ParseIcsAndSectionData(buf, bufBytes, uint32(bufBytes*8),
				1024, cfg.index, cfg.rate, 0, flagsLC)
			bitN := nativeaac.ParseIcsAndSectionDataBitPos(buf, bufBytes, uint32(bufBytes*8),
				1024, cfg.index, cfg.rate, 0, flagsLC)

			requireEqual(t, gotC, gotN, bitN, "overflow cfg=%d trial=%d over=%d", cfg.index, trial, over)
		}
	}
}

// windowGroupsOf mirrors IcsRead's window-group derivation from
// scale_factor_grouping (channelinfo.cpp:184) so the test can size the per-group
// section sweep. It is a fabrication helper, not the kernel under test.
func windowGroupsOf(grouping uint32) int {
	groups := 0
	for i := 0; i < 7; i++ {
		mask := uint32(1) << uint(6-i)
		if grouping&mask == 0 {
			groups++
		}
	}
	groups++
	return groups
}

// requireEqual asserts the C oracle result equals the Go port field-by-field
// (the flattened ics struct + codebook array + section count + return code) and
// that the Go bit position equals the C bit position. EXACT equality, no
// tolerance.
func requireEqual(t *testing.T, c icsResult, n nativeaac.IcsParseResult, bitN uint32, msg string, args ...any) {
	t.Helper()
	require.Equalf(t, c.windowGroupLength, n.WindowGroupLength, msg+" windowGroupLength", args...)
	require.Equalf(t, c.windowGroups, n.WindowGroups, msg+" windowGroups", args...)
	require.Equalf(t, c.valid, n.Valid, msg+" valid", args...)
	require.Equalf(t, c.windowShape, n.WindowShape, msg+" windowShape", args...)
	require.Equalf(t, c.windowSequence, n.WindowSequence, msg+" windowSequence", args...)
	require.Equalf(t, c.maxSfBands, n.MaxSfBands, msg+" maxSfBands", args...)
	require.Equalf(t, c.scaleFactorGrouping, n.ScaleFactorGrouping, msg+" scaleFactorGrouping", args...)
	require.Equalf(t, c.totalSfBands, n.TotalSfBands, msg+" totalSfBands", args...)
	require.Equalf(t, c.codeBook, n.CodeBook, msg+" codeBook", args...)
	require.Equalf(t, c.numberSection, n.NumberSection, msg+" numberSection", args...)
	require.Equalf(t, c.errorCode, n.ErrorCode, msg+" errorCode", args...)
	require.Equalf(t, c.bitPos, bitN, msg+" bitPos", args...)
}
