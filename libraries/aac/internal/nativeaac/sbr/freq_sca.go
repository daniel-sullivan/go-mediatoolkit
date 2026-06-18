// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// This file is the 1:1 port of the vendored libSBRdec sbrdec_freq_sca.cpp: the
// SBR master frequency table builder and the high/low/noise frequency-band
// mapping the decoder derives from the parsed SBR header. It is pure fixed-point
// integer arithmetic (UCHAR band tables, FIXP_SGL/FIXP_DBL band factors via the
// shared nativeaac multiply/log kernels), so EXACT-integer parity holds.
//
// HE-AAC v1 (STD) scope: the USAC / 4:1 QUAD-RATE / RSVD50 branches and the
// SBRDEC_QUAD_RATE flag paths are kept faithfully in the control flow but are
// never taken (the AAC SBR caller passes flags without those bits), and the
// USAC-only sbrdec_mapToStdSampleRate hook is therefore not reached and not
// ported in this batch. Everything else is a literal translation.

const (
	maxOctave       = 29 // MAX_OCTAVE (sbrdec_freq_sca.cpp:116)
	maxSecondRegion = 50 // MAX_SECOND_REGION (sbrdec_freq_sca.cpp:117)
)

// MAX_FREQ_COEFFS variants used by the stop-band range checks
// (lpp_tran.h:140-143).
const (
	maxFreqCoeffsDualRate = 48 // MAX_FREQ_COEFFS_DUAL_RATE (lpp_tran.h:138)
	maxFreqCoeffsQuadRate = 56 // MAX_FREQ_COEFFS_QUAD_RATE (lpp_tran.h:139)
	maxFreqCoeffsFs44100  = 35 // MAX_FREQ_COEFFS_FS44100 (lpp_tran.h:142)
	maxFreqCoeffsFs48000  = 32 // MAX_FREQ_COEFFS_FS48000 (lpp_tran.h:143)
)

// SBR header-data flags relevant to freq-scale derivation (env_extr.h). Only the
// bits the freq-scale logic reads are named; the AAC HE-AAC v1 caller leaves them
// all clear, so the USAC/QUAD branches are dead paths preserved for fidelity.
const (
	sbrdecSyntaxScal   = 2   // SBRDEC_SYNTAX_SCAL
	sbrdecSyntaxUsac   = 4   // SBRDEC_SYNTAX_USAC
	sbrdecSyntaxRsvd50 = 8   // SBRDEC_SYNTAX_RSVD50
	sbrdecQuadRate     = 128 // SBRDEC_QUAD_RATE
)

// SBR_ERROR codes (sbrdecoder.h:153-176). Only the subset this batch returns is
// named.
type sbrError int

const (
	sbrdecOK                   sbrError = 0 // SBRDEC_OK
	sbrdecInvalidArgument      sbrError = 1 // SBRDEC_INVALID_ARGUMENT
	sbrdecCreateError          sbrError = 2 // SBRDEC_CREATE_ERROR
	sbrdecNotInitialized       sbrError = 3 // SBRDEC_NOT_INITIALIZED
	sbrdecMemAllocFailed       sbrError = 4 // SBRDEC_MEM_ALLOC_FAILED
	sbrdecParseError           sbrError = 5 // SBRDEC_PARSE_ERROR
	sbrdecUnsupportedConfig    sbrError = 6 // SBRDEC_UNSUPPORTED_CONFIG
	sbrdecOutputBufferTooSmall sbrError = 8 // SBRDEC_OUTPUT_BUFFER_TOO_SMALL
)

const (
	sbrRateDual = 0 // DUAL (sbrdec_freq_sca.h:113)
	sbrRateQuad = 1 // QUAD
)

// fxDbl2FxSgl is FX_DBL2FX_SGL(x) == (FIXP_SGL)((x) >> (DFRACT_BITS-FRACT_BITS))
// == int16(x >> 16) (common_fix.h). Inlined twin matching the AAC-LC port's
// fdk_lpc_parcor.go note.
func fxDbl2FxSgl(x int32) int16 { return int16(x >> 16) }

