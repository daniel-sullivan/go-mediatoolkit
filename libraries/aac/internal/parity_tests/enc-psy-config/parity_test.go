// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package enc_psy_config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// These slices assert the pure-Go FDKaacEnc_InitPsyConfiguration port
// (nativeaac.InitPsyConfiguration) is bit-for-bit identical to the genuine
// vendored libfdk kernel. fdk-aac encode is fixed-point, so equality is EXACT
// int32/int16 — no tolerance.

// psyConfCase is one (bitrate, samplerate, bandwidth, blocktype, granuleLength,
// useIS, useMS, filterbank) parameter tuple to init the psy config for.
type psyConfCase struct {
	name          string
	bitrate       int
	samplerate    int
	bandwidth     int
	blocktype     int // 0 LONG, 1 START, 2 SHORT, 3 STOP
	granuleLength int
	useIS         int
	useMS         int
	filterbank    int // 0 FB_LC
}

// psyConfCases covers the AAC-LC granuleLength-1024 sampling rates and both long
// and short block types, across a spread of bitrates / bandwidths and the
// useIS/useMS toggles (which gate allowIS via the bitrate/bandwidth ratio).
var psyConfCases = []psyConfCase{
	{"44100_long_128k", 128000, 44100, 20000, 0, 1024, 1, 1, 0},
	{"44100_short_128k", 128000, 44100, 20000, 2, 1024, 1, 1, 0},
	{"48000_long_192k", 192000, 48000, 22000, 0, 1024, 1, 1, 0},
	{"48000_short_64k", 64000, 48000, 16000, 2, 1024, 0, 1, 0},
	{"32000_long_96k", 96000, 32000, 15000, 0, 1024, 1, 0, 0},
	{"24000_long_48k", 48000, 24000, 11000, 0, 1024, 1, 1, 0},
	{"22050_long_32k", 32000, 22050, 10000, 0, 1024, 1, 1, 0},
	{"16000_long_24k", 24000, 16000, 7800, 0, 1024, 1, 1, 0},
	{"16000_short_24k", 24000, 16000, 7800, 2, 1024, 1, 1, 0},
	{"12000_long_16k", 16000, 12000, 5800, 0, 1024, 1, 1, 0},
	{"11025_long_16k", 16000, 11025, 5400, 0, 1024, 1, 1, 0},
	{"8000_long_12k", 12000, 8000, 3900, 0, 1024, 1, 1, 0},
	{"8000_short_12k", 12000, 8000, 3900, 2, 1024, 1, 1, 0},
	{"64000_long_320k", 320000, 64000, 28000, 0, 1024, 1, 1, 0},
	{"88200_long_320k", 320000, 88200, 30000, 0, 1024, 1, 1, 0},
	{"96000_long_320k", 320000, 96000, 32000, 0, 1024, 1, 1, 0},
	// allowIS gate: low bitrate/bandwidth ratio -> allowIS off even with useIS.
	{"44100_long_lowis", 320000, 44100, 20000, 0, 1024, 1, 1, 0},
	{"44100_start_128k", 128000, 44100, 20000, 1, 1024, 1, 1, 0},
	{"44100_stop_128k", 128000, 44100, 20000, 3, 1024, 1, 1, 0},
}

func TestInitPsyConfigurationParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-psy-config parity asserts under -tags aac_strict")
	}

	for _, tc := range psyConfCases {
		t.Run(tc.name, func(t *testing.T) {
			cConf, cErr := cInitPsyConfiguration(tc.bitrate, tc.samplerate, tc.bandwidth,
				tc.blocktype, tc.granuleLength, tc.useIS, tc.useMS, tc.filterbank)

			var goConf nativeaac.PsyConfiguration
			goErr := nativeaac.InitPsyConfiguration(tc.bitrate, tc.samplerate, tc.bandwidth,
				tc.blocktype, tc.granuleLength, tc.useIS, tc.useMS, &goConf, tc.filterbank)

			require.Equal(t, cErr, goErr, "error code")
			if cErr != 0 {
				return
			}

			assert.Equal(t, cConf.sfbCnt, int32(goConf.SfbCnt), "sfbCnt")
			assert.Equal(t, cConf.sfbActive, int32(goConf.SfbActive), "sfbActive")
			assert.Equal(t, cConf.sfbActiveLFE, int32(goConf.SfbActiveLFE), "sfbActiveLFE")
			assert.Equal(t, cConf.filterbank, int32(goConf.Filterbank), "filterbank")
			assert.Equal(t, cConf.granuleLength, int32(goConf.GranuleLength), "granuleLength")
			assert.Equal(t, cConf.allowIS, int32(goConf.AllowIS), "allowIS")
			assert.Equal(t, cConf.allowMS, int32(goConf.AllowMS), "allowMS")
			assert.Equal(t, cConf.maxAllowedIncreaseFactor, int32(goConf.MaxAllowedIncreaseFactor), "maxAllowedIncreaseFactor")
			assert.Equal(t, cConf.minRemainingThresholdFactor, goConf.MinRemainingThresholdFactor, "minRemainingThresholdFactor")
			assert.Equal(t, cConf.lowpassLine, int32(goConf.LowpassLine), "lowpassLine")
			assert.Equal(t, cConf.lowpassLineLFE, int32(goConf.LowpassLineLFE), "lowpassLineLFE")
			assert.Equal(t, cConf.clipEnergy, goConf.ClipEnergy, "clipEnergy")

			assert.Equal(t, cConf.sfbOffset[:], goConf.SfbOffset[:], "sfbOffset")
			assert.Equal(t, cConf.sfbPcmQuantThreshold[:], goConf.SfbPcmQuantThreshold[:], "sfbPcmQuantThreshold")
			assert.Equal(t, cConf.sfbMaskLowFactor[:], goConf.SfbMaskLowFactor[:], "sfbMaskLowFactor")
			assert.Equal(t, cConf.sfbMaskHighFactor[:], goConf.SfbMaskHighFactor[:], "sfbMaskHighFactor")
			assert.Equal(t, cConf.sfbMaskLowFactorSprEn[:], goConf.SfbMaskLowFactorSprEn[:], "sfbMaskLowFactorSprEn")
			assert.Equal(t, cConf.sfbMaskHighFactorSprEn[:], goConf.SfbMaskHighFactorSprEn[:], "sfbMaskHighFactorSprEn")
			assert.Equal(t, cConf.sfbMinSnrLdData[:], goConf.SfbMinSnrLdData[:], "sfbMinSnrLdData")

			// tnsConf/pnsConf are FDKmemcleared and never repopulated by
			// InitPsyConfiguration, so the C side must be all-zero, and the Go
			// port leaves them zero-valued too.
			assert.Equal(t, int32(0), cConf.tnsConfAllZero, "C tnsConf must be zero")
			assert.Equal(t, int32(0), cConf.pnsConfAllZero, "C pnsConf must be zero")
			assert.Equal(t, nativeaac.TNSConfig{}, goConf.TnsConf, "Go TnsConf must be zero")
			assert.Equal(t, nativeaac.PNSConfig{}, goConf.PnsConf, "Go PnsConf must be zero")
		})
	}
}
