// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Encoder initialisation — a 1:1 Go translation of LAME 3.100's
// lame_init_params (libraries/mp3/liblame/libmp3lame/lame.c:540) and the small
// self-contained table-lookup helpers it depends on (SmpFrqIndex,
// FindNearestBitrate, BitrateIndex, map2MP3Frequency, linear_int from util.c /
// lame.c). lame_init_params is the function that, from the user's
// lame_global_flags, populates the immutable SessionConfig and the rest of the
// lame_internal_flags context: it selects the MPEG version / samplerate index /
// bitrate index, fills the scalefactor-band tables, seeds the ATH adjustment
// config and PE FIR history, sets up the padding accumulator, and drives the
// one-time table / psymodel / quantizer inits.
//
// # Scope and the EncoderStages seam
//
// lame_init_params calls out to several other LAME translation units —
// lame_init_params_ppflt (the polyphase filter design), apply_preset
// (presets.c), lame_init_qval, iteration_init (quantize_pvt.c), psymodel_init
// (psymodel.c, whose pure-Go port already exists as InitPsyModel), the
// bitrate/samplerate auto-bandwidth design (optimum_bandwidth /
// optimum_samplefreq) and get_max_frame_buffer_size_by_constraint. Those are
// separate slices; the port reaches them through the unified EncoderStages
// seam (context.go), exactly where the C calls out, so the init driver's
// control flow stays 1:1 while the callees land incrementally. The
// table-lookup helpers that ARE this slice's own (version / bitrate /
// samplerate selection — explicitly in scope) are ported directly below.
//
// # Floating-point
//
// lame_init_params's one-time setup uses double-precision pow / powf exactly
// as the C does (mirrored with math.Pow / a float32(math.Pow(...)) narrow).
// These are computed once and are not on the FMA-sensitive per-frame path, so
// they are not routed through the *_fp_strict helpers; the constant
// `0.25f`-scaled nspsytune adjustments are written as single-rounded float32
// constant expressions, matching the C's `* 0.25f` folded constants.
//
// Every ported function carries a doc comment naming its C counterpart as
// file:line so a future reader can diff against the vendored C.

import "math"

// LameGlobalFlags is the subset of LAME's lame_global_flags struct
// (lame_global_flags.h:32, the user-facing gfp) that lame_init_params reads or
// writes. The user fills it (through the lame_set_* API in other slices);
// lame_init_params consumes it to populate the internal context. Field names
// and types mirror the C members 1:1. Only the members lame_init_params
// touches are mapped; the not-yet-ported set_get / preset / id3 areas read the
// rest.
type LameGlobalFlags struct {
	NumChannels   int // gfp->num_channels
	SamplerateIn  int // gfp->samplerate_in
	SamplerateOut int // gfp->samplerate_out

	Scale      float32 // gfp->scale
	ScaleLeft  float32 // gfp->scale_left
	ScaleRight float32 // gfp->scale_right

	Analysis     int // gfp->analysis
	WriteLameTag int // gfp->write_lame_tag
	Quality      int // gfp->quality
	Mode         int // gfp->mode (MPEG_mode)
	ForceMs      int // gfp->force_ms
	FreeFormat   int // gfp->free_format

	FindReplayGain int // gfp->findReplayGain
	DecodeOnTheFly int // gfp->decode_on_the_fly

	SubstepShaping int // gfp->substep_shaping
	NoiseShaping   int // gfp->noise_shaping
	SubblockGain   int // gfp->subblock_gain
	UseBestHuffman int // gfp->use_best_huffman

	Brate            int     // gfp->brate
	CompressionRatio float32 // gfp->compression_ratio

	Copyright       int // gfp->copyright
	Original        int // gfp->original
	Extension       int // gfp->extension
	Emphasis        int // gfp->emphasis
	ErrorProtection int // gfp->error_protection

	StrictISO        int // gfp->strict_ISO
	DisableReservoir int // gfp->disable_reservoir

	QuantComp      int // gfp->quant_comp
	QuantCompShort int // gfp->quant_comp_short
	ExperimentalY  int // gfp->experimentalY
	ExperimentalZ  int // gfp->experimentalZ
	ExpNspsytune   int // gfp->exp_nspsytune

	Preset int // gfp->preset

	VBR            int     // gfp->VBR (vbr_mode)
	VBRqFrac       float32 // gfp->VBR_q_frac
	VBRq           int     // gfp->VBR_q
	VBRMeanBitrate int     // gfp->VBR_mean_bitrate_kbps
	VBRMinBitrate  int     // gfp->VBR_min_bitrate_kbps
	VBRMaxBitrate  int     // gfp->VBR_max_bitrate_kbps
	VBRHardMin     int     // gfp->VBR_hard_min

	LowpassFreq   int // gfp->lowpassfreq
	HighpassFreq  int // gfp->highpassfreq
	LowpassWidth  int // gfp->lowpasswidth
	HighpassWidth int // gfp->highpasswidth

	Maskingadjust      float32 // gfp->maskingadjust
	MaskingadjustShort float32 // gfp->maskingadjust_short

	ATHonly          int     // gfp->ATHonly
	ATHshort         int     // gfp->ATHshort
	NoATH            int     // gfp->noATH
	ATHtype          int     // gfp->ATHtype
	ATHcurve         float32 // gfp->ATHcurve
	ATHLowerDb       float32 // gfp->ATH_lower_db
	AthaaType        int     // gfp->athaa_type
	AthaaSensitivity float32 // gfp->athaa_sensitivity

	ShortBlocks  int     // gfp->short_blocks (short_block_t)
	UseTemporal  int     // gfp->useTemporal
	InterChRatio float32 // gfp->interChRatio
	Msfix        float32 // gfp->msfix

	Tune       int     // gfp->tune
	TuneValueA float32 // gfp->tune_value_a

	Attackthre  float32 // gfp->attackthre
	AttackthreS float32 // gfp->attackthre_s

	NogapTotal   int // gfp->nogap_total
	NogapCurrent int // gfp->nogap_current
}

