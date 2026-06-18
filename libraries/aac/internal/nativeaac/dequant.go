// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// This file ports the AAC-LC decode "dequant" orchestration 1:1 from the
// vendored Fraunhofer FDK reference: the three block.cpp drivers that turn the
// parsed section codebooks + raw quantized spectrum into scaled spectral
// coefficients with per-band block exponents, for one channel:
//
//   - readScaleFactorData  — CBlock_ReadScaleFactorData (block.cpp:158): walk
//     every transmitted (group,band) reading the per-band scalefactor with the
//     BOOKSCL Huffman codebook (delta-coded), the intensity-steering position,
//     or the PNS noise energy (via the ported CPns_Read), producing the flat
//     aScaleFactor[group*16+band] array.
//   - inverseQuantizeSpectralData — CBlock_InverseQuantizeSpectralData
//     (block.cpp:487): for every transmitted band, find the band max, derive
//     the per-band headroom scale, inverse-quantize the band in place, and
//     stamp the per-band sfb block exponent aSfbScale[window*16+band].
//   - scaleSpectralData — CBlock_ScaleSpectralData (block.cpp:217): per window,
//     fold the per-band sfb exponents (and any TNS headroom) into a single
//     per-window block exponent specScale[window], then down-shift the whole
//     window's spectrum to it.
//
// These drivers are pure integer/fixed-point: every quantized line is an int32
// FIXP_DBL, every exponent a SHORT carried as int16, and the per-band kernels
// they call (maxabsD / getScaleFromValue / inverseQuantizeBand from invquant.go,
// cBlockScaleSpectralDataTnsHeadroom from tns_filter.go) are themselves integer.
// They are therefore bit-identical regardless of build tag and carry no
// aac_strict FP gate — only the FDK-AAC license fence (aacfdk). Every ported
// function names its C counterpart as file:line; the algorithm is translated
// faithfully and is not "improved".
//
// To keep the area self-contained (interface-first, per the port discipline)
// the drivers take the load-bearing pieces of CAacDecoderChannelInfo /
// pDynData the C reads from — the icsInfo, the flat codebook/scalefactor/sfbScale
// arrays, the global gain, the TNS data, and the flat spectrum — as explicit
// parameters rather than reconstituting the giant channel-info struct. The
// control flow, bit reads, delta accumulators, band loops, scale derivation and
// down-shift are the genuine reference.

// The PNS noise-energy bias subtracted from the global gain to seed the first
// PNS band's energy is NOISE_OFFSET == 90 (aacdec_pns.cpp:113, per ISO 14496-3
// p. 175) — value-identical to the package-level noiseOffset constant declared
// in bitenc.go (NOISE_OFFSET there too), reused here.

// cPnsData ports the load-bearing fields of CPnsData (aacdec_pns.h:113): the
// running noise energy accumulator and the per-band PNS-active flag that
// CPns_Read mutates while reading scalefactor data. The inter-channel data and
// the random-seed pointers the dequant stage never touches are omitted; the
// scalefactor array CPns_Read writes is passed in as the shared aScaleFactor.
type cPnsData struct {
	pnsUsed       [8 * 16]uint8 // UCHAR pnsUsed[NO_OFBANDS]
	currentEnergy int           // int CurrentEnergy
	pnsActive     uint8         // UCHAR PnsActive
}

