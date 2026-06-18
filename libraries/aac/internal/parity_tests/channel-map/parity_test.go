// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package channelmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// allModes is the set of CHANNEL_MODE values channel_map.cpp lays out (the
// channelModeConfig[] table). Each is exercised through the full init tier.
var allModes = []struct {
	name string
	mode nativeaac.ChannelMode
}{
	{"MODE_1", nativeaac.ChannelMode1},
	{"MODE_2", nativeaac.ChannelMode2},
	{"MODE_1_2", nativeaac.ChannelMode1_2},
	{"MODE_1_2_1", nativeaac.ChannelMode1_2_1},
	{"MODE_1_2_2", nativeaac.ChannelMode1_2_2},
	{"MODE_1_2_2_1", nativeaac.ChannelMode1_2_2_1},
	{"MODE_1_2_2_2_1", nativeaac.ChannelMode1_2_2_2_1},
	{"MODE_6_1", nativeaac.ChannelMode6_1},
	{"MODE_7_1_BACK", nativeaac.ChannelMode7_1Back},
	{"MODE_7_1_TOP_FRONT", nativeaac.ChannelMode7_1TopFront},
	{"MODE_7_1_REAR_SURROUND", nativeaac.ChannelMode7_1RearSurr},
	{"MODE_7_1_FRONT_CENTER", nativeaac.ChannelMode7_1FrontCent},
}

// chOrders are the channel orderings the AAC encoder may use; CH_ORDER_MPEG is
// the production AAC-LC default (aacenc.cpp:361), the others exercise the
// default-table map path.
var chOrders = []struct {
	name string
	co   nativeaac.ChannelOrder
}{
	{"CH_ORDER_MPEG", nativeaac.ChOrderMPEG},
	{"CH_ORDER_WAV", nativeaac.ChOrderWAV},
	{"CH_ORDER_WG4", nativeaac.ChOrderWG4},
}

// TestDetermineEncoderModeParity asserts FDKaacEnc_DetermineEncoderMode resolves
// / validates identically for the unknown-mode (channel-count lookup) and the
// explicit-mode validation paths.
func TestDetermineEncoderModeParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("channel-map parity asserts exact int equality; run under aac_strict")
	}

	// Unknown mode, resolved by channel count (1..8 and a couple of misses).
	for _, nCh := range []int{1, 2, 3, 4, 5, 6, 7, 8, 0, 9} {
		gMode, gRC := nativeaac.DetermineEncoderMode(nativeaac.ChannelModeUnknown, nCh)
		cMode, cRC := cDetermineEncoderMode(int32(nativeaac.ChannelModeUnknown), nCh)
		assert.Equal(t, cRC, int(gRC), "rc (unknown) nCh=%d", nCh)
		assert.Equal(t, cMode, int32(gMode), "resolved mode (unknown) nCh=%d", nCh)
	}

	// Explicit mode validated against a matching / mismatching channel count.
	for _, m := range allModes {
		nChCfg, ok := nativeaac.ChannelModeNChannelsForTest(m.mode)
		require.True(t, ok, m.name)
		for _, nCh := range []int{nChCfg, nChCfg + 1, 1} {
			gMode, gRC := nativeaac.DetermineEncoderMode(m.mode, nCh)
			cMode, cRC := cDetermineEncoderMode(int32(m.mode), nCh)
			assert.Equal(t, cRC, int(gRC), "rc (explicit) %s nCh=%d", m.name, nCh)
			assert.Equal(t, cMode, int32(gMode), "mode (explicit) %s nCh=%d", m.name, nCh)
		}
	}
}

