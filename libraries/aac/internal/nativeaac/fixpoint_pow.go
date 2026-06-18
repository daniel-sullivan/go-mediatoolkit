// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Fixed-point power / division / arctangent helpers ported 1:1 from the
// vendored FDK-AAC reference (libFDK/src/fixpoint_math.cpp +
// libFDK/src/FDK_trigFcts.cpp). FDKaacEnc_InitPsyConfiguration's barc/spreading/
// minSnr sub-inits drive these: the unsigned fDivNorm, f2Pow / fPow antilog
// pair, and fixp_atan. Every value is an int32 FIXP_DBL Q-format quantity; the
// whole computation is pure integer fixed-point arithmetic (int64 products,
// arithmetic shifts, leading-bit counts, the schur division and the table-free
// Taylor/Chebyshev polynomials) — no float, no transcendental — so it is
// bit-identical regardless of vectorization and carries only the aacfdk fence
// (no aac_strict FP split).
//
// On the aarch64 build target (__arm__ && __ARM_ARCH_8__, FDK_archdef.h:182)
// POW2COEFF_16BIT is defined, so pow2Coeff is the FIXP_SGL (Q1.15 int16) variant
// and SINETABLE_16BIT is defined, so POW2_PRECISION == 5 (fixpoint_math.cpp:
// 124-129). fMult(LONG,LONG) == fixmul_DD == fixmulDDarm8 (keeps bit 31).

// pow2Precision is POW2_PRECISION (fixpoint_math.cpp:126-129): == 5 on this
// target because SINETABLE_16BIT is defined.
const pow2Precision = 5

// pow2Coeff is the 1:1 transcription of pow2Coeff[MAX_POW2_PRECISION]
// (fixpoint_math.cpp:155-164): the Taylor coefficients ln(2)^i / i! of 2^x. On
// the aarch64 target POW2COEFF_16BIT is defined, so this is the FIXP_SGL (Q1.15
// int16) variant. The first coefficient (a_0 == 1.0) is omitted by the series.
//
//	static const FIXP_SGL pow2Coeff[MAX_POW2_PRECISION] = {
//	    FL2FXCONST_SGL(0.693147180559945309417232121458177),   // ln(2)^1 /1!
//	    FL2FXCONST_SGL(0.240226506959100712333551263163332),   // ln(2)^2 /2!
//	    FL2FXCONST_SGL(0.0555041086648215799531422637686218),  // ln(2)^3 /3!
//	    FL2FXCONST_SGL(0.00961812910762847716197907157365887), // ln(2)^4 /4!
//	    FL2FXCONST_SGL(0.00133335581464284434234122219879962), // ln(2)^5 /5!
//	    FL2FXCONST_SGL(1.54035303933816099544370973327423e-4), // ln(2)^6 /6!
//	    FL2FXCONST_SGL(1.52527338040598402800254390120096e-5), // ln(2)^7 /7!
//	    FL2FXCONST_SGL(1.32154867901443094884037582282884e-6)  // ln(2)^8 /8!
//	};
var pow2Coeff = [8]int16{
	fl2fxconstSGL(0.693147180559945309417232121458177),
	fl2fxconstSGL(0.240226506959100712333551263163332),
	fl2fxconstSGL(0.0555041086648215799531422637686218),
	fl2fxconstSGL(0.00961812910762847716197907157365887),
	fl2fxconstSGL(0.00133335581464284434234122219879962),
	fl2fxconstSGL(1.54035303933816099544370973327423e-4),
	fl2fxconstSGL(1.52527338040598402800254390120096e-5),
	fl2fxconstSGL(1.32154867901443094884037582282884e-6),
}

