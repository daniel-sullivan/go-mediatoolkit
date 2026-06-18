package nativeopus

// 1:1 port of libopus/silk/float/LTP_scale_ctrl_FLP.c.
//
// Quirk: C expands `silk_SMULBB(psEncCtrl->LTPredCodGain, round_loss)`
// to `((opus_int32)(opus_int16)(float) * (opus_int32)(opus_int16)(int))`.
// The cast `(opus_int16)(float)` is UB when the float exceeds int16
// range; clang at -O2 on arm64 emits a single `fcvtzs w, s` to int32
// and skips the int16 narrowing (LLVM exploits the UB to drop the
// sxth). To match C bit-for-bit, Go must similarly NOT narrow the
// float-side operand to int16 — keep it as int32. The int-side
// operand (round_loss) IS a real integer, so it narrows normally.

func silk_LTP_scale_ctrl_FLP(psEnc *silk_encoder_state_FLP, psEncCtrl *silk_encoder_control_FLP, condCoding opus_int) {
	var round_loss opus_int32

	if condCoding == CODE_INDEPENDENTLY {
		round_loss = opus_int32(psEnc.sCmn.PacketLoss_perc) * opus_int32(psEnc.sCmn.nFramesPerPacket)
		if psEnc.sCmn.LBRR_flag != 0 {
			round_loss = 2 + silk_SMULBB(round_loss, round_loss)/100
		}
		// LTPredCodGain: float→int32 directly (no int16 narrowing,
		// matching the clang-arm64 UB shortcut).
		gainI32 := opus_int32(psEncCtrl.LTPredCodGain)
		// round_loss stays as int32; silk_SMULBB narrows it to int16.
		rlI32 := opus_int32(opus_int16(round_loss))
		smulbb := gainI32 * rlI32

		th1 := silk_log2lin(2900 - opus_int32(psEnc.sCmn.SNR_dB_Q7))
		th2 := silk_log2lin(3900 - opus_int32(psEnc.sCmn.SNR_dB_Q7))
		idx := opus_int8(0)
		if smulbb > th1 {
			idx++
		}
		if smulbb > th2 {
			idx++
		}
		psEnc.sCmn.indices.LTP_scaleIndex = idx
	} else {
		psEnc.sCmn.indices.LTP_scaleIndex = 0
	}

	psEncCtrl.LTP_scale = silk_float(silk_LTPScales_table_Q14[psEnc.sCmn.indices.LTP_scaleIndex]) / 16384.0
}
