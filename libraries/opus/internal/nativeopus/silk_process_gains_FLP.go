package nativeopus

import "math"

// 1:1 port of libopus/silk/float/process_gains_FLP.c.
//
// Mutates psEnc (GainsIndices, quantOffsetType, shape state
// LastGainIndex) and psEncCtrl (Gains, GainsUnq_Q16,
// lastGainIndexPrev, Lambda) per frame.
//
// Arithmetic notes
// ----------------
//   * Every unsuffixed C literal (e.g. 0.25, 12.0, 0.33, 21.0,
//     128.0, 1024.0) is implicitly `double`. The enclosing
//     expressions mix silk_float (= float32) inputs with those
//     doubles; under clang's usual arithmetic conversions the
//     operands are promoted to double for the scalar mul/sub, and
//     only narrowed back when written to a silk_float destination
//     or when passed as an argument of silk_float type.
//     We mirror that here by evaluating each such sub-expression
//     in float64 and narrowing at the boundary.
//   * silk_sigmoid(x) = (silk_float)(1.0 / (1.0 + exp(-x))) — the
//     computation is done in double and narrowed to float32.
//   * InvMaxSqrVal uses pow(2.0f, ...) but pow's prototype is double
//     only, so the `2.0f` promotes to 2.0; the result divided by an
//     int subfr_length stays in double and narrows to float at the
//     final `(silk_float)` cast.
//   * The sqrt in the Gains soft-limit: gain*gain + ResNrg*InvMaxSqrVal
//     is computed in float32 (all operands are silk_float) then
//     widened to double inside sqrt() and narrowed back.
//   * silk_float2int rounds float32 to int32 via round-to-nearest-even.
//
// The final Lambda accumulator chain adds LAMBDA_OFFSET (a plain
// double constant 1.2f) to a sum of `<double> * <float>` products;
// each `*` is done in float32 then added in float32 accumulated from
// left to right. We route the float32 adds through add_f32 and the
// multiplies through mul_f32 per the project's parity rules.

