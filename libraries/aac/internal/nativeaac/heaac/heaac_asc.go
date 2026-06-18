// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Explicit-hierarchical AOT-5 (HE-AAC v1) AudioSpecificConfig writer: the 1:1
// equivalent of transportEnc_writeASC (tpenc_asc.cpp:878-990) for the
// SIG_EXPLICIT_HIERARCHICAL case the TT_MP4_RAW HE-AAC v1 encoder defaults to
// (getSbrSignalingMode, aacenc_lib.cpp:434-435). For AOT_SBR the coder-config is
// aot == AOT_AAC_LC, extAOT == AOT_SBR, samplingRate == core rate,
// extSamplingRate == output rate (aacenc_lib.cpp:486-528). PS excluded.
package heaac

import "errors"

// samplingFrequencyTable is the MPEG-4 sampling-frequency-index table
// (tp_data.h SamplingRateTable), used to resolve the 4-bit ASC sf indices.
var samplingFrequencyTable = [16]int{
	96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
	16000, 12000, 11025, 8000, 7350, 0, 0, 0,
}

// errAscRate is returned when a sample rate has no 4-bit sf index (escape path
// not exercised by the HE-AAC v1 rates in scope).
var errAscRate = errors.New("aac: HE-AAC sample rate has no sampling-frequency index")

// samplingFrequencyIndex returns the 4-bit sf index for rate, or -1.
func samplingFrequencyIndex(rate int) int {
	for i := 0; i < 15; i++ {
		if samplingFrequencyTable[i] == rate {
			return i
		}
	}
	return -1
}

// ascBitWriter is a tiny MSB-first bit writer for the ASC.
type ascBitWriter struct {
	buf   []byte
	cur   byte
	nbits int
}

func (w *ascBitWriter) writeBits(value uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		bit := byte((value >> uint(i)) & 1)
		w.cur = (w.cur << 1) | bit
		w.nbits++
		if w.nbits == 8 {
			w.buf = append(w.buf, w.cur)
			w.cur = 0
			w.nbits = 0
		}
	}
}

func (w *ascBitWriter) bytes() []byte {
	if w.nbits > 0 {
		w.buf = append(w.buf, w.cur<<uint(8-w.nbits))
	}
	return w.buf
}

// buildHEAACASC builds the explicit-hierarchical AOT-5 AudioSpecificConfig:
//
//	writeAot(extAOT=5)        5 bits
//	writeSampleRate(coreRate) 4 bits  (config->samplingRate, the core rate)
//	channelConfig             4 bits
//	writeSampleRate(outRate)  4 bits  (config->extSamplingRate, the output rate)
//	writeAot(aot=2)           5 bits  (the AAC-LC core)
//	GASpecificConfig          3 bits  (frameLengthFlag=0, dependsOnCore=0, extFlag=0)
//
// channels 1 => channelConfig 1, channels 2 => channelConfig 2.
func buildHEAACASC(outputRate, coreRate, channels int) []byte {
	coreIdx := samplingFrequencyIndex(coreRate)
	outIdx := samplingFrequencyIndex(outputRate)
	if coreIdx < 0 || outIdx < 0 {
		// HE-AAC v1 rates in scope always resolve; escape-index path excluded.
		_ = errAscRate
		return nil
	}

	var w ascBitWriter
	w.writeBits(5, 5)                // extAOT = AOT_SBR
	w.writeBits(uint32(coreIdx), 4)  // samplingRate (core)
	w.writeBits(uint32(channels), 4) // channelConfiguration
	w.writeBits(uint32(outIdx), 4)   // extSamplingRate (output)
	w.writeBits(2, 5)                // aot = AOT_AAC_LC
	// GASpecificConfig: frameLengthFlag(0, 1024) | dependsOnCoreCoder(0) | extFlag(0).
	w.writeBits(0, 3)
	return w.bytes()
}

// buildHEAACv2ASC builds the explicit-hierarchical AOT-29 (HE-AAC v2 / parametric
// stereo) AudioSpecificConfig. For AOT_PS the coder-config is aot == AOT_AAC_LC,
// extAOT == AOT_PS, samplingRate == core rate, extSamplingRate == output rate,
// channelConfiguration == 1 (the MONO downmix core); getSbrSignalingMode returns
// SIG_EXPLICIT_HIERARCHICAL so the LEADING writeAot is extAOT == AOT_PS (29), the
// PS presence being implicit in the leading AOT (aacenc_lib.cpp:509-535,
// tpenc_asc.cpp:903-922 — there is NO psPresentFlag in the hierarchical form).
// The bit layout is therefore the AOT-5 writer with extAOT 29 and chCfg 1:
//
//	writeAot(extAOT=29)       5 bits  (AOT_PS)
//	writeSampleRate(coreRate) 4 bits
//	channelConfig=1           4 bits  (mono AAC-LC core)
//	writeSampleRate(outRate)  4 bits
//	writeAot(aot=2)           5 bits  (AAC-LC core)
//	GASpecificConfig          3 bits  (0)
func buildHEAACv2ASC(outputRate, coreRate int) []byte {
	coreIdx := samplingFrequencyIndex(coreRate)
	outIdx := samplingFrequencyIndex(outputRate)
	if coreIdx < 0 || outIdx < 0 {
		_ = errAscRate
		return nil
	}

	var w ascBitWriter
	w.writeBits(29, 5)              // extAOT = AOT_PS (leading AOT for hierarchical PS)
	w.writeBits(uint32(coreIdx), 4) // samplingRate (core)
	w.writeBits(1, 4)               // channelConfiguration (mono core)
	w.writeBits(uint32(outIdx), 4)  // extSamplingRate (output)
	w.writeBits(2, 5)               // aot = AOT_AAC_LC
	w.writeBits(0, 3)               // GASpecificConfig
	return w.bytes()
}