// MPEG_mode selector values (lame.h enum MPEG_mode). The decoder-side modeMono
// etc. consts (psymodel.go) only cover the subset the model branches on; the
// init driver needs the full enum, named with an Mpeg prefix to avoid colliding.
const (
	mpegStereo      = 0 // STEREO
	mpegJointStereo = 1 // JOINT_STEREO
	mpegDualChannel = 2 // DUAL_CHANNEL (not supported)
	mpegMono        = 3 // MONO
	mpegNotSet      = 4 // NOT_SET
)

// short_block_t selector values (lame.h). shortBlockNotSet etc. are already
// defined in psymodel.go; reused here.

// athLowerDbToFactor narrows the C `powf(10.f, x*0.1f)` ATH offset factor
// (lame.c:1191): single-precision pow of a single-precision argument.
func athLowerDbToFactor(athOffsetDb float32) float32 {
	return float32(math.Pow(10.0, float64(athOffsetDb)*0.1))
}

// smpFrqIndex is a 1:1 translation of SmpFrqIndex (util.c:443): map an output
// sample frequency to its (version, samplerate-index) pair. Returns index and
// sets *version; index is -1 for an unsupported rate. version is returned
// rather than written through a pointer (the C `int *version`).
func smpFrqIndex(sampleFreq int) (index, version int) {
	switch sampleFreq {
	case 44100:
		return 0, 1
	case 48000:
		return 1, 1
	case 32000:
		return 2, 1
	case 22050:
		return 0, 0
	case 24000:
		return 1, 0
	case 16000:
		return 2, 0
	case 11025:
		return 0, 0
	case 12000:
		return 1, 0
	case 8000:
		return 2, 0
	default:
		return -1, 0
	}
}

// findNearestBitrate is a 1:1 translation of FindNearestBitrate (util.c:310):
// pick the bitrate-table entry nearest bRate for the given version/samplerate.
func findNearestBitrate(bRate, version, samplerate int) int {
	if samplerate < 16000 {
		version = 2
	}
	bitrate := bitrateTable[version][1]
	for i := 2; i <= 14; i++ {
		if bitrateTable[version][i] > 0 {
			if absInt(bitrateTable[version][i]-bRate) < absInt(bitrate-bRate) {
				bitrate = bitrateTable[version][i]
			}
		}
	}
	return bitrate
}

// bitrateIndex is a 1:1 translation of BitrateIndex (util.c:422): convert a
// bitrate in kbps to its table index, or -1 if not a legal rate.
func bitrateIndex(bRate, version, samplerate int) int {
	if samplerate < 16000 {
		version = 2
	}
	for i := 0; i <= 14; i++ {
		if bitrateTable[version][i] > 0 {
			if bitrateTable[version][i] == bRate {
				return i
			}
		}
	}
	return -1
}

// map2MP3Frequency is a 1:1 translation of map2MP3Frequency (util.c:400): clamp
// an arbitrary frequency up to a valid MP3 output sample frequency.
func map2MP3Frequency(freq int) int {
	switch {
	case freq <= 8000:
		return 8000
	case freq <= 11025:
		return 11025
	case freq <= 12000:
		return 12000
	case freq <= 16000:
		return 16000
	case freq <= 22050:
		return 22050
	case freq <= 24000:
		return 24000
	case freq <= 32000:
		return 32000
	case freq <= 44100:
		return 44100
	default:
		return 48000
	}
}

// linearInt is a 1:1 translation of linear_int (lame.c:484): a + (b-a)*m.
func linearInt(a, b, m float64) float64 {
	return a + m*(b-a)
}

// (absInt — the C `ABS` macro, util.c:307 — is already defined in
// psymodel_masking.go and reused here.)

// bitrateTable is tables.c:526 const int bitrate_table[3][16] — bitrates in
// kbps indexed [version][index]; -1 marks an invalid entry.
var bitrateTable = [3][16]int{
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, -1},     // MPEG 2
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, -1}, // MPEG 1
	{0, 8, 16, 24, 32, 40, 48, 56, 64, -1, -1, -1, -1, -1, -1, -1},         // MPEG 2.5
}

// samplerateTable is tables.c:532 const int samplerate_table[3][4] — output
// sample rates in Hz indexed [version][index]; -1 marks an invalid entry.
// (GetVbrTag, the reader-side tag parser, indexes it.)
var samplerateTable = [3][4]int{
	{22050, 24000, 16000, -1}, // MPEG 2
	{44100, 48000, 32000, -1}, // MPEG 1
	{11025, 12000, 8000, -1},  // MPEG 2.5
}

