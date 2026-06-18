// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// Block switching: the AAC encoder's long-vs-short window DECISION kernel, a
// 1:1 fixed-point port of libAACenc/src/block_switch.cpp. It runs an IIR
// high-pass over the time signal, accumulates per-window energies, detects
// transients ("attacks"), and walks a window-sequence state machine to choose
// the next blockType (window sequence) and window shape. State lives in
// BlockSwitchingControl (block_switch.h:123); the energies and IIR delay-line
// carry from frame to frame.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32
// FIXP_DBL in Q-format. There is NO floating point in this stage, so it is an
// EXACT-INTEGER port: the IIR filter, the energy accumulation (fPow2Div2 +
// arithmetic shifts), and the attack comparisons are all int64-product/shift
// integer kernels, bit-identical regardless of build tag or vectorization. No
// aac_strict gate applies. Block exponents / scalings (the
// BLOCK_SWITCH_ENERGY_SHIFT and the tempUnfiltered <<15 prescale) are carried
// bit-for-bit.
//
// Build config note: on the build platform (aarch64), FDK_archdef.h:118
// promotes __aarch64__ to __arm__ (and FDK_archdef.h:166 sets __ARM_ARCH_8__),
// so the `defined(__arm__) && defined(__ARM_ARCH_8__)` branch (FDK_archdef.h:180)
// is taken: SINETABLE_16BIT IS defined → hiPassCoeff is FIXP_SGL (the
// `#else`/SINETABLE_16BIT branch, block_switch.cpp:149-150), and the arm
// fix-mul/fix-madd overrides (arm/fixmul_arm.h, arm/fixmadd_arm.h) apply. This
// matters for the rounding in two places vs the generic forms:
//   - fixmul_DD on __ARM_ARCH_8__ (fixmul_arm.h:155-191) is `smull; asr #31`
//     truncated to INT == (int64(a)*int64(b))>>31 keeping bit 31, which is NOT
//     the generic `(fixmuldiv2_DD(a,b))<<1` (that drops bit 31). The package
//     fMultDD (fixmul.go) is the generic form, so this stage uses the local
//     fixmulDDarm8 for the one fMult(DBL,DBL) site.
//   - the SGL coefficients route the IIR/threshold muls through the SD/DS
//     overloads (sign-extend the 16-bit coeff via <<16, then fixmuldiv2_DD),
//     fixmul_arm.h:155-162 / generic fixmul.h:183-194.
// fixmuldiv2_DD, fixmadddiv2_DD and fixpow2div2_D resolve to the same int64
// value as the generic form on this arch. SAMPLE_BITS == 16
// (machine_type.h:230), so INT_PCM is int16 and the time-sample prescale is
// `<< (DFRACT_BITS - SAMPLE_BITS - 1)` == `<< 15` (block_switch.cpp:389).

// --- psy_const.h block-type / window-shape / grouping constants -------------
//
// LongWindow/StartWindow/ShortWindow/StopWindow (= LONG/START/SHORT/STOP_WINDOW)
// and SineWindow/KbdWindow/LolWindow (= SINE/KBD/LOL_WINDOW) and TransFac are
// already declared in bitenc_types.go from the same psy_const.h enums. This
// stage additionally needs the two block-switch-internal codes and the table
// sizes below.

// lowOvWindow is the internal-only "low overlap" block type
// (psy_const.h:125, _LOWOV_WINDOW). It is never emitted outside block_switch;
// SyncBlockSwitching translates it to LONG_WINDOW + LOL_WINDOW shape.
const lowOvWindow = 4

// wrongWindow flags an illegal block-type combination (psy_const.h:126,
// WRONG_WINDOW); SyncBlockSwitching returns -1 if it ever appears.
const wrongWindow = 5

// nBlockTypes is the number of block types (psy_const.h:119, N_BLOCKTYPES).
const nBlockTypes = 6

// maxNoOfGroups is the maximum number of short-block groups
// (psy_const.h:141, MAX_NO_OF_GROUPS).
const maxNoOfGroups = 4

// --- block_switch.h defines -------------------------------------------------

// blockSwitchWindows is the number of windows used for energy calculation
// (block_switch.h:111, BLOCK_SWITCH_WINDOWS).
const blockSwitchWindows = 8