// fxSgl2FxDbl is FX_SGL2FX_DBL(x) == ((FIXP_DBL)(x) << 16).
func fxSgl2FxDbl(x int16) int32 { return int32(x) << 16 }

// getNumOctavesDiv8 is the 1:1 port of FDK_getNumOctavesDiv8 (transcendent.h:124):
// ld(a/b)/8 == (CalcLdInt(b)-CalcLdInt(a)) >> (FRACT_BITS-3), returned as FIXP_SGL.
func getNumOctavesDiv8(a, b int) int16 {
	return int16(int32(nativeaac.CalcLdInt(int32(b))-nativeaac.CalcLdInt(int32(a))) >> (fractBits - 3))
}

const fractBits = 16 // FRACT_BITS (common_fix.h): FIXP_SGL is Q1.15 -> 16 bits

// getStartBand is the 1:1 port of getStartBand (sbrdec_freq_sca.cpp:133-194):
// convert the bitstream startFreq index into a QMF start band. The
// USAC/RSVD50/QUAD branch (which would remap fs) is preserved but the AAC caller
// never sets those flags, so fsMapped == fs and rate == DUAL.
func getStartBand(fs uint, startFreq uint8, headerDataFlags uint) uint8 {
	var band int
	fsMapped := fs
	rate := sbrRateDual

	if headerDataFlags&(sbrdecSyntaxUsac|sbrdecSyntaxRsvd50) != 0 {
		if headerDataFlags&sbrdecQuadRate != 0 {
			rate = sbrRateQuad
		}
		// USAC-only remap (sbrdec_mapToStdSampleRate) — unreachable in HE-AAC v1.
		fsMapped = fs
	}

	sf := int(startFreq)
	switch fsMapped {
	case 192000:
		band = int(sbrStartFreq192[sf])
	case 176400:
		band = int(sbrStartFreq176[sf])
	case 128000:
		band = int(sbrStartFreq128[sf])
	case 96000, 88200:
		band = int(sbrStartFreq88[rate][sf])
	case 64000:
		band = int(sbrStartFreq64[rate][sf])
	case 48000:
		band = int(sbrStartFreq48[rate][sf])
	case 44100:
		band = int(sbrStartFreq44[rate][sf])
	case 40000:
		band = int(sbrStartFreq40[rate][sf])
	case 32000:
		band = int(sbrStartFreq32[rate][sf])
	case 24000:
		band = int(sbrStartFreq24[rate][sf])
	case 22050:
		band = int(sbrStartFreq22[rate][sf])
	case 16000:
		band = int(sbrStartFreq16[rate][sf])
	default:
		band = 255
	}
	return uint8(band)
}

// getStopBand is the 1:1 port of getStopBand (sbrdec_freq_sca.cpp:204-288).
func getStopBand(fs uint, stopFreq uint8, headerDataFlags uint, k0 uint8) uint8 {
	var k2 uint8

	if stopFreq < 14 {
		var stopMin int
		num := 2 * 64
		var diffTot [maxOctave + maxSecondRegion]uint8
		diff0 := diffTot[:]
		diff1 := diffTot[maxOctave:]

		if headerDataFlags&sbrdecQuadRate != 0 {
			num >>= 1
		}

		if fs < 32000 {
			stopMin = (((2 * 6000 * num) / int(fs)) + 1) >> 1
		} else if fs < 64000 {
			stopMin = (((2 * 8000 * num) / int(fs)) + 1) >> 1
		} else {
			stopMin = (((2 * 10000 * num) / int(fs)) + 1) >> 1
		}

		if stopMin > 64 {
			stopMin = 64
		}

		calcBands(diff0, uint8(stopMin), 64, 13)
		shellsort(diff0, 13)
		cumSum(uint8(stopMin), diff0, 13, diff1)
		k2 = diff1[stopFreq]
	} else if stopFreq == 14 {
		k2 = 2 * k0
	} else {
		k2 = 3 * k0
	}

	if k2 > 64 {
		k2 = 64
	}

	{
		maxFreqCoeffsVal := maxFreqCoeffs
		if headerDataFlags&sbrdecQuadRate != 0 {
			maxFreqCoeffsVal = maxFreqCoeffsQuadRate
		}
		if (int(k2)-int(k0) > maxFreqCoeffsVal) || (k2 <= k0) {
			return 255
		}
	}

	if headerDataFlags&sbrdecQuadRate != 0 {
		return k2
	}
	if headerDataFlags&(sbrdecSyntaxUsac|sbrdecSyntaxRsvd50) != 0 {
		if fs >= 42000 && int(k2)-int(k0) > maxFreqCoeffsFs44100 {
			return 255
		}
		if fs >= 46009 && int(k2)-int(k0) > maxFreqCoeffsFs48000 {
			return 255
		}
	} else {
		if fs == 44100 && int(k2)-int(k0) > maxFreqCoeffsFs44100 {
			return 255
		}
		if fs >= 48000 && int(k2)-int(k0) > maxFreqCoeffsFs48000 {
			return 255
		}
	}

	return k2
}

