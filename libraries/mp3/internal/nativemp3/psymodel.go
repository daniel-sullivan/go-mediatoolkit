// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// This file begins the 1:1 Go translation of LAME 3.100's psychoacoustic
// model — the encoder's perceptual analysis stage. The C reference lives in
// the vendored tree at libraries/mp3/liblame/libmp3lame/psymodel.c (the
// long/short windowing driver, threshold/masking calculation, attack
// detection and block-type decision, and the per-frame "bit allocation
// pre-step" that hands perceptual entropy and per-scalefactor-band maskings
// to the quantizer) and psymodel.h. The FFT it drives is in
// libraries/mp3/liblame/libmp3lame/fft.c (translated in fft.go).
//
// # Relationship to the rest of nativemp3
//
// The other files in this package translate the minimp3 *decoder*. The
// psychoacoustic model is an independent *encoder* component; it shares only
// this package's namespace and its mp3_strict FP-parity discipline. Its types
// (PsyConst, PsyStateVar, ...) are a 1:1 mapping of LAME's internal structs
// and do not interact with the decoder's Decoder / Scratch.
//
// # Sample layout
//
// All psymodel buffers are split (non-interleaved): per-channel arrays of
// float32 PCM (LAME's `sample_t`, a float32). The model is fed a 1024-sample
// window per channel centred over the 576-sample granule, and returns the
// perceptual data for the *previous* granule (one-granule delay), exactly as
// the C does.
//
// # Floating-point type
//
// LAME's `FLOAT` typedef is `float` (32-bit); see
// liblame/libmp3lame/machine.h. Every `FLOAT` below is therefore float32.
// The per-frame analysis arithmetic is single-precision and routed through
// the //go:noinline helpers in psymodel_fp_strict.go so the mp3_strict build
// cannot fuse a*b+c into an FMA (matching the cgo oracle compiled with
// -ffp-contract=off). The one-time psymodel_init constant setup uses LAME's
// double-precision pow/exp/atan/log exactly as the C does (those are computed
// once and are not on the FMA-sensitive per-frame path).
//
// Every ported function carries a doc comment naming its psymodel.c /
// psymodel.h / fft.c C counterpart as file:line so a future reader can diff
// against the vendored C.

import "math"

// Encoder block-type / structural constants from encoder.h.
//
//   - SBLIMIT is the number of subbands (encoder.h:93, #define SBLIMIT 32).
//   - PsyCBANDS is the number of partition ("critical") bands (encoder.h:96,
//     #define CBANDS 64). Named PsyCBANDS to avoid colliding with decoder
//     identifiers in this package.
//   - SBPSYl / SBPSYs are the masking scalefactor-band counts (encoder.h:99,
//     SBPSY_l 21 / SBPSY_s 12).
//   - SBMAXl / SBMAXs are the encoded scalefactor-band counts (encoder.h:103,
//     SBMAX_l 22 / SBMAX_s 13).
//   - PSFB21 / PSFB12 are the partitioned sfb21/sfb12 counts (encoder.h:105).
const (
	SBLIMIT   = 32
	PsyCBANDS = 64
	SBPSYl    = 21
	SBPSYs    = 12
	SBMAXl    = 22
	SBMAXs    = 13
	PSFB21    = 6
	PSFB12    = 6
)

// FFT size constants from encoder.h:111.
//
//   - BLKSIZE is the long-block FFT size (1024); HBLKSIZE its half+1.
//   - BLKSIZEs is the short-block FFT size (256); HBLKSIZEs its half+1.
const (
	BLKSIZE   = 1024
	HBLKSIZE  = BLKSIZE/2 + 1
	BLKSIZEs  = 256
	HBLKSIZEs = BLKSIZEs/2 + 1
)

// Block types (encoder.h:118).
const (
	NormType  = 0
	StartType = 1
	ShortType = 2
	StopType  = 3
)

// MPEG channel-coding mode extension values (encoder.h:139, enum
// MPEGChannelMode). Used by the model only indirectly; included for
// completeness of the 1:1 port.
const (
	MpgMdLRLR = 0
	MpgMdLRI  = 1
	MpgMdMSLR = 2
	MpgMdMSI  = 3
)