// blockSwitchingIIRLen is the length of the attack-detection high-pass IIR
// filter delay-line (block_switch.h:113, BLOCK_SWITCHING_IIR_LEN).
const blockSwitchingIIRLen = 2

// blockSwitchEnergyShift is the right-shift applied to window energies to avoid
// overflow (block_switch.h:115, BLOCK_SWITCH_ENERGY_SHIFT ==
// logDualis(BLOCK_SWITCH_WINDOW_LEN)).
const blockSwitchEnergyShift = 7

// --- block_switch.cpp constants (FL2FXCONST_DBL compile-time values) --------
//
// These int32 Q1.31 values are exactly what the C FL2FXCONST_DBL macro
// (common_fix.h:191) produces at compile time: round(val * 2^31 + 0.5) (or the
// negative-side variant). They are cross-checked against the genuine C macro by
// the parity oracle (blockswitch.cExportConsts).

// hiPassCoeff are the IIR high-pass coefficients (block_switch.cpp:149-150,
// FL2FXCONST_SGL(-0.5095) and FL2FXCONST_SGL(0.7548)). FIXP_SGL (int16) because
// SINETABLE_16BIT IS defined on this platform (see the build-config note above).
var hiPassCoeff = [blockSwitchingIIRLen]int16{-16695, 24733}

// accWindowNrgFac is the factor for accumulating filtered window energies
// (block_switch.cpp:152-153, FL2FXCONST_DBL(0.3f)). FIXP_DBL in both branches.
const accWindowNrgFac int32 = 644245120

// oneMinusAccWindowNrgFac (block_switch.cpp:154, FL2FXCONST_SGL(0.7f)). FIXP_SGL
// in the SINETABLE_16BIT branch.
const oneMinusAccWindowNrgFac int16 = 22938

// invAttackRatio is the inverted lower ratio limit for attacks
// (block_switch.cpp:156-157, FL2FXCONST_SGL(0.1f) == inverse of attackRatio 10).
// FIXP_SGL in the SINETABLE_16BIT branch.
const invAttackRatio int16 = 3277

// minAttackNrg is the minimum (scaled) energy for an attack
// (block_switch.cpp:143-145): FL2FXCONST_DBL(1e+6f*NORM_PCM_ENERGY) >>
// BLOCK_SWITCH_ENERGY_SHIFT == 2000000 >> 7 == 15625.
const minAttackNrg int32 = 15625

// maxvalDBL is the maximum FIXP_DBL value (common_fix.h:155, MAXVAL_DBL ==
// 0x7FFFFFFF), used to clamp the accumulated window energies.
const maxvalDBL int32 = 0x7FFFFFFF

// blockType2windowShape maps [allowShortFrames][lastWindowSequence] to a window
// shape (block_switch.cpp:122-124). Row 0 is LD (allowShortFrames==0), row 1 is
// LC (allowShortFrames==1).
var blockType2windowShape = [2][5]int{
	{SineWindow, KbdWindow, wrongWindow, SineWindow, KbdWindow}, // LD
	{KbdWindow, SineWindow, SineWindow, KbdWindow, wrongWindow}, // LC
}

// suggestedGroupingTable maps the attack window index to the suggested short
// grouping (block_switch.cpp:191-199, suggestedGroupingTable[TRANS_FAC][MAX_NO_OF_GROUPS]).
var suggestedGroupingTable = [TransFac][maxNoOfGroups]int{
	{1, 3, 3, 1}, // Attack in Window 0
	{1, 1, 3, 3}, // Attack in Window 1
	{2, 1, 3, 2}, // Attack in Window 2
	{3, 1, 3, 1}, // Attack in Window 3
	{3, 1, 1, 3}, // Attack in Window 4
	{3, 2, 1, 2}, // Attack in Window 5
	{3, 3, 1, 1}, // Attack in Window 6
	{3, 3, 1, 1}, // Attack in Window 7
}

// chgWndSq is the no-look-ahead block-type transition table indexed
// [attack][lastWindowSequence] (block_switch.cpp:204-210). Low-Delay path.
var chgWndSq = [2][nBlockTypes]int{
	// no attack
	{LongWindow, StopWindow, wrongWindow, LongWindow, StopWindow, wrongWindow},
	// attack
	{StartWindow, lowOvWindow, wrongWindow, StartWindow, lowOvWindow, wrongWindow},
}

