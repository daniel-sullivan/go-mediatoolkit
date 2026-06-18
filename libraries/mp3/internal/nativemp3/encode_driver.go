// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// PCM encode driver — a 1:1 Go translation of LAME 3.100's top-level encode
// interface from lame.c: the buffering loop that chops interleaved PCM into
// 576*mode_gr-sample granules/frames, runs the MDCT+encode pipeline once per
// frame (through EncodeMP3Frame, frame_encode.go), and emits the mp3 bytes,
// plus the end-of-stream flush.
//
// The C reference functions, in encode order:
//
//   - lame_init_internal_flags (lame.c:2330) seeds the encoder state the driver
//     relies on: OldValue / CurrentStep / masking_lower, mf_samples_to_encode =
//     ENCDELAY + POSTDELAY, mf_size = ENCDELAY - MDCTDELAY (the input is padded
//     with this many leading zeros), encoder_delay = ENCDELAY. NewEncoderContext
//     ports this constructor (plus the amp_filter = 1.0 seed the ppflt slice
//     would otherwise fill).
//   - lame_copy_inbuffer (lame.c:1821) applies the pcm_transform scale/downmix
//     matrix while widening int16 PCM to sample_t (float32).
//   - fill_buffer (util.c:673) copies the transformed input into mfbuf (the
//     non-resampling branch; the public encoder never resamples — samplerate_in
//     == samplerate_out).
//   - lame_encode_buffer_sample_t (lame.c:1704) is the main loop: fill mfbuf,
//     and whenever mf_size >= calcNeeded, encode one frame and shift mfbuf down.
//   - lame_encode_flush (lame.c:2092) sends ENCDELAY+POSTDELAY of zero padding so
//     the last real granule is decodable, flushes the bit reservoir
//     (flush_bitstream) and copies out the tail.
//
// # Floating-point
//
// The only per-sample float arithmetic here is lame_copy_inbuffer's
// pcm_transform (FLOAT == float32). It runs once per input sample, not on the
// FMA-sensitive iteration-loop path, but for parity it is routed through the
// //go:noinline drv* helpers (encode_driver_fp_strict.go) so the strict build
// separately-rounds the `xl*m00 + xr*m01` sums, matching the -ffp-contract=off
// oracle. The framing/shift bookkeeping is all integer.

// EncoderContext bundles the LAME encoder context with the writer-facing scratch
// the driver needs (the per-call in_buffer the C grows via update_inbuffer_size).
// It is the Go stand-in for the gfp+gfc pair the lame_encode_* entry points
// thread; the public libraries/mp3 nativeEncoder owns one.
type EncoderContext struct {
	Gfc *LameInternalFlags

	// Gfp is the user flags the context was built from. lame_get_lametag_frame
	// reads a few of its fields (VBR_q / quality / nogap), so the driver keeps
	// the handle to assemble the final Xing/Info/LAME tag frame on flush.
	Gfp *LameGlobalFlags

	// inBuffer0 / inBuffer1 are the transformed (downmixed, scaled) sample_t
	// copies of one EncodeBuffer call's PCM (sv_enc.in_buffer_0/1, grown by
	// update_inbuffer_size, lame.c:1629). They are reallocated to fit the call's
	// nsamples, exactly as the C grows them on demand.
	inBuffer0 []float32
	inBuffer1 []float32
}