// fDivNorm is the 1:1 port of fDivNorm(FIXP_DBL L_num, FIXP_DBL L_denum,
// INT *result_e) (fixpoint_math.cpp:453-477): the unsigned normalised division
// (num>=0, denum>0) returning the mantissa and (via the second return) the
// result exponent. CountLeadingBits == fNorm; FRACT_BITS == 16.
//
//	norm_num = CountLeadingBits(L_num);
//	L_num = L_num << norm_num; L_num = L_num >> 1;
//	*result_e = -norm_num + 1;
//	norm_den = CountLeadingBits(L_denum);
//	L_denum = L_denum << norm_den;
//	*result_e -= -norm_den;
//	div = schur_div(L_num, L_denum, FRACT_BITS);
func fDivNorm(lNum, lDenum int32) (div, resultE int32) {
	if lNum == 0 {
		return 0, 0
	}

	normNum := fNorm(lNum)
	lNum = lNum << uint(normNum)
	lNum = lNum >> 1
	resultE = -normNum + 1

	normDen := fNorm(lDenum)
	lDenum = lDenum << uint(normDen)
	resultE -= -normDen

	div = schurDiv(lNum, lDenum, fractBits) // FRACT_BITS == 16
	return div, resultE
}

// f2PowWithExp is the 1:1 port of f2Pow(const FIXP_DBL exp_m, const INT exp_e,
// INT *result_e) (fixpoint_math.cpp:593-636): 2^(exp_m * 2^exp_e), returning the
// mantissa and (via the second return) the result exponent. On this target
// pow2Coeff is FIXP_SGL, so fMultAddDiv2(result_m, pow2Coeff[i], p) takes the
// (FIXP_DBL, FIXP_SGL, FIXP_DBL) overload == fixmadddiv2_SD; the arm header
// defines FUNCTION_fixmadddiv2_DS (not _SD), so the generic fixmadddiv2_SD
// (fixmadd.h:130) routes to fixmadddiv2_DS(x, b, a) == x + fMultDiv2DS(p,
// pow2Coeff[i]). p = fMult(p, frac_part) uses fixmul_DD == fixmulDDarm8.
func f2PowWithExp(expM, expE int32) (resultM, resultE int32) {
	var fracPart, intPart int32

	if expE > 0 {
		expBits := int32(dfractBits) - 1 - expE
		intPart = expM >> uint(expBits)
		fracPart = expM - (intPart << uint(expBits))
		fracPart = fracPart << uint(expE)
	} else {
		intPart = 0
		fracPart = expM >> uint(-expE)
	}

	// Best accuracy is around 0, so try to get there with the fractional part.
	if fracPart > fl2fxconstDBL(0.5) {
		intPart = intPart + 1
		fracPart = fracPart + fl2fxconstDBL(-1.0)
	}
	if fracPart < fl2fxconstDBL(-0.5) {
		intPart = intPart - 1
		fracPart = -(fl2fxconstDBL(-1.0) - fracPart)
	}

	// "+ 1" compensates fMultAddDiv2() of the polynomial evaluation below.
	resultE = intPart + 1

	// Evaluate taylor polynomial which approximates 2^x.
	p := fracPart
	// First taylor series coefficient a_0 = 1.0, scaled by 0.5 due to fMultDiv2().
	resultM = fl2fxconstDBL(1.0 / 2.0)
	for i := 0; i < pow2Precision; i++ {
		resultM = resultM + fMultDiv2DS(p, pow2Coeff[i])
		p = fixmulDDarm8(p, fracPart)
	}
	return resultM, resultE
}

// f2Pow is the 1:1 port of f2Pow(const FIXP_DBL exp_m, const INT exp_e)
// (fixpoint_math.cpp:638-646): the exponent-0 form, saturating the result
// exponent to [-(DFRACT_BITS-1), DFRACT_BITS-1] before scaling.
func f2Pow(expM, expE int32) int32 {
	resultM, resultE := f2PowWithExp(expM, expE)
	resultE = int32(fixMin(int(dfractBits)-1, fixMax(-(int(dfractBits)-1), int(resultE))))
	return scaleValue(resultM, resultE)
}

// fPow is the 1:1 port of fPow(FIXP_DBL base_m, INT base_e, FIXP_DBL exp_m,
// INT exp_e, INT *result_e) (fixpoint_math.cpp:648-679): base^exp via
// log2/antilog. base_m <= 0 short-circuits to (0, 0). fLog2 is the
// exponent-carrying form (fLog2WithExp); CountLeadingBits == fNorm; fAbs ==
// fAbsDBL; fMult == fixmul_DD == fixmulDDarm8.
func fPow(baseM, baseE, expM, expE int32) (result, resultE int32) {
	if baseM <= 0 {
		return 0, 0
	}

	// Calc log2 of base.
	baseLg2, baseLg2E := fLog2WithExp(baseM, baseE)

	// Prepare exp.
	leadingBits := fNorm(fAbsDBL(expM))
	expM = expM << uint(leadingBits)
	expE -= leadingBits

	// Calc base pow exp.
	ansLg2 := fixmulDDarm8(baseLg2, expM)
	ansLg2E := expE + baseLg2E

	// Calc antilog.
	result, resultE = f2PowWithExp(ansLg2, ansLg2E)
	return result, resultE
}

