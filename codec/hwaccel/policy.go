package hwaccel

import (
	"fmt"
	"log"

	"go-mediatoolkit/events"
)

// Mode selects how aggressively Open* insists on hardware.
type Mode int

const (
	// PreferHardware (the default, zero value) tries every available
	// hardware backend and, only if none works, falls back to software
	// — publishing a HardwareFallbackEvent and logging a loud warning.
	PreferHardware Mode = iota
	// RequireHardware tries hardware only; on exhaustion it returns
	// ErrHardwareUnavailable rather than degrading.
	RequireHardware
	// SoftwareOnly skips hardware entirely and uses the software tier.
	SoftwareOnly
)

// String returns a human token for the mode.
func (m Mode) String() string {
	switch m {
	case RequireHardware:
		return "RequireHardware"
	case SoftwareOnly:
		return "SoftwareOnly"
	default:
		return "PreferHardware"
	}
}

// Policy controls backend selection for Open*. The zero Policy is valid
// and means PreferHardware with no event bus.
type Policy struct {
	// Mode is the selection strategy.
	Mode Mode
	// Bus, if non-nil, receives a HardwareFallbackEvent whenever a
	// PreferHardware open degrades to software. Independent of the loud
	// log warning, which always fires on fallback.
	Bus *events.Bus[HardwareFallbackEvent]
}

// OpenEncoder selects a backend from reg per the policy and constructs
// an encoder. See package docs and DESIGN.md for the full walk.
func (p Policy) OpenEncoder(reg *Registry, cfg Config) (Encoder, error) {
	if err := cfg.validateEncode(); err != nil {
		return nil, err
	}
	enc, err := open(p, reg, cfg, Encode,
		func(b Backend) (any, error) { return b.NewEncoder(cfg) })
	if err != nil {
		return nil, err
	}
	if enc == nil {
		return nil, nil
	}
	return enc.(Encoder), nil
}

// OpenDecoder selects a backend from reg per the policy and constructs
// a decoder.
func (p Policy) OpenDecoder(reg *Registry, cfg Config) (Decoder, error) {
	if err := cfg.validateDecode(); err != nil {
		return nil, err
	}
	dec, err := open(p, reg, cfg, Decode,
		func(b Backend) (any, error) { return b.NewDecoder(cfg) })
	if err != nil {
		return nil, err
	}
	if dec == nil {
		return nil, nil
	}
	return dec.(Decoder), nil
}

// open is the shared selection walk for both directions. build adapts a
// backend into the concrete Encoder/Decoder (returned as any so the two
// directions share one body). It returns (constructed, nil) on success;
// on failure it returns the policy-appropriate error.
func open(p Policy, reg *Registry, cfg Config, dir Direction, build func(Backend) (any, error)) (any, error) {
	// SoftwareOnly never touches hardware.
	if p.Mode == SoftwareOnly {
		return openSoftware(cfg, dir)
	}

	var attempted []string
	reasons := make(map[string]error)

	for _, b := range reg.Backends() {
		name := b.Name()
		attempted = append(attempted, name)

		if !b.Available() {
			reasons[name] = fmt.Errorf("%w: backend not available", ErrHardwareUnavailable)
			continue
		}
		caps, err := b.Probe()
		if err != nil {
			reasons[name] = err
			continue
		}
		if !caps.Supports(cfg.Codec, dir) {
			reasons[name] = fmt.Errorf("%w: %s %s", ErrUnsupportedCodec, cfg.Codec, dir)
			continue
		}
		built, err := build(b)
		if err != nil {
			reasons[name] = err
			continue
		}
		// Success — the first satisfying hardware backend wins.
		return built, nil
	}

	// No hardware backend worked.
	switch p.Mode {
	case RequireHardware:
		return nil, fmt.Errorf("%w (codec=%s dir=%s; tried %v)",
			ErrHardwareUnavailable, cfg.Codec, dir, attempted)
	default: // PreferHardware: fall back to software, loudly.
		return fallBackToSoftware(p, cfg, dir, attempted, reasons)
	}
}

// fallBackToSoftware attempts the software tier and, whether or not it
// succeeds, publishes a HardwareFallbackEvent and emits a heavy log
// warning. It returns the software encoder/decoder, or ErrNoBackend if
// the software tier is unavailable too.
func fallBackToSoftware(p Policy, cfg Config, dir Direction, attempted []string, reasons map[string]error) (any, error) {
	built, swErr := openSoftware(cfg, dir)

	fellBackTo := "software"
	if swErr != nil {
		fellBackTo = ""
	}

	evt := HardwareFallbackEvent{
		Codec:      cfg.Codec,
		Direction:  dir,
		Mode:       p.Mode,
		Attempted:  attempted,
		Reasons:    reasons,
		FellBackTo: fellBackTo,
	}
	if p.Bus != nil {
		p.Bus.Publish(evt)
	}
	logFallback(evt, reasons)

	if swErr != nil {
		return nil, ErrNoBackend
	}
	return built, nil
}

// logFallback emits the heavy, multi-line WARNING that makes a hardware
// fallback impossible to miss in logs. Loud on purpose.
func logFallback(evt HardwareFallbackEvent, reasons map[string]error) {
	var b []byte
	b = append(b, fmt.Sprintf(
		"hwaccel: WARNING hardware %s for %s UNAVAILABLE — falling back to %q (CPU-bound)\n",
		evt.Direction, evt.Codec, orNone(evt.FellBackTo))...)
	if len(evt.Attempted) == 0 {
		b = append(b, "hwaccel:   no hardware backends are registered for this platform\n"...)
	}
	for _, name := range evt.Attempted {
		b = append(b, fmt.Sprintf("hwaccel:   backend %-13s rejected: %v\n", name, reasons[name])...)
	}
	log.Print(string(b))
}

func orNone(s string) string {
	if s == "" {
		return "<none: no software tier>"
	}
	return s
}

// openSoftware is the software-tier seam. A pure-Go / cgo-free software
// encoder (libx264-class) can slot in here later; until then there is no
// software tier and it reports ErrNoBackend. Keeping the seam explicit
// lets the policy code stay direction-symmetric.
func openSoftware(cfg Config, dir Direction) (any, error) {
	_ = cfg
	_ = dir
	return nil, ErrNoBackend
}