// NewEncoderContext builds an encoder context for the given user flags, seeding
// the encoder state (lame_init_internal_flags, lame.c:2330), installing the
// concrete encode-pipeline stages, allocating the ATH, and running
// lame_init_params (init.go) to populate the immutable SessionConfig. It returns
// the context and the lame_init_params status (0 on success, -1 on an invalid
// parameter). The caller (the public nativeEncoder) supplies a fully-populated
// LameGlobalFlags.
func NewEncoderContext(gfp *LameGlobalFlags) (*EncoderContext, int) {
	gfc := new(LameInternalFlags)
	gfc.Stages = NewFrameEncodeStages()
	gfc.ATH = new(ATH)
	// cd_psy is left nil here: psymodel_init (psymodel.c:1897-1903) allocates it,
	// gated by `if (gfc->cd_psy != 0) return 0;`. lame_init_internal_flags
	// (lame.c:2330) does NOT pre-allocate cd_psy — only ATH — so pre-assigning it
	// would trip InitPsyModel's idempotency guard and skip the ENTIRE psymodel
	// init (the 1e20 en/thm/nb_l seed, the numline/bval/mld tables, the attack
	// thresholds and the ATH curve), leaving sv_psy zero and every per-granule
	// energy/threshold wrong. ATH alone is pre-allocated, matching the C contract
	// InitPsyModel documents ("pm.ATH must be non-nil").

	// lame_init_internal_flags (lame.c:2330): seed the encoder state the driver /
	// quantizer rely on before lame_init_params runs.
	gfc.SvQnt.OldValue[0] = 180
	gfc.SvQnt.OldValue[1] = 180
	gfc.SvQnt.CurrentStep[0] = 4
	gfc.SvQnt.CurrentStep[1] = 4
	gfc.SvQnt.MaskingLower = 1

	// mf_samples_to_encode = ENCDELAY + POSTDELAY; mf_size = ENCDELAY - MDCTDELAY
	// (we pad input with this many leading zeros). encoder_delay = ENCDELAY.
	gfc.SvEnc.MfSamplesToEncode = ENCDELAY + POSTDELAY
	gfc.SvEnc.MfSize = ENCDELAY - MDCTDELAY
	gfc.OvEnc.EncoderPadding = 0
	gfc.OvEnc.EncoderDelay = ENCDELAY

	// amp_filter = 1.0 for every band (lame_init_params_ppflt would fill this;
	// the ppflt slice is deferred, so seed the unfiltered passband so mdct_sub48
	// reads valid weights — see stages.go InitParamsPpflt).
	for i := range gfc.SvEnc.AmpFilter {
		gfc.SvEnc.AmpFilter[i] = 1.0
	}

	ret := LameInitParams(gfc, gfp)
	return &EncoderContext{Gfc: gfc, Gfp: gfp}, ret
}

// calcNeeded returns the mf_size threshold at which a frame can be encoded
// (calcNeeded, lame.c:1661): the larger of the FFT-window need and the
// polyphase-filter need, both for one frame.
func calcNeeded(cfg *SessionConfig) int {
	pcmSamplesPerFrame := 576 * cfg.ModeGr

	mfNeeded := BLKSIZE + pcmSamplesPerFrame - FFTOFFSET // amount needed for FFT
	if v := 512 + pcmSamplesPerFrame - 32; v > mfNeeded {
		mfNeeded = v
	}
	return mfNeeded
}

// fillBuffer copies nsamples of transformed input into mfbuf at mf_size,
// returning the count copied via nOut (and the count consumed via nIn)
// (fill_buffer, util.c:673, the non-resampling branch). The public encoder never
// resamples (samplerate_in == samplerate_out), so the resampling branch is not
// ported; a future resampler slice would add it behind isResamplingNecessary.
func (gfc *LameInternalFlags) fillBuffer(inBuffer0, inBuffer1 []float32, nsamples int, nIn, nOut *int) {
	cfg := &gfc.Cfg
	framesize := 576 * cfg.ModeGr
	mfSize := gfc.SvEnc.MfSize

	nout := framesize
	if nsamples < nout {
		nout = nsamples
	}
	copy(gfc.SvEnc.Mfbuf[0][mfSize:mfSize+nout], inBuffer0[:nout])
	if cfg.ChannelsOut == 2 {
		copy(gfc.SvEnc.Mfbuf[1][mfSize:mfSize+nout], inBuffer1[:nout])
	}
	*nOut = nout
	*nIn = nout
}

