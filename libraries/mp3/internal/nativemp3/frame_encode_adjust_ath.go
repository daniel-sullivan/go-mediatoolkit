// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// adjust_ATH — the per-frame auto-adjustment of the threshold of hearing, a
// 1:1 Go translation of LAME 3.100's adjust_ATH (encoder.c:56). It is the Stage
// 1.5 helper the frame dispatcher (frame_encode.go) calls between the psy model
// and the MDCT (encoder.c:397); it lives with the encoder.c frame-encode slice
// rather than psymodel.c despite the seam naming it "psymodel.c's adjust_ATH"
// in the work-list (the C function is defined in encoder.c, static).
//
// It uses the per-granule loudness (gfc->ov_psy.loudness_sq, filled by the psy
// model) to scale ATH->adjust_factor toward ATH->adjust_limit, raising the
// noise floor for loud frames and lowering it for quiet ones. jd 2001.
//
// # Floating-point parity
//
// FLOAT == float32. The arithmetic (the loudness sum, the 0.5 / aa_sensitivity_p
// scalings, the adj_lim_new curve and the gradual-descent multiply) is routed
// through the //go:noinline fe* helpers (frame_encode_fp_strict.go) so the
// mp3_strict build separately-rounds, matching the -ffp-contract=off oracle. The
// double-literal constants (0.5, 31.98, 0.000625, 0.075, 0.925) multiply the
// FLOAT operand in C double then narrow on store; those FLOAT*=double-const /
// double-const-combination expressions are written as float32(... float64 ...)
// so the narrow happens once, exactly as the C does.

// adjustATH is LAME's adjust_ATH (encoder.c:56).
func (gfc *LameInternalFlags) adjustATH() {
	cfg := &gfc.Cfg
	ath := gfc.ATH

	if ath.UseAdjust == 0 {
		ath.AdjustFactor = 1.0 // no adjustment
		return
	}

	// loudness based on equal loudness curve; use granule with maximum combined
	// loudness.
	maxPow := gfc.OvPsy.LoudnessSq[0][0]
	gr2Max := gfc.OvPsy.LoudnessSq[1][0]
	if cfg.ChannelsOut == 2 {
		maxPow = feAdd(maxPow, gfc.OvPsy.LoudnessSq[0][1])
		gr2Max = feAdd(gr2Max, gfc.OvPsy.LoudnessSq[1][1])
	} else {
		maxPow = feAdd(maxPow, maxPow)
		gr2Max = feAdd(gr2Max, gr2Max)
	}
	if cfg.ModeGr == 2 {
		maxPow = maxF32(maxPow, gr2Max)
	}
	// max_pow *= 0.5 (0.5 is a double literal; FLOAT*=double narrows once).
	maxPow = float32(float64(maxPow) * 0.5)

	// user tuning of ATH adjustment region (float32 product).
	maxPow = feMul(maxPow, ath.AaSensitivityP)

	if maxPow > 0.03125 { // ((1 - 0.000625)/ 31.98) from curve below
		if ath.AdjustFactor >= 1.0 {
			ath.AdjustFactor = 1.0
		} else if ath.AdjustFactor < ath.AdjustLimit {
			// preceding frame has lower ATH adjust; ascend only to its limit.
			ath.AdjustFactor = ath.AdjustLimit
		}
		ath.AdjustLimit = 1.0
	} else { // adjustment curve
		// about 32 dB maximum adjust (0.000625). 31.98 * max_pow + 0.000625 is
		// computed in double (the literals are double; FLOAT max_pow promotes) and
		// narrowed to the FLOAT adj_lim_new.
		adjLimNew := float32(31.98*float64(maxPow) + 0.000625)
		if ath.AdjustFactor >= adjLimNew { // descend gradually
			// adjust_factor *= adj_lim_new * 0.075 + 0.925 — the RHS factor is
			// double (adj_lim_new promoted), narrowed; the *= then narrows again.
			factor := float32(float64(adjLimNew)*0.075 + 0.925)
			ath.AdjustFactor = feMul(ath.AdjustFactor, factor)
			if ath.AdjustFactor < adjLimNew { // stop descent
				ath.AdjustFactor = adjLimNew
			}
		} else { // ascend
			if ath.AdjustLimit >= adjLimNew {
				ath.AdjustFactor = adjLimNew
			} else if ath.AdjustFactor < ath.AdjustLimit {
				// preceding frame has lower ATH adjust; ascend only to its limit.
				ath.AdjustFactor = ath.AdjustLimit
			}
		}
		ath.AdjustLimit = adjLimNew
	}
}
