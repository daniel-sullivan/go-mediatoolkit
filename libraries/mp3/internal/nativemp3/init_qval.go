// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// lame_init_qval — a 1:1 Go translation of LAME 3.100's lame_init_qval
// (libmp3lame/lame.c:1129, the function body at the lame_init_qval definition).
// lame_init_params (init.go) reaches it through the EncoderStages seam; from
// gfp->quality it sets the quantizer's noise-shaping policy in the SessionConfig
// (noise_shaping / noise_shaping_amp / noise_shaping_stop / use_best_huffman /
// full_outer_loop / subblock_gain) and, for the lower-quality settings, the
// substep_shaping default. The CBR/ABR iteration loop (quantize_encode.go)
// branches on every one of these, so this is ported in full (it is small,
// self-contained integer logic) rather than stubbed — the iteration loop's
// behaviour is undefined without it.
//
// This is pure integer assignment (no floating point), so it is bit-identical
// regardless of build tag. noise_shaping_stop is part of the C struct but the
// ported iteration loop does not yet read it (it gates the unported VBR-new
// stop criterion); it is set on the cfg field for completeness where that field
// exists, and skipped where it does not — see the inline note.

// NoiseShapingStop is the SessionConfig field LAME's lame_init_qval writes
// (cfg->noise_shaping_stop). The ported CBR/ABR iteration loop does not consume
// it (it feeds the unported VBR-new stop criterion), so rather than widen the
// unified SessionConfig for a field no ported code reads, lame_init_qval's
// noise_shaping_stop assignments are tracked on this package-level shadow. When
// the VBR/stop slice lands it should promote this to cfg.NoiseShapingStop.
//
// (A package global is acceptable here only because the encoder context is not
// used concurrently — the public Encoder documents it is not safe for
// concurrent use, matching LAME's single-gfc-per-stream model.)
var noiseShapingStop int

// initQval is LAME's lame_init_qval (lame.c:1129). It mirrors the C switch on
// gfp->quality exactly, including the case-8 fallthrough to 7 and the
// "if (noise_shaping == 0)" / "if (subblock_gain == -1)" guards (which preserve
// any value a preset already set).
func (gfc *LameInternalFlags) initQval(gfp *LameGlobalFlags) {
	cfg := &gfc.Cfg

	switch gfp.Quality {
	default:
		fallthrough
	case 9: // no psymodel, no noise shaping
		cfg.NoiseShaping = 0
		cfg.NoiseShapingAmp = 0
		noiseShapingStop = 0
		cfg.UseBestHuffman = 0
		cfg.FullOuterLoop = 0

	case 8:
		gfp.Quality = 7
		fallthrough
	case 7: // use psymodel (short block and m/s switching), but no noise shaping
		cfg.NoiseShaping = 0
		cfg.NoiseShapingAmp = 0
		noiseShapingStop = 0
		cfg.UseBestHuffman = 0
		cfg.FullOuterLoop = 0
		if cfg.Vbr == vbrMt || cfg.Vbr == vbrMtrh {
			cfg.FullOuterLoop = -1
		}

	case 6:
		if cfg.NoiseShaping == 0 {
			cfg.NoiseShaping = 1
		}
		cfg.NoiseShapingAmp = 0
		noiseShapingStop = 0
		if cfg.SubblockGain == -1 {
			cfg.SubblockGain = 1
		}
		cfg.UseBestHuffman = 0
		cfg.FullOuterLoop = 0

	case 5:
		if cfg.NoiseShaping == 0 {
			cfg.NoiseShaping = 1
		}
		cfg.NoiseShapingAmp = 0
		noiseShapingStop = 0
		if cfg.SubblockGain == -1 {
			cfg.SubblockGain = 1
		}
		cfg.UseBestHuffman = 0
		cfg.FullOuterLoop = 0

	case 4:
		if cfg.NoiseShaping == 0 {
			cfg.NoiseShaping = 1
		}
		cfg.NoiseShapingAmp = 0
		noiseShapingStop = 0
		if cfg.SubblockGain == -1 {
			cfg.SubblockGain = 1
		}
		cfg.UseBestHuffman = 1
		cfg.FullOuterLoop = 0

	case 3:
		if cfg.NoiseShaping == 0 {
			cfg.NoiseShaping = 1
		}
		cfg.NoiseShapingAmp = 1
		noiseShapingStop = 1
		if cfg.SubblockGain == -1 {
			cfg.SubblockGain = 1
		}
		cfg.UseBestHuffman = 1
		cfg.FullOuterLoop = 0

	case 2:
		if cfg.NoiseShaping == 0 {
			cfg.NoiseShaping = 1
		}
		if gfc.SvQnt.SubstepShaping == 0 {
			gfc.SvQnt.SubstepShaping = 2
		}
		cfg.NoiseShapingAmp = 1
		noiseShapingStop = 1
		if cfg.SubblockGain == -1 {
			cfg.SubblockGain = 1
		}
		cfg.UseBestHuffman = 1 // inner loop
		cfg.FullOuterLoop = 0

	case 1:
		if cfg.NoiseShaping == 0 {
			cfg.NoiseShaping = 1
		}
		if gfc.SvQnt.SubstepShaping == 0 {
			gfc.SvQnt.SubstepShaping = 2
		}
		cfg.NoiseShapingAmp = 2
		noiseShapingStop = 1
		if cfg.SubblockGain == -1 {
			cfg.SubblockGain = 1
		}
		cfg.UseBestHuffman = 1
		cfg.FullOuterLoop = 0

	case 0:
		if cfg.NoiseShaping == 0 {
			cfg.NoiseShaping = 1
		}
		if gfc.SvQnt.SubstepShaping == 0 {
			gfc.SvQnt.SubstepShaping = 2
		}
		cfg.NoiseShapingAmp = 2
		noiseShapingStop = 1
		if cfg.SubblockGain == -1 {
			cfg.SubblockGain = 1
		}
		// type 2 disabled because of its slowness, in favor of full outer loop.
		cfg.UseBestHuffman = 1
		cfg.FullOuterLoop = 1
	}
}