// LameInitParams is a 1:1 translation of lame_init_params (lame.c:540). From
// the user's gfp it populates gfc (the unified LameInternalFlags): selecting
// version / samplerate-index / bitrate-index, filling the scalefactor-band and
// partitioned-sfb tables, seeding the ATH adjustment config / PE FIR / padding
// accumulator, and driving the one-time table, quantizer and psymodel inits
// through the EncoderStages seam. It returns 0 on success and -1 on an invalid
// parameter (the C `return -1` paths).
//
// Where the C calls out to other translation units (apply_preset, lame_init_qval,
// lame_init_params_ppflt, iteration_init, psymodel_init,
// get_max_frame_buffer_size_by_constraint, optimum_bandwidth,
// optimum_samplefreq), this port reaches them through gfc.Stages at the same
// call sites. The C debug-only asserts are omitted (they emit no bytes and Go
// has no NDEBUG-gated assert).
func LameInitParams(gfc *LameInternalFlags, gfp *LameGlobalFlags) int {
	var i, j int

	cfg := &gfc.Cfg

	// lame.c:559-560: begin updating internal flags.
	gfc.LameInitParamsSuccessful = 0

	// lame.c:562-570: validate the input/output sample rate and channel count.
	if gfp.SamplerateIn < 1 {
		return -1
	}
	if gfp.NumChannels < 1 || 2 < gfp.NumChannels {
		return -1
	}
	if gfp.SamplerateOut != 0 {
		if idx, _ := smpFrqIndex(gfp.SamplerateOut); idx < 0 {
			return -1
		}
	}

	// lame.c:574-577.
	cfg.EnforceMinBitrate = gfp.VBRHardMin
	cfg.Analysis = gfp.Analysis
	if cfg.Analysis != 0 {
		gfp.WriteLameTag = 0
	}

	// lame.c:580-581: pinfo (analysis GUI) is not carried; the C disables the
	// Xing tag when pinfo != NULL, which never happens here.

	// lame.c:588-605: CPU feature detection — the port has no asm fast paths
	// gated on these, so the CPU_features bits are not modelled.

	cfg.Vbr = gfp.VBR                         // lame.c:608
	cfg.ErrorProtection = gfp.ErrorProtection // lame.c:609
	cfg.Copyright = gfp.Copyright             // lame.c:610
	cfg.Original = gfp.Original               // lame.c:611
	cfg.Extension = gfp.Extension             // lame.c:612
	cfg.Emphasis = gfp.Emphasis               // lame.c:613

	cfg.ChannelsIn = gfp.NumChannels // lame.c:615
	if cfg.ChannelsIn == 1 {         // lame.c:616
		gfp.Mode = mpegMono
	}
	if gfp.Mode == mpegMono { // lame.c:618
		cfg.ChannelsOut = 1
	} else {
		cfg.ChannelsOut = 2
	}
	if gfp.Mode != mpegJointStereo { // lame.c:619-620
		gfp.ForceMs = 0
	}
	cfg.ForceMs = gfp.ForceMs // lame.c:621

	// lame.c:623-624.
	if cfg.Vbr == vbrOff && gfp.VBRMeanBitrate != 128 && gfp.Brate == 0 {
		gfp.Brate = gfp.VBRMeanBitrate
	}

	// lame.c:626-635: free-format compatibility.
	switch cfg.Vbr {
	case vbrOff, vbrMtrh, vbrMt:
		// these modes can handle free format
	default:
		gfp.FreeFormat = 0
	}
	cfg.FreeFormat = gfp.FreeFormat // lame.c:637

	// lame.c:639-643: default compression ratio when nothing was specified.
	if cfg.Vbr == vbrOff && gfp.Brate == 0 {
		if gfp.CompressionRatio == 0 {
			gfp.CompressionRatio = 11.025
		}
	}

	// lame.c:646-662: derive a bitrate from a requested compression ratio.
	if cfg.Vbr == vbrOff && gfp.CompressionRatio > 0 {
		if gfp.SamplerateOut == 0 {
			gfp.SamplerateOut = map2MP3Frequency(int(0.97 * float64(gfp.SamplerateIn)))
		}
		gfp.Brate = int(float64(gfp.SamplerateOut) * 16 * float64(cfg.ChannelsOut) /
			(1.0e3 * float64(gfp.CompressionRatio)))
		cfg.SamplerateIndex, cfg.Version = smpFrqIndex(gfp.SamplerateOut)
		if cfg.FreeFormat == 0 {
			gfp.Brate = findNearestBitrate(gfp.Brate, cfg.Version, gfp.SamplerateOut)
		}
	}

	// lame.c:663-676: clamp the VBR mean bitrate to the samplerate's range.
	if gfp.SamplerateOut != 0 {
		switch {
		case gfp.SamplerateOut < 16000:
			gfp.VBRMeanBitrate = maxInt(gfp.VBRMeanBitrate, 8)
			gfp.VBRMeanBitrate = minInt(gfp.VBRMeanBitrate, 64)
		case gfp.SamplerateOut < 32000:
			gfp.VBRMeanBitrate = maxInt(gfp.VBRMeanBitrate, 8)
			gfp.VBRMeanBitrate = minInt(gfp.VBRMeanBitrate, 160)
		default:
			gfp.VBRMeanBitrate = maxInt(gfp.VBRMeanBitrate, 32)
			gfp.VBRMeanBitrate = minInt(gfp.VBRMeanBitrate, 320)
		}
	}

	// lame.c:677-717 ("WORK IN PROGRESS"): the vbr_mt/mtrh VBR-quality-to-internal
	// mapping. It only fires when the output rate is unset (the -V CLI path, and
	// the public encoder once SamplerateOut is left at the input rate via the
	// q-map / optimum_samplefreq derivation below), remapping VBR_q/VBR_q_frac as
	// a function of the input sample rate so the apply_preset LERP lands on the
	// right per-rate tuning (e.g. at 32k -V2 becomes VBR_q=1, frac=0.6 -> ATHcurve
	// 1.76 / msfix 1.2216, not the 44.1k/48k identity VBR_q=2). The break case
	// also pins samplerate_out and forces lowpassfreq=-1 (auto). For 44.1k/48k the
	// table is the identity (VBR_q=2, frac=0) and no break is taken, so the output
	// rate falls through to the lowpass/optimum_samplefreq auto-detect — matching
	// the genuine -V2 oracle this gate targets.
	if gfp.SamplerateOut == 0 && (cfg.Vbr == vbrMt || cfg.Vbr == vbrMtrh) {
		qval := float32(gfp.VBRq) + gfp.VBRqFrac
		type qMap struct {
			srA            int
			qa, qb, ta, tb float32
			lp             int
		}
		m := [9]qMap{
			{48000, 0.0, 6.5, 0.0, 6.5, 23700},
			{44100, 0.0, 6.5, 0.0, 6.5, 21780},
			{32000, 6.5, 8.0, 5.2, 6.5, 15800},
			{24000, 8.0, 8.5, 5.2, 6.0, 11850},
			{22050, 8.5, 9.01, 5.2, 6.5, 10892},
			{16000, 9.01, 9.4, 4.9, 6.5, 7903},
			{12000, 9.4, 9.6, 4.5, 6.0, 5928},
			{11025, 9.6, 9.9, 5.1, 6.5, 5446},
			{8000, 9.9, 10., 4.9, 6.5, 3952},
		}
		for i = 2; i < 9; i++ {
			if gfp.SamplerateIn == m[i].srA {
				if qval < m[i].qa {
					d := float64(qval) / float64(m[i].qa)
					d = d * float64(m[i].ta)
					gfp.VBRq = int(d)
					gfp.VBRqFrac = float32(d - float64(gfp.VBRq))
				}
			}
			if gfp.SamplerateIn >= m[i].srA {
				if m[i].qa <= qval && qval < m[i].qb {
					q := m[i].qb - m[i].qa
					t := m[i].tb - m[i].ta
					d := float64(m[i].ta) + float64(t)*float64(qval-m[i].qa)/float64(q)
					gfp.VBRq = int(d)
					gfp.VBRqFrac = float32(d - float64(gfp.VBRq))
					gfp.SamplerateOut = m[i].srA
					if gfp.LowpassFreq == 0 {
						gfp.LowpassFreq = -1
					}
					break
				}
			}
		}
	}

	// lame.c:722-780: lowpass auto-detect. Only runs when the user left the
	// lowpass frequency unset; the bitrate-driven optimum_bandwidth and the
	// VBR-quality lowpass tables are filter-design and are reached through the
	// seam. The common explicit-lowpass path skips this whole block.
	if gfp.LowpassFreq == 0 {
		lowpass := 16000.0
		switch cfg.Vbr {
		case vbrOff:
			lowpass, _ = gfc.Stages.OptimumBandwidth(gfp.Brate)
		case vbrAbr:
			lowpass, _ = gfc.Stages.OptimumBandwidth(gfp.VBRMeanBitrate)
		case vbrRh:
			x := [11]int{19500, 19000, 18600, 18000, 17500, 16000, 15600, 14900, 12500, 10000, 3950}
			if 0 <= gfp.VBRq && gfp.VBRq <= 9 {
				lowpass = linearInt(float64(x[gfp.VBRq]), float64(x[gfp.VBRq+1]), float64(gfp.VBRqFrac))
			} else {
				lowpass = 19500
			}
		case vbrMtrh, vbrMt:
			x := [11]int{24000, 19500, 18500, 18000, 17500, 17000, 16500, 15600, 15200, 7230, 3950}
			if 0 <= gfp.VBRq && gfp.VBRq <= 9 {
				lowpass = linearInt(float64(x[gfp.VBRq]), float64(x[gfp.VBRq+1]), float64(gfp.VBRqFrac))
			} else {
				lowpass = 21500
			}
		default:
			x := [11]int{19500, 19000, 18500, 18000, 17500, 16500, 15500, 14500, 12500, 9500, 3950}
			if 0 <= gfp.VBRq && gfp.VBRq <= 9 {
				lowpass = linearInt(float64(x[gfp.VBRq]), float64(x[gfp.VBRq+1]), float64(gfp.VBRqFrac))
			} else {
				lowpass = 19500
			}
		}
		if gfp.Mode == mpegMono && (cfg.Vbr == vbrOff || cfg.Vbr == vbrAbr) {
			lowpass *= 1.5
		}
		gfp.LowpassFreq = int(lowpass)
	}

	// lame.c:782-794: samplerate auto-detect from the lowpass cutoff.
	if gfp.SamplerateOut == 0 {
		if 2*gfp.LowpassFreq > gfp.SamplerateIn {
			gfp.LowpassFreq = gfp.SamplerateIn / 2
		}
		gfp.SamplerateOut = gfc.Stages.OptimumSamplefreq(gfp.LowpassFreq, gfp.SamplerateIn)
	}
	if cfg.Vbr == vbrMt || cfg.Vbr == vbrMtrh {
		gfp.LowpassFreq = minInt(24000, gfp.LowpassFreq)
	} else {
		gfp.LowpassFreq = minInt(20500, gfp.LowpassFreq)
	}
	gfp.LowpassFreq = minInt(gfp.SamplerateOut/2, gfp.LowpassFreq)

	// lame.c:796-802.
	if cfg.Vbr == vbrOff {
		gfp.CompressionRatio = float32(float64(gfp.SamplerateOut) * 16 * float64(cfg.ChannelsOut) /
			(1.0e3 * float64(gfp.Brate)))
	}
	if cfg.Vbr == vbrAbr {
		gfp.CompressionRatio = float32(float64(gfp.SamplerateOut) * 16 * float64(cfg.ChannelsOut) /
			(1.0e3 * float64(gfp.VBRMeanBitrate)))
	}

	cfg.DisableReservoir = gfp.DisableReservoir // lame.c:804
	cfg.LowpassFreq = gfp.LowpassFreq           // lame.c:805
	cfg.HighpassFreq = gfp.HighpassFreq         // lame.c:806
	cfg.SamplerateIn = gfp.SamplerateIn         // lame.c:807
	cfg.SamplerateOut = gfp.SamplerateOut       // lame.c:808
	if cfg.SamplerateOut <= 24000 {             // lame.c:809
		cfg.ModeGr = 1
	} else {
		cfg.ModeGr = 2
	}

	// lame.c:840-857: take a guess at the compression ratio per VBR mode.
	switch cfg.Vbr {
	case vbrMt, vbrRh, vbrMtrh:
		cmp := [10]float32{5.7, 6.5, 7.3, 8.2, 10, 11.9, 13, 14, 15, 16.5}
		gfp.CompressionRatio = cmp[gfp.VBRq]
	case vbrAbr:
		gfp.CompressionRatio = float32(float64(cfg.SamplerateOut) * 16 * float64(cfg.ChannelsOut) /
			(1.0e3 * float64(gfp.VBRMeanBitrate)))
	default:
		gfp.CompressionRatio = float32(float64(cfg.SamplerateOut) * 16 * float64(cfg.ChannelsOut) /
			(1.0e3 * float64(gfp.Brate)))
	}

	// lame.c:864-868: default mode = JOINT_STEREO.
	if gfp.Mode == mpegNotSet {
		gfp.Mode = mpegJointStereo
	}
	cfg.Mode = gfp.Mode

	// lame.c:871-902: user high/low pass filter normalisation.
	if cfg.HighpassFreq > 0 {
		cfg.Highpass1 = float32(2.0 * float64(cfg.HighpassFreq))
		if gfp.HighpassWidth >= 0 {
			cfg.Highpass2 = float32(2.0 * float64(cfg.HighpassFreq+gfp.HighpassWidth))
		} else {
			cfg.Highpass2 = float32((1 + 0.00) * 2.0 * float64(cfg.HighpassFreq))
		}
		cfg.Highpass1 /= float32(cfg.SamplerateOut)
		cfg.Highpass2 /= float32(cfg.SamplerateOut)
	} else {
		cfg.Highpass1 = 0
		cfg.Highpass2 = 0
	}
	cfg.Lowpass1 = 0
	cfg.Lowpass2 = 0
	if cfg.LowpassFreq > 0 && cfg.LowpassFreq < cfg.SamplerateOut/2 {
		cfg.Lowpass2 = float32(2.0 * float64(cfg.LowpassFreq))
		if gfp.LowpassWidth >= 0 {
			cfg.Lowpass1 = float32(2.0 * float64(cfg.LowpassFreq-gfp.LowpassWidth))
			if cfg.Lowpass1 < 0 {
				cfg.Lowpass1 = 0
			}
		} else {
			cfg.Lowpass1 = float32((1 - 0.00) * 2.0 * float64(cfg.LowpassFreq))
		}
		cfg.Lowpass1 /= float32(cfg.SamplerateOut)
		cfg.Lowpass2 /= float32(cfg.SamplerateOut)
	}

	// lame.c:910: polyphase filter design (separate ppflt slice).
	gfc.Stages.InitParamsPpflt(gfc)

	// lame.c:916-937: samplerate / bitrate index.
	cfg.SamplerateIndex, cfg.Version = smpFrqIndex(cfg.SamplerateOut)
	if cfg.Vbr == vbrOff {
		if cfg.FreeFormat != 0 {
			gfc.OvEnc.BitrateIndex = 0
		} else {
			gfp.Brate = findNearestBitrate(gfp.Brate, cfg.Version, cfg.SamplerateOut)
			gfc.OvEnc.BitrateIndex = bitrateIndex(gfp.Brate, cfg.Version, cfg.SamplerateOut)
			if gfc.OvEnc.BitrateIndex <= 0 {
				gfc.OvEnc.BitrateIndex = 8
			}
		}
	} else {
		gfc.OvEnc.BitrateIndex = 1
	}

	// lame.c:939: init_bit_stream_w resets the output bit stream cursors. It is
	// pure integer state-reset, ported inline here (init_bit_stream_w,
	// bitstream.c).
	initBitStreamW(gfc)

	// lame.c:941-960: scalefactor-band tables + partitioned sfb21 / sfb12.
	j = cfg.SamplerateIndex + (3 * cfg.Version)
	if cfg.SamplerateOut < 16000 {
		j += 6
	}
	for i = 0; i < SBMAXl+1; i++ {
		gfc.ScalefacBand.L[i] = sfBandIndex[j].L[i]
	}
	for i = 0; i < PSFB21+1; i++ {
		size := (gfc.ScalefacBand.L[22] - gfc.ScalefacBand.L[21]) / PSFB21
		start := gfc.ScalefacBand.L[21] + i*size
		gfc.ScalefacBand.Psfb21[i] = start
	}
	gfc.ScalefacBand.Psfb21[PSFB21] = 576

	for i = 0; i < SBMAXs+1; i++ {
		gfc.ScalefacBand.S[i] = sfBandIndex[j].S[i]
	}
	for i = 0; i < PSFB12+1; i++ {
		size := (gfc.ScalefacBand.S[13] - gfc.ScalefacBand.S[12]) / PSFB12
		start := gfc.ScalefacBand.S[12] + i*size
		gfc.ScalefacBand.Psfb12[i] = start
	}
	gfc.ScalefacBand.Psfb12[PSFB12] = 192

	// lame.c:962-969: side-information length.
	if cfg.ModeGr == 2 { // MPEG 1
		if cfg.ChannelsOut == 1 {
			cfg.SideinfoLen = 4 + 17
		} else {
			cfg.SideinfoLen = 4 + 32
		}
	} else { // MPEG 2
		if cfg.ChannelsOut == 1 {
			cfg.SideinfoLen = 4 + 9
		} else {
			cfg.SideinfoLen = 4 + 17
		}
	}
	if cfg.ErrorProtection != 0 {
		cfg.SideinfoLen += 2
	}

	// lame.c:971-979: seed the PE FIR history + default ATH type.
	for k := 0; k < 19; k++ {
		gfc.SvEnc.PefirBuf[k] = float32(700 * cfg.ModeGr * cfg.ChannelsOut)
	}
	if gfp.ATHtype == -1 {
		gfp.ATHtype = 4
	}

	// lame.c:984-1062: per-VBR-mode preset / sfb21-extra / quality clamping.
	switch cfg.Vbr {
	case vbrMt, vbrMtrh:
		// lame.c:988-989: `if (gfp->strict_ISO < 0) gfp->strict_ISO = MDB_MAXIMUM;`.
		// MDB_MAXIMUM == 2 (lame.h:141, MDB_DEFAULT=0/MDB_STRICT_ISO=1/MDB_MAXIMUM=2);
		// strict_ISO flows into getMaxFrameBufferSizeByConstraint as its `constraint`
		// arg (lame.c:1281), so this must be the mdbConstraintMaximum sentinel (2) to
		// select the 7680*(version+1) branch — not a magic 511 (which would wrongly
		// fall through to mdbConstraintDefault).
		if gfp.StrictISO < 0 {
			gfp.StrictISO = mdbConstraintMaximum
		}
		if gfp.UseTemporal < 0 {
			gfp.UseTemporal = 0
		}
		gfc.Stages.ApplyPreset(gfc, gfp, 500-(gfp.VBRq*10), 0)
		if gfp.Quality < 0 {
			gfp.Quality = lameDefaultQuality
		}
		if gfp.Quality < 5 {
			gfp.Quality = 0
		}
		if gfp.Quality > 7 {
			gfp.Quality = 7
		}
		if gfp.ExperimentalY != 0 {
			gfc.SvQnt.Sfb21Extra = 0
		} else {
			gfc.SvQnt.Sfb21Extra = b2i(cfg.SamplerateOut > 44000)
		}
	case vbrRh:
		gfc.Stages.ApplyPreset(gfc, gfp, 500-(gfp.VBRq*10), 0)
		if gfp.ExperimentalY != 0 {
			gfc.SvQnt.Sfb21Extra = 0
		} else {
			gfc.SvQnt.Sfb21Extra = b2i(cfg.SamplerateOut > 44000)
		}
		if gfp.Quality > 6 {
			gfp.Quality = 6
		}
		if gfp.Quality < 0 {
			gfp.Quality = lameDefaultQuality
		}
	default: // cbr / abr
		gfc.SvQnt.Sfb21Extra = 0
		if gfp.Quality < 0 {
			gfp.Quality = lameDefaultQuality
		}
		if cfg.Vbr == vbrOff {
			gfp.VBRMeanBitrate = gfp.Brate // lame_set_VBR_mean_bitrate_kbps
		}
		gfc.Stages.ApplyPreset(gfc, gfp, gfp.VBRMeanBitrate, 0)
		gfp.VBR = cfg.Vbr
	}

	// lame.c:1066-1073: common mask-adjust defaults.
	gfc.SvQnt.MaskAdjust = gfp.Maskingadjust
	gfc.SvQnt.MaskAdjustShort = gfp.MaskingadjustShort
	if gfp.Tune != 0 {
		gfc.SvQnt.MaskAdjust += gfp.TuneValueA
		gfc.SvQnt.MaskAdjustShort += gfp.TuneValueA
	}

	// lame.c:1076-1116: VBR min/max bitrate index selection.
	if cfg.Vbr != vbrOff {
		cfg.VbrMinBitrateIndex = 1
		cfg.VbrMaxBitrateIndex = 14
		if cfg.SamplerateOut < 16000 {
			cfg.VbrMaxBitrateIndex = 8
		}
		if gfp.VBRMinBitrate != 0 {
			gfp.VBRMinBitrate = findNearestBitrate(gfp.VBRMinBitrate, cfg.Version, cfg.SamplerateOut)
			cfg.VbrMinBitrateIndex = bitrateIndex(gfp.VBRMinBitrate, cfg.Version, cfg.SamplerateOut)
			if cfg.VbrMinBitrateIndex < 0 {
				cfg.VbrMinBitrateIndex = 1
			}
		}
		if gfp.VBRMaxBitrate != 0 {
			gfp.VBRMaxBitrate = findNearestBitrate(gfp.VBRMaxBitrate, cfg.Version, cfg.SamplerateOut)
			cfg.VbrMaxBitrateIndex = bitrateIndex(gfp.VBRMaxBitrate, cfg.Version, cfg.SamplerateOut)
			if cfg.VbrMaxBitrateIndex < 0 {
				if cfg.SamplerateOut < 16000 {
					cfg.VbrMaxBitrateIndex = 8
				} else {
					cfg.VbrMaxBitrateIndex = 14
				}
			}
		}
		gfp.VBRMinBitrate = bitrateTable[cfg.Version][cfg.VbrMinBitrateIndex]
		gfp.VBRMaxBitrate = bitrateTable[cfg.Version][cfg.VbrMaxBitrateIndex]
		gfp.VBRMeanBitrate = minInt(bitrateTable[cfg.Version][cfg.VbrMaxBitrateIndex], gfp.VBRMeanBitrate)
		gfp.VBRMeanBitrate = maxInt(bitrateTable[cfg.Version][cfg.VbrMinBitrateIndex], gfp.VBRMeanBitrate)
	}

	cfg.Preset = gfp.Preset                       // lame.c:1118
	cfg.WriteLameTag = gfp.WriteLameTag           // lame.c:1119
	gfc.SvQnt.SubstepShaping = gfp.SubstepShaping // lame.c:1120
	cfg.NoiseShaping = gfp.NoiseShaping           // lame.c:1121
	cfg.SubblockGain = gfp.SubblockGain           // lame.c:1122
	cfg.UseBestHuffman = gfp.UseBestHuffman       // lame.c:1123
	cfg.AvgBitrate = gfp.Brate                    // lame.c:1124
	cfg.VbrAvgBitrateKbps = gfp.VBRMeanBitrate    // lame.c:1125
	cfg.CompressionRatio = gfp.CompressionRatio   // lame.c:1126

	// lame.c:1129: internal qval settings (separate slice).
	gfc.Stages.InitQval(gfc, gfp)

	// lame.c:1134-1141: ATH auto-adjust scheme + adaptive sensitivity.
	if gfp.AthaaType < 0 {
		gfc.ATH.UseAdjust = 3
	} else {
		gfc.ATH.UseAdjust = gfp.AthaaType
	}
	gfc.ATH.AaSensitivityP = float32(math.Pow(10.0, float64(gfp.AthaaSensitivity)/-10.0))

	// lame.c:1144-1161: short-block policy.
	if gfp.ShortBlocks == shortBlockNotSet {
		gfp.ShortBlocks = shortBlockAllowed
	}
	if gfp.ShortBlocks == shortBlockAllowed &&
		(cfg.Mode == mpegJointStereo || cfg.Mode == mpegStereo) {
		gfp.ShortBlocks = shortBlockCoupled
	}
	cfg.ShortBlocks = gfp.ShortBlocks

	// lame.c:1164-1170: quant_comp / msfix defaults.
	if gfp.QuantComp < 0 {
		gfp.QuantComp = 1
	}
	if gfp.QuantCompShort < 0 {
		gfp.QuantCompShort = 0
	}
	if gfp.Msfix < 0 {
		gfp.Msfix = 0
	}

	// lame.c:1173: select psychoacoustic model (nspsytune | 1).
	gfp.ExpNspsytune |= 1

	// lame.c:1175-1185: ATH formula / curve / inter-channel / temporal defaults.
	if gfp.ATHtype < 0 {
		gfp.ATHtype = 4
	}
	if gfp.ATHcurve < 0 {
		gfp.ATHcurve = 4
	}
	if gfp.InterChRatio < 0 {
		gfp.InterChRatio = 0
	}
	if gfp.UseTemporal < 0 {
		gfp.UseTemporal = 1
	}

	cfg.InterChRatio = gfp.InterChRatio                       // lame.c:1188
	cfg.Msfix = gfp.Msfix                                     // lame.c:1189
	cfg.ATHOffsetDb = 0 - gfp.ATHLowerDb                      // lame.c:1190
	cfg.ATHOffsetFactor = athLowerDbToFactor(cfg.ATHOffsetDb) // lame.c:1191
	cfg.ATHcurve = gfp.ATHcurve                               // lame.c:1192
	cfg.ATHtype = gfp.ATHtype                                 // lame.c:1193
	cfg.ATHonly = gfp.ATHonly                                 // lame.c:1194
	cfg.ATHshort = gfp.ATHshort                               // lame.c:1195
	cfg.NoATH = gfp.NoATH                                     // lame.c:1196

	cfg.QuantComp = gfp.QuantComp           // lame.c:1198
	cfg.QuantCompShort = gfp.QuantCompShort // lame.c:1199

	cfg.UseTemporalMaskingEffect = gfp.UseTemporal // lame.c:1201
	if cfg.Mode == mpegJointStereo {               // lame.c:1202-1207
		cfg.UseSafeJointStereo = gfp.ExpNspsytune & 2
	} else {
		cfg.UseSafeJointStereo = 0
	}

	// lame.c:1208-1231: nspsytune per-region dB adjustments (0.25 dB steps).
	{
		bass := float32((gfp.ExpNspsytune >> 2) & 63)
		if bass >= 32.0 {
			bass -= 64.0
		}
		cfg.AdjustBassDb = bass * 0.25

		alto := float32((gfp.ExpNspsytune >> 8) & 63)
		if alto >= 32.0 {
			alto -= 64.0
		}
		cfg.AdjustAltoDb = alto * 0.25

		treble := float32((gfp.ExpNspsytune >> 14) & 63)
		if treble >= 32.0 {
			treble -= 64.0
		}
		cfg.AdjustTrebleDb = treble * 0.25

		sfb21 := float32((gfp.ExpNspsytune >> 20) & 63)
		if sfb21 >= 32.0 {
			sfb21 -= 64.0
		}
		sfb21 *= 0.25
		cfg.AdjustSfb21Db = sfb21 + cfg.AdjustTrebleDb
	}

	// lame.c:1236-1261: PCM input transform matrix (scale + downmix).
	{
		m := [2][2]float32{{1.0, 0.0}, {0.0, 1.0}}
		m[0][0] *= gfp.Scale
		m[0][1] *= gfp.Scale
		m[1][0] *= gfp.Scale
		m[1][1] *= gfp.Scale
		m[0][0] *= gfp.ScaleLeft
		m[0][1] *= gfp.ScaleLeft
		m[1][0] *= gfp.ScaleRight
		m[1][1] *= gfp.ScaleRight
		if cfg.ChannelsIn == 2 && cfg.ChannelsOut == 1 {
			m[0][0] = 0.5 * (m[0][0] + m[1][0])
			m[0][1] = 0.5 * (m[0][1] + m[1][1])
			m[1][0] = 0
			m[1][1] = 0
		}
		cfg.PcmTransform = m
	}

	// lame.c:1271-1274: padding accumulator seed (no padding for first frame).
	gfc.SvEnc.SlotLag = 0
	gfc.SvEnc.FracSpF = 0
	if cfg.Vbr == vbrOff {
		gfc.SvEnc.FracSpF = ((cfg.Version + 1) * 72000 * cfg.AvgBitrate) % cfg.SamplerateOut
		gfc.SvEnc.SlotLag = gfc.SvEnc.FracSpF
	}

	// lame.c:1276-1279: bitstream / table / psymodel inits (separate slices).
	lameInitBitstream(gfc)
	gfc.Stages.IterationInit(gfc)
	gfc.Stages.PsymodelInit(gfc, gfp)

	// lame.c:1281.
	cfg.BufferConstraint = gfc.Stages.GetMaxFrameBufferSizeByConstraint(gfc, gfp.StrictISO)

	// lame.c:1284-1297: ReplayGain (decode-on-the-fly) — config flags only; the
	// gain-analysis init is a separate slice.
	cfg.FindReplayGain = gfp.FindReplayGain
	cfg.DecodeOnTheFly = gfp.DecodeOnTheFly
	if cfg.DecodeOnTheFly != 0 {
		cfg.FindPeakSample = 1
	}

	// lame.c:1313.
	gfc.LameInitParamsSuccessful = 1
	return 0
}

