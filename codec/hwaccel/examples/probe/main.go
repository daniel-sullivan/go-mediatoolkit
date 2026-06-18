// Probe example: enumerate the hardware-codec backends compiled into this
// binary and report, per backend, exactly which codecs and directions the host
// supports. This teaches one concept — the Available()/Probe()/Capabilities
// story and the loud-fallback model — and nothing else.
//
// It runs on EVERY platform: on a host with hardware (a Mac with VideoToolbox,
// a Linux box with an Intel/AMD/NVIDIA GPU or a Pi) it prints the real
// capability matrix; on a host with no hardware backend it prints the empty
// registry and demonstrates the loud HardwareFallbackEvent that PreferHardware
// fires before returning ErrNoBackend.
//
// Usage: probe
package main

import (
	"log"

	"go-mediatoolkit/codec/hwaccel"
	"go-mediatoolkit/video"
)

func main() {
	log.SetFlags(0)

	reg := hwaccel.DefaultRegistry()
	backends := reg.Backends()

	if len(backends) == 0 {
		// No hardware backend is compiled in / registered for this platform.
		// This is the graceful no-hardware case the framework is built around.
		log.Println("no hardware codec backend registered on this host")
		log.Println("(PreferHardware would fall back to software loudly; see below)")
		demoFallback()
		return
	}

	log.Printf("registered backends (in selection-preference order): %d", len(backends))
	anyAvailable := false
	for _, b := range backends {
		avail := b.Available()
		log.Printf("  backend %-13s available=%v", b.Name(), avail)
		if !avail {
			continue
		}
		anyAvailable = true
		caps, err := b.Probe()
		if err != nil {
			log.Printf("    probe failed: %v", err)
			continue
		}
		if len(caps.Codecs) == 0 {
			log.Printf("    (probe reported no codecs)")
			continue
		}
		for _, c := range caps.Codecs {
			log.Printf("    %-5s encode=%-5v decode=%-5v profiles=%v",
				c.Codec, c.Encode, c.Decode, c.Profiles)
		}
	}

	if !anyAvailable {
		log.Println("no hardware codec available on this host (backends registered but none Available)")
		demoFallback()
	}
}

// demoFallback opens an encoder under PreferHardware to show the loud fallback
// path: with no usable hardware backend (and no software tier wired in yet) the
// framework publishes a HardwareFallbackEvent, logs a heavy WARNING, and returns
// ErrNoBackend. The example exits 0 — this is the designed graceful path.
func demoFallback() {
	bus := hwaccel.NewFallbackBus()
	bus.Subscribe(func(e hwaccel.HardwareFallbackEvent) {
		log.Printf("fallback event: %s %s tried=%v fellBackTo=%q",
			e.Direction, e.Codec, e.Attempted, e.FellBackTo)
	})

	_, err := hwaccel.OpenEncoder(
		hwaccel.Policy{Mode: hwaccel.PreferHardware, Bus: bus},
		hwaccel.NewConfig(
			hwaccel.WithCodec(video.H265),
			hwaccel.WithResolution(1280, 720),
		),
	)
	// ErrNoBackend is expected here when there is no hardware and no software
	// tier; it is reported, not fatal.
	if err != nil {
		log.Printf("OpenEncoder under PreferHardware returned: %v (expected with no backend)", err)
	}
}
