// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package aac

// FDK-AAC-derived adapter. The pure-Go AAC-LC decode engine under
// internal/nativeaac is a 1:1 fixed-point port of the vendored Fraunhofer
// FDK-AAC reference, so it is fenced behind the opt-in aacfdk build tag (a
// default `go build ./...` links none of it). This seam binds the
// always-available nativeDecodeEngine interface (native_stub.go) to that port.

import (
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/heaac"
)

// newNativeDecodeEngine constructs the internal/nativeaac AAC-LC decode core.
func newNativeDecodeEngine(frameSamples, sampleRate, channels int) (nativeDecodeEngine, error) {
	eng, err := nativeaac.NewDecoder(frameSamples, uint32(sampleRate), channels)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	return eng, nil
}

// newNativeSbrDecodeEngine constructs the HE-AAC v1 (AAC-LC core + SBR) decode
// engine. coreFrameLen is the AAC-LC core frame length (1024), coreRate the core
// sampling rate, channels the channel count, and outRate the SBR-doubled output
// rate. The returned engine decodes one access unit to 2*coreFrameLen samples per
// channel of interleaved int16 PCM. heaac.Decoder satisfies nativeDecodeEngine
// directly (DecodeAccessUnit + Reset).
func newNativeSbrDecodeEngine(coreFrameLen, coreRate, channels, outRate int) (nativeDecodeEngine, error) {
	dec, err := heaac.NewDecoder(coreFrameLen, coreRate, channels, outRate)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	return dec, nil
}

// newNativePsDecodeEngine constructs the HE-AAC v2 (AAC-LC mono core + SBR +
// parametric stereo) decode engine. coreFrameLen is the AAC-LC core frame length
// (1024), coreRate the core sampling rate, outRate the SBR-doubled output rate.
// The returned engine decodes one access unit (a mono SCE carrying ps_data in its
// SBR extension) to 2*coreFrameLen samples per channel of interleaved STEREO int16
// PCM. heaac.Decoder (built via NewPSDecoder) satisfies nativeDecodeEngine
// directly (DecodeAccessUnit + Reset).
func newNativePsDecodeEngine(coreFrameLen, coreRate, outRate int) (nativeDecodeEngine, error) {
	dec, err := heaac.NewPSDecoder(coreFrameLen, coreRate, outRate)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	return dec, nil
}

// newNativeEncodeEngine constructs the internal/nativeaac AAC-LC encode core
// (the 1:1 fixed-point port of the vendored Fraunhofer FDK-AAC reference). When
// vbrMode is 0 it builds a CBR encoder at bitRate; when vbrMode is 1..5 it builds
// a VBR encoder at that quality mode (the bitrate is then derived internally).
func newNativeEncodeEngine(sampleRate, channels, bitRate, vbrMode int) (nativeEncodeEngine, error) {
	var (
		eng    *nativeaac.Encoder
		encErr nativeaac.EncoderError
	)
	if vbrMode != 0 {
		eng, encErr = nativeaac.NewEncoderVBR(sampleRate, channels, nativeaac.AacencBitrateMode(vbrMode))
	} else {
		eng, encErr = nativeaac.NewEncoder(sampleRate, channels, bitRate)
	}
	if encErr != nativeaac.AacEncOK {
		return nil, ErrInvalidConfig
	}
	return &nativeEncodeEngineAdapter{eng: eng}, nil
}

// newNativeSbrEncodeEngine constructs the HE-AAC v1 (AOT-5) encode engine
// (internal/nativeaac/heaac): the SBR encoder over the full-rate input plus the
// AAC-LC core over the downsampled signal, emitting one raw AAC-LC+SBR access
// unit per 2*coreFrameLen-sample frame, with the explicit AOT-5 ASC. sampleRate
// is the input (SBR output) rate.
func newNativeSbrEncodeEngine(sampleRate, channels, bitRate int) (nativeSbrEncodeEngine, error) {
	eng, err := heaac.NewEncoder(sampleRate, channels, bitRate)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	return eng, nil
}

// newNativePsEncodeEngine constructs the HE-AAC v2 (AOT-29) encode engine
// (internal/nativeaac/heaac): the SBR+PS encoder over the full-rate STEREO input
// plus the AAC-LC core over the downsampled MONO downmix, emitting one raw
// AAC-LC+SBR access unit (carrying ps_data) per 2*coreFrameLen-sample frame, with
// the explicit AOT-29 ASC. sampleRate is the input (SBR output) rate; the input is
// always stereo. heaac.PSEncoder satisfies nativePsEncodeEngine directly
// (EncodeAccessUnit + FrameSamples + ASC).
func newNativePsEncodeEngine(sampleRate, bitRate int) (nativePsEncodeEngine, error) {
	eng, err := heaac.NewPSEncoder(sampleRate, bitRate)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	return eng, nil
}

// nativeEncodeEngineAdapter bridges nativeaac.Encoder (whose EncodeOneFrame
// returns the FDK EncoderError code) to the nativeEncodeEngine interface (which
// returns a Go error). A non-OK code maps to ErrEncodeFailed.
type nativeEncodeEngineAdapter struct {
	eng *nativeaac.Encoder
}

func (a *nativeEncodeEngineAdapter) FrameLength() int { return a.eng.FrameLength() }

func (a *nativeEncodeEngineAdapter) EncodeOneFrame(interleaved []int16) ([]byte, error) {
	au, encErr := a.eng.EncodeOneFrame(interleaved)
	if encErr != nativeaac.AacEncOK {
		return nil, ErrEncodeFailed
	}
	return au, nil
}
