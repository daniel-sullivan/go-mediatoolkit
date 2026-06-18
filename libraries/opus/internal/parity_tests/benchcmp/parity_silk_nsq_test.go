//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

func TestParity_SilkSetupComplexity(t *testing.T) {
	for _, fs_kHz := range []int{8, 12, 16} {
		for _, predictLPC := range []int{10, 16} {
			for complexity := 0; complexity <= 10; complexity++ {
				c := cSilkSetupComplexity(fs_kHz, predictLPC, complexity)
				g := nativeopus.ExportTestSilkSetupComplexity(fs_kHz, predictLPC, complexity)
				if c.PitchEstimationComplexity != g.PitchEstimationComplexity ||
					c.PitchEstimationThreshold_Q16 != g.PitchEstimationThreshold_Q16 ||
					c.PitchEstimationLPCOrder != g.PitchEstimationLPCOrder ||
					c.ShapingLPCOrder != g.ShapingLPCOrder ||
					c.LaShape != g.LaShape ||
					c.NStatesDelayedDecision != g.NStatesDelayedDecision ||
					c.UseInterpolatedNLSFs != g.UseInterpolatedNLSFs ||
					c.NLSF_MSVQ_Survivors != g.NLSF_MSVQ_Survivors ||
					c.Warping_Q16 != g.Warping_Q16 ||
					c.ShapeWinLength != g.ShapeWinLength ||
					c.Complexity != g.Complexity {
					t.Fatalf("setup_complexity mismatch fs=%d predict=%d complexity=%d\n C=%#v\n G=%#v", fs_kHz, predictLPC, complexity, c, g)
				}
			}
		}
	}
}

func TestParity_SilkSetupLBRR(t *testing.T) {
	r := rand.New(rand.NewSource(1001))
	for trial := 0; trial < 200; trial++ {
		prev := r.Intn(2)
		coded := r.Intn(2)
		plp := r.Intn(50)
		cEn, cGI, cR := cSilkSetupLBRR(prev, coded, plp)
		gEn, gGI, gR := nativeopus.ExportTestSilkSetupLBRR(prev, coded, plp)
		if cEn != gEn || cGI != gGI || cR != gR {
			t.Fatalf("setup_LBRR prev=%d coded=%d plp=%d: C=(%d,%d,%d) G=(%d,%d,%d)", prev, coded, plp, cEn, cGI, cR, gEn, gGI, gR)
		}
	}
}

func TestParity_SilkSetupFS(t *testing.T) {
	for _, curFs := range []int{0, 8, 12, 16} {
		for _, curNb := range []int{2, 4} {
			for _, curPkt := range []int{0, 10, 20, 40, 60} {
				for _, newFs := range []int{8, 12, 16} {
					for _, pktMs := range []int{10, 20, 40, 60} {
						c := cSilkSetupFS(curFs, curNb, curPkt, newFs, pktMs)
						g := nativeopus.ExportTestSilkSetupFS(curFs, curNb, curPkt, newFs, pktMs)
						if c != (SilkSetupFsOutC{
							Ret:              g.Ret,
							NFramesPerPacket: g.NFramesPerPacket,
							NbSubfr:          g.NbSubfr,
							FrameLength:      g.FrameLength,
							PitchLPCWinLen:   g.PitchLPCWinLen,
							SubfrLength:      g.SubfrLength,
							LtpMemLength:     g.LtpMemLength,
							LaPitch:          g.LaPitch,
							MaxPitchLag:      g.MaxPitchLag,
							PredictLPCOrder:  g.PredictLPCOrder,
							FsKHz:            g.FsKHz,
							PrevLag:          g.PrevLag,
							FirstFrameReset:  g.FirstFrameReset,
							LastGainIndex:    g.LastGainIndex,
							LagPrev:          g.LagPrev,
							PrevGainQ16:      g.PrevGainQ16,
							PrevSignalType:   g.PrevSignalType,
							InputBufIx:       g.InputBufIx,
							NFramesEncoded:   g.NFramesEncoded,
							TargetRateBps:    g.TargetRateBps,
							PacketSizeMs:     g.PacketSizeMs,
						}) {
							t.Fatalf("setup_fs curFs=%d curNb=%d curPkt=%d newFs=%d pktMs=%d\n C=%#v\n G=%#v", curFs, curNb, curPkt, newFs, pktMs, c, g)
						}
					}
				}
			}
		}
	}
}

