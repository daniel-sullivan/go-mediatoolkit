// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// This file holds the cross-area seams the ADTS frame-sync-parse slice calls
// but does not own. adtsRead_DecodeHeader in
// libfdk/libMpegTPDec/src/tpdec_adts.cpp invokes the FDK CRC engine
// (FDKcrc*, the crc area) and, for channel_config==0, the program-config-element
// parser CProgramConfig_Read / AudioSpecificConfig_Init (the pce-asc area).
// Those subsystems are translated in their own areas; here they are faithful
// no-op / boundary seams that preserve decodeHeader's control flow without
// reimplementing another area's algorithm. When the crc and pce-asc areas land,
// these forward to the real ports.

package nativeaac

// crcReset mirrors FDKcrcReset on pAdts->crcInfo. The CRC engine is the crc
// area; the protection_absent==1 fast path used by the default ADTS-LC stream
// never consults CRC state, so the seam is a no-op here.
func crcReset(pAdts *adts) {}

// crcStartReg mirrors FDKcrcStartReg(&pAdts->crcInfo, hBs, mBits). crc area.
func crcStartReg(pAdts *adts, hBs *adtsBitReader, mBits int) {}

// crcEndReg mirrors FDKcrcEndReg(&pAdts->crcInfo, hBs, crcReg). crc area.
func crcEndReg(pAdts *adts, hBs *adtsBitReader) {}

// crcGetCRC mirrors FDKcrcGetCRC(&pAdts->crcInfo). crc area; returns 0 until the
// crc area is ported.
func crcGetCRC(pAdts *adts) uint16 { return 0 }

// setCrcReadValue stores the CRC value read from the bitstream
// (pAdts->crcReadValue = crc_check). The persistent CRC field lives in the crc
// area's extension of the adts struct, so the seam drops it here.
func setCrcReadValue(pAdts *adts, v uint16) {}

// parsePCESeam stands in for the channel_config==0 PCE branch of
// adtsRead_DecodeHeader (CProgramConfig_Read / Compare in the pce-asc area).
// Until that area is ported, an implicit-PCE ADTS frame is reported as
// unsupported, matching the C path that returns TRANSPORTDEC_UNSUPPORTED_FORMAT
// when no usable program config is available.
func parsePCESeam() transportDecError { return transportDecUnsupportedFormat }
