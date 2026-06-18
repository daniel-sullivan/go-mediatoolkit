// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// samplingRateTable maps an MPEG sampling-frequency index to its sampling rate
// in Hz. 1:1 port of SamplingRateTable in
// libfdk/libMpegTPDec/include/tp_data.h:414.
var samplingRateTable = [32]uint32{
	96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025,
	8000, 7350, 0, 0, 57600, 51200, 40000, 38400, 34150, 28800, 25600,
	20000, 19200, 17075, 14400, 12800, 9600, 0, 0, 0, 0,
}

// getNumberOfTotalChannels returns the total channel count for a channel
// configuration. 1:1 port of getNumberOfTotalChannels in
// libfdk/libMpegTPDec/include/tp_data.h:437.
func getNumberOfTotalChannels(channelConfig int) int {
	switch channelConfig {
	case 1, 2, 3, 4, 5, 6:
		return channelConfig
	case 7, 12, 14:
		return 8
	case 11:
		return 7
	case 13:
		return 24
	default:
		return 0
	}
}

// getNumberOfEffectiveChannels returns the effective channel count used by the
// ADTS buffer-fullness computation. 1:1 port of getNumberOfEffectiveChannels in
// libfdk/libMpegTPDec/include/tp_data.h:459. Index range is 0..15.
func getNumberOfEffectiveChannels(channelConfig int) int {
	n := [16]int{0, 1, 2, 3, 4, 5, 5, 7, 0, 0, 0, 6, 7, 22, 7, 0}
	return n[channelConfig]
}
