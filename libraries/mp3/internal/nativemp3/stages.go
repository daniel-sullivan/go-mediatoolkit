// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Concrete EncoderStages — the real wiring of the per-frame encode pipeline and
// the one-time inits behind the EncoderStages seam (context.go). The frame
// dispatcher EncodeMP3Frame (frame_encode.go) and the init driver
// LameInitParams (init.go) call out through this interface exactly where the C
// encoder.c / lame.c call into the other translation units; FrameEncodeStages
// supplies the genuine callees (psymodel.c L3psycho_anal_vbr, newmdct.c
// mdct_sub48, quantize.c CBR/ABR_iteration_loop, bitstream.c
// format_bitstream / copy_buffer, quantize_pvt.c iteration_init), replacing the
// no-op stub that previously satisfied the seam.
//
// This is the slice the work-list calls "the CONCRETE FrameEncodeStages
// implementation that frame_encode.go's dispatcher calls". Each method is a thin
// adapter onto the 1:1 port living in its feature file; the adapters only bridge
// the seam's argument shapes (the dispatcher hands [2]float32 / [4]float32
// slices where the native psymodel takes []float32 over the same storage) and
// must not re-implement any algorithm.
//
// # Deferred init seams (not this slice)
//
// LameInitParams reaches several init helpers through this seam whose 1:1 ports
// are separate, not-yet-landed slices: lame_init_params_ppflt (the polyphase
// filter design), apply_preset (presets.c), and optimum_bandwidth /
// optimum_samplefreq (the bitrate auto-bandwidth design). FrameEncodeStages
// implements the ones whose ports exist or are load-bearing for the iteration
// loop — IterationInit (the quantize_pvt.c table fill + longfact/shortfact,
// ported in iteration_init.go), InitQval (lame_init_qval, ported in init_qval.go
// because the CBR loop is undefined without its noise-shaping policy),
// PsymodelInit (InitPsyModel), GetMaxFrameBufferSizeByConstraint
// (bitstream_format.go) — and supplies minimal, clearly-marked stand-ins for the
// unported filter/preset helpers so the build links and the CBR control flow is
// exercised. The stand-ins are NOT a 1:1 port and are flagged inline;
// wiring the real ppflt / presets / qval / optimum slices is their own future
// task, and until they land the end-to-end output is not bit-exact with liblame
// (the iteration loop, MDCT, psymodel and bitstream stages this slice wires ARE
// the 1:1 ports). The build gate — `CGO_ENABLED=0 go build -tags mp3lame` — and
// the per-stage parity oracles do not depend on the stand-ins.

// FrameEncodeStages is the concrete EncoderStages. It carries no state of its
// own; every method threads the gfc context the seam passes, exactly as the C
// functions take `lame_internal_flags *gfc`.
type FrameEncodeStages struct{}

// NewFrameEncodeStages returns the concrete encode-pipeline stages to install on
// LameInternalFlags.Stages before encoding.
func NewFrameEncodeStages() EncoderStages { return FrameEncodeStages{} }

// MdctSub48 wires newmdct.c's mdct_sub48 (mdct_analysis_filterbank.go), the
// polyphase + MDCT analysis writing GrInfo.Xr.
func (FrameEncodeStages) MdctSub48(gfc *LameInternalFlags, w0, w1 []float32) {
	mdctSub48(&gfc.Cfg, &gfc.SvEnc, &gfc.L3Side.Tt, w0, w1)
}

// L3PsychoAnalVbr wires psymodel.c's L3psycho_anal_vbr (psymodel_anal.go). The
// dispatcher passes the granule-sliced pe[gr] / pe_MS[gr] / tot_ener[gr] and the
// block-type out-array; the native port writes them through the same storage,
// so the adapter forwards the backing slices. It returns 0 on success.
func (FrameEncodeStages) L3PsychoAnalVbr(gfc *LameInternalFlags, bufp [2][]float32, gr int,
	maskingLR, maskingMS *[2][2]III_psy_ratio,
	pe, peMS *[2]float32, totEner *[4]float32, blocktype *[2]int) int {
	return gfc.L3psychoAnalVbr(bufp, gr, maskingLR, maskingMS, pe[:], peMS[:], totEner[:], blocktype[:])
}

// AdjustATH wires psymodel.c's adjust_ATH (psymodel.go). The native port's
// per-frame ATH auto-adjust hangs off the context.
func (FrameEncodeStages) AdjustATH(gfc *LameInternalFlags) {
	gfc.adjustATH()
}

