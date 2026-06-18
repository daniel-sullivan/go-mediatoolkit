// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// TNS bitstream parse: CTns_ReadDataPresentFlag and CTns_Read, ported 1:1 from
// libAACdec/src/aacdec_tns.cpp. Integer bit-unpacking — bit-identical
// regardless of build. The USAC/RSVD coefficient-width branches are transcribed
// verbatim for fidelity even though AAC-LC (flags == 0) never takes them.

// getWindowsPerFrame ports GetWindowsPerFrame (channelinfo.h:529): 8 for a short
// block, 1 otherwise.
func getWindowsPerFrame(p *cIcsInfo) int {
	if p.windowSequence == blockShort {
		return 8
	}
	return 1
}

// getScaleFactorBandsTotal ports GetScaleFactorBandsTotal (channelinfo.h:554).
func getScaleFactorBandsTotal(p *cIcsInfo) int { return int(p.totalSfBands) }

// getMaximumTnsBands ports GetMaximumTnsBands (channelinfo.h:559): the AAC-LC
// TNS_MAX_BANDS for the active block length and sampling rate.
func getMaximumTnsBands(p *cIcsInfo, samplingRateIndex int) uint8 {
	col := 0
	if !isLongBlock(p) {
		col = 1
	}
	return tnsMaxBandsTbl[samplingRateIndex][col]
}

// cTnsReset ports CTns_Reset (aacdec_tns.cpp:119): clear all filters, the
// per-window filter counts, and the DataPresent/Active flags. CChannelElement_Read
// calls this once per element before reading (channel.cpp:437) so stale filters
// from the previous frame never leak into a frame with tns_data_present == 0.
func cTnsReset(pTnsData *CTnsData) {
	*pTnsData = CTnsData{}
}

// cTnsReadDataPresentFlag ports CTns_ReadDataPresentFlag (aacdec_tns.cpp:128):
// read the 1-bit tns_data_present flag.
func cTnsReadDataPresentFlag(bs *bitStream, pTnsData *CTnsData) {
	pTnsData.DataPresent = uint8(bs.readBits(1))
}

// tnsSgnMask / tnsNegMask port the static sgn_mask[]/neg_mask[] tables in
// CTns_Read (aacdec_tns.cpp:209-210).
var (
	tnsSgnMask = [3]uint8{0x2, 0x4, 0x8}
	tnsNegMask = [3]int8{^int8(0x3), ^int8(0x7), ^int8(0xF)}
)

// cTnsRead ports CTns_Read (aacdec_tns.cpp:143): parse the TNS filters for one
// channel's frame from the bitstream. Returns AAC_DEC_OK / AAC_DEC_TNS_READ_ERROR.
func cTnsRead(bs *bitStream, pTnsData *CTnsData, pIcsInfo *cIcsInfo, flags uint32) aacDecoderError {
	errorStatus := aacDecOK

	if pTnsData.DataPresent == 0 {
		return errorStatus
	}

	startWindow := 0
	winsPerFrame := getWindowsPerFrame(pIcsInfo)
	isLongFlag := isLongBlock(pIcsInfo)

	pTnsData.GainLd = 0

	for window := startWindow; window < winsPerFrame; window++ {
		var nFilt uint8
		if isLongFlag {
			nFilt = uint8(bs.readBits(2))
		} else {
			nFilt = uint8(bs.readBits(1))
		}
		pTnsData.NumberOfFilters[window] = nFilt

		if nFilt != 0 {
			// coef_res
			coefRes := uint8(bs.readBits(1))

			nextStopBand := uint8(getScaleFactorBandsTotal(pIcsInfo))

			for index := 0; index < int(nFilt); index++ {
				filter := &pTnsData.Filter[window][index]

				var length uint8
				if isLongFlag {
					length = uint8(bs.readBits(6))
				} else {
					length = uint8(bs.readBits(4))
				}

				if length > nextStopBand {
					length = nextStopBand
				}

				filter.StartBand = nextStopBand - length
				filter.StopBand = nextStopBand
				nextStopBand = filter.StartBand

				var order uint8
				if flags&(acUSAC|acRSVD50|acRSV603DA) != 0 {
					// max(Order) = 15 (long), 7 (short)
					if isLongFlag {
						order = uint8(bs.readBits(4))
					} else {
						order = uint8(bs.readBits(3))
					}
					filter.Order = order
				} else {
					if isLongFlag {
						order = uint8(bs.readBits(5))
					} else {
						order = uint8(bs.readBits(3))
					}
					filter.Order = order

					if filter.Order > tnsMaximumOrder {
						errorStatus = aacDecTnsReadError
						return errorStatus
					}
				}

				if order != 0 {
					filter.Direction = 1
					if bs.readBits(1) != 0 {
						filter.Direction = -1
					}

					coefCompress := uint8(bs.readBits(1))

					filter.Resolution = int8(coefRes + 3)

					sMask := tnsSgnMask[coefRes+1-coefCompress]
					nMask := tnsNegMask[coefRes+1-coefCompress]

					for i := uint8(0); i < order; i++ {
						coef := uint8(bs.readBits(uint32(filter.Resolution) - uint32(coefCompress)))
						if coef&sMask != 0 {
							filter.Coeff[i] = int8(coef | uint8(nMask))
						} else {
							filter.Coeff[i] = int8(coef)
						}
					}
					pTnsData.GainLd = 4
				}
			}
		}
	}

	pTnsData.Active = 1

	return errorStatus
}
