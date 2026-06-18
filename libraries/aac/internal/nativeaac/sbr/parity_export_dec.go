// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// Thin exported drivers + ROM views for the sbr-dec-env cgo parity oracle
// (internal/parity_tests/sbr-dec-env). They add no logic: each mirrors exactly
// what the C reference does so the oracle can drive the Go port and the genuine
// vendored symbols with identical inputs and assert EXACT integer equality.

// --- ROM table views (sbr_rom.cpp) ------------------------------------------

// LimGains returns the narrowed FIXP_SGL FDK_sbrDecoder_sbr_limGains_m mantissas
// and the FDK_sbrDecoder_sbr_limGains_e exponents.
func LimGains() (m []int16, e []uint8) { return sbrLimGainsM[:], sbrLimGainsE[:] }

// SmoothFilter returns the narrowed FDK_sbrDecoder_sbr_smoothFilter ROM (4).
func SmoothFilter() []int16 { return sbrSmoothFilter[:] }

// LimiterBandsPerOctaveDiv4 returns the FIXP_SGL and FIXP_DBL limiter-band ROM.
func LimiterBandsPerOctaveDiv4() (sgl []int16, dbl []int32) {
	return sbrLimiterBPODiv4[:], sbrLimiterBPODiv4DBL[:]
}

// RandomPhaseFlat returns FDK_sbrDecoder_sbr_randomPhase as flat [re0,im0,...]
// int16 (1024 == 512 pairs).
func RandomPhaseFlat() []int16 {
	out := make([]int16, 2*sbrNFNoRandomVal)
	for i := 0; i < sbrNFNoRandomVal; i++ {
		out[2*i+0] = sbrRandomPhase[i][0]
		out[2*i+1] = sbrRandomPhase[i][1]
	}
	return out
}

// InvTable returns the FDK_sbrDecoder_invTable 1/x ROM (256 FIXP_SGL).
func InvTable() []int16 { return sbrInvTable[:] }

// --- Frequency-band-mapping driver (sbrdec_freq_sca.cpp) ---------------------

// FreqScaleResult is the flat result the oracle compares: the master table, the
// hi/lo/noise band tables, and the derived band counts / subband bounds.
type FreqScaleResult struct {
	Err           int
	NumMaster     uint8
	VKMaster      []uint8
	NSfbLo        uint8
	NSfbHi        uint8
	NNfb          uint8
	NInvfBands    uint8
	LowSubband    uint8
	HighSubband   uint8
	FreqBandLo    []uint8
	FreqBandHi    []uint8
	FreqBandNoise []uint8
}

// RunResetFreqBandTables is the exported driver mirroring the C bridge: it builds
// a fresh SBR_HEADER_DATA from the flat header fields, runs resetFreqBandTables
// (which itself runs sbrdecUpdateFreqScale + the hi/lo/noise derivation), and
// returns the full band-table result so the oracle can compare it bit-for-bit.
func RunResetFreqBandTables(sbrProcSmplRate uint, startFreq, stopFreq, freqScale, alterScale, noiseBands, xoverBand, numberOfAnalysisBands uint8, flags uint) FreqScaleResult {
	var hdr SbrHeaderData
	hdr.SbrProcSmplRate = sbrProcSmplRate
	hdr.NumberOfAnalysisBands = numberOfAnalysisBands
	hdr.BsData.StartFreq = startFreq
	hdr.BsData.StopFreq = stopFreq
	hdr.BsData.FreqScale = freqScale
	hdr.BsData.AlterScale = alterScale
	hdr.BsData.NoiseBands = noiseBands
	hdr.BsInfo.XoverBand = xoverBand

	err := resetFreqBandTables(&hdr, flags)

	f := &hdr.FreqBandData
	return FreqScaleResult{
		Err:           int(err),
		NumMaster:     f.NumMaster,
		VKMaster:      append([]uint8(nil), f.VKMaster[:]...),
		NSfbLo:        f.NSfb[0],
		NSfbHi:        f.NSfb[1],
		NNfb:          f.NNfb,
		NInvfBands:    f.NInvfBands,
		LowSubband:    f.LowSubband,
		HighSubband:   f.HighSubband,
		FreqBandLo:    append([]uint8(nil), f.FreqBandTableLo[:]...),
		FreqBandHi:    append([]uint8(nil), f.FreqBandTableHi[:]...),
		FreqBandNoise: append([]uint8(nil), f.FreqBandTableNoise[:]...),
	}
}

