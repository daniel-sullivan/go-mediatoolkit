// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encbandwidth

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// AACENC_BITRATE_MODE values (aacenc.h:191-199).
const (
	brCBR  = 0
	brVBR1 = 1
	brVBR5 = 5
	brSFR  = 7
	brFF   = 6
)

// CHANNEL_MODE values (FDK_audio.h:235) the bandwidth expert dispatches on.
const (
	mode1   = 1 // MODE_1   (mono)
	mode2   = 2 // MODE_2   (stereo)
	mode121 = 4 // MODE_1_2_1
)

// TestDetermineBandWidthParity asserts determineBandWidth == genuine
// FDKaacEnc_DetermineBandWidth (bandWidth + AAC_ENCODER_ERROR) across the AAC-LC
// CBR breakpoints, the VBR table path, the low-delay interpolation path, the
// proposed-bandwidth limiter, and the unsupported-mode/channel error returns.
func TestDetermineBandWidthParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}

	frameLengths := []int{1024, 960, 512, 480, 256, 240, 128, 120, 333 /* invalid */}
	sampleRates := []int{8000, 11025, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000}
	bitrateModes := []int{brCBR, brVBR1, brVBR5, brSFR, brFF, 99 /* unsupported */}
	encModes := []int{mode1, mode2, mode121, 0 /* MODE_UNKNOWN -> unsupported */}
	channels := []int{1, 2}
	proposed := []int{0, 12000, 25000}

	r := rand.New(rand.NewSource(0xBA7D))

	for _, fl := range frameLengths {
		for _, sr := range sampleRates {
			for _, bm := range bitrateModes {
				for _, em := range encModes {
					for _, nc := range channels {
						for _, pb := range proposed {
							// per-channel bitrate range: a spread that lands in every breakpoint
							bitrate := (8000 + r.Intn(560000)) * nc
							gbw, gerr := cDetermineBandWidth(pb, bitrate, bm, sr, fl, nc, em)
							nbw, nerr := nativeaac.ParityDetermineBandWidth(pb, bitrate, bm, sr, fl, nc, em)
							assert.Equalf(t, gerr, nerr,
								"err fl=%d sr=%d bm=%d em=%d nc=%d pb=%d br=%d", fl, sr, bm, em, nc, pb, bitrate)
							assert.Equalf(t, gbw, nbw,
								"bw fl=%d sr=%d bm=%d em=%d nc=%d pb=%d br=%d", fl, sr, bm, em, nc, pb, bitrate)
						}
					}
				}
			}
		}
	}
}

// TestGetBandwidthEntryParity asserts the static GetBandwidthEntry matches for
// the LC and every low-delay table, including the fixed-point interpolation
// branch, across the bitrate breakpoints.
func TestGetBandwidthEntryParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	frameLengths := []int{1024, 960, 512, 480, 256, 240, 128, 120, 333}
	sampleRates := []int{8000, 11025, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 96000}
	entryNos := []int{0, 1}

	for _, fl := range frameLengths {
		for _, sr := range sampleRates {
			for _, en := range entryNos {
				for cbr := 0; cbr <= 620000; cbr += 137 {
					g := cGetBandwidthEntry(fl, sr, cbr, en)
					n := nativeaac.ParityGetBandwidthEntry(fl, sr, cbr, en)
					assert.Equalf(t, g, n, "fl=%d sr=%d cbr=%d en=%d", fl, sr, cbr, en)
				}
			}
		}
	}
}
