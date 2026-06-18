// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psencsbrext

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// goRun mirrors sbrext_run using the pure-Go port.
func goRun(s sbrextScenario) (payload []byte, nBits int) {
	var henv, hnoise sbr.SbrCodeEnvelope
	var envData sbr.SbrEnvData
	nSfb := []int{int(s.noScfBands[0]), int(s.noScfBands[0])}
	df := nativeaac.Fl2fxconstDBLf(0.3)
	sbr.InitSbrCodeEnvelope(&henv, nSfb, 1, df, df)
	sbr.InitSbrCodeEnvelope(&hnoise, nSfb, 1, df, df)
	sbr.InitSbrHuffmanTables(&envData, &henv, &hnoise, sbr.AmpRes(s.ampRes))

	var grid sbr.SbrGrid
	grid.BufferFrameStart = 0
	grid.NumberTimeSlots = s.numberTimeSlots
	grid.FrameClass = sbr.FrameClass(0)
	grid.BsNumEnv = s.bsNumEnv
	grid.VF[0] = 0
	envData.HSbrBSGrid = &grid

	envData.LdGrid = 0
	envData.NoOfEnvelopes = s.noOfEnvelopes
	envData.NoOfnoisebands = s.noOfnoisebands
	envData.Balance = 0
	envData.CurrentAmpResFF = sbr.AmpRes(s.ampRes)

	for i := 0; i < s.noOfEnvelopes; i++ {
		envData.NoScfBands[i] = int(s.noScfBands[i])
		envData.DomainVec[i] = 0
		for b := 0; b < maxFreqCoeffsC; b++ {
			envData.Ienvelope[i][b] = int(s.ienvelopeFlat[i*maxFreqCoeffsC+b])
		}
	}
	envData.DomainVecNoise[0] = 0
	envData.DomainVecNoise[1] = 0
	for i := 0; i < maxFreqCoeffsC; i++ {
		envData.SbrNoiseLevels[i] = s.noiseLevels[i]
	}
	for i := 0; i < s.noOfnoisebands; i++ {
		envData.SbrInvfModeVec[i] = sbr.InvfMode(s.invfMode[i])
	}
	envData.AddHarmonicFlag = 0
	envData.NoHarmonics = 0

	var hdr sbr.EncSbrHeaderData
	hdr.SbrAmpRes = sbr.AmpRes(s.ampRes)
	hdr.SbrStartFrequency = s.startFreq
	hdr.SbrStopFrequency = s.stopFreq
	hdr.SbrXoverBand = s.xoverBand
	hdr.Coupling = 0

	var bsData sbr.SbrBitstreamData
	bsData.HeaderActive = s.headerActive

	// Build the parametric-stereo output handle.
	p := sbr.PSOutParity{
		EnablePSHeader: s.psHeader, EnableIID: s.enIID, IidMode: s.iidMode,
		EnableICC: s.enICC, IccMode: s.iccMode, EnableIpdOpd: 0,
		FrameClass: s.frameClass, NEnvelopes: s.psNEnv,
	}
	for i := 0; i < 4; i++ {
		p.FrameBorder[i] = int(s.frameBorder[i])
		p.DeltaIID[i] = int(s.deltaIID[i])
		p.DeltaICC[i] = int(s.deltaICC[i])
		for b := 0; b < 20; b++ {
			p.Iid[i][b] = int(s.iidFlat[i*20+b])
			p.Icc[i][b] = int(s.iccFlat[i*20+b])
		}
	}
	for b := 0; b < 20; b++ {
		p.IidLast[b] = int(s.iidLast[b])
		p.IccLast[b] = int(s.iccLast[b])
	}
	psh := sbr.PSOutHandleFromParity(p)

	mem := make([]byte, 512)
	bs := sbr.NewFdkWriteBitStream(mem)
	sbr.WriteEnvSingleChannelElementPS(&hdr, &bsData, &envData, psh, bs, 0)

	bits := int(bs.GetValidBits())
	nBytes := (bits + 7) >> 3
	return bs.Bytes()[:nBytes], bits
}

func nBandsForMode(mode int) int {
	switch mode {
	case 1, 4:
		return 20
	default:
		return 10
	}
}

func iidRange(mode int) (off, maxVal int) {
	if mode < 3 {
		return 14, 28
	}
	return 30, 60
}

func TestPSSbrExtParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5BE7))

	mkPS := func(psHeader, enIID, iidMode, enICC, iccMode, psNEnv int) (out struct {
		psHeader, enIID, iidMode, enICC, iccMode, frameClass, psNEnv int
		frameBorder                                                  [4]int32
		deltaIID, deltaICC                                           [4]int32
		iidFlat, iccFlat                                             [80]int32
		iidLast, iccLast                                             [20]int32
	}) {
		out.psHeader = psHeader
		out.enIID = enIID
		out.iidMode = iidMode
		out.enICC = enICC
		out.iccMode = iccMode
		out.frameClass = 0
		out.psNEnv = psNEnv
		iidBands := nBandsForMode(iidMode)
		iccBands := nBandsForMode(iccMode)
		iidOff, iidMax := iidRange(iidMode)
		iccOff, iccMax := 7, 14
		for b := 0; b < 20; b++ {
			out.iidLast[b] = int32(rng.Intn(iidMax+1) - iidOff)
			out.iccLast[b] = int32(rng.Intn(iccMax+1) - iccOff)
		}
		for e := 0; e < psNEnv; e++ {
			// FREQ DPCM only (deltaIID/deltaICC == 0) for this slice.
			out.deltaIID[e] = 0
			out.deltaICC[e] = 0
			lastIid, lastIcc := 0, 0
			for b := 0; b < iidBands; b++ {
				val := lastIid + (rng.Intn(iidMax+1) - iidOff)
				out.iidFlat[e*20+b] = int32(val)
				lastIid = val
			}
			for b := 0; b < iccBands; b++ {
				val := lastIcc + (rng.Intn(iccMax+1) - iccOff)
				out.iccFlat[e*20+b] = int32(val)
				lastIcc = val
			}
		}
		return
	}

	type tc struct {
		name                                     string
		ampRes, nEnv, nNoise, nScf, hdr          int
		psHeader, enIID, iidMode, enICC, iccMode int
		psNEnv                                   int
	}
	tcs := []tc{
		{"hdr_ps_iid_icc_coarse_1env", 1, 1, 1, 9, 1, 1, 1, 0, 1, 0, 1},
		{"nohdr_ps_iid_fine_2env", 1, 2, 2, 9, 0, 1, 1, 3, 1, 0, 2},
		{"hdr_ps_iid_mid_icc_mid", 1, 2, 2, 7, 1, 1, 1, 1, 1, 1, 2},
		{"ps_iid_only_coarse", 1, 1, 1, 11, 1, 1, 1, 0, 0, 0, 1},
		{"ps_no_env", 1, 1, 1, 9, 1, 1, 1, 0, 1, 0, 0},
		{"ps_4env", 1, 1, 1, 5, 0, 1, 1, 0, 1, 0, 4},
	}

	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			s := sbrextScenario{
				ampRes: c.ampRes, headerActive: c.hdr, startFreq: rng.Intn(16),
				stopFreq: rng.Intn(16), xoverBand: rng.Intn(8),
				noOfEnvelopes: c.nEnv, noOfnoisebands: c.nNoise, bsNumEnv: c.nEnv,
				numberTimeSlots: 16,
				noiseLevels:     make([]int8, maxFreqCoeffsC),
			}
			for i := 0; i < c.nEnv; i++ {
				s.noScfBands = append(s.noScfBands, int32(c.nScf))
			}
			s.ienvelopeFlat = make([]int32, c.nEnv*maxFreqCoeffsC)
			lav := 31
			if c.ampRes == 0 {
				lav = 60
			}
			for i := 0; i < c.nEnv; i++ {
				s.ienvelopeFlat[i*maxFreqCoeffsC+0] = int32(rng.Intn(lav + 1))
				for b := 1; b < c.nScf; b++ {
					s.ienvelopeFlat[i*maxFreqCoeffsC+b] = int32(rng.Intn(2*lav+1) - lav)
				}
			}
			nNoiseEnv := 1
			if c.nEnv > 1 {
				nNoiseEnv = 2
			}
			for ne := 0; ne < nNoiseEnv; ne++ {
				for b := 0; b < c.nNoise; b++ {
					idx := ne*c.nNoise + b
					if b == 0 {
						s.noiseLevels[idx] = int8(rng.Intn(20))
					} else {
						s.noiseLevels[idx] = int8(rng.Intn(63) - 31)
					}
				}
			}
			for i := 0; i < c.nNoise; i++ {
				s.invfMode = append(s.invfMode, int32(rng.Intn(4)))
			}

			ps := mkPS(c.psHeader, c.enIID, c.iidMode, c.enICC, c.iccMode, c.psNEnv)
			s.psHeader = ps.psHeader
			s.enIID = ps.enIID
			s.iidMode = ps.iidMode
			s.enICC = ps.enICC
			s.iccMode = ps.iccMode
			s.frameClass = ps.frameClass
			s.psNEnv = ps.psNEnv
			s.frameBorder = ps.frameBorder
			s.deltaIID = ps.deltaIID
			s.deltaICC = ps.deltaICC
			s.iidFlat = ps.iidFlat
			s.iccFlat = ps.iccFlat
			s.iidLast = ps.iidLast
			s.iccLast = ps.iccLast

			cPay, cBits := cRun(s)
			gPay, gBits := goRun(s)
			require.Equal(t, cBits, gBits, "bit count")
			assert.Equal(t, cPay, gPay, "SBR+ps_data payload bytes")
		})
	}
}