// chgWndSqLkAhd is the look-ahead block-type transition table indexed
// [lastattack][attack][lastWindowSequence] (block_switch.cpp:215-227).
var chgWndSqLkAhd = [2][2][nBlockTypes]int{
	// lastattack == no attack
	{
		// attack == no attack
		{LongWindow, ShortWindow, StopWindow, LongWindow, wrongWindow, wrongWindow},
		// attack
		{StartWindow, ShortWindow, ShortWindow, StartWindow, wrongWindow, wrongWindow},
	},
	// lastattack == attack
	{
		// attack == no attack
		{LongWindow, ShortWindow, ShortWindow, LongWindow, wrongWindow, wrongWindow},
		// attack
		{StartWindow, ShortWindow, ShortWindow, StartWindow, wrongWindow, wrongWindow},
	},
}

// synchronizedBlockTypeTable folds two channels' suggested block types into one
// agreed type (block_switch.cpp:411-424, synchronizedBlockTypeTable[5][5]).
var synchronizedBlockTypeTable = [5][5]int{
	{LongWindow, StartWindow, ShortWindow, StopWindow, lowOvWindow},
	{StartWindow, StartWindow, ShortWindow, ShortWindow, lowOvWindow},
	{ShortWindow, ShortWindow, ShortWindow, ShortWindow, wrongWindow},
	{StopWindow, ShortWindow, ShortWindow, StopWindow, lowOvWindow},
	{lowOvWindow, lowOvWindow, wrongWindow, lowOvWindow, lowOvWindow},
}

// --- BlockSwitchingControl (block_switch.h:123, BLOCK_SWITCHING_CONTROL) -----

// BlockSwitchingControl holds the per-channel block-switching state: the chosen
// window sequence/shape, the attack-detection bookkeeping, the suggested short
// grouping, the per-window (unfiltered + filtered) energies for the last and
// current frame, the recursively accumulated filtered energy, and the IIR
// delay-line. It maps the C struct field-for-field. Index 0 is LAST_WINDOW, 1
// is THIS_WINDOW (block_switch.h:119-120) for the windowNrg / windowNrgF
// double-buffers.
type BlockSwitchingControl struct {
	LastWindowSequence  int
	WindowShape         int
	LastWindowShape     int
	NBlockSwitchWindows uint
	Attack              int
	Lastattack          int
	AttackIndex         int
	LastAttackIndex     int
	AllowShortFrames    int
	AllowLookAhead      int
	NoOfGroups          int
	GroupLen            [maxNoOfGroups]int
	MaxWindowNrg        int32

	// WindowNrg is the per-window unfiltered time-signal energy, [last|this].
	WindowNrg [2][blockSwitchWindows]int32
	// WindowNrgF is the per-window filtered time-signal energy, [last|this].
	WindowNrgF [2][blockSwitchWindows]int32
	// AccWindowNrg is the recursively accumulated filtered window energy.
	AccWindowNrg int32

	// IIRStates is the high-pass filter delay-line.
	IIRStates [blockSwitchingIIRLen]int32
}

// --- fixed-point primitives needed here (generic-C forms) -------------------

// fMultAdd computes 2*(x + 0.5*a*b) == 2*x + a*b for FIXP_DBL operands. C
// counterpart: fixmadd_DD (fixmadd.h:250) == fixmadddiv2_DD(x,a,b) << 1, and
// fixmadddiv2_DD (fixmadd.h:124) == x + fMultDiv2(a,b). On this platform no
// arch header overrides it, so the generic form applies. The intermediate
// `x + fMultDiv2(a,b)` is computed in int32 (wrapping) exactly as the C does
// before the << 1.
func fMultAdd(x, a, b int32) int32 {
	return (x + fMultDiv2(a, b)) << 1
}

// fPow2Div2 returns 0.5*a*a for a FIXP_DBL. C counterpart: fixpow2div2_D
// (fixmul.h:277, no arm override) == fixmuldiv2_DD(a, a).
func fPow2Div2(a int32) int32 {
	return fMultDiv2DD(a, a)
}

// fPow2 returns a*a for a FIXP_DBL. C counterpart: fPow2(LONG) == fixpow2_D
// (common_fix.h:242 / fixmul.h:282), defined as fixpow2div2_D(a) << 1 ==
// fMultDiv2DD(a, a) << 1. Used by FDKaacEnc_CalcGaussWindow to square (i+0.5).
func fPow2(a int32) int32 {
	return fMultDiv2DD(a, a) << 1
}