// randomNSQInputs constructs a coherent randomized parameter set for the
// noise-shaping quantizer at the requested fs_kHz / nb_subfr / signalType.
func randomNSQInputs(r *rand.Rand, fs_kHz, nb_subfr, signalType int, voiced bool) nativeopus.SilkNSQInputs {
	const (
		MAX_LPC_ORDER       = 16
		MAX_NB_SUBFR        = 4
		LTP_ORDER           = 5
		MAX_SHAPE_LPC_ORDER = 24
		MAX_FRAME_LENGTH    = 320
	)
	predictLPC := 16
	if fs_kHz != 16 {
		predictLPC = 10
	}
	shapingLPC := 16
	warpMul := 0.015 * 65536.0
	warping := fs_kHz * int(warpMul+0.5)

	subfrLen := 5 * fs_kHz
	frameLen := subfrLen * nb_subfr
	ltpMem := 20 * fs_kHz

	x16 := make([]int16, frameLen)
	for i := range x16 {
		x16[i] = int16(r.Intn(8001) - 4000)
	}

	predCoef := make([]int16, 2*MAX_LPC_ORDER)
	// Small-valued coefs so filter doesn't blow up.
	for i := range predCoef {
		predCoef[i] = int16(r.Intn(400) - 200)
	}
	ltpCoef := make([]int16, LTP_ORDER*MAX_NB_SUBFR)
	for i := range ltpCoef {
		ltpCoef[i] = int16(r.Intn(4000) - 2000)
	}
	arQ13 := make([]int16, MAX_NB_SUBFR*MAX_SHAPE_LPC_ORDER)
	for i := range arQ13 {
		arQ13[i] = int16(r.Intn(4000) - 2000)
	}
	harm := make([]int, MAX_NB_SUBFR)
	for i := range harm {
		harm[i] = r.Intn(10000)
	}
	tilt := make([]int, MAX_NB_SUBFR)
	for i := range tilt {
		tilt[i] = r.Intn(8000) - 4000
	}
	lfShp := make([]int32, MAX_NB_SUBFR)
	for i := range lfShp {
		lfShp[i] = int32(r.Intn(1000000) - 500000)
	}
	gains := make([]int32, MAX_NB_SUBFR)
	for i := range gains {
		gains[i] = int32(65536 + r.Intn(65536*10))
	}
	pitchL := make([]int, MAX_NB_SUBFR)
	if voiced {
		for i := range pitchL {
			// Pitch lag in samples. Must be > predictLPCOrder + LTP_ORDER/2 + 1 for voiced.
			pitchL[i] = 80 + r.Intn(40)
		}
	} else {
		for i := range pitchL {
			pitchL[i] = 100
		}
	}

	// Init state.
	initSLTPShp := make([]int32, 2*MAX_FRAME_LENGTH)
	for i := 0; i < ltpMem; i++ {
		initSLTPShp[i] = int32(r.Intn(1<<20) - (1 << 19))
	}
	initXq := make([]int16, 2*MAX_FRAME_LENGTH)
	for i := 0; i < ltpMem; i++ {
		initXq[i] = int16(r.Intn(4001) - 2000)
	}
	initSLPC := make([]int32, 16)
	for i := range initSLPC {
		initSLPC[i] = int32(r.Intn(1<<20) - (1 << 19))
	}
	initSAR2 := make([]int32, 24)
	for i := range initSAR2 {
		initSAR2[i] = int32(r.Intn(1<<20) - (1 << 19))
	}

	nlsfInterp := r.Intn(5) // 0..4
	quantOffset := r.Intn(2)

	return nativeopus.SilkNSQInputs{
		FsKHz:             fs_kHz,
		NbSubfr:           nb_subfr,
		PredictLPCOrder:   predictLPC,
		ShapingLPCOrder:   shapingLPC,
		Warping_Q16:       warping,
		X16:               x16,
		PredCoef_Q12:      predCoef,
		LTPCoef_Q14:       ltpCoef,
		AR_Q13:            arQ13,
		HarmShapeGain_Q14: harm,
		Tilt_Q14:          tilt,
		LF_shp_Q14:        lfShp,
		Gains_Q16:         gains,
		PitchL:            pitchL,
		Lambda_Q10:        1000 + r.Intn(2000),
		LTP_scale_Q14:     r.Intn(16384),
		SignalType:        signalType,
		QuantOffsetType:   quantOffset,
		NLSFInterpCoef_Q2: nlsfInterp,
		Seed:              int8(r.Intn(4)),
		InitLagPrev:       80,
		InitPrevGainQ16:   int32(65536 + r.Intn(65536*5)),
		InitSLTPShpQ14:    initSLTPShp,
		InitXq:            initXq,
		InitSLPCQ14:       initSLPC,
		InitSAR2Q14:       initSAR2,
		InitSLFARShpQ14:   int32(r.Intn(1<<20) - (1 << 19)),
		InitSDiffShpQ14:   int32(r.Intn(1<<20) - (1 << 19)),
	}
}

