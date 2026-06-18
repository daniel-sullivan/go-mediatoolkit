// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// TNS encoder Gauss autocorrelation window. 1:1 port of
// FDKaacEnc_CalcGaussWindow (libAACenc/src/aacenc_tns.cpp:1094-1139, identical to
// the forward declaration's body at :250). It builds the Gaussian ACF window the
// TNS analysis applies to the autocorrelation before the Levinson-Durbin LPC,
// derived from the requested time resolution.
//
// Pure fixed-point (FIXP_DBL == int32): the window exponent is computed in the
// fDivNorm/fMult/fMultNorm/fPow2Div2 block-floating-point domain and each
// coefficient is fPow(EULER_M, EULER_E, ...) then scaleValueSaturate'd. No
// floating point, no transcendental — fPow is the ROM-table log2/antilog kernel.
// aacfdk-fenced.

package nativeaac

// TNS encoder window constants (aacenc_tns.cpp:1098-1106).
const (
	tnsTimeresScale = 1 // TNS_TIMERES_SCALE (aacenc_tns.cpp:129)

	gaussPIE            = 2 // PI_E
	gaussEulerE         = 2 // EULER_E
	gaussCoeffLoopScale = 4 // COEFF_LOOP_SCALE
)

// gaussPIM is PI_M == FL2FXCONST_DBL(3.1416f / (1<<PI_E)) (aacenc_tns.cpp:1099).
var gaussPIM = fl2fxconstDBL(float64(float32(3.1416)) / float64(int32(1)<<gaussPIE))

// gaussEulerM is EULER_M == FL2FXCONST_DBL(2.7183 / (1<<EULER_E))
// (aacenc_tns.cpp:1102).
var gaussEulerM = fl2fxconstDBL(2.7183 / float64(int32(1)<<gaussEulerE))

// calcGaussWindow is the 1:1 port of FDKaacEnc_CalcGaussWindow
// (aacenc_tns.cpp:1094-1139). It fills win[0:winSize] with the Gaussian ACF
// window for the given samplingRate / transformResolution (granule length) and
// requested timeResolution (mantissa timeResolution, exponent timeResolutionE).
//
//	gaussExp = PI * samplingRate * 0.001f * timeResolution / transformResolution;
//	gaussExp = -0.5f * gaussExp * gaussExp;
//	win[i]   = (float)exp( gaussExp * (i+0.5) * (i+0.5) );
func calcGaussWindow(win []int32, winSize, samplingRate, transformResolution int,
	timeResolution int32, timeResolutionE int) {
	// gaussExp_m = fMultNorm(timeResolution,
	//                fMult(PI_M, fDivNorm(samplingRate,
	//                       (LONG)(transformResolution * 1000.f), &e1)), &e2);
	div, e1 := fDivNorm(int32(samplingRate), int32(float32(transformResolution)*1000.0))
	// fMult == fixmul_DD == arm8 smull>>31 on this target (see fMultDD note);
	// fixmulDDarm8 is that exact kernel.
	gaussExpM, e2 := fMultNorm(timeResolution, fixmulDDarm8(gaussPIM, div))

	// gaussExp_m = -fPow2Div2(gaussExp_m);
	gaussExpM = -fPow2Div2(gaussExpM)
	// gaussExp_e = 2 * (e1 + e2 + timeResolution_e + PI_E);
	gaussExpE := 2 * (int(e1) + int(e2) + timeResolutionE + gaussPIE)

	// FDK_ASSERT(winSize < (1 << COEFF_LOOP_SCALE));

	// coeffStep = FL2FXCONST_DBL(1.f / (1<<COEFF_LOOP_SCALE))
	// coeffHalf = FL2FXCONST_DBL(.5f / (1<<COEFF_LOOP_SCALE))
	coeffStep := fl2fxconstDBL(1.0 / float64(int32(1)<<gaussCoeffLoopScale))
	coeffHalf := fl2fxconstDBL(0.5 / float64(int32(1)<<gaussCoeffLoopScale))

	for i := 0; i < winSize; i++ {
		// win[i] = fPow(EULER_M, EULER_E,
		//            fMult(gaussExp_m, fPow2((i*coeffStep + coeffHalf))),
		//            gaussExp_e + 2*COEFF_LOOP_SCALE, &e1);
		arg := fixmulDDarm8(gaussExpM, fPow2(int32(i)*coeffStep+coeffHalf))
		w, we := fPow(gaussEulerM, gaussEulerE, arg, int32(gaussExpE+2*gaussCoeffLoopScale))

		// win[i] = scaleValueSaturate(win[i], e1);
		win[i] = scaleValueSaturate(w, we)
	}
}