// fMultDiv2SD multiplies a FIXP_SGL by a FIXP_DBL fraction, scaled down by 2. C
// counterpart: fixmuldiv2_SD on __ARM_ARCH_8__ (fixmul_arm.h:157-162):
// fixmuldiv2_DD((INT)(a << 16), b). a is the int16 coefficient promoted to int
// and shifted left 16 (the SHORT-to-32-bit widen). This is the form taken by the
// `fMultDiv2(FIXP_SGL, FIXP_DBL)` calls in CalcWindowEnergy/BlockSwitching.
func fMultDiv2SD(a int16, b int32) int32 {
	return fMultDiv2DD(int32(a)<<16, b)
}

// fMultDSarm is the FIXP_DBL*FIXP_SGL full-scale multiply, C fixmul_DS. No arm
// override exists for fixmul_DS, so it is the generic form (fixmul.h:236-243):
// since FUNCTION_fixmuldiv2_SD is defined (arm), fixmuldiv2_DS forwards to
// fixmuldiv2_SD(b, a) == fixmuldiv2_DD((INT)(b<<16), a); fixmul_DS == that << 1.
// (Equivalent to the package fMultDS in fixmul.go; restated here as a local for
// the int16-coefficient call site.)
func fMultDSarm(a int32, b int16) int32 {
	return fMultDiv2DD(int32(b)<<16, a) << 1
}

// fixmulDDarm8 is the FIXP_DBL*FIXP_DBL full-scale multiply on __ARM_ARCH_8__ (C
// fixmul_DD, fixmul_arm.h:156-191): `smull x0,w1,w2; asr x0,x0,#31` then (INT)x0
// == the low 32 bits of (int64(a)*int64(b))>>31. This KEEPS bit 31, unlike the
// generic/package fMultDD which is (fixmuldiv2_DD(a,b))<<1 and drops it; the
// difference is the LSB. Used for the one fMult(FIXP_DBL,FIXP_DBL) site in the
// frame-border attack check (block_switch.cpp:318-320).
func fixmulDDarm8(a, b int32) int32 {
	return int32((int64(a) * int64(b)) >> 31)
}

// The larger of two FIXP_DBL values (C fixMax == fMax, common_fix.h:307) is the
// already-ported fMax (tns_scale.go). DFRACT_BITS == 32 is the already-ported
// package const dfractBits (aac_rom_stereo.go:20). fMultDiv2(FIXP_DBL,FIXP_DBL)
// == fixmuldiv2_DD is the already-ported fMultDiv2 (tns_apply.go); fixmadd_DD
// (fMultAdd below) and fixpow2div2_D (fPow2Div2 above) resolve to the same int64
// values on this arch.

// --- routines ---------------------------------------------------------------

// InitBlockSwitching zeroes the control and sets the start state for a channel.
// C counterpart: FDKaacEnc_InitBlockSwitching, block_switch.cpp:168-189.
// isLowDelay selects the LD configuration (4 windows, no short frames, no
// look-ahead); otherwise LC (8 windows, short frames + look-ahead allowed).
func InitBlockSwitching(b *BlockSwitchingControl, isLowDelay int) {
	*b = BlockSwitchingControl{}

	if isLowDelay != 0 {
		b.NBlockSwitchWindows = 4
		b.AllowShortFrames = 0
		b.AllowLookAhead = 0
	} else {
		b.NBlockSwitchWindows = 8
		b.AllowShortFrames = 1
		b.AllowLookAhead = 1
	}

	b.NoOfGroups = maxNoOfGroups

	// Initialize startvalue for blocktype.
	b.LastWindowSequence = LongWindow
	b.WindowShape = blockType2windowShape[b.AllowShortFrames][b.LastWindowSequence]
}

