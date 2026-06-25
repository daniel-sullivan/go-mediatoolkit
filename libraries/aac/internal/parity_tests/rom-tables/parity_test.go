// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package rom_tables

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// frameLengths enumerates every samplesPerFrame value getSamplingRateInfo's
// switch (channelinfo.cpp:264) accepts (1024 / 960 / 768 / 512 / 480) plus a
// representative unsupported value (1023) that drives the default-branch
// AAC_DEC_UNSUPPORTED_FORMAT return. These are the only frame lengths the parse
// stage ever passes in.
var frameLengths = []uint32{1024, 960, 768, 512, 480, 1023}

// sampleRates pairs an MPEG sampling-rate index (0..15) with its nominal rate,
// mirroring the standard sampling-frequency table the transport layer resolves
// before calling getSamplingRateInfo. Index 13/14 are reserved (rate 0 here);
// 15 forces the rate-search path. Index, rate, and the search path are all
// exercised so both the direct-index branch and the ISO/IEC 13818-7 8.2.4
// border search (channelinfo.cpp:230) are covered on both sides.
var sampleRates = []struct {
	idx  uint32
	rate uint32
}{
	{0, 96000}, {1, 88200}, {2, 64000}, {3, 48000}, {4, 44100},
	{5, 32000}, {6, 24000}, {7, 22050}, {8, 16000}, {9, 12000},
	{10, 11025}, {11, 8000}, {12, 7350}, {13, 0}, {14, 0},
	// Index 15 forces getSamplingRateInfo to re-derive the index from the
	// rate via the border search; sweep a spread of rates through it.
	{15, 96000}, {15, 48000}, {15, 44100}, {15, 32000}, {15, 24000},
	{15, 22050}, {15, 16000}, {15, 12000}, {15, 11025}, {15, 8000},
	{15, 7350}, {15, 100000}, {15, 1},
}

// TestParityGetSamplingRateInfo sweeps getSamplingRateInfo over every frame
// length crossed with every (sampling-rate index, rate) pair and asserts the
// pure-Go nativeaac port reproduces the vendored C reference EXACTLY: the raw
// AAC_DECODER_ERROR code, the resolved long/short band counts, the
// sampling-rate index/rate, whether each resolved table pointer is NULL, and —
// when non-NULL — every int16 offset in the resolved long and short tables
// [0 .. count] inclusive (the terminating transform length included). This is a
// pure integer ROM lookup, so the comparison is exact int16 / int equality with
// no tolerance; it is fenced by aacfdk alone (no aac_strict — no float in this
// path), matching the C kernel bit-for-bit in any build.
func TestParityGetSamplingRateInfo(t *testing.T) {
	for _, fl := range frameLengths {
		for _, sr := range sampleRates {
			fl, sr := fl, sr
			got := cGetSamplingRateInfo(fl, sr.idx, sr.rate)
			info, errN := nativeaac.GetSamplingRateInfo(fl, sr.idx, sr.rate)

			ctx := func(field string) string {
				return field + " (samplesPerFrame=" +
					itoa(fl) + " srIdx=" + itoa(sr.idx) +
					" rate=" + itoa(sr.rate) + ")"
			}

			require.Equal(t, got.err, errN, ctx("err"))
			assert.Equal(t, got.samplingRateIdx, info.SamplingRateIndex,
				ctx("samplingRateIndex"))
			assert.Equal(t, got.samplingRate, info.SamplingRate,
				ctx("samplingRate"))
			assert.Equal(t, got.numberOfSfbLong, info.NumberOfScaleFactorBandsLong,
				ctx("numberOfScaleFactorBandsLong"))
			assert.Equal(t, got.numberOfSfbShort, info.NumberOfScaleFactorBandsShort,
				ctx("numberOfScaleFactorBandsShort"))

			// The C reference reports whether each resolved pointer was NULL;
			// the Go port reports a nil slice for the same NULL pointer.
			assert.Equal(t, got.longIsNull, info.ScaleFactorBandsLong == nil,
				ctx("longIsNull"))
			assert.Equal(t, got.shortIsNull, info.ScaleFactorBandsShort == nil,
				ctx("shortIsNull"))

			// Compare the resolved offset tables value-for-value over the
			// [0 .. count] inclusive range the oracle copied (the C copies
			// number_of_sfb + 1 entries, including the terminating transform
			// length).
			if !got.longIsNull && info.ScaleFactorBandsLong != nil {
				n := int(got.numberOfSfbLong) + 1
				require.GreaterOrEqual(t, len(info.ScaleFactorBandsLong), n,
					ctx("long table too short"))
				assert.Equal(t, got.longOffsets, info.ScaleFactorBandsLong[:n],
					ctx("longOffsets"))
			}
			if !got.shortIsNull && info.ScaleFactorBandsShort != nil {
				n := int(got.numberOfSfbShort) + 1
				require.GreaterOrEqual(t, len(info.ScaleFactorBandsShort), n,
					ctx("short table too short"))
				assert.Equal(t, got.shortOffsets, info.ScaleFactorBandsShort[:n],
					ctx("shortOffsets"))
			}
		}
	}
}

// itoa renders a uint32 for the test failure context strings without pulling
// strconv into the assertion hot path's signature.
func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