// sbrdecUpdateFreqScale is the 1:1 port of sbrdecUpdateFreqScale
// (sbrdec_freq_sca.cpp:299-474): build the master frequency table v_k_master.
func sbrdecUpdateFreqScale(vKMaster []uint8, numMaster *uint8, fs uint, hHeaderData *SbrHeaderData, flags uint) sbrError {
	var bpoDiv16 int16
	dk := 0

	var k0, k2 uint8
	var numBands0 uint8
	var numBands1 uint8
	var diffTot [maxOctave + maxSecondRegion]uint8
	diff0 := diffTot[:]
	diff1 := diffTot[maxOctave:]
	var k2Achived, k2Diff int
	incr := 0

	if flags&sbrdecQuadRate != 0 {
		fs >>= 1
	}

	k0 = getStartBand(fs, hHeaderData.BsData.StartFreq, flags)
	if k0 == 255 {
		return sbrdecUnsupportedConfig
	}

	k2 = getStopBand(fs, hHeaderData.BsData.StopFreq, flags, k0)
	if k2 == 255 {
		return sbrdecUnsupportedConfig
	}

	if hHeaderData.BsData.FreqScale > 0 { // Bark
		var k1 int

		switch hHeaderData.BsData.FreqScale {
		case 1:
			bpoDiv16 = nativeaac.Fl2fxconstSGLf(12.0 / 16.0)
		case 2:
			bpoDiv16 = nativeaac.Fl2fxconstSGLf(10.0 / 16.0)
		default:
			bpoDiv16 = nativeaac.Fl2fxconstSGLf(8.0 / 16.0)
		}

		if flags&sbrdecQuadRate != 0 {
			if int16(k0) < (bpoDiv16 >> ((fractBits - 1) - 4)) {
				bpoDiv16 = int16(int16(k0&0xfe) << ((fractBits - 1) - 4))
			}
		}

		if 1000*int(k2) > 2245*int(k0) { // Two or more regions
			k1 = 2 * int(k0)

			numBands0 = uint8(numberOfBands(bpoDiv16, int(k0), k1, 0))
			numBands1 = uint8(numberOfBands(bpoDiv16, k1, int(k2), int(hHeaderData.BsData.AlterScale)))
			if numBands0 < 1 {
				return sbrdecUnsupportedConfig
			}
			if numBands1 < 1 {
				return sbrdecUnsupportedConfig
			}

			calcBands(diff0, k0, uint8(k1), numBands0)
			shellsort(diff0, numBands0)
			if diff0[0] == 0 {
				return sbrdecUnsupportedConfig
			}

			cumSum(k0, diff0, numBands0, vKMaster)

			calcBands(diff1, uint8(k1), k2, numBands1)
			shellsort(diff1, numBands1)
			if diff0[numBands0-1] > diff1[0] {
				if modifyBands(diff0[numBands0-1], diff1, numBands1) != sbrdecOK {
					return sbrdecUnsupportedConfig
				}
			}

			cumSum(uint8(k1), diff1, numBands1, vKMaster[numBands0:])
			*numMaster = numBands0 + numBands1
		} else { // Only one region
			k1 = int(k2)

			numBands0 = uint8(numberOfBands(bpoDiv16, int(k0), k1, 0))
			if numBands0 < 1 {
				return sbrdecUnsupportedConfig
			}
			calcBands(diff0, k0, uint8(k1), numBands0)
			shellsort(diff0, numBands0)
			if diff0[0] == 0 {
				return sbrdecUnsupportedConfig
			}

			cumSum(k0, diff0, numBands0, vKMaster)
			*numMaster = numBands0
		}
	} else { // Linear mode
		if hHeaderData.BsData.AlterScale == 0 {
			dk = 1
			numBands0 = (k2 - k0) & 254
		} else {
			dk = 2
			numBands0 = (((k2 - k0) >> 1) + 1) & 254
		}

		if numBands0 < 1 {
			return sbrdecUnsupportedConfig
		}

		k2Achived = int(k0) + int(numBands0)*dk
		k2Diff = int(k2) - k2Achived

		for i := uint8(0); i < numBands0; i++ {
			diffTot[i] = uint8(dk)
		}

		var i int
		if k2Diff < 0 {
			incr = 1
			i = 0
		}
		if k2Diff > 0 {
			incr = -1
			i = int(numBands0) - 1
		}

		for k2Diff != 0 {
			diffTot[i] = uint8(int(diffTot[i]) - incr)
			i = i + incr
			k2Diff = k2Diff + incr
		}

		cumSum(k0, diffTot[:], numBands0, vKMaster)
		*numMaster = numBands0
	}

	if *numMaster < 1 {
		return sbrdecUnsupportedConfig
	}

	if flags&sbrdecQuadRate != 0 {
		for k := 1; k < int(*numMaster); k++ {
			if !(int(vKMaster[k])-int(vKMaster[k-1]) <= int(k0)-2) {
				return sbrdecUnsupportedConfig
			}
		}
	}

	return sbrdecOK
}

