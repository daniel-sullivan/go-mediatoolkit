// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Parity-only exports for the bandwidth-expert (bandwidth.cpp) port. These thin
// wrappers let the cgo parity slice under internal/parity_tests/enc-bandwidth/
// drive the unexported determineBandWidth / getBandwidthEntry against the
// genuine vendored FDKaacEnc_DetermineBandWidth and compare bit-for-bit. Not
// part of the production API.

// ParityDetermineBandWidth runs determineBandWidth for the given parameters,
// building a one-field ChannelMapping carrying nChannelsEff/encMode, and returns
// (bandWidth, errorCode) where errorCode is the int AAC_ENCODER_ERROR value.
func ParityDetermineBandWidth(proposedBandWidth, bitrate, bitrateMode, sampleRate,
	frameLength, nChannelsEff, encoderMode int) (int, int) {
	cm := &ChannelMapping{NChannelsEff: nChannelsEff}
	bw, err := determineBandWidth(proposedBandWidth, bitrate, AacencBitrateMode(bitrateMode),
		sampleRate, frameLength, cm, ChannelMode(encoderMode))
	return bw, int(err)
}

// ParityGetBandwidthEntry wraps getBandwidthEntry.
func ParityGetBandwidthEntry(frameLength, sampleRate, chanBitRate, entryNo int) int {
	return getBandwidthEntry(frameLength, sampleRate, chanBitRate, entryNo)
}
