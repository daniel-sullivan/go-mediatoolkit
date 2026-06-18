// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencbitwrite

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// goWriteSCE mirrors bwparity_run_sce using the pure-Go port.
func goWriteSCE(s sceScenario) (payload []byte, nBits int) {
	var henv, hnoise sbr.SbrCodeEnvelope
	var envData sbr.SbrEnvData
	nSfb := []int{int(s.noScfBands[0]), int(s.noScfBands[0])}
	df := nativeaac.Fl2fxconstDBLf(0.3)
	sbr.InitSbrCodeEnvelope(&henv, nSfb, 1, df, df)
	sbr.InitSbrCodeEnvelope(&hnoise, nSfb, 1, df, df)
	sbr.InitSbrHuffmanTables(&envData, &henv, &hnoise, sbr.AmpRes(s.ampRes))

	var grid sbr.SbrGrid
	grid.BufferFrameStart = s.bufferFrameStart
	grid.NumberTimeSlots = s.numberTimeSlots
	grid.FrameClass = sbr.FrameClass(s.frameClass)
	grid.BsNumEnv = s.bsNumEnv
	grid.VF[0] = s.vf0
	envData.HSbrBSGrid = &grid

	envData.LdGrid = 0
	envData.NoOfEnvelopes = s.noOfEnvelopes
	envData.NoOfnoisebands = s.noOfnoisebands
	envData.Balance = 0
	envData.CurrentAmpResFF = sbr.AmpRes(s.ampRes)

	for i := 0; i < s.noOfEnvelopes; i++ {
		envData.NoScfBands[i] = int(s.noScfBands[i])
		envData.DomainVec[i] = int(s.domainVec[i])
		for b := 0; b < maxFreqCoeffsC; b++ {
			envData.Ienvelope[i][b] = int(s.ienvelopeFlat[i*maxFreqCoeffsC+b])
		}
	}
	envData.DomainVecNoise[0] = int(s.domainVecNoise[0])
	envData.DomainVecNoise[1] = int(s.domainVecNoise[1])
	for i := 0; i < maxFreqCoeffsC; i++ {
		envData.SbrNoiseLevels[i] = s.noiseLevels[i]
	}
	for i := 0; i < s.noOfnoisebands; i++ {
		envData.SbrInvfModeVec[i] = sbr.InvfMode(s.invfMode[i])
	}
	envData.AddHarmonicFlag = s.addHarmonicFlag
	envData.NoHarmonics = s.noHarmonics
	for i := 0; i < maxFreqCoeffsC; i++ {
		envData.AddHarmonic[i] = s.addHarmonic[i]
	}

	var hdr sbr.EncSbrHeaderData
	hdr.SbrAmpRes = sbr.AmpRes(s.sbrAmpRes)
	hdr.SbrStartFrequency = s.startFreq
	hdr.SbrStopFrequency = s.stopFreq
	hdr.SbrXoverBand = s.xoverBand
	hdr.FreqScale = s.freqScale
	hdr.AlterScale = s.alterScale
	hdr.SbrNoiseBands = s.noiseBands
	hdr.SbrLimiterBands = s.limiterBands
	hdr.SbrLimiterGains = s.limiterGains
	hdr.SbrInterpolFreq = s.interpolFreq
	hdr.SbrSmoothingLength = s.smoothingLength
	hdr.HeaderExtra1 = s.headerExtra1
	hdr.HeaderExtra2 = s.headerExtra2
	hdr.Coupling = 0

	var bsData sbr.SbrBitstreamData
	bsData.HeaderActive = s.headerActive

	mem := make([]byte, 512)
	bs := sbr.NewFdkWriteBitStream(mem)
	sbr.WriteEnvSingleChannelElement(&hdr, &bsData, &envData, bs, 0)

	bits := int(bs.GetValidBits())
	nBytes := (bits + 7) >> 3
	return bs.Bytes()[:nBytes], bits
}

func TestBitWriteSCEParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0xB17))

	mkScenario := func(ampRes, nEnv, nNoise, nScf int, hdrActive, hx1, hx2 int, dirMix bool) sceScenario {
		s := sceScenario{
			ampRes: ampRes, headerActive: hdrActive, headerExtra1: hx1, headerExtra2: hx2,
			sbrAmpRes: ampRes, startFreq: rng.Intn(16), stopFreq: rng.Intn(16),
			xoverBand: rng.Intn(8), freqScale: rng.Intn(4), alterScale: rng.Intn(2),
			noiseBands: rng.Intn(4), limiterBands: rng.Intn(4), limiterGains: rng.Intn(4),
			interpolFreq: rng.Intn(2), smoothingLength: rng.Intn(2),
			frameClass: 0 /*FIXFIX*/, bsNumEnv: nEnv, vf0: rng.Intn(2),
			bufferFrameStart: 0, numberTimeSlots: 16,
			noOfEnvelopes: nEnv, noOfnoisebands: nNoise,
			noiseLevels: make([]int8, maxFreqCoeffsC),
			addHarmonic: make([]uint8, maxFreqCoeffsC),
		}
		for i := 0; i < nEnv; i++ {
			s.noScfBands = append(s.noScfBands, int32(nScf))
			dir := int32(0) // FREQ
			if dirMix && i > 0 {
				dir = int32(rng.Intn(2))
			}
			s.domainVec = append(s.domainVec, dir)
		}
		s.domainVecNoise = [2]int32{0, int32(rng.Intn(2))}
		s.ienvelopeFlat = make([]int32, nEnv*maxFreqCoeffsC)
		lav := 31
		if ampRes == 0 {
			lav = 60
		}
		for i := 0; i < nEnv; i++ {
			// ienvelope[i][0]: for a FREQ envelope it is the absolute start written
			// raw (start_env_bits, always >= lav width); for a TIME envelope it is a
			// delta that must satisfy |delta| <= lav. Keeping it in [0, lav]
			// satisfies both so the test scenario stays valid under either dir.
			s.ienvelopeFlat[i*maxFreqCoeffsC+0] = int32(rng.Intn(lav + 1))
			for b := 1; b < nScf; b++ {
				// delta in [-lav, lav]
				s.ienvelopeFlat[i*maxFreqCoeffsC+b] = int32(rng.Intn(2*lav+1) - lav)
			}
		}
		// noise levels: layout is nNoiseEnv blocks of noOfnoisebands. Each block's
		// first entry is an absolute start (non-negative); the rest are deltas in
		// [-31,31] (CODE_BOOK_SCF_LAV11). nNoiseEnv = (noOfEnvelopes>1?2:1).
		nNoiseEnv := 1
		if nEnv > 1 {
			nNoiseEnv = 2
		}
		for ne := 0; ne < nNoiseEnv; ne++ {
			for b := 0; b < nNoise; b++ {
				idx := ne*nNoise + b
				if b == 0 {
					s.noiseLevels[idx] = int8(rng.Intn(20))
				} else {
					s.noiseLevels[idx] = int8(rng.Intn(63) - 31)
				}
			}
		}
		for i := 0; i < nNoise; i++ {
			s.invfMode = append(s.invfMode, int32(rng.Intn(4)))
		}
		if rng.Intn(2) == 1 {
			s.addHarmonicFlag = 1
			s.noHarmonics = nScf
			for i := 0; i < nScf; i++ {
				s.addHarmonic[i] = uint8(rng.Intn(2))
			}
		}
		return s
	}

	type tc struct {
		name                       string
		ampRes, nEnv, nNoise, nScf int
		hdrActive, hx1, hx2        int
		dirMix                     bool
	}
	tcs := []tc{
		{"amp30_1env_hdr", 1, 1, 1, 9, 1, 1, 1, false},
		{"amp30_2env", 1, 2, 2, 9, 0, 0, 0, true},
		{"amp15_2env_hdr", 0, 2, 2, 7, 1, 1, 0, true},
		{"amp30_3env_nohdr", 1, 3, 2, 11, 0, 1, 1, true},
		{"amp30_4env", 1, 4, 2, 5, 0, 0, 0, true},
		{"amp15_1env", 0, 1, 1, 6, 1, 0, 1, false},
	}
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			s := mkScenario(c.ampRes, c.nEnv, c.nNoise, c.nScf, c.hdrActive, c.hx1, c.hx2, c.dirMix)
			cPay, cBits := cWriteSCE(s)
			gPay, gBits := goWriteSCE(s)
			require.Equal(t, cBits, gBits, "bit count")
			assert.Equal(t, cPay, gPay, "SBR payload bytes")
		})
	}
}