// calcFactorPerBand is the 1:1 port of calcFactorPerBand
// (sbrdec_freq_sca.cpp:485-530).
func calcFactorPerBand(kStart, kStop, numBands int) int16 {
	bandfactor := nativeaac.Fl2fxconstDBLf(0.25)
	step := nativeaac.Fl2fxconstDBLf(0.125)
	direction := 1

	const dfractBits = 32
	start := int32(kStart) << (dfractBits - 8)
	stop := int32(kStop) << (dfractBits - 8)

	i := 0
	for step > 0 {
		i++
		temp := stop
		for j := 0; j < numBands; j++ {
			// temp = fMult(temp,bandfactor); == fMultDiv2(temp,bandfactor)<<2
			temp = nativeaac.FMultDiv2DD(temp, bandfactor) << 2
		}

		if temp < start { // Factor too strong, make it weaker
			if direction == 0 {
				step = int32(step) >> 1
			}
			direction = 1
			bandfactor = bandfactor + step
		} else { // Factor is too weak: make it stronger
			if direction == 1 {
				step = int32(step) >> 1
			}
			direction = 0
			bandfactor = bandfactor - step
		}

		if i > 100 {
			step = 0
		}
	}
	if bandfactor >= nativeaac.Fl2fxconstDBL(0.5) {
		return 0x7fff // MAXVAL_SGL
	}
	return fxDbl2FxSgl(bandfactor << 1)
}

// numberOfBands is the 1:1 port of numberOfBands (sbrdec_freq_sca.cpp:542-569).
func numberOfBands(bpoDiv16 int16, start, stop, warpFlag int) int {
	var numBandsDiv128 int16

	numBandsDiv128 = fxDbl2FxSgl(nativeaac.FMultDD(fxSgl2FxDbl(getNumOctavesDiv8(start, stop)), fxSgl2FxDbl(bpoDiv16)))

	if warpFlag != 0 {
		numBandsDiv128 = fxDbl2FxSgl(nativeaac.FMultDD(fxSgl2FxDbl(numBandsDiv128), fxSgl2FxDbl(nativeaac.Fl2fxconstSGL(25200.0/32768.0))))
	}

	numBandsDiv128 = numBandsDiv128 + nativeaac.Fl2fxconstSGLf(1.0/128.0)
	numBands := 2 * (int32(numBandsDiv128) >> (fractBits - 7))

	return int(numBands)
}

