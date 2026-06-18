// Package hwaccel is a backend-pluggable framework for hardware-
// accelerated video encode and decode of H.264 and H.265. It targets
// the NVR / transcode use case: take raw frames (or an incoming stream)
// and re-encode on whatever fixed-function silicon the host exposes,
// falling back to software loudly — never silently — when no hardware
// path exists.
//
// The whole subsystem is CGO_ENABLED=0: pure syscall where the kernel
// ABI is stable (V4L2 on Linux SoCs), purego dlopen for vendor
// userspace libraries (VideoToolbox, VAAPI, NVENC). See DESIGN.md for
// the package layout, backend matrix, and fallback semantics.
//
// # Usage
//
//	enc, err := hwaccel.OpenEncoder(
//	    hwaccel.Policy{Mode: hwaccel.PreferHardware},
//	    hwaccel.NewConfig(
//	        hwaccel.WithCodec(video.H265),
//	        hwaccel.WithResolution(1920, 1080),
//	        hwaccel.WithBitrate(6_000_000),
//	        hwaccel.WithFrameRate(30, 1),
//	    ),
//	)
//	// feed frames:
//	pkts, err := enc.Encode(frame)
//	// at end of stream:
//	tail, err := enc.Flush()
//	enc.Close()
//
// # Concurrency
//
// The Registry is safe for concurrent registration and lookup. A
// constructed Encoder or Decoder is NOT safe for concurrent use; drive
// each from a single goroutine.
package hwaccel

import (
	"sync"

	"go-mediatoolkit/video"
)

// Encoder consumes raw frames and produces encoded packets. Hardware
// codecs are pipelined, so a single Encode may return zero packets (the
// frame is buffered) or several (a parameter-set packet plus the
// frame). Flush drains the pipeline at end of stream.
//
// Not safe for concurrent use.
type Encoder interface {
	// Encode submits one raw frame and returns any packets the
	// accelerator has finished. The returned slice may be empty.
	Encode(f video.Frame) ([]video.Packet, error)
	// Flush completes all pending frames and returns the remaining
	// packets. Call once at end of stream, before Close.
	Flush() ([]video.Packet, error)
	// Close releases the session and any OS resources. Idempotent.
	Close() error
}

// Decoder consumes encoded packets and produces raw frames. Like
// Encoder it is pipelined: Decode may return zero or several frames.
//
// Not safe for concurrent use.
type Decoder interface {
	// Decode submits one encoded packet and returns any frames the
	// accelerator has finished. The returned slice may be empty.
	Decode(p video.Packet) ([]video.Frame, error)
	// Flush drains the decode pipeline and returns the remaining
	// frames. Call once at end of stream, before Close.
	Flush() ([]video.Frame, error)
	// Close releases the session and any OS resources. Idempotent.
	Close() error
}

// Backend is one accelerator family — typically one per platform or
// vendor (VideoToolbox, V4L2, VAAPI, NVENC). Backends register
// themselves with the default Registry from a build-tagged init so the
// active set reflects the build platform.
type Backend interface {
	// Name is the stable identifier ("videotoolbox", "v4l2", "vaapi",
	// "nvenc") used in capability reports and fallback events.
	Name() string
	// Available is a cheap gate: can the backend dlopen its library or
	// open its device node at all? Used to skip a backend before paying
	// for a full Probe.
	Available() bool
	// Probe queries the accelerator for the precise set of codecs,
	// directions, and profiles it supports on this host. Results may be
	// cached. Returns an error only if the probe itself fails (not if a
	// codec is merely unsupported — that is reflected in Capabilities).
	Probe() (Capabilities, error)
	// NewEncoder constructs an encoder for cfg, or an error
	// (ErrUnsupportedCodec, ErrInvalidConfig, or a wrapped backend
	// failure).
	NewEncoder(cfg Config) (Encoder, error)
	// NewDecoder constructs a decoder for cfg.
	NewDecoder(cfg Config) (Decoder, error)
}

// Registry holds the set of available backends in registration order.
// Selection (see Policy) walks the registry in that order, so backends
// register in platform-preference order.
type Registry struct {
	mu       sync.RWMutex
	backends []Backend
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds b to the registry. Backends with duplicate names are
// permitted (the first registered wins selection), but in practice each
// platform registers a distinct set.
func (r *Registry) Register(b Backend) {
	r.mu.Lock()
	r.backends = append(r.backends, b)
	r.mu.Unlock()
}

// Backends returns a snapshot of the registered backends in
// registration order.
func (r *Registry) Backends() []Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Backend, len(r.backends))
	copy(out, r.backends)
	return out
}

// Get returns the registered backend with the given name, or nil.
func (r *Registry) Get(name string) Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.backends {
		if b.Name() == name {
			return b
		}
	}
	return nil
}

// defaultRegistry is populated by build-tagged backend_*.go init
// functions. Platform backends register into it at package load so the
// top-level Open* entry points see exactly the backends compiled in.
var defaultRegistry = NewRegistry()

// DefaultRegistry returns the process-wide registry that platform
// backends register into and that the top-level OpenEncoder /
// OpenDecoder use.
func DefaultRegistry() *Registry { return defaultRegistry }

// Probe runs Probe on every registered backend that is Available and
// returns the successful results. Backends that are unavailable are
// skipped; a backend whose Probe errors is omitted (the error is not
// surfaced — this is a best-effort capability snapshot).
func (r *Registry) Probe() []Capabilities {
	var out []Capabilities
	for _, b := range r.Backends() {
		if !b.Available() {
			continue
		}
		caps, err := b.Probe()
		if err != nil {
			continue
		}
		out = append(out, caps)
	}
	return out
}

// OpenEncoder builds an encoder for cfg using the default registry under
// policy p. See Policy.OpenEncoder.
func OpenEncoder(p Policy, cfg Config) (Encoder, error) {
	return p.OpenEncoder(defaultRegistry, cfg)
}

// OpenDecoder builds a decoder for cfg using the default registry under
// policy p. See Policy.OpenDecoder.
func OpenDecoder(p Policy, cfg Config) (Decoder, error) {
	return p.OpenDecoder(defaultRegistry, cfg)
}
