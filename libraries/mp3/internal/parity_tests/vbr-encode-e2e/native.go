// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbre2e

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 encoder the same way oracle.c drives the
// vendored C: build a -V2 (vbr_mtrh, VBR_q=2) LameGlobalFlags, run the encode
// over the byte-identical int16 PCM the oracle exported, flush, and assemble the
// file-layout stream — the placeholder Xing/Info frame the encoder emits first,
// overwritten by the finalized lame_get_lametag_frame on close. This is the exact
// splice the public libraries/mp3 nativeEncoder.Close performs; the slice
// reproduces it here so it can import only internal/nativemp3 (never
// libraries/mp3, which would duplicate the LAME symbols at link time).

// newV2Gfp builds the -V2 user flags, mirroring the public package's
// newLameGlobalFlags (native_encoder.go) seeded with lame_init_old defaults plus
// the lameNew (-V2) setters. VBR_q is pinned to 2 to match the oracle's
// lame_set_VBR_q(2), independent of quality.
func newV2Gfp(samplerate, channels int) *nativemp3.LameGlobalFlags {
	gfp := &nativemp3.LameGlobalFlags{
		// lame_init_old defaults the encoder reads.
		StrictISO:      2, // MDB_MAXIMUM (lame.h:141)
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

	// Mirror oracle.c: set ONLY in_samplerate and let lame_init_params derive the
	// output rate (the q-map at <44kHz remaps VBR_q so the preset tuning matches a
	// genuine -V2 stream). At 44.1k/48k the q-map is the identity.
	gfp.SamplerateIn = samplerate
	gfp.NumChannels = channels
	gfp.Quality = 5 // lameNew default quality (encoder_cgo.go)
	if channels == 1 {
		gfp.Mode = 3 // MONO
	} else {
		gfp.Mode = 1 // JOINT_STEREO
	}

	// -V2: lame_set_VBR(vbr_default) == vbr_mtrh, lame_set_VBR_q(2).
	gfp.VBR = 4 // vbr_mtrh (vbr_default)
	gfp.VBRq = 2

	return gfp
}

// nativeStream runs the pure-Go -V2 encode over the interleaved int16 pcm and
// returns the assembled file-layout stream plus the spliced tag length. Returns
// nil on a lame_init_params failure.
func nativeStream(samplerate, channels int, pcm []int16) (stream []byte, tagLen int) {
	gfp := newV2Gfp(samplerate, channels)
	ec, ret := nativemp3.NewEncoderContext(gfp)
	if ret != 0 {
		return nil, 0
	}

	nsamples := len(pcm) / channels
	out := ec.EncodeBuffer(pcm, nsamples, nil)
	stream = append(stream, out...)
	stream = ec.EncodeFlush(stream)

	if tag := ec.LametagFrame(); len(tag) > 0 && len(tag) <= len(stream) {
		copy(stream[:len(tag)], tag)
		tagLen = len(tag)
	}
	return stream, tagLen
}