// calcBands is the 1:1 port of CalcBands (sbrdec_freq_sca.cpp:578-609).
func calcBands(diff []uint8, start, stop, numBands uint8) {
	var previous, current int
	var exact, temp int16
	bandfactor := calcFactorPerBand(int(start), int(stop), int(numBands))

	previous = int(stop)
	exact = int16(int(stop) << (fractBits - 8))

	for i := int(numBands) - 1; i >= 0; i-- {
		exact = fxDbl2FxSgl(nativeaac.FMultDD(fxSgl2FxDbl(exact), fxSgl2FxDbl(bandfactor)))
		temp = exact + nativeaac.Fl2fxconstSGL(128.0/32768.0)
		current = int(int32(temp) >> (fractBits - 8))
		diff[i] = uint8(previous - current)
		previous = current
	}
}

// cumSum is the 1:1 port of cumSum (sbrdec_freq_sca.cpp:614-620).
func cumSum(startValue uint8, diff []uint8, length uint8, startAddress []uint8) {
	startAddress[0] = startValue
	for i := 1; i <= int(length); i++ {
		startAddress[i] = startAddress[i-1] + diff[i-1]
	}
}

// modifyBands is the 1:1 port of modifyBands (sbrdec_freq_sca.cpp:629-643).
func modifyBands(maxBandPrevious uint8, diff []uint8, length uint8) sbrError {
	change := int(maxBandPrevious) - int(diff[0])

	if change > (int(diff[length-1])-int(diff[0]))>>1 {
		change = (int(diff[length-1]) - int(diff[0])) >> 1
	}

	diff[0] = uint8(int(diff[0]) + change)
	diff[length-1] = uint8(int(diff[length-1]) - change)
	shellsort(diff, length)

	return sbrdecOK
}

// sbrdecUpdateHiRes is the 1:1 port of sbrdecUpdateHiRes
// (sbrdec_freq_sca.cpp:648-658).
func sbrdecUpdateHiRes(hHires []uint8, numHires *uint8, vKMaster []uint8, numBands, xoverBand uint8) {
	*numHires = numBands - xoverBand
	for i := xoverBand; i <= numBands; i++ {
		hHires[i-xoverBand] = vKMaster[i]
	}
}

// sbrdecUpdateLoRes is the 1:1 port of sbrdecUpdateLoRes
// (sbrdec_freq_sca.cpp:663-681).
func sbrdecUpdateLoRes(hLores []uint8, numLores *uint8, hHires []uint8, numHires uint8) {
	if numHires&1 == 0 {
		*numLores = numHires >> 1
		for i := uint8(0); i <= *numLores; i++ {
			hLores[i] = hHires[i*2]
		}
	} else {
		*numLores = (numHires + 1) >> 1
		hLores[0] = hHires[0]
		for i := uint8(1); i <= *numLores; i++ {
			hLores[i] = hHires[i*2-1]
		}
	}
}

// sbrdecDownSampleLoRes is the 1:1 port of sbrdecDownSampleLoRes
// (sbrdec_freq_sca.cpp:687-713).
func sbrdecDownSampleLoRes(vResult []uint8, numResult uint8, freqBandTableRef []uint8, numRef uint8) {
	var vIndex [maxFreqCoeffs >> 1]int

	orgLength := int(numRef)
	resultLength := int(numResult)

	vIndex[0] = 0
	i := 0
	for orgLength > 0 {
		i++
		step := orgLength / resultLength
		orgLength = orgLength - step
		resultLength--
		vIndex[i] = vIndex[i-1] + step
	}

	for j := 0; j <= i; j++ {
		vResult[j] = freqBandTableRef[vIndex[j]]
	}
}

