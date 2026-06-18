// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported helper for the channel-map parity oracle
// (internal/parity_tests/channel-map). The init/config-tier entry points
// (DetermineEncoderMode / InitChannelMapping / InitElementBits) and the
// ChannelMapping / QcState / ElementBits structs are already exported and drive
// the oracle directly; this only surfaces the channelModeConfig[] nChannels
// lookup the explicit-mode DetermineEncoderMode parity case needs, without
// leaking the unexported channelModeConfigTabEntry type.

// ChannelModeNChannelsForTest returns channelModeConfig[mode].nChannels (the
// FDKaacEnc_GetChannelModeConfiguration()->nChannels the explicit-mode
// FDKaacEnc_DetermineEncoderMode path validates against), or (0, false) when the
// mode is not in the table.
func ChannelModeNChannelsForTest(mode ChannelMode) (int, bool) {
	cfg := getChannelModeConfiguration(mode)
	if cfg == nil {
		return 0, false
	}
	return cfg.nChannels, true
}