// copyInbuffer applies the pcm_transform scale/downmix matrix while widening the
// int16 PCM to sample_t (lame_copy_inbuffer, lame.c:1821, pcm_short_type, jump=1,
// s=1.0). It fills ec.inBuffer0/1 for one EncodeBuffer call. pcmL / pcmR are the
// de-interleaved int16 channels (pcmR aliases pcmL for mono, as the C does).
func (ec *EncoderContext) copyInbuffer(pcmL, pcmR []int16, nsamples int) {
	cfg := &ec.Gfc.Cfg
	t := &cfg.PcmTransform

	// m[i][j] = s * pcm_transform[i][j] with s = 1.0 -> m == pcm_transform.
	m00, m01 := t[0][0], t[0][1]
	m10, m11 := t[1][0], t[1][1]

	for i := 0; i < nsamples; i++ {
		xl := float32(pcmL[i])
		xr := float32(pcmR[i])
		ec.inBuffer0[i] = drvTransform(xl, m00, xr, m01) // u = xl*m00 + xr*m01
		ec.inBuffer1[i] = drvTransform(xl, m10, xr, m11) // v = xl*m10 + xr*m11
	}
}

// updateInbufferSize grows ec.inBuffer0/1 to hold nsamples, mirroring
// update_inbuffer_size (lame.c:1629). The C reallocs (and zero-fills) on demand;
// the port reallocates when the current buffers are too small. lame_calloc
// zero-fills, so the port allocates fresh zeroed slices.
func (ec *EncoderContext) updateInbufferSize(nsamples int) {
	if cap(ec.inBuffer0) < nsamples {
		ec.inBuffer0 = make([]float32, nsamples)
		ec.inBuffer1 = make([]float32, nsamples)
	} else {
		ec.inBuffer0 = ec.inBuffer0[:nsamples]
		ec.inBuffer1 = ec.inBuffer1[:nsamples]
	}
}

// EncodeBuffer compresses nsamples of interleaved int16 PCM, appending the
// produced mp3 bytes to dst and returning the extended slice (lame_encode_buffer
// -> lame_encode_buffer_template -> lame_encode_buffer_sample_t, lame.c:1910/
// 1874/1704, pcm_short_type). pcm is interleaved [L0,R0,L1,...] for stereo or
// [S0,S1,...] for mono; nsamples is the per-channel count. It honours LAME's
// delay/padding: the encoder context is constructed with mf_size primed to
// ENCDELAY-MDCTDELAY, so the leading zeros shift the audio by the encoder delay.
func (ec *EncoderContext) EncodeBuffer(pcm []int16, nsamples int, dst []byte) []byte {
	gfc := ec.Gfc
	cfg := &gfc.Cfg

	if nsamples == 0 {
		return dst
	}

	// De-interleave the int16 PCM into per-channel scratch the transform reads.
	// (The C entry points take already-de-interleaved pcm_l / pcm_r; the public
	// libraries/mp3 Encoder hands interleaved samples, so the driver splits them
	// here before lame_copy_inbuffer.)
	var pcmL, pcmR []int16
	if cfg.ChannelsIn == 2 {
		pcmL = make([]int16, nsamples)
		pcmR = make([]int16, nsamples)
		for i := 0; i < nsamples; i++ {
			pcmL[i] = pcm[2*i]
			pcmR[i] = pcm[2*i+1]
		}
	} else {
		pcmL = pcm[:nsamples]
		pcmR = pcmL // mono: r aliases l (lame_encode_buffer_template, lame.c:1901)
	}

	ec.updateInbufferSize(nsamples)
	ec.copyInbuffer(pcmL, pcmR, nsamples)

	return ec.encodeBufferSampleT(nsamples, dst)
}