// BlockSwitching runs the per-frame block-switch decision for one channel. C
// counterpart: FDKaacEnc_BlockSwitching, block_switch.cpp:229-346. granuleLength
// is the frame length in time samples (nTimeSamples == 1024 for AAC-LC); isLFE
// forces a LONG/SINE window; pTimeSignal is the granuleLength-long INT_PCM
// (int16, SAMPLE_BITS==16) time-domain frame. Returns 0 (the C return value).
func BlockSwitching(b *BlockSwitchingControl, granuleLength int, isLFE int, pTimeSignal []int16) int {
	nBlockSwitchWindows := b.NBlockSwitchWindows

	// for LFE : only LONG window allowed
	if isLFE != 0 {
		// case LFE: only long blocks, always sine windows.
		b.LastWindowSequence = LongWindow
		b.WindowShape = SineWindow
		b.NoOfGroups = 1
		b.GroupLen[0] = 1
		return 0
	}

	// Save current attack index as last attack index.
	b.Lastattack = b.Attack
	b.LastAttackIndex = b.AttackIndex

	// Save current window energy as last window energy.
	b.WindowNrg[0] = b.WindowNrg[1]
	b.WindowNrgF[0] = b.WindowNrgF[1]

	if b.AllowShortFrames != 0 {
		// Calculate suggested grouping info for the last frame.

		// Reset grouping info.
		b.GroupLen = [maxNoOfGroups]int{}

		// Set grouping info.
		b.NoOfGroups = maxNoOfGroups
		b.GroupLen = suggestedGroupingTable[b.LastAttackIndex]

		if b.Attack == 1 { // TRUE
			b.MaxWindowNrg = getWindowEnergy(b.WindowNrg[0][:], b.LastAttackIndex)
		} else {
			b.MaxWindowNrg = 0
		}
	}

	// Calculate unfiltered and filtered energies in subwindows and combine to
	// segments.
	windowLen := granuleLength >> 3
	if nBlockSwitchWindows == 4 {
		windowLen = granuleLength >> 2
	}
	calcWindowEnergy(b, windowLen, pTimeSignal)

	// now calculate if there is an attack

	// reset attack
	b.Attack = 0 // FALSE

	// look for attack
	var enMax int32 = 0
	enM1 := b.WindowNrgF[0][nBlockSwitchWindows-1]

	for i := uint(0); i < nBlockSwitchWindows; i++ {
		// fMultDiv2(FIXP_SGL oneMinus, FIXP_DBL acc) == fixmuldiv2_SD.
		tmp := fMultDiv2SD(oneMinusAccWindowNrgFac, b.AccWindowNrg)
		// fMultAdd(FIXP_DBL, FIXP_DBL, FIXP_DBL) == fixmadd_DD.
		b.AccWindowNrg = fMultAdd(tmp, accWindowNrgFac, enM1)

		// fMult(FIXP_DBL windowNrgF, FIXP_SGL invAttackRatio) == fixmul_DS.
		if fMultDSarm(b.WindowNrgF[1][i], invAttackRatio) > b.AccWindowNrg {
			b.Attack = 1 // TRUE
			b.AttackIndex = int(i)
		}
		enM1 = b.WindowNrgF[1][i]
		enMax = fMax(enMax, enM1)
	}

	if enMax < minAttackNrg {
		b.Attack = 0 // FALSE
	}

	// Check if attack spreads over frame border.
	if b.Attack == 0 && b.Lastattack == 1 {
		// if attack is in last window repeat SHORT_WINDOW
		// fMult(FIXP_DBL, FIXP_DBL) == fixmul_DD (arm __ARM_ARCH_8__ variant).
		if (b.WindowNrgF[0][nBlockSwitchWindows-1]>>4) >
			fixmulDDarm8(int32(10<<(dfractBits-1-4)), b.WindowNrgF[1][1]) &&
			b.LastAttackIndex == int(nBlockSwitchWindows)-1 {
			b.Attack = 1 // TRUE
			b.AttackIndex = 0
		}
	}

	if b.AllowLookAhead != 0 {
		b.LastWindowSequence =
			chgWndSqLkAhd[b.Lastattack][b.Attack][b.LastWindowSequence]
	} else {
		// Low Delay
		b.LastWindowSequence = chgWndSq[b.Attack][b.LastWindowSequence]
	}

	// update window shape
	b.WindowShape = blockType2windowShape[b.AllowShortFrames][b.LastWindowSequence]

	return 0
}

// getWindowEnergy returns the energy for a block-switching analysis window. C
// counterpart: FDKaacEnc_GetWindowEnergy, block_switch.cpp:348-357 — a plain
// table lookup used to compare left/right channel max energy.
func getWindowEnergy(in []int32, blSwWndIdx int) int32 {
	return in[blSwWndIdx]
}