// cPnsRead ports CPns_Read (aacdec_pns.cpp:211): decode one PNS band's noise
// energy into pScaleFactor[group*16+band]. The first PNS band reads a 9-bit
// absolute noise start value (biased by 256) and seeds CurrentEnergy from the
// global gain minus NOISE_OFFSET; every subsequent band reads a delta with the
// BOOKSCL Huffman codebook (biased by 60). hcb is the BOOKSCL codebook
// descriptor.
//
// C counterpart: CPns_Read (libAACdec/src/aacdec_pns.cpp:211).
func cPnsRead(pPnsData *cPnsData, bs *bitStream, hcb *codeBookDescription, pScaleFactor []int16, globalGain uint8, band, group int) {
	var delta int
	pnsBand := group*16 + band

	if pPnsData.pnsActive != 0 {
		// Next PNS band case.
		delta = decodeHuffmanWord(bs, hcb.codeBook) - 60
	} else {
		// First PNS band case.
		noiseStartValue := int(bs.readBits(9))

		delta = noiseStartValue - 256
		pPnsData.pnsActive = 1
		pPnsData.currentEnergy = int(globalGain) - noiseOffset
	}

	pPnsData.currentEnergy += delta
	pScaleFactor[pnsBand] = int16(pPnsData.currentEnergy)

	pPnsData.pnsUsed[pnsBand] = 1
}

// readScaleFactorData ports CBlock_ReadScaleFactorData (block.cpp:158): for
// every window group and every transmitted scalefactor band, fill
// pScaleFactor[group*16+band] from the per-band section codebook in pCodeBook:
//
//   - ZERO_HCB                  → scalefactor 0.
//   - a regular spectral book   → delta-decode the scalefactor with the BOOKSCL
//     codebook (factor accumulator, MIDFAC 1.5 dB,
//     then -100 bias). The (USAC/RSVD/RSV603DA) &&
//     band==0 && group==0 special-case skips the
//     first delta — transcribed though AAC-LC never
//     sets those flags.
//   - INTENSITY_HCB(2)          → delta-decode the intensity position.
//   - NOISE_HCB                 → CPns_Read (errors out under MPEGD_RES/USAC/...).
//
// globalGain is RawDataInfo.GlobalGain, the scalefactor and PNS-energy
// accumulator seed. pPnsData carries the running PNS state. Returns the C
// AAC_DECODER_ERROR.
//
// C counterpart: CBlock_ReadScaleFactorData (libAACdec/src/block.cpp:158).
func readScaleFactorData(bs *bitStream, p *cIcsInfo, sri *samplingRateInfo, pCodeBook []uint8, pScaleFactor []int16, globalGain uint8, pPnsData *cPnsData, flags uint32) aacDecoderError {
	var temp int
	var band int
	var group int
	position := 0             // accu for intensity delta coding
	factor := int(globalGain) // accu for scale factor delta coding
	hcb := &aacCodeBookDescriptionTable[bookscl]
	codeBook := hcb.codeBook

	// Offsets into the flat 8*16 arrays, advanced per group (C does
	// pCodeBook += 16 / pScaleFactor += 16 at the bottom of the loop).
	cbOff := 0
	sfOff := 0

	scaleFactorBandsTransmitted := getScaleFactorBandsTransmitted(p)
	for group = 0; group < getWindowGroups(p); group++ {
		for band = 0; band < scaleFactorBandsTransmitted; band++ {
			switch int(pCodeBook[cbOff+band]) {
			case zeroHCB: // zero book
				pScaleFactor[sfOff+band] = 0

			case intensityHCB, intensityHCB2: // intensity steering
				temp = decodeHuffmanWordCB(bs, codeBook)
				position += temp - 60
				pScaleFactor[sfOff+band] = int16(position - 100)

			case noiseHCB: // PNS
				if flags&(acMPEGDRes|acUSAC|acRSVD50|acRSV603DA) != 0 {
					return aacDecParseError
				}
				cPnsRead(pPnsData, bs, hcb, pScaleFactor, globalGain, band, group)

			default: // decode scale factor
				if !(flags&(acUSAC|acRSVD50|acRSV603DA) != 0 && band == 0 && group == 0) {
					temp = decodeHuffmanWordCB(bs, codeBook)
					factor += temp - 60 // MIDFAC 1.5 dB
				}
				pScaleFactor[sfOff+band] = int16(factor - 100)
			}
		}
		cbOff += 16
		sfOff += 16
	}

	return aacDecOK
}