func TestParity_SilkNSQ(t *testing.T) {
	r := rand.New(rand.NewSource(2001))
	fsSet := []int{8, 12, 16}
	nbSet := []int{2, 4}
	sigSet := []int{0, 2} // 0=no-voice, 2=voiced
	for _, fs := range fsSet {
		for _, nb := range nbSet {
			for _, sig := range sigSet {
				for trial := 0; trial < 8; trial++ {
					in := randomNSQInputs(r, fs, nb, sig, sig == 2)
					cOut := cSilkNSQ(in.FsKHz, in.NbSubfr, in.PredictLPCOrder, in.ShapingLPCOrder, in.Warping_Q16,
						in.X16, in.PredCoef_Q12, in.LTPCoef_Q14, in.AR_Q13,
						in.HarmShapeGain_Q14, in.Tilt_Q14, in.LF_shp_Q14, in.Gains_Q16, in.PitchL,
						in.Lambda_Q10, in.LTP_scale_Q14,
						in.Seed, int8(in.SignalType), int8(in.QuantOffsetType), int8(in.NLSFInterpCoef_Q2),
						in.InitLagPrev, in.InitPrevGainQ16,
						in.InitSLTPShpQ14, in.InitXq, in.InitSLPCQ14, in.InitSAR2Q14,
						in.InitSLFARShpQ14, in.InitSDiffShpQ14)
					gOut := nativeopus.ExportTestSilkNSQ(in)
					if !eqInt8Slice(cOut.Pulses, gOut.Pulses) {
						t.Fatalf("NSQ pulses mismatch fs=%d nb=%d sig=%d trial=%d", fs, nb, sig, trial)
					}
					if !eqInt16Slice(cOut.XQ, gOut.XQ) {
						t.Fatalf("NSQ xq mismatch fs=%d nb=%d sig=%d trial=%d", fs, nb, sig, trial)
					}
					if !eqInt32Slice(cOut.SLTP_shp_Q14, gOut.SLTP_shp_Q14) {
						t.Fatalf("NSQ sLTP_shp mismatch fs=%d nb=%d sig=%d trial=%d", fs, nb, sig, trial)
					}
					if !eqInt32Slice(cOut.SLPC_Q14, gOut.SLPC_Q14) {
						t.Fatalf("NSQ sLPC mismatch fs=%d nb=%d sig=%d trial=%d", fs, nb, sig, trial)
					}
					if !eqInt32Slice(cOut.SAR2_Q14, gOut.SAR2_Q14) {
						t.Fatalf("NSQ sAR2 mismatch fs=%d nb=%d sig=%d trial=%d", fs, nb, sig, trial)
					}
					if cOut.SLF_AR_shp_Q14 != gOut.SLF_AR_shp_Q14 ||
						cOut.SDiff_shp_Q14 != gOut.SDiff_shp_Q14 ||
						cOut.LagPrev != gOut.LagPrev ||
						cOut.SLTP_buf_idx != gOut.SLTP_buf_idx ||
						cOut.SLTP_shp_buf_idx != gOut.SLTP_shp_buf_idx ||
						cOut.RandSeed != gOut.RandSeed ||
						cOut.PrevGainQ16 != gOut.PrevGainQ16 ||
						cOut.RewhiteFlag != gOut.RewhiteFlag {
						t.Fatalf("NSQ state scalars mismatch fs=%d nb=%d sig=%d trial=%d\n C=%+v\n G=%+v",
							fs, nb, sig, trial, cOut, gOut)
					}
				}
			}
		}
	}
}