// shellsort is the 1:1 port of shellsort (sbrdec_freq_sca.cpp:718-739).
func shellsort(in []uint8, n uint8) {
	inc := 1
	for {
		inc = 3*inc + 1
		if inc > int(n) {
			break
		}
	}

	for {
		inc = inc / 3
		for i := inc; i < int(n); i++ {
			v := int(in[i])
			j := i
			for {
				w := int(in[j-inc])
				if w <= v {
					break
				}
				in[j] = uint8(w)
				j -= inc
				if j < inc {
					break
				}
			}
			in[j] = uint8(v)
		}
		if inc <= 1 {
			break
		}
	}
}

// resetFreqBandTables is the 1:1 port of resetFreqBandTables
// (sbrdec_freq_sca.cpp:745-838): derive the hi/lo/noise band tables from the
// master table and stamp lowSubband/highSubband/nSfb/nNfb/nInvfBands.
func resetFreqBandTables(hHeaderData *SbrHeaderData, flags uint) sbrError {
	var k2, kx, lsb, usb int
	var intTemp int
	var nBandsLo, nBandsHi uint8
	hFreq := &hHeaderData.FreqBandData

	err := sbrdecUpdateFreqScale(hFreq.VKMaster[:], &hFreq.NumMaster, hHeaderData.SbrProcSmplRate, hHeaderData, flags)

	if err != sbrdecOK || hHeaderData.BsInfo.XoverBand > hFreq.NumMaster {
		return sbrdecUnsupportedConfig
	}

	sbrdecUpdateHiRes(hFreq.FreqBandTable(1), &nBandsHi, hFreq.VKMaster[:], hFreq.NumMaster, hHeaderData.BsInfo.XoverBand)
	sbrdecUpdateLoRes(hFreq.FreqBandTable(0), &nBandsLo, hFreq.FreqBandTable(1), nBandsHi)

	loLimit := maxFreqCoeffsDualRate
	if hHeaderData.NumberOfAnalysisBands == 16 {
		loLimit = maxFreqCoeffsQuadRate
	}
	if !(nBandsLo > 0) || nBandsLo > uint8(loLimit>>1) {
		return sbrdecUnsupportedConfig
	}

	hFreq.NSfb[0] = nBandsLo
	hFreq.NSfb[1] = nBandsHi

	lsb = int(hFreq.FreqBandTable(0)[0])
	usb = int(hFreq.FreqBandTable(0)[nBandsLo])

	quadLimit := 32
	if flags&sbrdecQuadRate != 0 {
		quadLimit = 16
	}
	if lsb > quadLimit || lsb >= usb {
		return sbrdecUnsupportedConfig
	}

	k2 = int(hFreq.FreqBandTable(1)[nBandsHi])
	kx = int(hFreq.FreqBandTable(1)[0])

	if hHeaderData.BsData.NoiseBands == 0 {
		hFreq.NNfb = 1
	} else {
		intTemp = int(int32(getNumOctavesDiv8(kx, k2)) >> 2)
		intTemp = intTemp * int(hHeaderData.BsData.NoiseBands)
		intTemp = intTemp + int(nativeaac.Fl2fxconstSGLf(0.5/32.0))
		intTemp = intTemp >> (fractBits - 1 - 5)

		if intTemp == 0 {
			intTemp = 1
		}
		if intTemp > maxNoiseCoeffs {
			return sbrdecUnsupportedConfig
		}
		hFreq.NNfb = uint8(intTemp)
	}

	hFreq.NInvfBands = hFreq.NNfb

	sbrdecDownSampleLoRes(hFreq.FreqBandTableNoise[:], hFreq.NNfb, hFreq.FreqBandTable(0), nBandsLo)

	hFreq.OvHighSubband = hFreq.HighSubband

	hFreq.LowSubband = uint8(lsb)
	hFreq.HighSubband = uint8(usb)

	return sbrdecOK
}