// psymodel.h tuning constants (psymodel.h:42).
//
//   - Rpelev / Rpelev2 / RpelevS / Rpelev2S are pre-echo limit multipliers.
//   - DELBARK is the partition-band width in barks (psymodel.h:48, .34).
//   - VOScale scales the loudness approximation (psymodel.h:52).
//   - TemporalmaskSustainSec is the temporal-masking decay time (psymodel.h:54).
//   - NsPreechoAtt0..2 are pre-echo attenuation factors (psymodel.h:56).
//   - NsMsfix / NsAttackThre / NsAttackThreS are M/S and attack thresholds
//     (psymodel.h:60).
const (
	Rpelev   = 2
	Rpelev2  = 16
	RpelevS  = 2
	Rpelev2S = 16

	DELBARK = 0.34

	VOScale = (1.0 / (14752.0 * 14752.0)) / (BLKSIZE / 2)

	TemporalmaskSustainSec = 0.01

	NsPreechoAtt0 = 0.8
	NsPreechoAtt1 = 0.6
	NsPreechoAtt2 = 0.3

	NsMsfix       = 3.5
	NsAttackThre  = 4.4
	NsAttackThreS = 25
)

// nsfirLen is the high-pass FIR length used in attack detection
// (psymodel.c:159, #define NSFIRLEN 21).
const nsfirLen = 21

// lnToLog10 is LAME's LN_TO_LOG10 (psymodel.c:162, #define LN_TO_LOG10
// (M_LN10/10)). The C macro divides the runtime DOUBLE M_LN10 by 10 at use, so
// the result is 0x3fcd791c5f888823. A Go compile-time constant `math.Ln10/10`
// (or a literal/10.0) is evaluated in arbitrary precision and rounds to a
// DIFFERENT double (…824/…822), which then shifts s3_func's exp() argument by a
// ULP and the spreading table with it. Force the same runtime float64 division
// of math.Ln10 (== M_LN10, 0x40026bb1bbb55516) by using a package var, not a
// const, so it lands on the C's bit pattern exactly.
var lnToLog10 = math.Ln10 / lnToLog10Div

// lnToLog10Div is a non-const 10 so the division above is a runtime float64
// divide matching the C macro (a const denominator would let the compiler fold
// math.Ln10/10 at arbitrary precision and diverge by a ULP).
var lnToLog10Div = float64(10)

// Math constants matching LAME's util.h (util.h:56/64/70/76). LAME defines
// these from <math.h> M_* when available, else these literal doubles.
const (
	piConst    = float64(3.14159265358979323846) // PI
	log2Const  = float64(0.69314718055994530942) // LOG2
	log10Const = float64(2.30258509299404568402) // LOG10
	sqrt2Const = float64(1.41421356237309504880) // SQRT2
)

// floatMax is LAME's FLOAT_MAX = FLT_MAX (machine.h:128). FLOAT is float32 so
// this is the largest finite float32.
var floatMax = float64(math.MaxFloat32)

// III_psy_xmin holds the per-scalefactor-band perceptual data for one channel
// (l3side.h:37). l[] carries the SBMAXl long-block values; s[][] the SBMAXs ×
// 3 short-block (sub-block) values.
type III_psy_xmin struct {
	L [SBMAXl]float32
	S [SBMAXs][3]float32
}

// III_psy_ratio pairs the masking threshold (thm) and energy (en) per-band
// data returned to the quantizer for one channel (l3side.h:42).
type III_psy_ratio struct {
	Thm III_psy_xmin
	En  III_psy_xmin
}

// ATH holds LAME's threshold-of-hearing related state (util.h:166, ATH_t).
// Only the fields the psymodel reads or writes are exercised here, but the
// full struct is mapped 1:1 so init and analysis index it identically to C.
type ATH struct {
	UseAdjust      int                  // use_adjust
	AaSensitivityP float32              // aa_sensitivity_p
	AdjustFactor   float32              // adjust_factor
	AdjustLimit    float32              // adjust_limit
	Decay          float32              // decay
	Floor          float32              // floor
	L              [SBMAXl]float32      // l
	S              [SBMAXs]float32      // s
	Psfb21         [PSFB21]float32      // psfb21
	Psfb12         [PSFB12]float32      // psfb12
	CbL            [PsyCBANDS]float32   // cb_l
	CbS            [PsyCBANDS]float32   // cb_s
	EqlW           [BLKSIZE / 2]float32 // eql_w
}

// maxSBMAX is Max(SBMAX_l,SBMAX_s) used for several fixed-size arrays in
// PsyConstCB2SB (util.h:193/194/198/199).
const maxSBMAX = SBMAXl