// calcWindowEnergy computes the per-window unfiltered + high-pass-filtered
// energies for the current frame and advances the IIR delay-line. C
// counterpart: FDKaacEnc_CalcWindowEnergy, block_switch.cpp:359-409. The
// hiPassCoeff branch taken is the FIXP_DBL one (SINETABLE_16BIT undefined), and
// the time-sample prescale is `<< (DFRACT_BITS - SAMPLE_BITS - 1)` == `<< 15`
// (SAMPLE_BITS==16). The energy accumulators are unsigned 32-bit (ULONG) and
// the final value is clamped to MAXVAL_DBL via fMin on UINTs.
func calcWindowEnergy(b *BlockSwitchingControl, windowLen int, pTimeSignal []int16) {
	hiPassCoeff0 := hiPassCoeff[0]
	hiPassCoeff1 := hiPassCoeff[1]

	tempIirState0 := b.IIRStates[0]
	tempIirState1 := b.IIRStates[1]

	pos := 0

	// sum up scalarproduct of timesignal as windowed Energies
	for w := uint(0); w < b.NBlockSwitchWindows; w++ {
		var tempWindowNrg uint32 = 0
		var tempWindowNrgF uint32 = 0

		// windowNrg = sum(timesample^2)
		for i := 0; i < windowLen; i++ {
			// tempUnfiltered is scaled with 1 to prevent overflows during
			// calculation of tempFiltred. SAMPLE_BITS(16) != DFRACT_BITS(32) →
			// the `#else` branch: (FIXP_DBL)*pTimeSignal << (32 - 16 - 1).
			tempUnfiltered := int32(pTimeSignal[pos]) << (dfractBits - sampleBits - 1)
			pos++

			// fMultDiv2(FIXP_SGL hiPassCoeff, FIXP_DBL) == fixmuldiv2_SD.
			t1 := fMultDiv2SD(hiPassCoeff1, tempUnfiltered-tempIirState0)
			t2 := fMultDiv2SD(hiPassCoeff0, tempIirState1)
			tempIirState0 = tempUnfiltered
			tempIirState1 = (t1 - t2) << 1

			// (LONG)fPow2Div2(state) >> (BLOCK_SWITCH_ENERGY_SHIFT - 1 - 2)
			// accumulated into an unsigned 32-bit total. The C `>>` is on the
			// signed LONG result of fPow2Div2 (arithmetic), then summed into a
			// ULONG (wrapping unsigned add).
			tempWindowNrg += uint32(fPow2Div2(tempIirState0) >> (blockSwitchEnergyShift - 1 - 2))
			tempWindowNrgF += uint32(fPow2Div2(tempIirState1) >> (blockSwitchEnergyShift - 1 - 2))
		}
		// (LONG)fMin(temp_windowNrg, (UINT)MAXVAL_DBL): unsigned min then store
		// as signed.
		b.WindowNrg[1][w] = int32(uMin(tempWindowNrg, uint32(maxvalDBL)))
		b.WindowNrgF[1][w] = int32(uMin(tempWindowNrgF, uint32(maxvalDBL)))
	}
	b.IIRStates[0] = tempIirState0
	b.IIRStates[1] = tempIirState1
}

// sampleBits is the time-signal sample width (machine_type.h:230,
// SAMPLE_BITS == 16; INT_PCM is int16).
const sampleBits = 16