// CBRIterationLoop wires quantize.c's CBR_iteration_loop (quantize_encode.go).
func (FrameEncodeStages) CBRIterationLoop(gfc *LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]III_psy_ratio) {
	gfc.cbrIterationLoop(peUse, msEnerRatio, masking)
}

// ABRIterationLoop wires quantize.c's ABR_iteration_loop (quantize_encode.go).
func (FrameEncodeStages) ABRIterationLoop(gfc *LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]III_psy_ratio) {
	gfc.abrIterationLoop(peUse, msEnerRatio, masking)
}

// VBROldIterationLoop wires quantize.c's VBR_old_iteration_loop
// (quantize_encode_vbr.go), the vbr_rh whole-frame driver.
func (FrameEncodeStages) VBROldIterationLoop(gfc *LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]III_psy_ratio) {
	gfc.vbrOldIterationLoop(peUse, msEnerRatio, masking)
}

// VBRNewIterationLoop wires quantize.c's VBR_new_iteration_loop
// (quantize_encode_vbr.go), the vbr_mtrh / vbr_mt (-V) whole-frame driver.
func (FrameEncodeStages) VBRNewIterationLoop(gfc *LameInternalFlags, peUse *[2][2]float32, msEnerRatio *[2]float32, masking *[2][2]III_psy_ratio) {
	gfc.vbrNewIterationLoop(peUse, msEnerRatio, masking)
}

// FormatBitstream wires bitstream.c's format_bitstream (bitstream_format.go).
func (FrameEncodeStages) FormatBitstream(gfc *LameInternalFlags) {
	gfc.formatBitstream()
}

// CopyBuffer wires bitstream.c's copy_buffer (bitstream_format.go).
func (FrameEncodeStages) CopyBuffer(gfc *LameInternalFlags, mp3buf []byte, mp3bufSize, mt int) int {
	return gfc.copyBuffer(mp3buf, mp3bufSize, mt)
}

// AddVbrFrame wires VbrTag.c's AddVbrFrame (vbrtag.go): append the current
// frame's bitrate to the Xing/LAME VBR seek table. The dispatcher only invokes
// it when cfg.WriteLameTag is set (InitVbrTag clears WriteLameTag when the tag
// frame does not fit).
func (FrameEncodeStages) AddVbrFrame(gfc *LameInternalFlags) { AddVbrFrame(gfc) }

// UpdateStats is lame.c's updateStats (the per-frame bitrate / block-type
// histograms). It emits no mp3 bytes (only fills ov_enc histogram counters the
// not-yet-ported stats slice owns), so it is a no-op here; the frame output is
// unaffected.
func (FrameEncodeStages) UpdateStats(gfc *LameInternalFlags) {}

// InitParamsPpflt wires lame.c's lame_init_params_ppflt (ppflt.go), the polyphase
// lowpass/highpass filter design: it band-quantizes cfg.Lowpass1/2 (Highpass1/2)
// to the actual filterbank transition band and fills sv_enc.amp_filter[32], which
// mdct_sub48 reads to attenuate/zero the bands above the lowpass.
func (FrameEncodeStages) InitParamsPpflt(gfc *LameInternalFlags) { gfc.initParamsPpflt() }

// ApplyPreset is presets.c's apply_preset (presets.go). The seam's `bitrate` is
// the C `preset` argument (lame.c:995/1020 pass 500-VBR_q*10 for the VBR modes,
// lame.c:1057 passes VBR_mean_bitrate_kbps for cbr/abr); `cbr` is the C `enforce`
// flag (always 0 from lame_init_params). It expands the chosen -V / ABR level into
// the gfp tuning fields lame_init_params then copies into cfg, plus cfg.Minval /
// cfg.ATHfixpoint directly.
func (FrameEncodeStages) ApplyPreset(gfc *LameInternalFlags, gfp *LameGlobalFlags, bitrate, cbr int) {
	gfc.applyPreset(gfp, bitrate, cbr != 0)
}

// InitQval wires lame.c's lame_init_qval (init_qval.go), the
// quality-to-noise-shaping mapping the iteration loop branches on. It is a full
// 1:1 port (not a stub) because the CBR/ABR loop is undefined without
// noise_shaping / noise_shaping_amp / use_best_huffman / full_outer_loop.
func (FrameEncodeStages) InitQval(gfc *LameInternalFlags, gfp *LameGlobalFlags) {
	gfc.initQval(gfp)
}

// IterationInit wires quantize_pvt.c's iteration_init (iteration_init.go), the
// one-time quantizer table fill + longfact/shortfact setup.
func (FrameEncodeStages) IterationInit(gfc *LameInternalFlags) {
	gfc.iterationInit()
}

