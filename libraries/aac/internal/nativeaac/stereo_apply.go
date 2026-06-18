// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// M/S and intensity apply drivers — the band/group iteration around the
// generateMSOutput / intensity kernels, ported 1:1 from the
// non-complex-prediction paths of libAACdec/src/stereo.cpp. The C fMin(INT,INT)
// clamps on band counts map to Go's builtin min; dfractBits (DFRACT_BITS) is
// shared from invquant_tables.go.

// ApplyMS performs M/S (mid/side) stereo decoding in place, the M/S branch of
// CJointStereo_ApplyMS (libAACdec/src/stereo.cpp:1072-1162). The
// complex-prediction branch is out of scope for this area (see stereo.go).
//
// Layout, matching the C caller's flat per-window arrays:
//   - spectrumL/spectrumR hold all windows back to back; window w starts at
//     w*granuleLength (the SPEC macro, overlapadd.h:115).
//   - sfbLeftScale/sfbRightScale hold 16 scale exponents per window: band b of
//     window w is index w*16+b (the &SFBleftScale[window*16] base in C).
//   - sfbOffsets[band] is the first spectral line of band; len == nbands+1.
//   - windowGroupLength[group] is the number of windows in group g.
//
// scaleFactorBandsTransmittedL/R are the per-channel transmitted band counts;
// maxSfbSteOutside bounds the M/S-processed bands (== min of the two in the
// common common_window case).
func ApplyMS(
	jsd *JointStereoData,
	spectrumL, spectrumR []int32,
	sfbLeftScale, sfbRightScale []int16,
	sfbOffsets []int16,
	windowGroupLength []uint8,
	windowGroups int,
	maxSfbSteOutside int,
	scaleFactorBandsTransmittedL int,
	scaleFactorBandsTransmittedR int,
	granuleLength int,
) {
	scaleFactorBandsTransmitted := min(scaleFactorBandsTransmittedL, scaleFactorBandsTransmittedR)

	window := 0
	for group := 0; group < windowGroups; group++ {
		groupMask := uint8(1) << uint(group)

		for groupwin := 0; groupwin < int(windowGroupLength[group]); groupwin++ {
			leftSpectrum := spectrumL[window*granuleLength:]
			rightSpectrum := spectrumR[window*granuleLength:]
			leftScale := sfbLeftScale[window*16:]
			rightScale := sfbRightScale[window*16:]

			var band int
			for band = 0; band < maxSfbSteOutside; band++ {
				if jsd.MsUsed[band]&groupMask != 0 {
					lScale := int(leftScale[band])
					rScale := int(rightScale[band])
					commonScale := lScale
					if rScale > lScale {
						commonScale = rScale
					}

					commonScale++
					leftScale[band] = int16(commonScale)
					rightScale[band] = int16(commonScale)

					lShift := min(dfractBits-1, commonScale-lScale)
					rShift := min(dfractBits-1, commonScale-rScale)

					offsetCurrBand := int(sfbOffsets[band])
					offsetNextBand := int(sfbOffsets[band+1])

					generateMSOutput(
						leftSpectrum[offsetCurrBand:],
						rightSpectrum[offsetCurrBand:],
						uint(lShift), uint(rShift),
						offsetNextBand-offsetCurrBand,
					)
				}
			}

			if scaleFactorBandsTransmittedL > scaleFactorBandsTransmitted {
				for ; band < scaleFactorBandsTransmittedL; band++ {
					if jsd.MsUsed[band]&groupMask != 0 {
						rightScale[band] = leftScale[band]
						for index := int(sfbOffsets[band]); index < int(sfbOffsets[band+1]); index++ {
							rightSpectrum[index] = leftSpectrum[index]
						}
					}
				}
			} else if scaleFactorBandsTransmittedR > scaleFactorBandsTransmitted {
				for ; band < scaleFactorBandsTransmittedR; band++ {
					if jsd.MsUsed[band]&groupMask != 0 {
						leftScale[band] = rightScale[band]
						for index := int(sfbOffsets[band]); index < int(sfbOffsets[band+1]); index++ {
							rightCoefficient := rightSpectrum[index]
							leftSpectrum[index] = rightCoefficient
							rightSpectrum[index] = -rightCoefficient
						}
					}
				}
			}

			window++
		}
	}

	// Reset MsUsed flags if no explicit signalling was transmitted. Necessary
	// for intensity coding; PNS correlation signalling was mapped before
	// calling ApplyMS. (stereo.cpp:1154-1160)
	if jsd.MsMaskPresent == 2 {
		for i := range jsd.MsUsed {
			jsd.MsUsed[i] = 0
		}
	}
}