// encodeBufferSampleT is the buffering loop of lame_encode_buffer_sample_t
// (lame.c:1704), operating on the already-transformed ec.inBuffer0/1. It drains
// any buffered tag bytes, fills mfbuf, and encodes a frame whenever mf_size
// reaches calcNeeded, shifting the consumed samples out of mfbuf.
func (ec *EncoderContext) encodeBufferSampleT(nsamples int, dst []byte) []byte {
	gfc := ec.Gfc
	cfg := &gfc.Cfg
	esv := &gfc.SvEnc
	pcmSamplesPerFrame := 576 * cfg.ModeGr

	// copy out any tags that may have been written into the bitstream (mp3data=0).
	dst = ec.drainBitBuffer(dst, 0)

	inOff0, inOff1 := 0, 0 // advancing in_buffer cursors
	mfNeeded := calcNeeded(cfg)

	for nsamples > 0 {
		var nIn, nOut int
		gfc.fillBuffer(ec.inBuffer0[inOff0:], ec.inBuffer1[inOff1:], nsamples, &nIn, &nOut)

		// update in_buffer counters
		nsamples -= nIn
		inOff0 += nIn
		if cfg.ChannelsOut == 2 {
			inOff1 += nIn
		}

		// update mfbuf counters
		esv.MfSize += nOut

		// lame_encode_flush may have zeroed mf_samples_to_encode; reinit it.
		if esv.MfSamplesToEncode < 1 {
			esv.MfSamplesToEncode = ENCDELAY + POSTDELAY
		}
		esv.MfSamplesToEncode += nOut

		if esv.MfSize >= mfNeeded {
			// encode the frame.
			dst = ec.encodeOneFrame(dst)

			// shift out old samples
			esv.MfSize -= pcmSamplesPerFrame
			esv.MfSamplesToEncode -= pcmSamplesPerFrame
			for ch := 0; ch < cfg.ChannelsOut; ch++ {
				copy(esv.Mfbuf[ch][:esv.MfSize], esv.Mfbuf[ch][pcmSamplesPerFrame:pcmSamplesPerFrame+esv.MfSize])
			}
		}
	}
	return dst
}

// encodeOneFrame runs EncodeMP3Frame over the current mfbuf granule and appends
// the emitted bytes to dst (the lame_encode_mp3_frame call inside
// lame_encode_buffer_sample_t, lame.c:1793). The mp3 scratch is sized to LAME's
// worst-case frame; EncodeMP3Frame returns the byte count.
func (ec *EncoderContext) encodeOneFrame(dst []byte) []byte {
	gfc := ec.Gfc
	esv := &gfc.SvEnc

	// LAME passes mp3buf = caller buffer with buf_size = INT_MAX when the user
	// gave size 0; the port hands EncodeMP3Frame a scratch large enough for any
	// single frame (LAME's documented worst case is 1.25*1152 + 7200 bytes).
	mp3buf := make([]byte, encodeFrameScratch)
	n := EncodeMP3Frame(gfc, esv.Mfbuf[0][:], esv.Mfbuf[1][:], mp3buf, len(mp3buf))
	if n < 0 {
		// negative is an internal error (e.g. psy-model abort); emit nothing, as
		// the public encoder surfaces the failure via a sentinel at its boundary.
		return dst
	}
	return append(dst, mp3buf[:n]...)
}

// encodeFrameScratch sizes the per-frame mp3 scratch buffer. LAME's documented
// worst case for one call is 1.25*num_samples + 7200; for a single
// 1152-sample-per-channel frame that is comfortably under 8192 bytes, so the
// port uses a fixed 8 KiB scratch (the actual byte count comes from copy_buffer).
const encodeFrameScratch = 8192

// drainBitBuffer copies whatever is currently in the internal bit buffer into a
// scratch slice and appends it to dst, mirroring the copy_buffer calls the C
// driver makes (mp3data selects tag vs mp3 framing — see copy_buffer,
// bitstream_format.go). Returns the extended dst.
func (ec *EncoderContext) drainBitBuffer(dst []byte, mp3data int) []byte {
	gfc := ec.Gfc
	scratch := make([]byte, encodeFrameScratch)
	n := gfc.copyBuffer(scratch, len(scratch), mp3data)
	if n <= 0 {
		return dst
	}
	return append(dst, scratch[:n]...)
}

