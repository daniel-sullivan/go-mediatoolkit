package r128

import "testing"

// TestModeConstantsMatchHeader pins the unexported mode bits to the exact
// numeric values of libebur128's `enum mode` (loudness/libebur128/ebur128.h).
// These values are load-bearing: the root loudness.Mode passes straight
// through to r128.NewState as raw bits, and each parity slice hands the
// identical integer to the C ebur128_init, so a single wrong bit here would
// silently meter the wrong thing while still "matching" the wrong C call.
func TestModeConstantsMatchHeader(t *testing.T) {
	// Literal values transcribed from ebur128.h's `enum mode`:
	//   EBUR128_MODE_M           = (1 << 0)                                   = 1
	//   EBUR128_MODE_S           = (1 << 1) | M                               = 3
	//   EBUR128_MODE_I           = (1 << 2) | M                               = 5
	//   EBUR128_MODE_LRA         = (1 << 3) | S                               = 11
	//   EBUR128_MODE_SAMPLE_PEAK = (1 << 4) | M                               = 17
	//   EBUR128_MODE_TRUE_PEAK   = (1 << 5) | M | SAMPLE_PEAK                 = 49
	//   EBUR128_MODE_HISTOGRAM   = (1 << 6)                                   = 64
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"modeM", modeM, 1},
		{"modeS", modeS, 3},
		{"modeI", modeI, 5},
		{"modeLRA", modeLRA, 11},
		{"modeSamplePeak", modeSamplePeak, 17},
		{"modeTruePeak", modeTruePeak, 49},
		{"modeHistogram", modeHistogram, 64},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d (ebur128.h enum mode)", c.name, c.got, c.want)
		}
	}
}

// TestChannelConstantsMatchHeader pins the unexported channel enum values
// the gating-block weighting and default channel map depend on to
// libebur128's `enum channel` (ebur128.h). A wrong value would misapply the
// BS.1770 surround/dual-mono weights or the UNUSED skip.
func TestChannelConstantsMatchHeader(t *testing.T) {
	// Literal values transcribed from ebur128.h's `enum channel`:
	//   EBUR128_UNUSED = 0, EBUR128_LEFT = 1, EBUR128_RIGHT = 2,
	//   EBUR128_CENTER = 3, EBUR128_LEFT_SURROUND = 4,
	//   EBUR128_RIGHT_SURROUND = 5, EBUR128_DUAL_MONO = 6, then the
	//   sequential ITU positions (MpSC=7, MmSC=8) put Mp060=9, Mm060=10,
	//   Mp090=11, Mm090=12.
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"channelUnused", channelUnused, 0},
		{"channelLeft", channelLeft, 1},
		{"channelRight", channelRight, 2},
		{"channelCenter", channelCenter, 3},
		{"channelLeftSurround", channelLeftSurround, 4},
		{"channelRightSurround", channelRightSurround, 5},
		{"channelDualMono", channelDualMono, 6},
		{"channelMp060", channelMp060, 9},
		{"channelMm060", channelMm060, 10},
		{"channelMp090", channelMp090, 11},
		{"channelMm090", channelMm090, 12},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d (ebur128.h enum channel)", c.name, c.got, c.want)
		}
	}
}