func TestParity_SilkNSQDelDec(t *testing.T) {
	r := rand.New(rand.NewSource(3001))
	fsSet := []int{8, 12, 16}
	nbSet := []int{2, 4}
	sigSet := []int{0, 2}
	nStatesSet := []int{1, 2, 3, 4}
	for _, fs := range fsSet {
		for _, nb := range nbSet {
			for _, sig := range sigSet {
				for _, ns := range nStatesSet {
					for trial := 0; trial < 4; trial++ {
						in := randomNSQInputs(r, fs, nb, sig, sig == 2)
						cOut, cSeed := cSilkNSQDelDec(in.FsKHz, in.NbSubfr, in.PredictLPCOrder, in.ShapingLPCOrder, in.Warping_Q16, ns,
							in.X16, in.PredCoef_Q12, in.LTPCoef_Q14, in.AR_Q13,
							in.HarmShapeGain_Q14, in.Tilt_Q14, in.LF_shp_Q14, in.Gains_Q16, in.PitchL,
							in.Lambda_Q10, in.LTP_scale_Q14,
							in.Seed, int8(in.SignalType), int8(in.QuantOffsetType), int8(in.NLSFInterpCoef_Q2),
							in.InitLagPrev, in.InitPrevGainQ16,
							in.InitSLTPShpQ14, in.InitXq, in.InitSLPCQ14, in.InitSAR2Q14,
							in.InitSLFARShpQ14, in.InitSDiffShpQ14)
						gOut, gSeed := nativeopus.ExportTestSilkNSQDelDec(in, ns)
						if !eqInt8Slice(cOut.Pulses, gOut.Pulses) {
							t.Fatalf("NSQ_del_dec pulses mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
						}
						if !eqInt16Slice(cOut.XQ, gOut.XQ) {
							t.Fatalf("NSQ_del_dec xq mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
						}
						if !eqInt32Slice(cOut.SLTP_shp_Q14, gOut.SLTP_shp_Q14) {
							t.Fatalf("NSQ_del_dec sLTP_shp mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
						}
						if !eqInt32Slice(cOut.SLPC_Q14, gOut.SLPC_Q14) {
							t.Fatalf("NSQ_del_dec sLPC mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
						}
						if !eqInt32Slice(cOut.SAR2_Q14, gOut.SAR2_Q14) {
							t.Fatalf("NSQ_del_dec sAR2 mismatch fs=%d nb=%d sig=%d ns=%d trial=%d", fs, nb, sig, ns, trial)
						}
						if cOut.SLF_AR_shp_Q14 != gOut.SLF_AR_shp_Q14 ||
							cOut.SDiff_shp_Q14 != gOut.SDiff_shp_Q14 ||
							cOut.LagPrev != gOut.LagPrev ||
							cOut.SLTP_buf_idx != gOut.SLTP_buf_idx ||
							cOut.SLTP_shp_buf_idx != gOut.SLTP_shp_buf_idx ||
							cOut.RandSeed != gOut.RandSeed ||
							cOut.PrevGainQ16 != gOut.PrevGainQ16 ||
							cOut.RewhiteFlag != gOut.RewhiteFlag ||
							cSeed != gSeed {
							t.Fatalf("NSQ_del_dec scalars mismatch fs=%d nb=%d sig=%d ns=%d trial=%d\n C=%+v/seed=%d\n G=%+v/seed=%d",
								fs, nb, sig, ns, trial, cOut, cSeed, gOut, gSeed)
						}
					}
				}
			}
		}
	}
}