// inverseQuantizeSpectralData ports CBlock_InverseQuantizeSpectralData
// (block.cpp:487): for every window group and every transmitted scalefactor
// band, inverse-quantize the band's quantized lines in place and produce the
// per-band sfb block exponent aSfbScale[window*16+band].
//
//   - ZERO_HCB / INTENSITY_HCB(2) bands are skipped (no spectral data).
//   - NOISE_HCB bands get a PNS headroom exponent ((scalefactor>>2)+1) and are
//     skipped (their spectral data is generated later, not inverse-quantized).
//   - a regular band: find the band max (maxabsD), reject an out-of-range max
//     (> MAX_QUANTIZED_VALUE → parse error), then with msb = scalefactor>>2 and
//     lsb = scalefactor&3 derive the headroom scale = CntLeadingZeros(locMax) -
//     EvaluatePower43(locMax,lsb) - 2, set the sfb exponent to msb - scale, and
//     inverse-quantize the band (inverseQuantizeBand). An empty band keeps msb.
//
// active_band_search / band_is_noise drive the encoder-side band-is-noise
// detection; the AAC-LC decode path passes activeBandSearch == 0 and a nil
// bandIsNoise (the branch is transcribed 1:1 for fidelity). The trailing
// spectrum clear (BandOffsets[transmitted] .. BandOffsets[total]) zeros the
// untransmitted high lines per window, exactly as the C.
//
// spectrum is the flat MDCT buffer laid out window-major with stride
// granuleLength; sfbScale and codeBook/scaleFactor are the flat 8*16 arrays.
// Returns the C AAC_DECODER_ERROR.
//
// C counterpart: CBlock_InverseQuantizeSpectralData
// (libAACdec/src/block.cpp:487).
func inverseQuantizeSpectralData(p *cIcsInfo, sri *samplingRateInfo, pCodeBook []uint8, pSfbScale []int16, pScaleFactor []int16, spectrum []int32, granuleLength int, bandIsNoise []uint8, activeBandSearch uint8) aacDecoderError {
	scaleFactorBandsTransmitted := getScaleFactorBandsTransmitted(p)
	bandOffsets := getScaleFactorBandOffsets(p, sri)
	totalBands := int(p.totalSfBands) // GetScaleFactorBandsTotal

	// FDKmemclear(pDynData->aSfbScale, (8*16) * sizeof(SHORT)).
	for i := range pSfbScale[:8*16] {
		pSfbScale[i] = 0
	}

	window := 0
	for group := 0; group < getWindowGroups(p); group++ {
		for groupwin := 0; groupwin < getWindowGroupLength(p, group); groupwin, window = groupwin+1, window+1 {
			// inverse quantization
			for band := 0; band < scaleFactorBandsTransmitted; band++ {
				specBase := window*granuleLength + int(bandOffsets[band])

				noLines := int(bandOffsets[band+1]) - int(bandOffsets[band])
				bnds := group*16 + band

				if pCodeBook[bnds] == zeroHCB || pCodeBook[bnds] == intensityHCB || pCodeBook[bnds] == intensityHCB2 {
					continue
				}

				if pCodeBook[bnds] == noiseHCB {
					// Leave headroom for PNS values. +1 because
					// ceil(log2(2^(0.25*3))) = 1, worst case of additional
					// headroom required because of the scalefactor.
					pSfbScale[window*16+band] = (pScaleFactor[bnds] >> 2) + 1
					continue
				}

				pSpectralCoefficient := spectrum[specBase : specBase+noLines]
				locMax := maxabsD(pSpectralCoefficient, noLines)

				if activeBandSearch != 0 {
					if locMax != 0 {
						bandIsNoise[group*16+band] = 0
					}
				}

				// Cheap robustness improvement - Do not remove!!!
				if fixabsD(locMax) > int32(maxQuantizedValue) {
					return aacDecParseError
				}

				msb := pScaleFactor[bnds] >> 2

				// Inverse quantize band only if it is not empty.
				if locMax != 0 {
					lsb := uint32(pScaleFactor[bnds] & 0x03)

					scale := evaluatePower43(&locMax, lsb)
					scale = fixnormzD(locMax) - scale - 2

					pSfbScale[window*16+band] = msb - int16(scale)
					inverseQuantizeBand(pSpectralCoefficient, inverseQuantTable[:], mantissaTable[lsb][:], exponentTable[lsb][:], noLines, scale)
				} else {
					pSfbScale[window*16+band] = msb
				}
			} // for band

			// Make sure the array is cleared to the end.
			startClear := int(bandOffsets[scaleFactorBandsTransmitted])
			endClear := int(bandOffsets[totalBands])
			diffClear := endClear - startClear
			base := window*granuleLength + startClear
			for i := 0; i < diffClear; i++ {
				spectrum[base+i] = 0
			}
		}
	}

	return aacDecOK
}

