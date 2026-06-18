// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// LPC lattice/direct-form helpers used by the ENCODE-side TNS filter, ported 1:1
// from libFDK/src/FDK_lpc.cpp. On the build target FIXP_LPC == FIXP_SGL == int16
// (FDK_lpc.h:128), so these are the "Default version" overloads (FX_LPC2FX_DBL ==
// FX_SGL2FX_DBL, FX_DBL2FX_LPC == FX_DBL2FX_SGL). Both are pure integer
// fixed-point kernels — int64-intermediate fixmul, arithmetic shifts, the arm8
// fixmul forms — bit-identical regardless of vectorization, so no aac_strict
// gating. They back FDKaacEnc_TnsEncode (enc_tns_encode.go):
//   CLpc_ParcorToLpc  — reflection (ParCor) -> direct-form LPC + return shift.
//   CLpc_Analysis     — FIR analysis (residual) filter applied to the spectrum.
//
// LPC_MAX_ORDER == 24 (lpcMaxOrder, tns_apply.go). TNS_MAX_ORDER == 12, so
// order <= 12 here.

// clpcParcorToLpc ports the default-version CLpc_ParcorToLpc(const FIXP_LPC
// reflCoeff[], FIXP_LPC LpcCoeff[], INT numOfCoeff, FIXP_DBL workBuffer[])
// (FDK_lpc.cpp:393-432): convert reflection (ParCor) coefficients to direct-form
// LPC coefficients, returning the LPC exponent (par2LpcShiftVal - shiftval).
// workBuffer must have length >= numOfCoeff; reflCoeff/LpcCoeff are FIXP_LPC
// (int16). fMult(FIXP_LPC, FIXP_DBL) == fixmul_SD == fMultDS with swapped args on
// this target. FX_LPC2FX_DBL(x) == int32(x)<<16; FX_DBL2FX_LPC(x) == int16(x>>16).
func clpcParcorToLpc(reflCoeff, lpcCoeff []int16, numOfCoeff int, workBuffer []int32) int {
	const par2LpcShiftVal = 6 // 6 should be enough, bec. max(numOfCoeff) = 20
	var maxVal int32

	// workBuffer[0] = FX_LPC2FX_DBL(reflCoeff[0]) >> par2LpcShiftVal;
	workBuffer[0] = (int32(reflCoeff[0]) << 16) >> par2LpcShiftVal
	var i, j int
	for i = 1; i < numOfCoeff; i++ {
		for j = 0; j < i/2; j++ {
			tmp1 := workBuffer[j]
			tmp2 := workBuffer[i-1-j]
			// fMult(reflCoeff[i], tmp2) == fixmul_SD(SGL, DBL) == fMultDS(tmp2, reflCoeff[i]).
			workBuffer[j] += fMultDS(tmp2, reflCoeff[i])
			workBuffer[i-1-j] += fMultDS(tmp1, reflCoeff[i])
		}
		if i&1 != 0 {
			workBuffer[j] += fMultDS(workBuffer[j], reflCoeff[i])
		}

		workBuffer[i] = (int32(reflCoeff[i]) << 16) >> par2LpcShiftVal
	}

	// calculate exponent
	for i = 0; i < numOfCoeff; i++ {
		maxVal = fMax(maxVal, fAbsDBL(workBuffer[i]))
	}

	shiftval := int(fMin(fNorm(maxVal), par2LpcShiftVal))

	for i = 0; i < numOfCoeff; i++ {
		// LpcCoeff[i] = FX_DBL2FX_LPC(workBuffer[i] << shiftval);
		lpcCoeff[i] = int16((workBuffer[i] << uint(shiftval)) >> 16)
	}

	return par2LpcShiftVal - shiftval
}

// clpcAnalysis ports the FIR CLpc_Analysis(FIXP_DBL *signal, int signal_size,
// const FIXP_LPC lpcCoeff_m[], int lpcCoeff_e, int order, FIXP_DBL *filtState,
// int *filtStateIndex) (FDK_lpc.cpp:301-353): the analysis (residual) filter
// applied in place to signal[0:signal_size]. lpcCoeff_m is FIXP_LPC (int16);
// filtState has length >= order. filtStateIndex is passed by pointer in C; the
// TNS call site passes NULL, so this port takes it as a nil-able *int (nil ==
// the NULL path, stateIndex starts at 0 and is not written back).
//
// fMultAddDiv2(FIXP_DBL, FIXP_LPC, FIXP_DBL) == fixmadddiv2_SD == on this target
// the generic fixmadddiv2_DD(x, FX_SGL2FX_DBL(a), b) == x + fMultDiv2SD(a, b).
func clpcAnalysis(signal []int32, signalSize int, lpcCoeff []int16, lpcCoeffE,
	order int, filtState []int32, filtStateIndex *int) {

	shift := lpcCoeffE + 1 // +1, because fMultDiv2
	if order <= 0 {
		return
	}
	var stateIndex int
	if filtStateIndex != nil {
		stateIndex = *filtStateIndex
	} else {
		stateIndex = 0
	}

	// keep filter coefficients twice and save memory copy operation in modulo
	// state buffer
	var coeff [2 * lpcMaxOrder]int16
	copy(coeff[0:order], lpcCoeff[:order])
	copy(coeff[order:2*order], lpcCoeff[:order])

	for j := 0; j < signalSize; j++ {
		base := order - stateIndex // pCoeff = &coeff[order - stateIndex]

		tmp := signal[j] >> uint(shift)
		for i := 0; i < order; i++ {
			// tmp = fMultAddDiv2(tmp, pCoeff[i], filtState[i]);
			tmp += fMultDiv2SD(coeff[base+i], filtState[i])
		}

		if stateIndex-1 < 0 {
			stateIndex = stateIndex - 1 + order
		} else {
			stateIndex = stateIndex - 1
		}
		filtState[stateIndex] = signal[j]

		signal[j] = tmp << uint(shift)
	}

	if filtStateIndex != nil {
		*filtStateIndex = stateIndex
	}
}
