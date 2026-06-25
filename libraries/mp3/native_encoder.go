// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package mp3

import (
	"io"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// This file is the public-package seam for the pure-Go MP3 encoder, a 1:1
// translation of LAME (internal/nativemp3). Because that port is a derivative
// work of the LGPL-licensed LAME, the adapter is fenced behind the mp3lame
// build tag; the !mp3lame sibling (native_encoder_disabled.go) returns
// ErrEncoderRequiresLAME so a default build links no LGPL code.

// newNativeEncoder builds an Encoder backed by the pure-Go nativemp3 encoder.
// It maps the public StreamInfo / encoderConfig onto a LameGlobalFlags seeded
// with LAME's lame_init_old defaults plus the lameNew setters the cgo backend
// uses (mode, quality, vbr, brate), then runs lame_init_params. A non-zero
// init status surfaces as ErrInternal, matching the cgo backend's behaviour on
// a lame_init_params failure.
func newNativeEncoder(w io.Writer, info StreamInfo, cfg encoderConfig) (Encoder, error) {
	gfp := newLameGlobalFlags(info, cfg)
	ec, ret := nativemp3.NewEncoderContext(gfp)
	if ret != 0 {
		return nil, ErrInternal
	}
	e := &nativeEncoder{w: w, info: info, cfg: cfg, ec: ec}
	// When the LAME tag is enabled (lame_init_bitstream ran InitVbrTag), the
	// encoder emits a placeholder Xing/Info frame at the front of the stream that
	// Close must overwrite with the finalized tag. Splicing requires the whole
	// stream in memory, so buffer it rather than streaming straight to w. LAME's
	// own file output does the equivalent: it writes the placeholder, then
	// fseek(0) and rewrites the real tag on close. Streams without a tag go
	// straight to w.
	e.bufferStream = ec.Gfc.Cfg.WriteLameTag != 0
	return e, nil
}

// newLameGlobalFlags builds the LAME user flags for the requested stream. It
// mirrors lame_init_old (lame.c:2384) for the defaults the encoder relies on
// and the lameNew (encoder_cgo.go) setters for the user-controlled params, so
// the pure-Go path configures identically to the cgo libmp3lame path.
func newLameGlobalFlags(info StreamInfo, cfg encoderConfig) *nativemp3.LameGlobalFlags {
	gfp := &nativemp3.LameGlobalFlags{
		// lame_init_old defaults the encoder reads.
		StrictISO:      2, // MDB_MAXIMUM (lame.h:141; lame_init_old sets strict_ISO = MDB_MAXIMUM)
		Original:       1,
		WriteLameTag:   1,
		ShortBlocks:    -1, // short_block_not_set
		SubblockGain:   -1,
		LowpassWidth:   -1,
		HighpassWidth:  -1,
		VBRq:           4,
		VBRMeanBitrate: 128,
		QuantComp:      -1,
		QuantCompShort: -1,
		Msfix:          -1,
		Attackthre:     -1,
		AttackthreS:    -1,
		Scale:          1,
		ScaleLeft:      1,
		ScaleRight:     1,
		ATHcurve:       -1,
		ATHtype:        -1,
		AthaaType:      -1,
		UseTemporal:    -1,
		InterChRatio:   -1,
	}

	// lameNew setters (encoder_cgo.go): in samplerate, channels, quality, mode.
	// The cgo backend (encoder_cgo.go) and the genuine LAME CLI set ONLY
	// in_samplerate and let lame_init_params derive the output rate; for the
	// vbr_mtrh/-V modes that derivation runs the lame.c:677 q-map, which at <44kHz
	// remaps VBR_q/VBR_q_frac (e.g. 32k -V2 -> VBR_q=1, frac=0.6) so the preset
	// tuning matches a real -V stream. Pre-setting SamplerateOut would skip the
	// q-map and mis-tune <44k VBR, so we leave it 0 for the VBR-new modes and only
	// pin it for the CBR path. The CBR lowpass auto-bandwidth (optimum_bandwidth)
	// IS a full port now (stages.go), so a CBR encode with SamplerateOut left unset
	// would derive identically; pinning it to the input rate is the equivalent for
	// the 44.1k/48k/32k MPEG-1 rates this encoder targets, where optimum_samplefreq
	// returns the input rate anyway. At 44.1k/48k the q-map is the identity, so the
	// derived rate equals the input rate exactly as before.
	gfp.SamplerateIn = info.SampleRate
	gfp.NumChannels = info.Channels
	gfp.Quality = cfg.quality
	if info.Channels == 1 {
		gfp.Mode = 3 // MONO
	} else {
		gfp.Mode = 1 // JOINT_STEREO
	}

	if cfg.vbr {
		// lame_set_VBR(vbr_default) == vbr_mtrh; lame_set_VBR_q(quality). The
		// pure-Go VBR_new iteration loop (vbr_mtrh) drives the encode; the public
		// encoder buffers the stream so Close can splice the finalized Xing/LAME
		// tag frame over the leading placeholder InitVbrTag emitted. Output rate is
		// left unset so lame_init_params' q-map / optimum_samplefreq derives it.
		gfp.VBR = 4 // vbr_mtrh (vbr_default)
		gfp.VBRq = cfg.quality
	} else {
		gfp.VBR = 0                         // vbr_off
		gfp.SamplerateOut = info.SampleRate // CBR keeps the explicit output rate
		gfp.Brate = cfg.bitRate / 1000
	}

	return gfp
}

// nativeEncoder adapts the pure-Go MP3 port to the public Encoder interface.
type nativeEncoder struct {
	w      io.Writer
	info   StreamInfo
	cfg    encoderConfig
	ec     *nativemp3.EncoderContext
	closed bool

	// bufferStream is set when the LAME tag is enabled: the encoder writes a
	// placeholder Xing/Info frame first that Close splices the finalized tag
	// over, so the whole stream is accumulated in stream until Close.
	bufferStream bool
	stream       []byte
}

// EncodeFrame submits interleaved samples for compression, writing any produced
// MP3 bytes to the underlying writer. buf is interleaved int16 PCM whose length
// must be a multiple of the channel count.
func (e *nativeEncoder) EncodeFrame(buf []int16) error {
	if e.closed {
		return ErrClosed
	}
	ch := e.info.Channels
	if ch != 0 && len(buf)%ch != 0 {
		return ErrBadArg
	}
	if len(buf) == 0 {
		return nil
	}

	nsamples := len(buf) / ch
	out := e.ec.EncodeBuffer(buf, nsamples, nil)
	if len(out) == 0 {
		return nil
	}
	if e.bufferStream {
		e.stream = append(e.stream, out...)
		return nil
	}
	_, err := e.w.Write(out)
	return err
}

// Close flushes pending frames (including the LAME encoder's final flush) and
// releases resources. After Close the Encoder must not be used.
func (e *nativeEncoder) Close() error {
	if e.closed {
		return ErrClosed
	}
	e.closed = true

	out := e.ec.EncodeFlush(nil)

	if !e.bufferStream {
		// No LAME tag: the stream was written frame-by-frame; emit the flush tail.
		if len(out) == 0 {
			return nil
		}
		_, err := e.w.Write(out)
		return err
	}

	// Tag enabled: the whole stream (placeholder Xing/Info frame first, then the
	// audio frames, then the flush tail) is buffered. Build the finalized tag
	// from the accumulated seek table / CRC and splice it over the leading
	// placeholder, matching LAME's fseek-to-start file rewrite. lame_get_lametag_
	// frame's TotalFrameSize is exactly the placeholder frame length, so the
	// overwrite is in place and the stream length is unchanged.
	e.stream = append(e.stream, out...)
	if tag := e.ec.LametagFrame(); len(tag) > 0 && len(tag) <= len(e.stream) {
		copy(e.stream[:len(tag)], tag)
	}
	if len(e.stream) == 0 {
		return nil
	}
	_, err := e.w.Write(e.stream)
	e.stream = nil
	return err
}