// lameDefaultQuality is LAME_DEFAULT_QUALITY (lame.c, #define
// LAME_DEFAULT_QUALITY 3).
const lameDefaultQuality = 3

// b2i converts a Go bool to the C 0/1 int idiom (the C `(samplerate_out >
// 44000)` etc.).
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// minInt / maxInt are the C Min / Max macros (util.h) for ints.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// bufferSize is BUFFER_SIZE (util.h:89), the byte size of the encoder's
// working output bit-stream buffer gfc->bs.buf. It expands to LAME_MAXMP3BUFFER
// = 16384 + LAME_MAXALBUMART, and LAME_MAXALBUMART = 128*1024 (lame.h:1315,
// 1320). lame_calloc'd in init_bit_stream_w; the port allocates it there too.
const bufferSize = 16384 + 128*1024

// initBitStreamW is a 1:1 translation of init_bit_stream_w (bitstream.c:1097):
// allocate and reset the output bit-stream buffer + cursors before encoding.
//
//	void
//	init_bit_stream_w(lame_internal_flags * gfc)
//	{
//	    EncStateVar_t *const esv = &gfc->sv_enc;
//	    esv->h_ptr = esv->w_ptr = 0;
//	    esv->header[esv->h_ptr].write_timing = 0;
//	    gfc->bs.buf = lame_calloc(unsigned char, BUFFER_SIZE);
//	    gfc->bs.buf_size = BUFFER_SIZE;
//	    gfc->bs.buf_byte_idx = -1;
//	    gfc->bs.buf_bit_idx = 0;
//	    gfc->bs.totbit = 0;
//	}
func initBitStreamW(gfc *LameInternalFlags) {
	esv := &gfc.SvEnc
	esv.HPtr = 0
	esv.WPtr = 0
	esv.Header[esv.HPtr].WriteTiming = 0
	gfc.Bs.Buf = make([]byte, bufferSize) // lame_calloc zero-fills.
	gfc.Bs.BufSize = bufferSize
	gfc.Bs.BufByteIdx = -1
	gfc.Bs.BufBitIdx = 0
	gfc.Bs.Totbit = 0
}

// lameInitBitstream is a 1:1 translation of lame_init_bitstream (lame.c:2053):
// reset the per-stream frame counter and VBR-tag bookkeeping at the start of a
// stream. The VBR seek-table init (InitVbrTag) is a separate slice; the
// frame-counter reset is ported here.
//
//	int
//	lame_init_bitstream(lame_global_flags * gfp)
//	{
//	    lame_internal_flags *const gfc = gfp->internal_flags;
//	    gfc->ov_enc.frame_number = 0;
//	    if (gfp->write_lame_tag)
//	        (void) InitVbrTag(gfp);
//	    return 0;
//	}
func lameInitBitstream(gfc *LameInternalFlags) {
	gfc.OvEnc.FrameNumber = 0
	// gfp->write_lame_tag gates InitVbrTag (VbrTag.c, ported in vbrtag.go). The
	// public encoder mirrors gfp->write_lame_tag onto cfg.WriteLameTag in
	// lame_init_params, so the unified context reads it from there.
	if gfc.Cfg.WriteLameTag != 0 {
		InitVbrTag(gfc)
	}
}