// ApplyIS performs intensity-stereo decoding in place, reconstructing the
// right channel from the left for bands coded with the INTENSITY_HCB /
// INTENSITY_HCB2 pseudo-codebooks. C counterpart: CJointStereo_ApplyIS,
// libAACdec/src/stereo.cpp:1164.
//
// Per ISO/IEC 14496-3 §4.6.8.2.3 intensity steering only appears in the right
// channel of a common-window channel pair. For each intensity band the right
// spectrum is fMult(left, scale) where scale is a MantissaTable[lsb][0] gain
// whose sign is flipped for out-of-phase steering (driven by the codebook and
// the M/S flag).
//
// codeBook/rightScaleFactor are the right channel's per-group codebook and
// scalefactor arrays (group*16 base in C). leftSfbScale/rightSfbScale are the
// per-window SFB scale exponents (window*16 base). spectrumL/spectrumR are the
// flat per-window spectra (SPEC offset == window*granuleLength).
func ApplyIS(
	jsd *JointStereoData,
	spectrumL, spectrumR []int32,
	codeBook []uint8,
	rightScaleFactor []int16,
	leftSfbScale, rightSfbScale []int16,
	sfbOffsets []int16,
	windowGroupLength []uint8,
	windowGroups int,
	scaleFactorBandsTransmitted int,
	granuleLength int,
) {
	window := 0
	for group := 0; group < windowGroups; group++ {
		groupMask := uint8(1) << uint(group)

		codeBookGrp := codeBook[group*16:]
		scaleFactorGrp := rightScaleFactor[group*16:]

		for groupwin := 0; groupwin < int(windowGroupLength[group]); groupwin++ {
			leftSpectrum := spectrumL[window*granuleLength:]
			rightSpectrum := spectrumR[window*granuleLength:]
			leftScale := leftSfbScale[window*16:]
			rightScale := rightSfbScale[window*16:]

			for band := 0; band < scaleFactorBandsTransmitted; band++ {
				if codeBookGrp[band] == intensityHCB || codeBookGrp[band] == intensityHCB2 {
					bandScale := -(int(scaleFactorGrp[band]) + 100)

					msb := bandScale >> 2
					lsb := bandScale & 0x03

					// exponent of MantissaTable[lsb][0] is 1, thus msb+1 below.
					scale := mantissaTable[lsb][0]

					rightScale[band] = leftScale[band] + int16(msb) + 1

					if jsd.MsUsed[band]&groupMask != 0 {
						if codeBookGrp[band] == intensityHCB { // _NOT_ in-phase
							scale = -scale
						}
					} else {
						if codeBookGrp[band] == intensityHCB2 { // out-of-phase
							scale = -scale
						}
					}

					for index := int(sfbOffsets[band]); index < int(sfbOffsets[band+1]); index++ {
						// rightSpectrum[index] = fMult(leftSpectrum[index], scale)
						// (stereo.cpp:1232). fMult(FIXP_DBL,FIXP_DBL) == fixmul_DD,
						// which on the build target (aarch64 → forced __arm__ +
						// __ARM_ARCH_8__) takes the ARMv8 override `smull; asr #31`
						// (fixmul_arm.h:156-191) == fixmulDDarm8, NOT the generic
						// ((a*b)>>32)<<1 (fMultDD) which drops bit 31. Using fMultDD
						// here cost 1 LSB on products with bit 31 set — the sole source
						// of the CPE second-channel intensity-stereo residual.
						rightSpectrum[index] = fMult(leftSpectrum[index], scale)
					}
				}
			}

			window++
		}
	}
}