func silk_process_gains_FLP(
	psEnc *silk_encoder_state_FLP,
	psEncCtrl *silk_encoder_control_FLP,
	condCoding opus_int,
) {
	psShapeSt := &psEnc.sShape
	var pGains_Q16 [MAX_NB_SUBFR]opus_int32
	var s, InvMaxSqrVal, gain, quant_offset silk_float

	// Gain reduction when LTP coding gain is high.
	if psEnc.sCmn.indices.signalType == TYPE_VOICED {
		// s = 1.0f - 0.5f * silk_sigmoid( 0.25f * ( LTPredCodGain - 12.0f ) )
		arg := mul_f32(0.25, sub_f32(psEncCtrl.LTPredCodGain, 12.0))
		sig := silk_sigmoid_flp(arg)
		s = sub_f32(1.0, mul_f32(0.5, sig))
		for k := opus_int(0); k < psEnc.sCmn.nb_subfr; k++ {
			psEncCtrl.Gains[k] = mul_f32(psEncCtrl.Gains[k], s)
		}
	}

	// Limit the quantized signal.
	// C: ( silk_float )( pow( 2.0f, 0.33f * ( 21.0f - SNR_dB_Q7 * ( 1/128.0f ) ) ) / subfr_length )
	// `0.33f` / `21.0f` / `1/128.0f` — the unsuffixed `1` in `1/128.0f` is int;
	// 1 / 128.0f is a float32 constant (~0.0078125). The multiplication
	// SNR_dB_Q7 * (1/128.0f) is int*float32 → float32. Then 21.0f - <float32>
	// stays float32. Multiply by 0.33f (also float32 constant) stays float32.
	// pow() takes double → promotes to double, divides by subfr_length (int→double),
	// then narrows back to silk_float. We match that mixed evaluation exactly.
	snr128 := mul_f32(float32(psEnc.sCmn.SNR_dB_Q7), float32(1.0)/128.0)
	expArg := mul_f32(0.33, sub_f32(21.0, snr128))
	// pow(2.0f, expArg) — convert expArg to double, compute pow, divide by
	// subfr_length (an int, promoted to double), narrow to float.
	InvMaxSqrVal = silk_float(math.Pow(2.0, float64(expArg)) / float64(psEnc.sCmn.subfr_length))

	for k := opus_int(0); k < psEnc.sCmn.nb_subfr; k++ {
		// Soft limit on ratio residual energy and squared gains.
		gain = psEncCtrl.Gains[k]
		// gain*gain + ResNrg[k]*InvMaxSqrVal — both products in float32,
		// sum in float32, then celt_sqrt-style narrow through (float)sqrt((double)x).
		sumF := fma_add(mul_f32(gain, gain), psEncCtrl.ResNrg[k], InvMaxSqrVal)
		gain = silk_float(math.Sqrt(float64(sumF)))
		psEncCtrl.Gains[k] = silk_min_float(gain, 32767.0)
	}

	// Prepare gains for noise shaping quantization.
	// C: (opus_int32)( Gains[k] * 65536.0f ) — truncating cast, NOT
	// silk_float2int. Gains[k] is clamped to <=32767 above, so
	// Gains[k]*65536 stays well within int32 range.
	for k := opus_int(0); k < psEnc.sCmn.nb_subfr; k++ {
		pGains_Q16[k] = opus_int32(mul_f32(psEncCtrl.Gains[k], 65536.0))
	}

	// Save unquantized gains and gain Index.
	for k := opus_int(0); k < psEnc.sCmn.nb_subfr; k++ {
		psEncCtrl.GainsUnq_Q16[k] = pGains_Q16[k]
	}
	psEncCtrl.lastGainIndexPrev = psShapeSt.LastGainIndex

	// Quantize gains.
	var conditional opus_int
	if condCoding == CODE_CONDITIONALLY {
		conditional = 1
	}
	silk_gains_quant(psEnc.sCmn.indices.GainsIndices[:], pGains_Q16[:],
		&psShapeSt.LastGainIndex, conditional, psEnc.sCmn.nb_subfr)

	// Overwrite unquantized gains with quantized gains and convert back to Q0 from Q16.
	for k := opus_int(0); k < psEnc.sCmn.nb_subfr; k++ {
		psEncCtrl.Gains[k] = mul_f32(float32(pGains_Q16[k]), 1.0/65536.0)
	}
	// The C code writes `pGains_Q16[k] / 65536.0f`. In float32, `/ 65536.0f`
	// is equivalent to `* (1.0f/65536.0f)` because 1/65536 is exactly
	// representable. We use the multiply form for clarity; both produce
	// identical results.

	// Set quantizer offset for voiced signals.
	if psEnc.sCmn.indices.signalType == TYPE_VOICED {
		// LTPredCodGain + input_tilt_Q15 * (1.0f / 32768.0f) > 1.0f
		tilt := mul_f32(float32(psEnc.sCmn.input_tilt_Q15), 1.0/32768.0)
		if add_f32(psEncCtrl.LTPredCodGain, tilt) > 1.0 {
			psEnc.sCmn.indices.quantOffsetType = 0
		} else {
			psEnc.sCmn.indices.quantOffsetType = 1
		}
	}

	// Quantizer boundary adjustment.
	quant_offset = mul_f32(
		float32(silk_Quantization_Offsets_Q10[psEnc.sCmn.indices.signalType>>1][psEnc.sCmn.indices.quantOffsetType]),
		1.0/1024.0)

	// Lambda = LAMBDA_OFFSET
	//        + LAMBDA_DELAYED_DECISIONS * nStatesDelayedDecision
	//        + LAMBDA_SPEECH_ACT        * speech_activity_Q8 * (1/256.0f)
	//        + LAMBDA_INPUT_QUALITY     * input_quality
	//        + LAMBDA_CODING_QUALITY    * coding_quality
	//        + LAMBDA_QUANT_OFFSET      * quant_offset
	// All constants are float32 (`1.2f`, `-0.05f`, …). Each product is
	// float32, the chain of `+` adds is left-to-right in float32.
	lam := silk_float(LAMBDA_OFFSET)
	lam = fma_add(lam, LAMBDA_DELAYED_DECISIONS, float32(psEnc.sCmn.nStatesDelayedDecision))
	// speech_activity_Q8 * (1/256.0f) — float32; then LAMBDA_SPEECH_ACT *
	// that float32; then accumulated into lam.
	spAct := mul_f32(float32(psEnc.sCmn.speech_activity_Q8), 1.0/256.0)
	lam = fma_add(lam, LAMBDA_SPEECH_ACT, spAct)
	lam = fma_add(lam, LAMBDA_INPUT_QUALITY, psEncCtrl.input_quality)
	lam = fma_add(lam, LAMBDA_CODING_QUALITY, psEncCtrl.coding_quality)
	lam = fma_add(lam, LAMBDA_QUANT_OFFSET, quant_offset)
	psEncCtrl.Lambda = lam
}

// silk_sigmoid_flp — (silk_float)(1.0 / (1.0 + exp(-x))). The math is
// performed in double and narrowed to float32 at the return.
//
// The C implementation uses exp() (double), then 1.0/(1.0+…) in
// double, then casts to silk_float. Go's math.Exp is double.
//
//go:noinline
func silk_sigmoid_flp(x silk_float) silk_float {
	return silk_float(1.0 / (1.0 + math.Exp(-float64(x))))
}

// silk_min_float — C: #define silk_min_float( a, b ) (((a) < (b)) ? (a) : (b))
func silk_min_float(a, b silk_float) silk_float {
	if a < b {
		return a
	}
	return b
}

// silk_float2int — rounds float32 to int32 with round-to-nearest-even.
// C: (opus_int32)float2int(x). float2int in the modern builds is lrintf.
func silk_float2int(x silk_float) opus_int32 {
	return float2int(x)
}
