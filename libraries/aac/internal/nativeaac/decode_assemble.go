// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Top-level AAC-LC decode assembly: CChannelElement_Decode (channel.cpp:162)
// for the AAC-LC SCE/CPE paths, the per-channel CBlock_FrequencyToTime dispatch,
// and the aacDecoder_DecodeFrame output stage (aacdecoder_lib.cpp:1985-2016)
// that narrows the int32 PCM_DEC time samples to interleaved int16 INT_PCM with
// the limiter disabled. The cgo parity oracle disables the FDK PCM limiter
// (AAC_PCM_LIMITER_ENABLE = 0), so the output is the deterministic
// scaleValuesSaturate(INT_PCM, PCM_DEC, n, PCM_OUT_HEADROOM) + interleave path.

// aacOutDataHeadroom ports self->aacOutDataHeadroom (aacdecoder.cpp:1568).
const aacOutDataHeadroom = 3

// pcmOutHeadroom ports PCM_OUT_HEADROOM (mdct.h:111). With the limiter disabled
// and no DRC, pcmLimiterScale == PCM_OUT_HEADROOM at the final narrowing
// (aacdecoder_lib.cpp:1911 + 2002).
const pcmOutHeadroom = 8

// Decoder is the persistent pure-Go AAC-LC decoder state: the resolved
// sampling-rate ROM and one IMDCT overlap-add handle per output channel.
type Decoder struct {
	sri      samplingRateInfo
	channels int
	frameLen int
	states   []*channelState
	scratch  []int32
}

// samplingRateIndexFor returns the MPEG sampling-frequency index for an exact
// sampling rate, or 15 (escape) if not found — matching the
// getSamplingRateInfo Table-38 search fallback.
func samplingRateIndexFor(rate uint32) int {
	for i := 0; i < 13; i++ {
		if samplingRateTable[i] == rate {
			return i
		}
	}
	return 15
}

// NewDecoder builds an AAC-LC decoder for the given frame length / sampling
// rate / channel count. It resolves the sampling-rate index from the rate and
// mirrors the per-channel mdct_init the FDK decoder runs once at config time
// (aacdecoder.cpp:2409).
func NewDecoder(frameLength int, samplingRate uint32, channels int) (*Decoder, error) {
	d := &Decoder{channels: channels, frameLen: frameLength}
	idx := samplingRateIndexFor(samplingRate)
	if getSamplingRateInfo(&d.sri, uint32(frameLength), uint32(idx), samplingRate) != aacDecOK {
		return nil, errUnsupportedConfig
	}
	d.states = make([]*channelState, channels)
	for i := range d.states {
		d.states[i] = newChannelState()
	}
	return d, nil
}

// Reset clears the per-channel overlap-add state for a new stream.
func (d *Decoder) Reset() {
	for i := range d.states {
		d.states[i] = newChannelState()
	}
}

// DecodeAccessUnit decodes one AAC-LC raw_data_block (one access unit) into
// interleaved int16 PCM (the FDK INT_PCM output with the limiter disabled),
// returning the samples-per-channel produced. pkt is the raw access unit; out
// must hold frameLen*channels int16. It walks the syntactic elements until
// ID_END, decoding the single SCE (mono) or CPE (stereo) the AAC-LC .m4a path
// carries.
func (d *Decoder) DecodeAccessUnit(pkt []byte, out []int16) (int, error) {
	// The FDK bit buffer requires a power-of-two byte length; pad a copy.
	bufSize := uint32(1)
	for bufSize < uint32(len(pkt)) {
		bufSize <<= 1
	}
	if bufSize < 1 {
		bufSize = 1
	}
	buf := make([]byte, bufSize)
	copy(buf, pkt)

	var bs bitStream
	initBitStream(&bs, buf, bufSize, uint32(len(pkt))*8)

	flags := uint32(0)
	var l, r channelData
	var jsd JointStereoData
	var commonWindow uint8
	sawElement := false

	for {
		id := int(bs.readBits(3)) // id_syn_ele

		switch id {
		case idSCE:
			if d.channels != 1 {
				return 0, errChannelMismatch
			}
			// CTns_Reset per element (channel.cpp:437) so a tns_data_present==0
			// frame does not inherit the previous frame's filters.
			cTnsReset(&l.tns)
			if err := readSingleChannelElement(&bs, &l, &d.sri, d.frameLen, flags); err != aacDecOK {
				return 0, decodeError(err)
			}
			decodeMonoElement(&l, &d.sri, d.frameLen, flags)
			frequencyToTimeChannel(d.states[0], &l, &d.sri, d.frameLen, d.tmp())
			writeInterleavedMono(out, l.timePCM, d.frameLen)
			sawElement = true

		case idCPE:
			if d.channels != 2 {
				return 0, errChannelMismatch
			}
			cTnsReset(&l.tns)
			cTnsReset(&r.tns)
			if err := readChannelPairElement(&bs, &l, &r, &commonWindow, &jsd, &d.sri, d.frameLen, flags); err != aacDecOK {
				return 0, decodeError(err)
			}
			decodeStereoElement(&l, &r, &jsd, commonWindow, &d.sri, d.frameLen, flags)
			frequencyToTimeChannel(d.states[0], &l, &d.sri, d.frameLen, d.tmp())
			frequencyToTimeChannel(d.states[1], &r, &d.sri, d.frameLen, d.tmp())
			writeInterleavedStereo(out, l.timePCM, r.timePCM, d.frameLen)
			sawElement = true

		case idEND:
			if !sawElement {
				return 0, errNoElement
			}
			return d.frameLen, nil

		case idFIL:
			// fill_element: count (4 bits) + optional escape, then count bytes.
			cnt := int(bs.readBits(4))
			if cnt == 15 {
				cnt += int(bs.readBits(8)) - 1
			}
			for i := 0; i < cnt; i++ {
				bs.readBits(8)
			}

		case idDSE:
			// data_stream_element: element_instance_tag(4), data_byte_align_flag(1),
			// count(8) [+ esc 8], align, count bytes.
			bs.readBits(4)
			align := bs.readBits(1)
			cnt := int(bs.readBits(8))
			if cnt == 255 {
				cnt += int(bs.readBits(8))
			}
			if align != 0 {
				bs.byteAlign()
			}
			for i := 0; i < cnt; i++ {
				bs.readBits(8)
			}

		default:
			return 0, errUnsupportedElement
		}
	}
}

// tmp returns a reusable frameLen int32 scratch for frequencyToTime.
func (d *Decoder) tmp() []int32 {
	if cap(d.scratch) < d.frameLen {
		d.scratch = make([]int32, d.frameLen)
	}
	return d.scratch[:d.frameLen]
}
