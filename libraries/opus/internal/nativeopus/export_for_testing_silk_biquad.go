package nativeopus

// Thin exports for SILK biquad / ana_filt_bank / LP_variable_cutoff
// parity tests.

func ExportTestSilkBiquadAltStride1(in_ []int16, B_Q28, A_Q28, S []int32) (out []int16, Sout []int32) {
	sCopy := append([]int32(nil), S...)
	out = make([]int16, len(in_))
	silk_biquad_alt_stride1(in_, B_Q28, A_Q28, sCopy, out, int32(len(in_)))
	return out, sCopy
}

func ExportTestSilkBiquadAltStride2(in_ []int16, B_Q28, A_Q28, S []int32) (out []int16, Sout []int32) {
	sCopy := append([]int32(nil), S...)
	out = make([]int16, len(in_))
	// len_ passed to the stride2 kernel is the count of *pairs*; C
	// oracle loops 0..len and touches in[2k], in[2k+1].
	silk_biquad_alt_stride2_c(in_, B_Q28, A_Q28, sCopy, out, int32(len(in_)/2))
	return out, sCopy
}

// ExportTestSilkBiquadAltStride2SIMDRef routes through the pure-Go
// 4-lane SIMD-style reference kernel. Used by the parity test to
// gate the SIMD path against the scalar C reference.
func ExportTestSilkBiquadAltStride2SIMDRef(in_ []int16, B_Q28, A_Q28, S []int32) (out []int16, Sout []int32) {
	sCopy := append([]int32(nil), S...)
	out = make([]int16, len(in_))
	silk_biquad_alt_stride2_simd_ref(in_, B_Q28, A_Q28, sCopy, out, int32(len(in_)/2))
	return out, sCopy
}

// ExportTestSilkBiquadAltStride2Arch routes through the arch-aware
// SIMD dispatch: the arm64 NEON path when available, scalar C
// otherwise. Same parity contract as silk_biquad_alt_stride2_c.
func ExportTestSilkBiquadAltStride2Arch(in_ []int16, B_Q28, A_Q28, S []int32) (out []int16, Sout []int32) {
	sCopy := append([]int32(nil), S...)
	out = make([]int16, len(in_))
	if silkBiquadAltStride2SIMDAvailable {
		silk_biquad_alt_stride2_simd(in_, B_Q28, A_Q28, sCopy, out, int32(len(in_)/2))
	} else {
		silk_biquad_alt_stride2_c(in_, B_Q28, A_Q28, sCopy, out, int32(len(in_)/2))
	}
	return out, sCopy
}

// ExportTestSilkBiquadAltStride2SIMDAvailable — true when the
// compile-time NEON SIMD path is wired in for silk_biquad_alt_stride2.
func ExportTestSilkBiquadAltStride2SIMDAvailable() bool {
	return silkBiquadAltStride2SIMDAvailable
}

func ExportTestSilkAnaFiltBank1(in_ []int16, S []int32) (outL, outH []int16, Sout []int32) {
	sCopy := append([]int32(nil), S...)
	n := len(in_) / 2
	outL = make([]int16, n)
	outH = make([]int16, n)
	silk_ana_filt_bank_1(in_, sCopy, outL, outH, int32(len(in_)))
	return outL, outH, sCopy
}

// ExportTestSilkLPVariableCutoff runs the full LP path on a single
// frame. Returns the filtered output and the updated state vector.
func ExportTestTransitionFrames() int { return TRANSITION_FRAMES }

func ExportTestSilkLPVariableCutoff(frame []int16, mode int, transFrameNo int32, InLPState []int32) (
	out []int16, outState []int32, outTransFrameNo int32) {
	var st silk_LP_state
	st.mode = mode
	st.transition_frame_no = transFrameNo
	if len(InLPState) >= 2 {
		st.In_LP_State[0] = InLPState[0]
		st.In_LP_State[1] = InLPState[1]
	}
	out = append([]int16(nil), frame...)
	silk_LP_variable_cutoff(&st, out, len(out))
	return out, []int32{st.In_LP_State[0], st.In_LP_State[1]}, st.transition_frame_no
}