// --- SBR bitstream-extraction drivers (env_extr.cpp) ------------------------

// HeaderParseResult is the flat parse result of sbrGetHeaderData: the returned
// SBR_HEADER_STATUS plus every bs_data / bs_info field the parser writes.
type HeaderParseResult struct {
	Status int // SBR_HEADER_STATUS (headerOK / headerReset)

	AmpResolution uint8
	XoverBand     uint8

	StartFreq       uint8
	StopFreq        uint8
	FreqScale       uint8
	AlterScale      uint8
	NoiseBands      uint8
	LimiterBands    uint8
	LimiterGains    uint8
	InterpolFreq    uint8
	SmoothingLength uint8
}

// RunParseHeaderData drives sbrGetHeaderData over payload[:bufSize] (validBits
// valid bits) starting from a header whose syncState is preSyncState, and
// returns the status + the populated bs_data/bs_info fields. flags is 0 and
// configMode 0 for the HE-AAC v1 (AAC) path; fIsSbrData is 1.
func RunParseHeaderData(payload []byte, bufSize, validBits uint32, preSyncState int, flags uint, fIsSbrData int, configMode uint8) HeaderParseResult {
	var hdr SbrHeaderData
	hdr.SyncState = SbrSyncState(preSyncState)
	bs := newSbrBitStream(payload, bufSize, validBits)
	status := sbrGetHeaderData(&hdr, bs, flags, fIsSbrData, configMode)
	return HeaderParseResult{
		Status:          status,
		AmpResolution:   hdr.BsInfo.AmpResolution,
		XoverBand:       hdr.BsInfo.XoverBand,
		StartFreq:       hdr.BsData.StartFreq,
		StopFreq:        hdr.BsData.StopFreq,
		FreqScale:       hdr.BsData.FreqScale,
		AlterScale:      hdr.BsData.AlterScale,
		NoiseBands:      hdr.BsData.NoiseBands,
		LimiterBands:    hdr.BsData.LimiterBands,
		LimiterGains:    hdr.BsData.LimiterGains,
		InterpolFreq:    hdr.BsData.InterpolFreq,
		SmoothingLength: hdr.BsData.SmoothingLength,
	}
}

// FrameDataResult is the flat parse result of one SBR_FRAME_DATA — every field
// the bitstream-extraction path populates, so the oracle can assert each one
// EXACT against the genuine C struct.
type FrameDataResult struct {
	Ok            int // sbrGetChannelElement return (1 ok, 0 error)
	NScaleFactors int

	// FRAME_INFO
	FrameClass    uint8
	NEnvelopes    uint8
	Borders       []uint8
	FreqRes       []uint8
	TranEnv       int8
	NNoiseEnv     uint8
	BordersNoise  []uint8
	NoisePosition uint8
	VarLength     uint8

	DomainVec      []uint8
	DomainVecNoise []uint8

	SbrInvfMode            []int // INVF_MODE per band
	Coupling               int
	AmpResolutionCurrFrame int

	AddHarmonics []uint32

	IEnvelope          []int16
	SbrNoiseFloorLevel []int16
}

