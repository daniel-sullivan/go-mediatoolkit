// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// Thin exported driver for the sbr-env-calc cgo parity oracle
// (internal/parity_tests/sbr-env-calc). It adds no logic: it builds the same
// SBR_HEADER_DATA / SBR_FRAME_DATA / QMF_SCALE_FACTOR / SBR_CALCULATE_ENVELOPE
// the C bridge builds — but from the C-resolved freq/limiter band tables (passed
// in) so env_calc runs on BYTE-identical fixtures — then runs CalculateSbrEnvelope
// and returns the mutated QMF buffers + scale factors + cal-env state for the
// oracle to assert EXACT against the genuine vendored calculateSbrEnvelope.

// EnvCalcConfig is the flat fixture the oracle feeds to the Go driver. The freq /
// limiter band tables are the C-resolved ones (so both sides use identical
// inputs); the rest is the frame grid + raw energies + scale config.
type EnvCalcConfig struct {
	NumberTimeSlots, TimeStep int
	InterpolFreq              int
	SmoothingLength           int
	LimiterGains              int
	AmpResolution             int
	Flags                     uint

	// C-resolved freq band fixtures.
	NumMaster      uint8
	VKMaster       []uint8
	NSfb           [2]uint8
	NNfb           uint8
	NInvfBands     uint8
	LowSubband     uint8
	HighSubband    uint8
	FreqBandLo     []uint8
	FreqBandHi     []uint8
	FreqBandNoise  []uint8
	LimiterBandTab []uint8
	NoLimiterBands uint8

	// frame grid + raw energies.
	NEnvelopes, TranEnv, ITESactive, InterTempShapeMode0 int
	Borders, FreqRes, BordersNoise                       []uint8
	NNoiseEnvelopes                                      int
	IEnvelope, SbrNoiseFloorLevel                        []int16
	AddHarmonics                                         [2]uint32

	HbScale, OvHbScale, OvLbScale, LbScale int
	UseLP, FrameErrorFlag                  int
}

// EnvCalcResult is the flat result of the Go CalculateSbrEnvelope run.
type EnvCalcResult struct {
	HbScale             int
	OvHbScale           int
	PrevTranEnv         int
	HarmIndex           uint8
	PhaseIndex          int
	HarmFlagsPrev       []uint32
	HarmFlagsPrevActive []uint32
	RealFlat            []int32
	ImagFlat            []int32
}

// RunCalculateSbrEnvelope drives the Go port over slot-major QMF buffers
// (realFlat/imagFlat each nSlots*64, mutated in place) using the C-resolved
// fixtures in cfg, and returns the mutated buffers + scale factors + cal-env
// state. Mirrors the C bridge exactly.
func RunCalculateSbrEnvelope(cfg EnvCalcConfig, realFlat, imagFlat, degreeAlias []int32, nSlots int) EnvCalcResult {
	var hdr SbrHeaderData
	hdr.SyncState = SbrSyncState(sbrActive)
	hdr.NumberTimeSlots = uint8(cfg.NumberTimeSlots)
	hdr.TimeStep = uint8(cfg.TimeStep)
	hdr.FrameError = uint8(cfg.FrameErrorFlag)
	hdr.BsData.InterpolFreq = uint8(cfg.InterpolFreq)
	hdr.BsData.SmoothingLength = uint8(cfg.SmoothingLength)
	hdr.BsData.LimiterGains = uint8(cfg.LimiterGains)
	hdr.BsInfo.AmpResolution = uint8(cfg.AmpResolution)

	f := &hdr.FreqBandData
	f.NumMaster = cfg.NumMaster
	copy(f.VKMaster[:], cfg.VKMaster)
	f.NSfb = cfg.NSfb
	f.NNfb = cfg.NNfb
	f.NInvfBands = cfg.NInvfBands
	f.LowSubband = cfg.LowSubband
	f.HighSubband = cfg.HighSubband
	f.OvHighSubband = cfg.HighSubband
	copy(f.FreqBandTableLo[:], cfg.FreqBandLo)
	copy(f.FreqBandTableHi[:], cfg.FreqBandHi)
	copy(f.FreqBandTableNoise[:], cfg.FreqBandNoise)
	copy(f.LimiterBandTab[:], cfg.LimiterBandTab)
	f.NoLimiterBands = cfg.NoLimiterBands

	var fd SbrFrameData
	fd.FrameInfo.FrameClass = 0
	fd.FrameInfo.NEnvelopes = uint8(cfg.NEnvelopes)
	copy(fd.FrameInfo.Borders[:], cfg.Borders)
	copy(fd.FrameInfo.FreqRes[:], cfg.FreqRes)
	fd.FrameInfo.TranEnv = int8(cfg.TranEnv)
	fd.FrameInfo.NNoiseEnv = uint8(cfg.NNoiseEnvelopes)
	copy(fd.FrameInfo.BordersNoise[:], cfg.BordersNoise)
	fd.ITESactive = uint8(cfg.ITESactive)
	fd.InterTempShapeMode[0] = uint8(cfg.InterTempShapeMode0)
	copy(fd.IEnvelope[:], cfg.IEnvelope)
	copy(fd.SbrNoiseFloorLevel[:], cfg.SbrNoiseFloorLevel)
	fd.AddHarmonics = cfg.AddHarmonics

	sf := ScaleFactor{HbScale: cfg.HbScale, OvHbScale: cfg.OvHbScale, OvLbScale: cfg.OvLbScale, LbScale: cfg.LbScale}

	var hCalEnv SbrCalculateEnvelope
	hCalEnv.PrevTranEnv = -1
	resetSbrEnvelopeCalc(&hCalEnv)

	re := unflattenQMF(realFlat, nSlots)
	im := unflattenQMF(imagFlat, nSlots)

	useLP := cfg.UseLP != 0
	CalculateSbrEnvelope(&sf, &hCalEnv, &hdr, &fd, re, im, useLP, degreeAlias, cfg.Flags, cfg.FrameErrorFlag != 0)

	return EnvCalcResult{
		HbScale:             sf.HbScale,
		OvHbScale:           sf.OvHbScale,
		PrevTranEnv:         hCalEnv.PrevTranEnv,
		HarmIndex:           hCalEnv.HarmIndex,
		PhaseIndex:          hCalEnv.PhaseIndex,
		HarmFlagsPrev:       []uint32{hCalEnv.HarmFlagsPrev[0], hCalEnv.HarmFlagsPrev[1]},
		HarmFlagsPrevActive: []uint32{hCalEnv.HarmFlagsPrevActive[0], hCalEnv.HarmFlagsPrevActive[1]},
		RealFlat:            realFlat,
		ImagFlat:            imagFlat,
	}
}

// resetSbrEnvelopeCalc is the 1:1 port of resetSbrEnvelopeCalc
// (env_calc.cpp:1795-1805): reset the per-channel envelope-calc state.
//
// C counterpart: resetSbrEnvelopeCalc (env_calc.cpp:1795).
func resetSbrEnvelopeCalc(hCalEnv *SbrCalculateEnvelope) {
	hCalEnv.PhaseIndex = 0
	hCalEnv.FiltBufferNoiseE = 0
	hCalEnv.StartUp = 1
}

// unflattenQMF views the slot-major flat buffer as nSlots rows of 64, sharing the
// backing array so in-place mutation is visible to the caller.
func unflattenQMF(flat []int32, nSlots int) [][]int32 {
	rows := make([][]int32, nSlots)
	for i := 0; i < nSlots; i++ {
		rows[i] = flat[i*64 : i*64+64]
	}
	return rows
}