// TestInitChannelMappingParity asserts FDKaacEnc_InitChannelMapping produces a
// byte-identical CHANNEL_MAPPING (every ELEMENT_INFO field) for every mode and
// channel order.
func TestInitChannelMappingParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("channel-map parity asserts exact int equality; run under aac_strict")
	}

	for _, m := range allModes {
		for _, o := range chOrders {
			t.Run(m.name+"/"+o.name, func(t *testing.T) {
				var cm nativeaac.ChannelMapping
				gRC := nativeaac.InitChannelMapping(m.mode, o.co, &cm)
				c, cRC := cInitChannelMapping(int32(m.mode), int(o.co))

				assert.Equal(t, cRC, int(gRC), "rc")
				assert.Equal(t, c.encMode, int32(cm.EncMode), "encMode")
				assert.Equal(t, c.nChannels, int32(cm.NChannels), "nChannels")
				assert.Equal(t, c.nChannelsEff, int32(cm.NChannelsEff), "nChannelsEff")
				assert.Equal(t, c.nElements, int32(cm.NElements), "nElements")
				for i := 0; i < 8; i++ {
					assert.Equal(t, c.elType[i], int32(cm.ElInfo[i].ElType), "elInfo[%d].elType", i)
					assert.Equal(t, c.instanceTag[i], int32(cm.ElInfo[i].InstanceTag), "elInfo[%d].instanceTag", i)
					assert.Equal(t, c.nChannelsInEl[i], int32(cm.ElInfo[i].NChannelsInEl), "elInfo[%d].nChannelsInEl", i)
					assert.Equal(t, c.channelIndex0[i], int32(cm.ElInfo[i].ChannelIndex[0]), "elInfo[%d].ChannelIndex[0]", i)
					assert.Equal(t, c.channelIndex1[i], int32(cm.ElInfo[i].ChannelIndex[1]), "elInfo[%d].ChannelIndex[1]", i)
					assert.Equal(t, c.relativeBits[i], cm.ElInfo[i].RelativeBits, "elInfo[%d].relativeBits", i)
				}
			})
		}
	}
}

// TestInitElementBitsParity asserts FDKaacEnc_InitElementBits splits the bit
// budget across QC_STATE.elementBits[] identically, over a range of realistic
// CBR bit budgets (incl. the LFE modes that exercise the GetInvInt / fMax path).
func TestInitElementBitsParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("channel-map parity asserts exact int equality; run under aac_strict")
	}

	// (bitrateTot, averageBitsTot, maxChannelBits) triples spanning low..high CBR.
	budgets := []struct{ bitrate, avgBits, maxChBits int }{
		{64000, 1280, 6144},
		{128000, 2880, 6144},
		{192000, 4416, 6144},
		{256000, 5952, 6144},
		{320000, 7296, 6144},
		{96000, 2112, 6144},
		{48000, 1024, 6144},
	}

	for _, m := range allModes {
		for _, o := range chOrders {
			for _, b := range budgets {
				name := m.name + "/" + o.name
				t.Run(name, func(t *testing.T) {
					// Build the genuine-equivalent CHANNEL_MAPPING via the Go port.
					var cm nativeaac.ChannelMapping
					require.Equal(t, nativeaac.AacEncOK,
						nativeaac.InitChannelMapping(m.mode, o.co, &cm), "channel mapping init")

					hQC := new(nativeaac.QcState)
					for i := range hQC.ElementBits {
						hQC.ElementBits[i] = new(nativeaac.ElementBits)
					}
					gRC := nativeaac.InitElementBits(hQC, &cm, b.bitrate, b.avgBits, b.maxChBits)

					c, cRC := cInitElementBits(int32(m.mode), int(o.co), b.bitrate, b.avgBits, b.maxChBits)
					require.NotEqual(t, -100, cRC, "C channel mapping init failed")

					assert.Equal(t, cRC, int(gRC), "rc bitrate=%d", b.bitrate)
					for i := 0; i < 8; i++ {
						assert.Equal(t, c.chBitrateEl[i], int32(hQC.ElementBits[i].ChBitrateEl), "elementBits[%d].chBitrateEl bitrate=%d", i, b.bitrate)
						assert.Equal(t, c.maxBitsEl[i], int32(hQC.ElementBits[i].MaxBitsEl), "elementBits[%d].maxBitsEl bitrate=%d", i, b.bitrate)
						assert.Equal(t, c.relativeBitsEl[i], hQC.ElementBits[i].RelativeBitsEl, "elementBits[%d].relativeBitsEl bitrate=%d", i, b.bitrate)
					}
				})
			}
		}
	}
}
