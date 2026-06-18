package hwaccel

import "errors"

var (
	// ErrNoBackend is returned when no backend (hardware or software)
	// can satisfy the request. Under SoftwareOnly it means no software
	// tier is wired in yet; under PreferHardware it means both the
	// hardware walk and the software fallback came up empty.
	ErrNoBackend = errors.New("hwaccel: no backend available for codec/direction")

	// ErrHardwareUnavailable is returned by Open* under RequireHardware
	// when no hardware backend can satisfy the request. It deliberately
	// does not degrade to software; the caller decides what to do.
	ErrHardwareUnavailable = errors.New("hwaccel: required hardware backend unavailable")

	// ErrUnsupportedCodec is returned when a backend is asked to build
	// an encoder/decoder for a codec it does not implement.
	ErrUnsupportedCodec = errors.New("hwaccel: codec not supported by backend")

	// ErrInvalidConfig is returned when a Config is missing required
	// fields (codec, non-zero resolution) or carries contradictory ones.
	ErrInvalidConfig = errors.New("hwaccel: invalid encoder/decoder config")

	// ErrUnsupportedPixelFormat is returned by an encoder when a Frame's
	// pixel format is not one the backend accepts.
	ErrUnsupportedPixelFormat = errors.New("hwaccel: unsupported pixel format")

	// ErrClosed is returned by Encode/Decode/Flush after Close.
	ErrClosed = errors.New("hwaccel: encoder or decoder is closed")

	// ErrBackendFailure wraps a non-zero status from the underlying
	// accelerator (an OSStatus, a VA status, an ioctl errno). Callers
	// inspect the wrapped error for the platform-specific code.
	ErrBackendFailure = errors.New("hwaccel: backend operation failed")

	// ErrParameterSetsMissing is returned by a decoder when an access unit
	// arrives before any parameter sets (SPS/PPS, or VPS/SPS/PPS) have been
	// seen, so the picture cannot be configured.
	ErrParameterSetsMissing = errors.New("hwaccel: decode before parameter sets (no keyframe seen)")

	// ErrBitstreamParse is returned when a NAL unit's RBSP cannot be parsed
	// far enough to fill the VA parameter buffers (truncated or malformed
	// SPS/PPS/slice header).
	ErrBitstreamParse = errors.New("hwaccel: malformed or truncated bitstream")

	// ErrEncodeUnsupportedOnDriver is returned when a codec's encode path is
	// not yet drivable through this backend on the host driver even though
	// the hardware advertises the entrypoint. It is used for VAAPI H.265
	// low-power encode on the Intel iHD driver, whose picture submission
	// rejects an otherwise spec- and trace-conformant packet (see the
	// vaapi_encode_hevc_linux.go header comment).
	ErrEncodeUnsupportedOnDriver = errors.New("hwaccel: codec encode not drivable on this driver")
)