func frameDataToResult(ok int, fd *SbrFrameData) FrameDataResult {
	invf := make([]int, len(fd.SbrInvfMode))
	for i, m := range fd.SbrInvfMode {
		invf[i] = int(m)
	}
	fi := &fd.FrameInfo
	return FrameDataResult{
		Ok:                     ok,
		NScaleFactors:          fd.NScaleFactors,
		FrameClass:             fi.FrameClass,
		NEnvelopes:             fi.NEnvelopes,
		Borders:                append([]uint8(nil), fi.Borders[:]...),
		FreqRes:                append([]uint8(nil), fi.FreqRes[:]...),
		TranEnv:                fi.TranEnv,
		NNoiseEnv:              fi.NNoiseEnv,
		BordersNoise:           append([]uint8(nil), fi.BordersNoise[:]...),
		NoisePosition:          fi.NoisePosition,
		VarLength:              fi.VarLength,
		DomainVec:              append([]uint8(nil), fd.DomainVec[:]...),
		DomainVecNoise:         append([]uint8(nil), fd.DomainVecNoise[:]...),
		SbrInvfMode:            invf,
		Coupling:               fd.Coupling,
		AmpResolutionCurrFrame: fd.AmpResolutionCurrFrame,
		AddHarmonics:           append([]uint32(nil), fd.AddHarmonics[:]...),
		IEnvelope:              append([]int16(nil), fd.IEnvelope[:]...),
		SbrNoiseFloorLevel:     append([]int16(nil), fd.SbrNoiseFloorLevel[:]...),
	}
}

// RunParseChannelElement builds an SBR_HEADER_DATA from the flat header fields,
// runs resetFreqBandTables to populate the band counts the parser reads
// (nSfb/nInvfBands/nNfb), then parses payload[:bufSize] (validBits valid bits)
// via sbrGetChannelElement. ampResolution and xoverBand come from bs_info (the
// SBR header element); they are passed flat because resetFreqBandTables consumes
// xoverBand and sbrGetEnvelope consumes ampResolution. nCh selects SCE (1) or
// CPE (2). Returns the parsed left (and right, for CPE) frame-data, plus the
// overall ok flag.
func RunParseChannelElement(sbrProcSmplRate uint, startFreq, stopFreq, freqScale, alterScale, noiseBands, xoverBand, numberOfAnalysisBands, ampResolution, numberTimeSlots, timeStep uint8, nCh int, overlap int, payload []byte, bufSize, validBits uint32, flags uint) (left, right FrameDataResult) {
	var hdr SbrHeaderData
	hdr.SbrProcSmplRate = sbrProcSmplRate
	hdr.NumberOfAnalysisBands = numberOfAnalysisBands
	hdr.NumberTimeSlots = numberTimeSlots
	hdr.TimeStep = timeStep
	hdr.SyncState = SbrSyncState(sbrActive)
	hdr.BsData.StartFreq = startFreq
	hdr.BsData.StopFreq = stopFreq
	hdr.BsData.FreqScale = freqScale
	hdr.BsData.AlterScale = alterScale
	hdr.BsData.NoiseBands = noiseBands
	hdr.BsInfo.XoverBand = xoverBand
	hdr.BsInfo.AmpResolution = ampResolution

	resetFreqBandTables(&hdr, flags)

	var prev SbrPrevFrameData
	bs := newSbrBitStream(payload, bufSize, validBits)

	var fdLeft, fdRight SbrFrameData
	var ok int
	if nCh == 2 {
		ok = sbrGetChannelElement(&hdr, &fdLeft, &fdRight, &prev, 0, bs, nil, flags, overlap)
		return frameDataToResult(ok, &fdLeft), frameDataToResult(ok, &fdRight)
	}
	ok = sbrGetChannelElement(&hdr, &fdLeft, nil, &prev, 0, bs, nil, flags, overlap)
	return frameDataToResult(ok, &fdLeft), FrameDataResult{}
}

// --- SBR envelope dequantization driver (env_dec.cpp) -----------------------

// DecodeResult is the flat dequantized result of decodeSbrData: the energy and
// noise-floor pseudo-float arrays plus the previous-frame delta-coding carry
// state, so the oracle can assert each EXACT against the genuine C.
type DecodeResult struct {
	Ok                 int
	NScaleFactors      int
	Coupling           uint8
	IEnvelope          []int16
	SbrNoiseFloorLevel []int16
	SfbNrgPrev         []int16
	PrevNoiseLevel     []int16
	FrameError         uint8
}