// scaleSpectralData ports CBlock_ScaleSpectralData (block.cpp:217): per window,
// reduce the per-band sfb block exponents (pSfbScale) to a single per-window
// block exponent (pSpecScale[window]) and down-shift the whole window's
// spectrum to it.
//
// For each window it takes the max sfb exponent over [0, maxSfbs), folds in any
// TNS mantissa headroom (cBlockScaleSpectralDataTnsHeadroom), stores the result
// as pSpecScale[window], then for every band right-shifts the band's lines by
// (SpecScale_window - sfbScale[band]) clamped to DFRACT_BITS-1.
//
// spectrum is the flat MDCT buffer (stride granuleLength); pSfbScale and
// pSpecScale are the flat per-(window,sfb) and per-window exponent arrays;
// tnsData / maxTnsBands feed the TNS headroom branch (pass an inactive CTnsData
// for the no-TNS path).
//
// C counterpart: CBlock_ScaleSpectralData (libAACdec/src/block.cpp:217).
func scaleSpectralData(p *cIcsInfo, sri *samplingRateInfo, maxSfbs uint8, pSfbScale []int16, pSpecScale []int16, spectrum []int32, granuleLength int, tnsData *CTnsData, maxTnsBands uint8) {
	bandOffsets := getScaleFactorBandOffsets(p, sri)

	// FDKmemclear(pSpecScale, 8 * sizeof(SHORT)).
	for i := 0; i < 8; i++ {
		pSpecScale[i] = 0
	}

	window := 0
	for group := 0; group < getWindowGroups(p); group++ {
		for groupwin := 0; groupwin < getWindowGroupLength(p, group); groupwin, window = groupwin+1, window+1 {
			specScaleWindow := int32(pSpecScale[window])
			specBase := window * granuleLength

			// Find scaling for current window.
			for band := 0; band < int(maxSfbs); band++ {
				specScaleWindow = fMax(specScaleWindow, int32(pSfbScale[window*16+band]))
			}

			// Fold in TNS mantissa headroom (block.cpp:247-294); a no-op when
			// no TNS filter is active for this window.
			specScaleWindow = cBlockScaleSpectralDataTnsHeadroom(
				tnsData, window, specScaleWindow, pSfbScale, bandOffsets,
				spectrum[specBase:], maxTnsBands)

			// Store scaling of current window.
			pSpecScale[window] = int16(specScaleWindow)

			for band := 0; band < int(maxSfbs); band++ {
				scale := fMin(int32(dfractBits-1), specScaleWindow-int32(pSfbScale[window*16+band]))
				if scale != 0 {
					// FDK_ASSERT(scale > 0).
					maxIndex := int(bandOffsets[band+1])
					for index := int(bandOffsets[band]); index < maxIndex; index++ {
						spectrum[specBase+index] >>= uint(scale)
					}
				}
			}
		}
	}
}
