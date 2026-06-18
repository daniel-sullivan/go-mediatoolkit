//go:build !darwin && !linux

// On platforms without a hardware backend wired in, the default
// registry stays empty: Open* under PreferHardware will fall back to
// software (loudly), and under RequireHardware will return
// ErrHardwareUnavailable. darwin registers VideoToolbox (backend_darwin.go)
// and linux registers VAAPI / V4L2 (backend_linux.go); every other
// platform lands here. The build tag tightens further as the nvenc
// backend (cross-platform, gated on the NVENC library being dlopen-able)
// lands.
//
// This file exists so the framework compiles and the registry/policy
// machinery is exercised on every platform.

package hwaccel

// No init: no backends to register on this platform yet.