// RunDecodeSbrData builds an SBR_HEADER_DATA from the flat header fields, runs
// resetFreqBandTables, parses payload[:bufSize] (validBits valid bits) via the
// already-ported sbrGetChannelElement, then runs DecodeSbrData (the env_dec.cpp
// port) over the parsed frame data and a fresh previous-frame whose stopPos is
// set so the FIX-FIX start border matches (the normal, non-concealment path).
// It returns the dequantized energy/noise arrays plus the updated prev-frame
// carry. nCh selects SCE (1) or CPE (2). Mirrors the C bridge exactly.
func RunDecodeSbrData(sbrProcSmplRate uint, startFreq, stopFreq, freqScale, alterScale, noiseBands, xoverBand, numberOfAnalysisBands, ampResolution, numberTimeSlots, timeStep uint8, nCh int, overlap int, payload []byte, bufSize, validBits uint32, flags uint) (left, right DecodeResult) {
	var hdr SbrHeaderData
	hdr.SbrProcSmplRate = sbrProcSmplRate
	hdr.NumberOfAnalysisBands = numberOfAnalysisBands
	hdr.NumberTimeSlots = numberTimeSlots
	hdr.TimeStep = timeStep
	hdr.SyncState = SbrSyncState(sbrActive)
	hdr.BsData.StartFreq = startFreq
	hdr.BsData.StopFreq = stopFreq
	hdr.BsData.FreqScale = freqScale
	hdr.BsData.AlterScale = alterScale
	hdr.BsData.NoiseBands = noiseBands
	hdr.BsInfo.XoverBand = xoverBand
	hdr.BsInfo.AmpResolution = ampResolution

	resetFreqBandTables(&hdr, flags)

	var prevL, prevR SbrPrevFrameData
	prevL.StopPos = numberTimeSlots
	prevR.StopPos = numberTimeSlots

	bs := newSbrBitStream(payload, bufSize, validBits)

	var fdLeft, fdRight SbrFrameData
	var ok int
	if nCh == 2 {
		ok = sbrGetChannelElement(&hdr, &fdLeft, &fdRight, &prevL, 0, bs, nil, flags, overlap)
	} else {
		ok = sbrGetChannelElement(&hdr, &fdLeft, nil, &prevL, 0, bs, nil, flags, overlap)
	}
	// Only decode if the parse succeeded; on a parse error the C bridge returns
	// early with its zero-initialized frame data, so we mirror that (the
	// decodeToResult below over the zero structs yields the same all-zero arrays).
	if ok != 0 {
		if nCh == 2 {
			DecodeSbrData(&hdr, &fdLeft, &prevL, &fdRight, &prevR)
		} else {
			DecodeSbrData(&hdr, &fdLeft, &prevL, nil, nil)
		}
	}

	left = decodeToResult(ok, &fdLeft, &prevL, hdr.FrameError)
	right = decodeToResult(ok, &fdRight, &prevR, hdr.FrameError)
	return left, right
}

func decodeToResult(ok int, fd *SbrFrameData, prev *SbrPrevFrameData, frameError uint8) DecodeResult {
	return DecodeResult{
		Ok:                 ok,
		NScaleFactors:      fd.NScaleFactors,
		Coupling:           uint8(fd.Coupling),
		IEnvelope:          append([]int16(nil), fd.IEnvelope[:]...),
		SbrNoiseFloorLevel: append([]int16(nil), fd.SbrNoiseFloorLevel[:]...),
		SfbNrgPrev:         append([]int16(nil), prev.SfbNrgPrev[:]...),
		PrevNoiseLevel:     append([]int16(nil), prev.PrevNoiseLevel[:]...),
		FrameError:         frameError,
	}
}
