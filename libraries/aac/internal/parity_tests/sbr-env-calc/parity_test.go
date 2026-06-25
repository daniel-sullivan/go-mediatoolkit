// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrenvcalc

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file pins the Go port of env_calc.cpp (CalculateSbrEnvelope +
// ResetLimiterBands + the gain/noise/limiter/smoothing/adjustTimeSlot math)
// against the genuine vendored calculateSbrEnvelope. The C bridge resolves the
// freq/limiter band fixtures (resetFreqBandTables + ResetLimiterBands) and runs
// the genuine calculateSbrEnvelope; the Go driver then runs CalculateSbrEnvelope
// over the SAME C-resolved fixtures + the SAME QMF buffers, and the mutated QMF
// buffers + scale factors + cal-env state are asserted EXACT-integer equal (the
// whole SBR subsystem is fixed-point — no tolerance).

// packEnv packs (mant, exp) into the iEnvelope pseudo-float format: low EXP_BITS
// (6) bits hold (exp & MASK_E), the rest hold the mantissa. Mirrors env_dec's
// packEnvVal so the energies the gain calc reads are well-formed.
func packEnv(mant int16, exp int) int16 {
	const expBits = 6
	const maskE = (1 << expBits) - 1
	return (mant &^ maskE) | int16(exp&maskE)
}

// qmfRamp fills a slot-major QMF buffer (nSlots*64) with a deterministic ramp so
// every band/timeslot carries non-trivial energy. seed offsets real vs imag.
func qmfRamp(nSlots, seed int) []int32 {
	buf := make([]int32, nSlots*64)
	for s := 0; s < nSlots; s++ {
		for b := 0; b < 64; b++ {
			// a mid-range Q31 value that varies per (slot,band).
			v := int32(((s*7+b*3+seed)%37 - 18)) << 22
			buf[s*64+b] = v
		}
	}
	return buf
}

func TestCalculateSbrEnvelopeParity(t *testing.T) {
	const nSlots = 40 // overlap(6) + 2*timeStep*numberTimeSlots fits comfortably

	base := func() envCalcConfig {
		// FIX-FIX, 2 envelopes, 2 noise envelopes (16 timeslots, timeStep 2).
		nIEnv := 2 * 56
		nNoise := 2 * 5
		iEnv := make([]int16, nIEnv)
		for i := range iEnv {
			iEnv[i] = packEnv(int16(0x4000), 18+(i%4))
		}
		noise := make([]int16, nNoise)
		for i := range noise {
			noise[i] = packEnv(int16(0x2000), 38+(i%3))
		}
		return envCalcConfig{
			sbrProcSmplRate:       44100,
			startFreq:             5,
			stopFreq:              6,
			freqScale:             2,
			alterScale:            1,
			noiseBands:            2,
			xoverBand:             0,
			numberOfAnalysisBands: 32,
			ampResolution:         1,
			numberTimeSlots:       16,
			timeStep:              2,
			interpolFreq:          1,
			smoothingLength:       0,
			limiterBands:          2,
			limiterGains:          1,
			flags:                 0,

			nEnvelopes:          2,
			tranEnv:             2,
			iTESactive:          0,
			interTempShapeMode0: 0,
			borders:             []uint8{0, 8, 16},
			freqRes:             []uint8{1, 1},
			nNoiseEnvelopes:     2,
			bordersNoise:        []uint8{0, 8, 16},
			iEnvelope:           iEnv,
			sbrNoiseFloorLevel:  noise,
			addHarmonics:        [2]uint32{0, 0},

			hbScale:        4,
			ovHbScale:      4,
			ovLbScale:      6,
			lbScale:        6,
			useLP:          0,
			frameErrorFlag: 0,

			nSlots: nSlots,
		}
	}

	cases := []struct {
		name   string
		mutate func(*envCalcConfig)
	}{
		{"hq_interpol_nosine", func(c *envCalcConfig) {}},
		{"hq_noInterpol", func(c *envCalcConfig) { c.interpolFreq = 0 }},
		{"hq_withSine", func(c *envCalcConfig) { c.addHarmonics[0] = 0xC0000000 }},
		{"hq_smoothing", func(c *envCalcConfig) { c.smoothingLength = 1; c.tranEnv = 1 }},
		{"hq_frameError", func(c *envCalcConfig) { c.frameErrorFlag = 1 }},
		{"hq_limiterGains3", func(c *envCalcConfig) { c.limiterGains = 3 }},
		{"hq_limiterBands0", func(c *envCalcConfig) { c.limiterBands = 0 }},
		{"hq_oneEnv", func(c *envCalcConfig) {
			c.nEnvelopes = 1
			c.borders = []uint8{0, 16}
			c.freqRes = []uint8{1}
			c.tranEnv = 1
			c.nNoiseEnvelopes = 1
			c.bordersNoise = []uint8{0, 16}
		}},
		{"lp_real", func(c *envCalcConfig) { c.useLP = 1 }},
		{"lp_real_noInterpol", func(c *envCalcConfig) { c.useLP = 1; c.interpolFreq = 0 }},
		{"lp_real_withSine", func(c *envCalcConfig) { c.useLP = 1; c.addHarmonics[0] = 0xC0000000 }},
		{"hq_ites", func(c *envCalcConfig) { c.iTESactive = 1; c.interTempShapeMode0 = 1 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			tc.mutate(&cfg)

			real0 := qmfRamp(cfg.nSlots, 0)
			imag0 := qmfRamp(cfg.nSlots, 11)
			degree := make([]int32, 64)

			// C side: resolves fixtures + runs genuine calculateSbrEnvelope.
			cRes := cCalculateSbrEnvelope(cfg, real0, imag0, degree)
			require.Equal(t, 0, cRes.err, "ResetLimiterBands must succeed")

			// Go side: same fixtures + same starting QMF.
			goCfg := sbr.EnvCalcConfig{
				NumberTimeSlots:     cfg.numberTimeSlots,
				TimeStep:            cfg.timeStep,
				InterpolFreq:        cfg.interpolFreq,
				SmoothingLength:     cfg.smoothingLength,
				LimiterGains:        cfg.limiterGains,
				AmpResolution:       cfg.ampResolution,
				Flags:               cfg.flags,
				NumMaster:           cRes.numMaster,
				VKMaster:            cRes.vKMaster,
				NSfb:                cRes.nSfb,
				NNfb:                cRes.nNfb,
				NInvfBands:          cRes.nInvfBands,
				LowSubband:          cRes.lowSubband,
				HighSubband:         cRes.highSubband,
				FreqBandLo:          cRes.freqBandLo,
				FreqBandHi:          cRes.freqBandHi,
				FreqBandNoise:       cRes.freqBandNoise,
				LimiterBandTab:      cRes.limiterBandTab,
				NoLimiterBands:      cRes.noLimiterBands,
				NEnvelopes:          cfg.nEnvelopes,
				TranEnv:             cfg.tranEnv,
				ITESactive:          cfg.iTESactive,
				InterTempShapeMode0: cfg.interTempShapeMode0,
				Borders:             cfg.borders,
				FreqRes:             cfg.freqRes,
				BordersNoise:        cfg.bordersNoise,
				NNoiseEnvelopes:     cfg.nNoiseEnvelopes,
				IEnvelope:           cfg.iEnvelope,
				SbrNoiseFloorLevel:  cfg.sbrNoiseFloorLevel,
				AddHarmonics:        cfg.addHarmonics,
				HbScale:             cfg.hbScale,
				OvHbScale:           cfg.ovHbScale,
				OvLbScale:           cfg.ovLbScale,
				LbScale:             cfg.lbScale,
				UseLP:               cfg.useLP,
				FrameErrorFlag:      cfg.frameErrorFlag,
			}

			goReal := qmfRamp(cfg.nSlots, 0)
			goImag := qmfRamp(cfg.nSlots, 11)
			goDegree := make([]int32, 64)

			goRes := sbr.RunCalculateSbrEnvelope(goCfg, goReal, goImag, goDegree, cfg.nSlots)

			// EXACT-integer equality on the mutated QMF buffers.
			assert.Equal(t, cRes.realFlat, goRes.RealFlat, "adjusted QMF real")
			if cfg.useLP == 0 {
				assert.Equal(t, cRes.imagFlat, goRes.ImagFlat, "adjusted QMF imag")
			}

			// scale factors + cal-env state.
			assert.Equal(t, cRes.hbScale, goRes.HbScale, "hb_scale")
			assert.Equal(t, cRes.ovHbScale, goRes.OvHbScale, "ov_hb_scale")
			assert.Equal(t, cRes.prevTranEnv, goRes.PrevTranEnv, "prevTranEnv")
			assert.Equal(t, cRes.harmIndex, goRes.HarmIndex, "harmIndex")
			assert.Equal(t, cRes.phaseIndex, goRes.PhaseIndex, "phaseIndex")
			assert.Equal(t, cRes.harmFlagsPrev, goRes.HarmFlagsPrev, "harmFlagsPrev")
			assert.Equal(t, cRes.harmFlagsPrevActive, goRes.HarmFlagsPrevActive, "harmFlagsPrevActive")
		})
	}
}