// PsyConstCB2SB is LAME's PsyConst_CB2SB_t (util.h:188): the per-partition and
// per-scalefactor-band constants computed once for the long, short and
// long-to-short mappings.
type PsyConstCB2SB struct {
	MaskingLower    [PsyCBANDS]float32 // masking_lower
	Minval          [PsyCBANDS]float32 // minval
	Rnumlines       [PsyCBANDS]float32 // rnumlines
	MldCb           [PsyCBANDS]float32 // mld_cb
	Mld             [maxSBMAX]float32  // mld
	BoWeight        [maxSBMAX]float32  // bo_weight
	AttackThreshold float32            // attack_threshold
	S3ind           [PsyCBANDS][2]int  // s3ind
	Numlines        [PsyCBANDS]int     // numlines
	Bm              [maxSBMAX]int      // bm
	Bo              [maxSBMAX]int      // bo
	Npart           int                // npart
	NSb             int                // n_sb
	S3              []float32          // s3 (allocated, length = #non-zero spread entries)
}

// PsyConst is LAME's PsyConst_t (util.h:209): the global, session-constant
// psychoacoustic data — FFT windows, the three CB2SB mappings, attack
// thresholds, temporal decay and the force-short-block flag.
type PsyConst struct {
	Window              [BLKSIZE]float32      // window
	WindowS             [BLKSIZEs / 2]float32 // window_s
	L                   PsyConstCB2SB         // l
	S                   PsyConstCB2SB         // s
	LToS                PsyConstCB2SB         // l_to_s
	AttackThreshold     [4]float32            // attack_threshold
	Decay               float32               // decay
	ForceShortBlockCalc int                   // force_short_block_calc
}

// PsyStateVar is LAME's PsyStateVar_t (util.h:220): the cross-frame state the
// model carries (previous-granule noise floors, en/thm, loudness, attack
// history and block types).
type PsyStateVar struct {
	NbL1 [4][PsyCBANDS]float32 // nb_l1
	NbL2 [4][PsyCBANDS]float32 // nb_l2
	NbS1 [4][PsyCBANDS]float32 // nb_s1
	NbS2 [4][PsyCBANDS]float32 // nb_s2

	Thm [4]III_psy_xmin // thm
	En  [4]III_psy_xmin // en

	LoudnessSqSave [2]float32 // loudness_sq_save

	TotEner [4]float32 // tot_ener

	LastEnSubshort [4][9]float32 // last_en_subshort
	LastAttacks    [4]int        // last_attacks

	BlocktypeOld [2]int // blocktype_old
}

// PsyResult is LAME's PsyResult_t (util.h:240): the per-granule loudness^2
// result the model produces.
type PsyResult struct {
	LoudnessSq [2][2]float32 // loudness_sq
}

// short_block_t selector values from LAME (lame.h). Only the ones the model
// branches on are needed.
const (
	shortBlockNotSet    = -1
	shortBlockAllowed   = 0
	shortBlockCoupled   = 1
	shortBlockDispensed = 2
	shortBlockForced    = 3
)

// MPEG mode selector values (lame.h MPEG_mode). JointStereo is the only one
// the model branches on by name.
const (
	modeStereo      = 0
	modeJointStereo = 1
	modeMono        = 3
	modeNotSet      = 4
)

// SessionConfig_t (util.h:356) is now defined once, in the unified context
// (context.go), as SessionConfig. The psychoacoustic model reads its
// SamplerateOut / ChannelsOut / ModeGr / Mode / ShortBlocks /
// UseSafeJointStereo / Analysis / ATHtype / ATHcurve / Msfix / ATHOffsetFactor
// / Minval members through gfc.Cfg.

// PsyInitParams carries the lame_global_flags fields psymodel_init reads
// (psymodel.c:1878). LAME reads them from gfp; collected here so InitPsyModel
// has a single parameter object instead of the full global-flags struct.
type PsyInitParams struct {
	ExperimentalZ int     // gfp->experimentalZ -> force_short_block_calc
	Attackthre    float32 // gfp->attackthre
	AttackthreS   float32 // gfp->attackthre_s
	VBRq          int     // gfp->VBR_q
	VBRqFrac      float32 // gfp->VBR_q_frac
}

// scalefac_struct (l3side.h:28) is now defined once, in the unified context
// (context.go), as ScalefacBand; psymodel_init reads gfc.ScalefacBand.L / .S.
//
// The psychoacoustic model formerly hung off a per-slice PsyModel receiver
// bundling gfc->cfg / sv_psy / ov_psy / cd_psy / ATH / scalefac_band and the
// quantizer's sv_qnt.masking_lower. Those are now sub-fields of the unified
// LameInternalFlags context (context.go), and every psymodel method receives
// *LameInternalFlags directly — exactly as the C functions take `gfc`. The
// field routing is: gfc.Cfg / gfc.SvPsy / gfc.OvPsy / gfc.CdPsy / gfc.ATH /
// gfc.ScalefacBand / gfc.SvQnt.MaskingLower.
