// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// AAC-LC inverse-filterbank dispatch: the radix-2 window-slope and DCT-table
// selection (FDKgetWindowSlope / dct_getTables for the 1024- and 128-line
// transforms) plus CBlock_FrequencyToTime, ported 1:1 from
// libAACdec/src/block.cpp:1016 (the non-LPD AAC-LC branch) and
// libFDK/src/FDK_tools_rom.cpp:4070 / dct.cpp:127. Integer fixed-point — the
// IMDCT, overlap-add, and output scaling are all bit-exact regardless of build.

// getWindowShape ports GetWindowShape (channelinfo.h:503).
func getWindowShape(p *cIcsInfo) uint8 { return p.windowShape }

// getWindowSlopeRadix2 ports FDKgetWindowSlope (FDK_tools_rom.cpp:4070) for the
// AAC-LC radix-2 lengths (1024, 128) and shapes (0 sine, 1 KBD). length is the
// slope length (fl or fr); the returned table is the windowSlopes[shape][0][...]
// entry. AAC-LC never uses the 10ms / 3-4 rasters or low-overlap shape (2), so
// only the radix-2 sine/KBD entries are mapped.
func getWindowSlopeRadix2(length int, shape uint8) []fixSTP {
	switch length {
	case 1024:
		if shape&1 == 1 {
			return kbdWindow1024[:]
		}
		return sineWindow1024[:]
	case 128:
		if shape&1 == 1 {
			return kbdWindow128[:]
		}
		return sineWindow128[:]
	default:
		return nil
	}
}

// dctTablesRadix2 ports the radix-2 (case 0x4) branch of dct_getTables
// (dct.cpp:127) for AAC-LC: sin_twiddle is always SineTable1024, sin_step is
// 1<<(10-ld2_length), and ptwiddle is windowSlopes[0][0][ld2_length-1]
// (SineWindow1024 for tl=1024, SineWindow128 for tl=128). Returns
// (twiddle, sinTwiddle, sinStep).
func dctTablesRadix2(tl int) (twiddle, sinTwiddle []fixSTP, sinStep int) {
	sinTwiddle = sineTable1024[:]
	sinStep = dctGetTablesSinStep(tl)
	// ptwiddle = windowSlopes[0][0][ld2_length-1]; for tl this is the SINE slope
	// of the same length (SineWindow1024 / SineWindow128).
	twiddle = getWindowSlopeRadix2(tl, 0)
	return
}

// frequencyToTime ports CBlock_FrequencyToTime (block.cpp:1016) for the AAC-LC
// non-LPD path: it picks fl/fr/tl/nSpec from the window sequence, runs the
// inverse MLT (imltBlock) into a scratch int32 buffer, then scales-and-saturates
// into the PCM_DEC (int32) output with MDCT_OUT_HEADROOM - aacOutDataHeadroom.
//
// out is the per-channel planar PCM_DEC (int32) time buffer (length frameLen);
// spectrum is the channel's flat dequantized/TNS'd MDCT lines; specScale the
// per-window block exponents; mdct the channel's persistent overlap-add state.
// aacOutDataHeadroom is 3 (aacdecoder.cpp:1568). scratch must hold frameLen int32.
func frequencyToTime(mdct *mdctT, p *cIcsInfo, out, spectrum []int32, specScale []int16,
	frameLen int, aacOutDataHeadroom int, scratch []int32) {
	// getWindow2Nr(length, shape) is 0 for sine/KBD (shape != 2), so for
	// AAC-LC fr == frameLen on a long block.
	var fl, fr, tl, nSpec int
	tl = frameLen
	nSpec = 1

	switch p.windowSequence {
	default:
	case blockLong:
		fl = frameLen
		fr = frameLen // getWindow2Nr == 0 for AAC-LC shapes
		if mdct.prevTl == 0 {
			fl = fr
		}
	case blockStop:
		fl = frameLen >> 3
		fr = frameLen
	case blockStart:
		fl = frameLen
		fr = frameLen >> 3
	case blockShort:
		fl = frameLen >> 3
		fr = frameLen >> 3
		tl >>= 3
		nSpec = 8
	}

	shape := getWindowShape(p)
	wls := getWindowSlopeRadix2(fl, shape)
	wrs := getWindowSlopeRadix2(fr, shape)
	twiddle, sinTwiddle, sinStep := dctTablesRadix2(tl)

	imltBlock(mdct, scratch, spectrum, specScale, nSpec, frameLen, tl,
		wls, fl, wrs, fr, 0, 0, sinStep, twiddle, sinTwiddle)

	// scaleValuesSaturate(outSamples, tmp, frameLen, MDCT_OUT_HEADROOM -
	// aacOutDataHeadroom) (block.cpp:1240).
	scaleValuesSaturateDst(out, scratch, frameLen, int32(mdctOutHeadroom-aacOutDataHeadroom))
}