// Arctangent input/output Q-format constants (FDK_trigFcts.h:122-126).
//
//	#define Q_ATANINP (25)  // Input in q25, Output in q30
//	#define Q_ATANOUT (30)
//	#define ATI_SF ((DFRACT_BITS - 1) - Q_ATANINP) // 6
const (
	qAtanInp = 25
	qAtanOut = 30
	atiSF    = (dfractBits - 1) - qAtanInp // 6
)

// fixpAtan is the 1:1 port of fixp_atan(FIXP_DBL x) (FDK_trigFcts.cpp:238-303):
// fixed-point arctangent, input q25, output q30. SNR 56 dB. The |x| < 1/64
// branch is a 7th-order Chebyshev polynomial; the 1/64 <= |x| < 1.28/64 branch a
// linear+quadratic correction around pi/4; the |x| >= 1.28/64 branch the
// pi/2 - atan(1/x) identity via fDivNorm. fPow2(x) == fixpow2_D(x) ==
// fPow2Div2(x)<<1; fMult == fixmul_DD == fixmulDDarm8; fMultAddDiv2 over FIXP_DBL
// args == the (DBL,DBL,DBL) overload.
func fixpAtan(x int32) int32 {
	var sign int
	var result, temp int32

	// P281 = 0.281 in q18; ONEP571 = 1.571 in q30.
	const p281 = int32(0x00013000)
	const onep571 = int32(0x6487ef00)

	if x < 0 {
		sign = 1
		x = -x
	} else {
		sign = 0
	}

	// FDK_ASSERT(FL2FXCONST_DBL(1.0/64.0) == Q(Q_ATANINP)) — input range gate.
	if x < fl2fxconstDBL(1.0/64.0) {
		// 7th-order Chebyshev polynomial approximation of atan(x). The three
		// coefficients carry the C `f` suffix (FL2FXCONST_DBL(0.1449...f)), so
		// each literal is narrowed through float32 BEFORE the Q1.31 conversion —
		// reproduced as fl2fxconstDBL(float64(float32(c))). The -0.03825...
		// addend has NO `f` suffix in the C, so it stays double. Getting this
		// float-vs-double narrowing right is load-bearing for bit-exactness (the
		// float coeffs differ from their double forms in the low bits).
		x <<= atiSF
		x2 := fPow2Div2(x) << 1 // fPow2(x) == fixpow2_D
		temp = fMultAddDiv2(fl2fxconstDBL(float64(float32(0.1449824901444650)))>>1, x2,
			fl2fxconstDBL(-0.0382544649702990))
		temp = fMultAddDiv2(fl2fxconstDBL(float64(float32(-0.3205332923816640)))>>2, x2, temp)
		temp = fMultAddDiv2(fl2fxconstDBL(float64(float32(0.9991334482227801)))>>3, x2, temp)
		result = fixmulDDarm8(x, temp<<2)
	} else if x < fl2fxconstDBL(1.28/64.0) {
		// pi/4 in q30.
		piBy4 := fl2fxconstDBL(3.1415926/4.0) >> 1

		deltaFix := (x - fl2fxconstDBL(1.0/64.0)) << 5 // q30
		result = piBy4 + (deltaFix >> 1) - fPow2Div2(deltaFix)
	} else {
		// Other approximation for |x| > 1.28.
		temp = fPow2Div2(x)            // q25 * q25 - (DFRACT_BITS-1) - 1 = q18
		temp = temp + p281             // q18 + q18 = q18
		div, resE := fDivNorm(x, temp) // fDivNorm(x, temp, &res_e)
		result = scaleValue(div, (qAtanOut-qAtanInp+18-dfractBits+1)+resE)
		result = onep571 - result // q30 + q30 = q30
	}
	if sign != 0 {
		result = -result
	}
	return result
}
