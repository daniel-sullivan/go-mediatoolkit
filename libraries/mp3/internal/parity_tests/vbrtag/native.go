// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrtag

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"

// reconstructFromOracle builds a pure-Go LameInternalFlags + LameGlobalFlags
// that mirror the C oracle's captured -V2 encode state (cfg / ov_enc / ov_rpg /
// nMusicCRC / VBR_seek_table and the gfp bits PutLameVBR reads), so the native
// lame_get_lametag_frame produces a tag frame byte-identical to the genuine
// lame_get_lametag_frame the oracle captured. This is the same "inject identical
// state, run the genuine path on both sides" approach the other VBR slices use.
func reconstructFromOracle(c *cgoHandle) (*nativemp3.LameInternalFlags, *nativemp3.LameGlobalFlags) {
	gfc := new(nativemp3.LameInternalFlags)
	cfg := &gfc.Cfg

	cfg.WriteLameTag = c.cfg(C_VT_CFG_write_lame_tag)
	cfg.SideinfoLen = c.cfg(C_VT_CFG_sideinfo_len)
	cfg.ErrorProtection = c.cfg(C_VT_CFG_error_protection)
	cfg.Vbr = c.cfg(C_VT_CFG_vbr)
	cfg.Version = c.cfg(C_VT_CFG_version)
	cfg.SamplerateOut = c.cfg(C_VT_CFG_samplerate_out)
	cfg.SamplerateIndex = c.cfg(C_VT_CFG_samplerate_index)
	cfg.Extension = c.cfg(C_VT_CFG_extension)
	cfg.Mode = c.cfg(C_VT_CFG_mode)
	cfg.Copyright = c.cfg(C_VT_CFG_copyright)
	cfg.Original = c.cfg(C_VT_CFG_original)
	cfg.Emphasis = c.cfg(C_VT_CFG_emphasis)
	cfg.AvgBitrate = c.cfg(C_VT_CFG_avg_bitrate)
	cfg.FreeFormat = c.cfg(C_VT_CFG_free_format)
	cfg.NoiseShaping = c.cfg(C_VT_CFG_noise_shaping)
	cfg.ATHtype = c.cfg(C_VT_CFG_ATHtype)
	cfg.UseSafeJointStereo = c.cfg(C_VT_CFG_use_safe_joint_stereo)
	cfg.ForceMs = c.cfg(C_VT_CFG_force_ms)
	cfg.SamplerateIn = c.cfg(C_VT_CFG_samplerate_in)
	cfg.ShortBlocks = c.cfg(C_VT_CFG_short_blocks)
	cfg.LowpassFreq = c.cfg(C_VT_CFG_lowpassfreq)
	cfg.HighpassFreq = c.cfg(C_VT_CFG_highpassfreq)
	cfg.DisableReservoir = c.cfg(C_VT_CFG_disable_reservoir)
	cfg.FindReplayGain = c.cfg(C_VT_CFG_findReplayGain)
	cfg.FindPeakSample = c.cfg(C_VT_CFG_findPeakSample)
	cfg.VbrAvgBitrateKbps = c.cfg(C_VT_CFG_vbr_avg_bitrate_kbps)
	cfg.VbrMinBitrateIndex = c.cfg(C_VT_CFG_vbr_min_bitrate_index)
	cfg.Preset = c.cfg(C_VT_CFG_preset)
	cfg.ATHonly = c.cfg(C_VT_CFG_ATHonly)
	cfg.NoATH = c.cfg(C_VT_CFG_noATH)

	gfc.OvEnc.BitrateIndex = c.ovEnc(C_VT_OV_bitrate_index)
	gfc.OvEnc.ModeExt = c.ovEnc(C_VT_OV_mode_ext)
	gfc.OvEnc.EncoderDelay = c.ovEnc(C_VT_OV_encoder_delay)
	gfc.OvEnc.EncoderPadding = c.ovEnc(C_VT_OV_encoder_padding)

	gfc.OvRpg.RadioGain = c.radioGain()
	gfc.OvRpg.PeakSample = c.peakSample()
	gfc.NMusicCRC = c.musicCRC()

	st := &gfc.VBRSeekTable
	st.Sum = c.seekSum()
	st.Seen = c.seekSeen()
	st.Want = c.seekWant()
	st.Pos = c.seekPos()
	st.Size = c.seekSize()
	st.NVbrNumFrames = c.seekNFrames()
	st.NBytesWritten = c.seekNBytes()
	st.TotalFrameSize = c.seekTotalFrameSize()
	if st.Size > 0 {
		st.Bag = make([]int, st.Size)
		for i := 0; i < st.Size; i++ {
			st.Bag[i] = c.seekBag(i)
		}
	}

	gfp := &nativemp3.LameGlobalFlags{
		VBRq:         c.gfpVBRq(),
		Quality:      c.gfpQuality(),
		NogapTotal:   c.gfpNogapTotal(),
		NogapCurrent: c.gfpNogapCurrent(),
	}

	return gfc, gfp
}

// C selector mirrors of the oracle.h enums (the cgo bridge can't index C enum
// constants directly from another file without re-importing; mirror them here so
// native.go does not need its own cgo preamble).
const (
	C_VT_CFG_write_lame_tag = iota
	C_VT_CFG_sideinfo_len
	C_VT_CFG_error_protection
	C_VT_CFG_vbr
	C_VT_CFG_version
	C_VT_CFG_samplerate_out
	C_VT_CFG_samplerate_index
	C_VT_CFG_extension
	C_VT_CFG_mode
	C_VT_CFG_copyright
	C_VT_CFG_original
	C_VT_CFG_emphasis
	C_VT_CFG_avg_bitrate
	C_VT_CFG_free_format
	C_VT_CFG_noise_shaping
	C_VT_CFG_ATHtype
	C_VT_CFG_use_safe_joint_stereo
	C_VT_CFG_force_ms
	C_VT_CFG_samplerate_in
	C_VT_CFG_short_blocks
	C_VT_CFG_lowpassfreq
	C_VT_CFG_highpassfreq
	C_VT_CFG_disable_reservoir
	C_VT_CFG_findReplayGain
	C_VT_CFG_findPeakSample
	C_VT_CFG_vbr_avg_bitrate_kbps
	C_VT_CFG_vbr_min_bitrate_index
	C_VT_CFG_preset
	C_VT_CFG_ATHonly
	C_VT_CFG_noATH
)

const (
	C_VT_OV_bitrate_index = iota
	C_VT_OV_mode_ext
	C_VT_OV_encoder_delay
	C_VT_OV_encoder_padding
)