// PsymodelInit wires psymodel.c's psymodel_init to the existing pure-Go port
// InitPsyModel (psymodel_init.go), bridging the LameGlobalFlags the init driver
// holds to the PsyInitParams the native initialiser takes.
func (FrameEncodeStages) PsymodelInit(gfc *LameInternalFlags, gfp *LameGlobalFlags) int {
	return gfc.InitPsyModel(psyInitParamsFromGfp(gfp))
}

// GetMaxFrameBufferSizeByConstraint wires bitstream.c's
// get_max_frame_buffer_size_by_constraint (bitstream_format.go).
func (FrameEncodeStages) GetMaxFrameBufferSizeByConstraint(gfc *LameInternalFlags, strictISO int) int {
	return gfc.getMaxFrameBufferSizeByConstraint(strictISO)
}

// OptimumBandwidth is a 1:1 translation of lame.c's optimum_bandwidth
// (lame.c:195-272): the bitrate-driven lowpass auto-bandwidth the CBR/ABR init
// reaches when the user left the lowpass unset (lame.c:728/732). It maps the
// total bitrate to the best input-filter lowpass cutoff via freq_map, indexed by
// nearestBitrateFullIndex. The highpass (upper) limit is "currently not used" in
// LAME — the C function leaves *upperlimit untouched, the caller's highpass local
// stays uninitialised and is never read, and the Go caller (init.go) discards
// upper with `_`. So upper carries no meaning; we return 0 (lower is the only
// meaningful output).
func (FrameEncodeStages) OptimumBandwidth(bitrate int) (lower, upper float64) {
	freqMap := [17]int{
		2000,  // 8
		3700,  // 16
		3900,  // 24
		5500,  // 32
		7000,  // 40
		7500,  // 48
		10000, // 56
		11000, // 64
		13500, // 80
		15100, // 96
		15600, // 112
		17000, // 128
		17500, // 160
		18600, // 192
		19400, // 224
		19700, // 256
		20500, // 320
	}
	lower = float64(freqMap[nearestBitrateFullIndex(bitrate)])
	return lower, 0
}

// OptimumSamplefreq is a 1:1 translation of lame.c's optimum_samplefreq
// (lame.c:275-356), the output-rate auto-detect LameInitParams reaches when the
// output rate is left unset (the -V path: the q-map at <44k leaves it 0, so the
// driver derives it here from the lowpass cutoff). lowpassFreq == -1 means "no
// lowpass constraint" (the q-map break case).
func (FrameEncodeStages) OptimumSamplefreq(lowpassFreq, inputSamplefreq int) int {
	suggested := 44100
	switch {
	case inputSamplefreq >= 48000:
		suggested = 48000
	case inputSamplefreq >= 44100:
		suggested = 44100
	case inputSamplefreq >= 32000:
		suggested = 32000
	case inputSamplefreq >= 24000:
		suggested = 24000
	case inputSamplefreq >= 22050:
		suggested = 22050
	case inputSamplefreq >= 16000:
		suggested = 16000
	case inputSamplefreq >= 12000:
		suggested = 12000
	case inputSamplefreq >= 11025:
		suggested = 11025
	case inputSamplefreq >= 8000:
		suggested = 8000
	}

	if lowpassFreq == -1 {
		return suggested
	}

	if lowpassFreq <= 15960 {
		suggested = 44100
	}
	if lowpassFreq <= 15250 {
		suggested = 32000
	}
	if lowpassFreq <= 11220 {
		suggested = 24000
	}
	if lowpassFreq <= 9970 {
		suggested = 22050
	}
	if lowpassFreq <= 7230 {
		suggested = 16000
	}
	if lowpassFreq <= 5420 {
		suggested = 12000
	}
	if lowpassFreq <= 4510 {
		suggested = 11025
	}
	if lowpassFreq <= 3970 {
		suggested = 8000
	}

	if inputSamplefreq < suggested {
		// choose a valid MPEG sample frequency above the input sample frequency
		// to avoid SFB21/12 bitrate bloat (rh 061115).
		switch {
		case inputSamplefreq > 44100:
			return 48000
		case inputSamplefreq > 32000:
			return 44100
		case inputSamplefreq > 24000:
			return 32000
		case inputSamplefreq > 22050:
			return 24000
		case inputSamplefreq > 16000:
			return 22050
		case inputSamplefreq > 12000:
			return 16000
		case inputSamplefreq > 11025:
			return 12000
		case inputSamplefreq > 8000:
			return 11025
		default:
			return 8000
		}
	}
	return suggested
}