// uMin returns the smaller of two unsigned values (the (UINT) overload of fMin,
// common_fix.h, used to clamp the unsigned energy accumulators).
func uMin(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

// SyncBlockSwitching synchronizes the block types and grouping of a stereo pair
// (or normalizes a mono channel). C counterpart: FDKaacEnc_SyncBlockSwitching,
// block_switch.cpp:426-582. Returns -1 on an illegal type mix (WRONG_WINDOW),
// else 0. For nChannels==1, pass right==nil.
func SyncBlockSwitching(left, right *BlockSwitchingControl, nChannels int, commonWindow int) int {
	patchType := LongWindow

	if nChannels == 2 && commonWindow == 1 {
		// get suggested Block Types and synchronize
		patchType = synchronizedBlockTypeTable[patchType][left.LastWindowSequence]
		patchType = synchronizedBlockTypeTable[patchType][right.LastWindowSequence]

		// sanity check (no change from low overlap window to short window and
		// vice versa).
		if patchType == wrongWindow {
			return -1 // mixed up AAC-LC and AAC-LD
		}

		// Set synchronized Blocktype.
		left.LastWindowSequence = patchType
		right.LastWindowSequence = patchType

		// update window shape
		left.WindowShape = blockType2windowShape[left.AllowShortFrames][left.LastWindowSequence]
		right.WindowShape = blockType2windowShape[left.AllowShortFrames][right.LastWindowSequence]
	}

	if left.AllowShortFrames != 0 {
		if nChannels == 2 {
			if commonWindow == 1 {
				// Synchronize grouping info.
				windowSequenceLeftOld := left.LastWindowSequence
				windowSequenceRightOld := right.LastWindowSequence

				// Long Blocks
				if patchType != ShortWindow {
					// Set grouping info.
					left.NoOfGroups = 1
					right.NoOfGroups = 1
					left.GroupLen[0] = 1
					right.GroupLen[0] = 1

					for i := 1; i < maxNoOfGroups; i++ {
						left.GroupLen[i] = 0
						right.GroupLen[i] = 0
					}
				} else { // Short Blocks
					// in case all two channels were detected as short-blocks
					// before syncing, use the grouping of channel with higher
					// maxWindowNrg.
					if windowSequenceLeftOld == ShortWindow && windowSequenceRightOld == ShortWindow {
						if left.MaxWindowNrg > right.MaxWindowNrg {
							// Left Channel wins
							right.NoOfGroups = left.NoOfGroups
							for i := 0; i < maxNoOfGroups; i++ {
								right.GroupLen[i] = left.GroupLen[i]
							}
						} else {
							// Right Channel wins
							left.NoOfGroups = right.NoOfGroups
							for i := 0; i < maxNoOfGroups; i++ {
								left.GroupLen[i] = right.GroupLen[i]
							}
						}
					} else if windowSequenceLeftOld == ShortWindow && windowSequenceRightOld != ShortWindow {
						// else use grouping of short-block channel
						right.NoOfGroups = left.NoOfGroups
						for i := 0; i < maxNoOfGroups; i++ {
							right.GroupLen[i] = left.GroupLen[i]
						}
					} else if windowSequenceRightOld == ShortWindow && windowSequenceLeftOld != ShortWindow {
						left.NoOfGroups = right.NoOfGroups
						for i := 0; i < maxNoOfGroups; i++ {
							left.GroupLen[i] = right.GroupLen[i]
						}
					} else {
						// syncing a start and stop window ...
						left.NoOfGroups = 2
						right.NoOfGroups = 2
						left.GroupLen[0] = 4
						right.GroupLen[0] = 4
						left.GroupLen[1] = 4
						right.GroupLen[1] = 4
					}
				} // Short Blocks
			} else {
				// stereo, no common window
				if left.LastWindowSequence != ShortWindow {
					left.NoOfGroups = 1
					left.GroupLen[0] = 1
					for i := 1; i < maxNoOfGroups; i++ {
						left.GroupLen[i] = 0
					}
				}
				if right.LastWindowSequence != ShortWindow {
					right.NoOfGroups = 1
					right.GroupLen[0] = 1
					for i := 1; i < maxNoOfGroups; i++ {
						right.GroupLen[i] = 0
					}
				}
			} // common window
		} else {
			// Mono
			if left.LastWindowSequence != ShortWindow {
				left.NoOfGroups = 1
				left.GroupLen[0] = 1
				for i := 1; i < maxNoOfGroups; i++ {
					left.GroupLen[i] = 0
				}
			}
		}
	} // allowShortFrames

	// Translate LOWOV_WINDOW block type to a meaningful window shape.
	if left.AllowShortFrames == 0 {
		if left.LastWindowSequence != LongWindow && left.LastWindowSequence != StopWindow {
			left.LastWindowSequence = LongWindow
			left.WindowShape = LolWindow
		}
	}
	if nChannels == 2 {
		if right.AllowShortFrames == 0 {
			if right.LastWindowSequence != LongWindow && right.LastWindowSequence != StopWindow {
				right.LastWindowSequence = LongWindow
				right.WindowShape = LolWindow
			}
		}
	}

	return 0
}
