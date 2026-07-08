package fvad

// This file ports src/vad/vad_gmm.c — the Gaussian probability
// calculation used by core.go.

const (
	kCompVar = int32(22005)
	kLog2Exp = int16(5909) // log2(exp(1)) in Q12.
)

// GaussianProbability ports WebRtcVad_GaussianProbability. For a normal
// distribution, the probability of input is calculated and returned in
// Q20:
//
//	1 / s * exp(-(x - m)^2 / (2 * s^2))
//
// with m = mean (Q7), s = std (Q7), x = input (Q4). delta (Q11) =
// (x - m) / s^2 is returned for use when updating the noise/speech
// model.
func GaussianProbability(input, mean, std int16) (probability int32, delta int16) {
	// Calculate invStd = 1 / s, in Q10.
	// 131072 = 1 in Q17, and (std >> 1) is for rounding instead of
	// truncation. Q-domain: Q17 / Q7 = Q10.
	tmp32 := int32(131072) + int32(std>>1)
	invStd := int16(DivW32W16(tmp32, std))

	// Calculate invStd2 = 1 / s^2, in Q14.
	tmp16 := invStd >> 2 // Q10 -> Q8.
	// Q-domain: (Q8 * Q8) >> 2 = Q14.
	invStd2 := int16((int32(tmp16) * int32(tmp16)) >> 2)

	tmp16 = int16(int32(input) << 3) // Q4 -> Q7
	tmp16 = tmp16 - mean             // Q7 - Q7 = Q7 (int16 wrap == C truncation)

	// delta = (x - m) / s^2, in Q11.
	// Q-domain: (Q14 * Q7) >> 10 = Q11.
	delta = int16((int32(invStd2) * int32(tmp16)) >> 10)

	// Calculate the exponent tmp32 = (x - m)^2 / (2 * s^2), in Q10.
	// Replacing division by two with one shift.
	// Q-domain: (Q11 * Q7) >> 8 = Q10.
	tmp32 = (int32(delta) * int32(tmp16)) >> 9

	// If the exponent is small enough to give a non-zero probability we
	// calculate expValue ~= exp(-(x - m)^2 / (2 * s^2))
	//                    ~= exp2(-log2(exp(1)) * tmp32).
	expValue := int16(0)
	if tmp32 < kCompVar {
		// Calculate tmp16 = log2(exp(1)) * tmp32, in Q10.
		// Q-domain: (Q12 * Q10) >> 12 = Q10.
		tmp16 = int16((int32(kLog2Exp) * tmp32) >> 12)
		tmp16 = -tmp16
		expValue = 0x0400 | (tmp16 & 0x03FF)
		tmp16 = ^tmp16 // C: tmp16 ^= 0xFFFF (int-promote, XOR, truncate) == 16-bit NOT
		tmp16 >>= 10
		tmp16++
		// Get expValue = exp(-tmp32) in Q10.
		//
		// TRAP, faithfully replicated: when tmp32 is NEGATIVE (only
		// reachable through delta's int16 wrap on extreme inputs — a
		// "probability > 1" exponent), the C computes a shift count
		// tmp16 that is negative or ≥ 32, making `exp_value >>= tmp16`
		// undefined behaviour. Every reference build resolves it the
		// same way: exp_value is promoted to 32-bit int and the shift
		// count is masked to 5 bits by the hardware (x86 SAR r32,CL
		// masks CL & 31; ARM64 ASR Wd,Wn,Wm takes Wm mod 32), so the
		// port emulates exactly that. For the normal in-range counts
		// (0..31) the mask is a no-op.
		expValue = int16(int32(expValue) >> (uint16(tmp16) & 31))
	}

	// Calculate and return (1 / s) * exp(-(x - m)^2 / (2 * s^2)), in
	// Q20. Q-domain: Q10 * Q10 = Q20.
	return int32(invStd) * int32(expValue), delta
}