// EncodeFlush sends the final ENCDELAY+POSTDELAY zero padding so the last real
// granule is decodable, flushes the bit reservoir and copies out the tail,
// appending all of it to dst (lame_encode_flush, lame.c:2092). It returns the
// extended slice. The id3v1-tag append and the gain-value save are out of this
// slice's scope (separate id3 / gain slices); cfg.write_lame_tag is 0 in the
// CBR-first init so no Xing/LAME header is spliced.
func (ec *EncoderContext) EncodeFlush(dst []byte) []byte {
	gfc := ec.Gfc
	cfg := &gfc.Cfg
	esv := &gfc.SvEnc
	pcmSamplesPerFrame := 576 * cfg.ModeGr

	// Was flush already called?
	if esv.MfSamplesToEncode < 1 {
		return dst
	}

	samplesToEncode := esv.MfSamplesToEncode - POSTDELAY

	// The public encoder never resamples, so the resampling delay correction
	// (samples_to_encode += 16/resample_ratio) does not apply.
	endPadding := pcmSamplesPerFrame - (samplesToEncode % pcmSamplesPerFrame)
	if endPadding < 576 {
		endPadding += pcmSamplesPerFrame
	}
	gfc.OvEnc.EncoderPadding = endPadding

	framesLeft := (samplesToEncode + endPadding) / pcmSamplesPerFrame

	mfNeeded := calcNeeded(cfg)
	zeros := make([]int16, 1152*cfg.ChannelsIn) // interleaved zero PCM

	for framesLeft > 0 {
		frameNum := gfc.OvEnc.FrameNumber
		bunch := mfNeeded - esv.MfSize
		// resample_ratio == 1 (no resampling), so bunch is unchanged.
		if bunch > 1152 {
			bunch = 1152
		}
		if bunch < 1 {
			bunch = 1
		}

		// send in a frame of zero padding until all internal buffers are flushed.
		dst = ec.EncodeBuffer(zeros[:bunch*cfg.ChannelsIn], bunch, dst)

		// even a single pcm sample can produce several frames.
		newFrames := gfc.OvEnc.FrameNumber - frameNum
		if newFrames > 0 {
			framesLeft -= newFrames
		}
	}

	// Set mf_samples_to_encode to 0 so a repeated flush is a no-op.
	esv.MfSamplesToEncode = 0

	// mp3 bit buffer might still contain some data; flush the reservoir.
	gfc.flushBitstream()
	dst = ec.drainBitBuffer(dst, 1)

	// id3tag_write_v1 / save_gain_values are separate slices, omitted (cfg has
	// no write_id3tag_automatic in the ported context; the container layer owns
	// ID3 tags per the package decision).
	return dst
}

// LametagFrame assembles the finalized Xing/Info/LAME tag frame (the one
// lame_get_lametag_frame builds, vbrtag.go) from the seek-table / CRC state the
// encode accumulated. It returns nil when the tag is disabled or has no data
// (cfg.write_lame_tag == 0, or no frames were seen). The caller splices the
// returned bytes over the leading placeholder frame InitVbrTag wrote at the
// front of the stream, matching LAME's fseek-to-start file-output rewrite.
//
// This must be called after EncodeFlush: the seek table / music CRC and the
// total byte count are only complete once the final frame has been emitted.
func (ec *EncoderContext) LametagFrame() []byte {
	gfc := ec.Gfc
	// lame_get_lametag_frame returns the required size first (with a nil
	// buffer it returns 0), so size the buffer from the seek table and call once.
	n := int(gfc.VBRSeekTable.TotalFrameSize)
	if n <= 0 {
		return nil
	}
	buf := make([]byte, n)
	got := lameGetLametagFrame(gfc, ec.Gfp, buf, n)
	if got != n {
		// 0 == tag disabled / no data; any other value is a sizing mismatch the
		// caller can treat as "no tag" rather than splice a partial frame.
		return nil
	}
	return buf
}
