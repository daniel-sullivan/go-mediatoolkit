package aac

import "errors"

var (
	// ErrBadArg indicates an invalid argument — unsupported sample rate,
	// channel count, or AudioSpecificConfig.
	ErrBadArg = errors.New("aac: bad argument")

	// ErrEngineRequiresFDK indicates the tag-routed [NewDecoder] /
	// [NewEncoder] were called in a build without the aacfdk tag. FDK-AAC
	// is the only AAC engine and is fenced behind the opt-in aacfdk build
	// tag (and cgo), so the default build links zero FDK-AAC. Rebuild with
	// `-tags aacfdk` (cgo enabled), or use [NewNativeDecoder] /
	// [NewNativeEncoder] to force the always-available pure-Go port.
	ErrEngineRequiresFDK = errors.New("aac: codec requires the aacfdk build tag")

	// ErrInvalidPacket indicates a corrupted or malformed AAC access unit.
	ErrInvalidPacket = errors.New("aac: invalid packet")

	// ErrInvalidConfig indicates a malformed or unsupported
	// AudioSpecificConfig.
	ErrInvalidConfig = errors.New("aac: invalid AudioSpecificConfig")

	// ErrBufferTooSmall indicates the output buffer is too small for the
	// decoded frame.
	ErrBufferTooSmall = errors.New("aac: output buffer too small")

	// ErrUnimplemented indicates a profile or coding tool that is not yet
	// implemented by the pure-Go port.
	ErrUnimplemented = errors.New("aac: unimplemented")

	// ErrInternal indicates an internal codec error.
	ErrInternal = errors.New("aac: internal error")

	// ErrEncodeFailed indicates the encoder rejected the input frame (a non-OK
	// AAC_ENCODER_ERROR from the 1:1 FDK encode path).
	ErrEncodeFailed = errors.New("aac: encode failed")

	// ErrPSRequiresStereo indicates an HE-AAC v2 (AOT-29 parametric stereo)
	// encoder was requested with a channel count other than 2. PS encodes a
	// stereo input down to a mono core carrying ps_data, so it requires exactly
	// two input channels.
	ErrPSRequiresStereo = errors.New("aac: parametric stereo encode requires 2 input channels")
)
